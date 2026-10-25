// It's a re implementation but credits are needed to the contributors
// of the NocturnalRTOS project (https://github.com/Data-Frog-Central/NocturnalRTOS)
// that did the hard part of the work, more specifically the file:
// https://github.com/Data-Frog-Central/NocturnalRTOS/blob/266b153d689e384f093dba3963eeb86a7795e9f9/components/applications/apps-phobos/source/drivers/sf2000_gfx.c
// A HUGE thanks for they all

package display

import (
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"
)

// SF3000/MIPS uses the MIPS _IOW encoding (not the generic Linux value).
const (
	fbioGetVScreenInfo = 0x4600
	fbioPutVScreenInfo = 0x4601
	fbioGetFScreenInfo = 0x4602
	fbioPanDisplay     = 0x4606
	fbioBlank          = 0x4611
	fbioWaitForVSync   = 0x80044620
)

type fbBitfield struct {
	Offset, Length, MsbRight uint32
}

// fbVarScreeninfo mirrors struct fb_var_screeninfo.
type fbVarScreeninfo struct {
	Xres, Yres                 uint32
	XresVirtual, YresVirtual   uint32
	Xoffset, Yoffset           uint32
	BitsPerPixel, Grayscale    uint32
	Red, Green, Blue, Transp   fbBitfield
	Nonstd, Activate           uint32
	Height, Width              uint32
	AccelFlags                 uint32
	Pixclock                   uint32
	LeftMargin, RightMargin    uint32
	UpperMargin, LowerMargin   uint32
	HsyncLen, VsyncLen         uint32
	Sync, Vmode, Rotate, Color uint32
	Reserved                   [4]uint32
}

type fbFixScreeninfo struct {
	ID           [16]byte
	SmemStart    uintptr
	SmemLen      uint32
	Type         uint32
	TypeAux      uint32
	Visual       uint32
	Xpanstep     uint16
	Ypanstep     uint16
	Ywrapstep    uint16
	LineLength   uint32
	MmioStart    uintptr
	MmioLen      uint32
	Accel        uint32
	Capabilities uint16
	Reserved     [2]uint16
}

func fbIoctl(fd int, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

// fbIoctlVal is for ioctls that take a plain value instead of a pointer.
func fbIoctlVal(fd int, req, val uintptr) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, val); e != 0 {
		return e
	}
	return nil
}

// fbDetectRotation returns the clockwise rotation (0, 90, 180 or 270) to
// apply and where it came from
func fbDetectRotation(xres, yres int) (int, string) {
	// On this device, /hcrtos/rotate/rotate == 00 00 01 0e = 270.
	for _, base := range []string{
		"/proc/device-tree",
		"/sys/firmware/devicetree/base",
	} {
		path := base + "/hcrtos/rotate/rotate"
		b, err := os.ReadFile(path)
		if err == nil && len(b) >= 4 {
			deg := int(binary.BigEndian.Uint32(b))
			if deg < 360 && deg%90 == 0 {
				return (deg + 180) % 360, "device tree rotate (180 corrected)"
			}
		}
	}

	for _, base := range []string{
		"/proc/device-tree",
		"/sys/firmware/devicetree/base",
	} {
		b, err := os.ReadFile(base + "/hcrtos/de-engine/rotated-screen")
		if err == nil && len(b) >= 4 {
			if binary.BigEndian.Uint32(b) == 0 {
				return 270, "device tree rotated-screen"
			}
		}
	}

	if yres > xres {
		return 270, "portrait panel"
	}
	return 0, "default"
}

// FBDisplayController draws straight into /dev/fb0, without cgo or a vendor
// driver. Center and Fit can display colored background bars.
type FBDisplayController struct {
	frame

	vsyncActive bool

	xmap       []int32 // panel x -> offset of its source pixel inside a row start
	ystart     []int32 // panel y -> index in buf where its source pixels start
	srcX       []int32 // panel x -> buf row (rot 90/270) or buf column
	srcY       []int32 // panel y -> buf column (rot 90/270) or buf row
	fd         int
	mem        []byte
	pix        []uint16 // mem as RGB565 pixels
	vinfo      fbVarScreeninfo
	xres       int
	yres       int
	stride     int    // pixels per framebuffer row
	pages      int    // 1 or 2 output slots
	pageIDs    [2]int // actual framebuffer page indices (not slot indices)
	pagePrimed [2]bool
	back       int // slot to draw next

	pend [2]damage // changes each page has not received yet

	check   bool   // compare frames, skipping or shrinking unchanged ones
	skipped uint64 // frames skipped because they were identical

	rot           int             // clockwise degrees the frame is rotated by on the way out
	rotSource     string          // detection or explicit override
	originalVInfo fbVarScreeninfo // framebuffer mode/pan at initialization
	lastPage      int             // last page successfully displayed by this controller
	panChanged    bool

	row []uint16 // retained for the reference blitter and compatibility tests

	// surface is the final, rotated RGB565 image in framebuffer coordinates.
	// Drawing operations update it directly via the precomputed inverse maps.
	surface        []uint16
	surfaceReady   bool
	surfaceSync    bool   // Clear or raw writes require final-pixel reconciliation
	rawAccess      bool   // Buffer and SetBuffer can change without drawing calls
	surfaceTouched damage // direct writes since the previous successful Update
	invX, invY     []fbSpan

	lastCopy, lastWait, lastShow time.Duration
}

var _ DisplayController = (*FBDisplayController)(nil)

// NewFBDisplayController opens /dev/fb0 without changing its display mode.
// Inactive framebuffer pages require exclusive ownership.
func NewFBDisplayController(cfg Config) (*FBDisplayController, error) {
	// Not os.OpenFile: an *os.File would close the fd when garbage collected.
	fd, err := syscall.Open("/dev/fb0", syscall.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/fb0: %w", err)
	}

	fail := func(err error) (*FBDisplayController, error) {
		_ = syscall.Close(fd)
		return nil, err
	}

	var fix fbFixScreeninfo
	var v fbVarScreeninfo
	if err := fbIoctl(fd, fbioGetFScreenInfo, unsafe.Pointer(&fix)); err != nil {
		return fail(fmt.Errorf("FBIOGET_FSCREENINFO: %w", err))
	}
	if err := fbIoctl(fd, fbioGetVScreenInfo, unsafe.Pointer(&v)); err != nil {
		return fail(fmt.Errorf("FBIOGET_VSCREENINFO: %w", err))
	}

	panelW, panelH := int(v.Xres), int(v.Yres)
	// Do not force RGB565, resize the virtual framebuffer, or reset the pan
	// offset. The vendor driver may have configured all of these already.
	if v.BitsPerPixel != 16 ||
		v.Red.Offset != 11 || v.Red.Length != 5 || v.Green.Offset != 5 ||
		v.Green.Length != 6 || v.Blue.Offset != 0 || v.Blue.Length != 5 ||
		v.Transp.Length != 0 {
		return fail(fmt.Errorf("framebuffer mode is not RGB565: bpp=%d R=%v G=%v B=%v A=%v",
			v.BitsPerPixel, v.Red, v.Green, v.Blue, v.Transp))
	}
	if panelW <= 0 || panelH <= 0 || int(v.XresVirtual) < panelW {
		return fail(fmt.Errorf("invalid framebuffer geometry %dx%d virtual=%dx%d",
			panelW, panelH, v.XresVirtual, v.YresVirtual))
	}

	rot, rotSource := fbDetectRotation(panelW, panelH)
	if cfg.Rotation != RotationNone {
		rot = cfg.Rotation.Degrees() // forced, instead of the device's
		rotSource = "Config.Rotation=" + cfg.Rotation.String()
	}

	// The app always sees the landscape panel, whatever the fb mode is.
	lw, lh := max(panelW, panelH), min(panelW, panelH)

	var baseFrame frame
	if err := baseFrame.configure(cfg, lw, lh); err != nil {
		return fail(err)
	}

	xres, yres := int(v.Xres), int(v.Yres)
	stride := max(int(fix.LineLength)/2, xres)

	if fix.LineLength == 0 || int(fix.LineLength)%2 != 0 || stride < xres {
		return fail(fmt.Errorf("invalid framebuffer line length %d", fix.LineLength))
	}
	// Only complete, addressable framebuffer pages may be used. Extra virtual
	// height (15 pages on the observed SF3000) is preserved without modification.
	pageBytes := stride * yres * 2
	available := min(int(v.YresVirtual)/yres, int(fix.SmemLen)/pageBytes)
	if available < 3 || int(v.Yoffset)%yres != 0 || v.Xoffset != 0 ||
		int(v.Yoffset)/yres >= available {
		return fail(fmt.Errorf("cannot safely select inactive framebuffer pages: pages=%d offset=(%d,%d)",
			available, v.Xoffset, v.Yoffset))
	}
	// Pick two distinct pages different from the page currently displayed.
	// The kernel does not expose page ownership: the caller must guarantee
	// exclusive framebuffer access while this backend runs.
	current := int(v.Yoffset) / yres
	pageIDs := [2]int{(current + 1) % available, (current + 2) % available}
	pages := 2

	mem, err := syscall.Mmap(fd, 0, int(fix.SmemLen),
		syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return fail(fmt.Errorf("mmap /dev/fb0: %w", err))
	}

	_ = fbIoctlVal(fd, fbioBlank, 0) // unblank

	vsyncOK := false
	if cfg.VSync {
		var zero uint32
		vsyncOK = fbIoctl(fd, fbioWaitForVSync, unsafe.Pointer(&zero)) == nil
	}

	d := &FBDisplayController{
		vsyncActive:   vsyncOK,
		fd:            fd,
		mem:           mem,
		pix:           unsafe.Slice((*uint16)(unsafe.Pointer(&mem[0])), len(mem)/2),
		vinfo:         v,
		xres:          xres,
		yres:          yres,
		stride:        stride,
		pages:         pages,
		pageIDs:       pageIDs,
		back:          0, // first selected inactive page
		check:         !cfg.DisableFrameCheck,
		pend:          [2]damage{noDamage, noDamage},
		rot:           rot,
		rotSource:     rotSource,
		originalVInfo: v,
		row:           make([]uint16, xres),
	}
	d.frame = baseFrame

	d.initMaps()
	d.initOperationSurface()

	return d, nil
}

// fbSampleCenter maps destination pixel centers to source pixel centers.
// Unlike edge-aligned division, it avoids a consistent one-sided bias.
func fbSampleCenter(i, source, destination int) int {
	return int((int64(2*i+1) * int64(source)) / (2 * int64(destination)))
}

// initMaps combines display rotation and presentation scaling once.
// An invalid coordinate (-1) identifies a background bar.
func (d *FBDisplayController) initMaps() {
	p := d.presentation
	d.xmap = make([]int32, d.xres)
	d.ystart = make([]int32, d.yres)
	d.srcX = make([]int32, d.xres)
	d.srcY = make([]int32, d.yres)

	// Stretch preserves its established rotation direction.
	oldStretch := p.mode == ScalingStretch
	for x := 0; x < d.xres; x++ {
		if d.rot == 90 || d.rot == 270 {
			coord := fbSampleCenter(x, p.height, d.xres)
			if d.rot == 90 && !oldStretch {
				coord = p.height - 1 - coord
			}
			sy := p.ymap[coord]
			if d.rot == 90 && oldStretch && sy >= 0 {
				sy = d.h - 1 - sy
			}
			d.srcX[x] = int32(sy)
			if sy < 0 {
				d.xmap[x] = -1
			} else {
				d.xmap[x] = int32(sy * d.w)
			}
		} else {
			coord := fbSampleCenter(x, p.width, d.xres)
			if d.rot == 180 && !oldStretch {
				coord = p.width - 1 - coord
			}
			sx := p.xmap[coord]
			if d.rot == 180 && oldStretch && sx >= 0 {
				sx = d.w - 1 - sx
			}
			d.srcX[x] = int32(sx)
			d.xmap[x] = int32(sx)
		}
	}
	for y := 0; y < d.yres; y++ {
		if d.rot == 90 || d.rot == 270 {
			coord := fbSampleCenter(y, p.width, d.yres)
			if d.rot == 270 && !oldStretch {
				coord = p.width - 1 - coord
			}
			sx := p.xmap[coord]
			if d.rot == 270 && oldStretch && sx >= 0 {
				sx = d.w - 1 - sx
			}
			d.srcY[y] = int32(sx)
			d.ystart[y] = int32(sx)
		} else {
			coord := fbSampleCenter(y, p.height, d.yres)
			if d.rot == 180 && !oldStretch {
				coord = p.height - 1 - coord
			}
			sy := p.ymap[coord]
			if d.rot == 180 && oldStretch && sy >= 0 {
				sy = d.h - 1 - sy
			}
			d.srcY[y] = int32(sy)
			if sy < 0 {
				d.ystart[y] = -1
			} else {
				d.ystart[y] = int32(sy * d.w)
			}
		}
	}
}

// fbSpan is the inclusive physical range covered by a logical coordinate.
// Empty spans describe pixels discarded by noninteger downsampling.
type fbSpan struct{ lo, hi int }

func (s fbSpan) empty() bool { return s.hi < s.lo }

// initOperationSurface prepares the inverse rotation/scaling maps once.
// Physical output is retained independently of the two framebuffer pages.
func (d *FBDisplayController) initOperationSurface() {
	d.surface = make([]uint16, d.xres*d.yres)
	d.invX = make([]fbSpan, d.w)
	d.invY = make([]fbSpan, d.h)
	for i := range d.invX {
		d.invX[i] = fbSpan{0, -1}
	}
	for i := range d.invY {
		d.invY[i] = fbSpan{0, -1}
	}
	for x, v := range d.srcX {
		if v < 0 {
			continue
		}
		if d.rot == 90 || d.rot == 270 {
			d.invY[v] = extendFBSpan(d.invY[v], x)
		} else {
			d.invX[v] = extendFBSpan(d.invX[v], x)
		}
	}
	for y, v := range d.srcY {
		if v < 0 {
			continue
		}
		if d.rot == 90 || d.rot == 270 {
			d.invX[v] = extendFBSpan(d.invX[v], y)
		} else {
			d.invY[v] = extendFBSpan(d.invY[v], y)
		}
	}
	d.surfaceTouched = noDamage
}

func extendFBSpan(s fbSpan, value int) fbSpan {
	if s.empty() {
		return fbSpan{value, value}
	}
	if value < s.lo {
		s.lo = value
	}
	if value > s.hi {
		s.hi = value
	}
	return s
}

func rangeFBSpan(spans []fbSpan, first, last int) fbSpan {
	out := fbSpan{0, -1}
	first = max(first, 0)
	last = min(last, len(spans)-1)
	for i := first; i <= last; i++ {
		if !spans[i].empty() {
			out = extendFBSpan(out, spans[i].lo)
			out = extendFBSpan(out, spans[i].hi)
		}
	}
	return out
}

// physicalDamage maps a logical rectangle to the destination pixel bounds.
// It also handles pixels lost to unavoidable physical downsampling.
func (d *FBDisplayController) physicalDamage(dm damage) damage {
	if dm.empty() {
		return noDamage
	}
	px, py := rangeFBSpan(d.invX, dm.c0, dm.c1), rangeFBSpan(d.invY, dm.r0, dm.r1)
	if px.empty() || py.empty() {
		return noDamage
	}
	if d.rot == 90 || d.rot == 270 {
		return damage{px.lo, px.hi, py.lo, py.hi}
	}
	return damage{py.lo, py.hi, px.lo, px.hi}
}

// paintPhysical applies a logical dirty rectangle directly to the rotated surface.
// The output never needs to be resampled again when switching framebuffer pages.
func (d *FBDisplayController) paintPhysical(dm damage) {
	phys := d.physicalDamage(dm)
	if phys.empty() {
		return
	}
	for y := phys.r0; y <= phys.r1; y++ {
		row := y * d.xres
		for x := phys.c0; x <= phys.c1; x++ {
			a, b := d.xmap[x], d.ystart[y]
			if a >= 0 && b >= 0 {
				d.surface[row+x] = d.buf[a+b]
			} else {
				d.surface[row+x] = d.presentation.color
			}
		}
	}
}

// syncSurface reconciles drawing and raw writes against the final logical image.
// Full clears are deferred, avoiding redundant rotated writes before Update.
func (d *FBDisplayController) syncSurface(dm damage) {
	if !d.surfaceReady {
		fillPixels(d.surface, d.presentation.color)
		d.paintPhysical(d.fullDamage())
		d.surfaceReady = true
	} else if d.surfaceSync {
		d.paintPhysical(dm.union(d.surfaceTouched))
	}
	d.surfaceSync = false
	d.surfaceTouched = noDamage
}

// SetPixel writes the logical buffer and updates its rotated destination.
func (d *FBDisplayController) SetPixel(x, y int, color uint16) {
	if x < 0 || y < 0 || x >= d.w || y >= d.h {
		return
	}
	if d.buf[y*d.w+x] == color {
		return
	}
	d.frame.SetPixel(x, y, color)
	if d.surfaceReady && !d.surfaceSync {
		d.paintPhysical(damage{y, y, x, x})
		d.surfaceTouched = d.surfaceTouched.union(damage{y, y, x, x})
	}
}

// HLine draws in logical coordinates and updates the rotated destination.
func (d *FBDisplayController) HLine(x0, x1, y int, color uint16) {
	if y < 0 || y >= d.h {
		return
	}
	x0, x1 = max(0, x0), min(d.w-1, x1)
	if x0 > x1 {
		return
	}
	d.frame.HLine(x0, x1, y, color)
	if d.surfaceReady && !d.surfaceSync {
		d.paintPhysical(damage{y, y, x0, x1})
		d.surfaceTouched = d.surfaceTouched.union(damage{y, y, x0, x1})
	}
}

// Clear clears logical pixels; the finished frame is rotated once at Update.
func (d *FBDisplayController) Clear(color uint16) {
	d.frame.Clear(color)
	d.surfaceSync = true
}

// Buffer exposes the logical RGB565 data and enables raw-write reconciliation.
func (d *FBDisplayController) Buffer() []uint16 {
	d.rawAccess = true
	return d.frame.Buffer()
}

// SetBuffer replaces logical pixels and invalidates the rotated surface.
func (d *FBDisplayController) SetBuffer(b []uint16) {
	d.frame.SetBuffer(b)
	d.rawAccess = true
	d.surfaceSync = true
}

// MarkDirtyRect marks raw writes so the rotated surface is synchronized.
func (d *FBDisplayController) MarkDirtyRect(x0, y0, x1, y1 int) {
	d.frame.MarkDirtyRect(x0, y0, x1, y1)
	d.surfaceSync = true
}

// copyPhysical copies only changed physical pixels to the inactive page.
// No source-coordinate mapping, rotation or division runs in this hot path.
func (d *FBDisplayController) copyPhysical(page int, dm damage) {
	if dm.empty() {
		return
	}
	base := page * d.yres * d.stride
	for y := dm.r0; y <= dm.r1; y++ {
		from := y*d.xres + dm.c0
		to := base + y*d.stride + dm.c0
		copy(d.pix[to:to+dm.c1-dm.c0+1], d.surface[from:from+dm.c1-dm.c0+1])
	}
}

// Update copies changed physical pixels and pans to the completed page.
// It keeps both framebuffer pages coherent across incremental updates.
func (d *FBDisplayController) Update() error {
	if d.mem == nil || len(d.buf) == 0 {
		return nil
	}
	start := time.Now()
	dm := d.changed(d.check)
	if d.rawAccess && d.surfaceReady {
		d.surfaceSync = true
	}
	d.syncSurface(dm)
	if d.check && dm.empty() {
		d.lastCopy = time.Since(start)
		d.skipped++
		return nil
	}
	changed := d.physicalDamage(dm)
	if changed.empty() && d.pagePrimed[0] && d.pagePrimed[1] {
		d.lastCopy = time.Since(start)
		d.skipped++
		return nil
	}
	d.pend[0] = d.pend[0].union(changed)
	d.pend[1] = d.pend[1].union(changed)
	copyArea := d.pend[d.back]
	if !d.pagePrimed[d.back] {
		copyArea = damage{0, d.yres - 1, 0, d.xres - 1}
	}
	d.copyPhysical(d.pageIDs[d.back], copyArea)
	d.pagePrimed[d.back] = true
	d.pend[d.back] = noDamage
	d.lastCopy = time.Since(start)
	d.lastWait = 0
	panStart := time.Now()
	d.vinfo.Xoffset = 0
	d.vinfo.Yoffset = uint32(d.pageIDs[d.back] * d.yres)
	if err := fbIoctl(d.fd, fbioPanDisplay, unsafe.Pointer(&d.vinfo)); err != nil {
		return fmt.Errorf("FBIOPAN_DISPLAY: %w", err)
	}
	d.lastPage = d.pageIDs[d.back]
	d.panChanged = true
	d.back ^= 1
	if d.vsyncActive {
		w := time.Now()
		d.waitVSync()
		d.lastWait = time.Since(w)
	}
	d.lastShow = time.Since(panStart)
	return nil
}

func (d *FBDisplayController) waitVSync() {
	var zero uint32
	_ = fbIoctl(d.fd, fbioWaitForVSync, unsafe.Pointer(&zero))
}

// blit is retained as the reference pixel mapper for regression tests.
// Production Update uses the pre-rotated operation surface above.
func (d *FBDisplayController) blit(page int, dm damage) {
	if dm.empty() {
		return
	}
	if d.presentation.centered && !d.pagePrimed[d.back] {
		d.fillCenteredBackground(page)
	}
	base := page * d.yres * d.stride
	p := d.presentation
	if p.centered && !p.scaled && d.rot == 0 && p.width == d.xres && p.height == d.yres {
		for y := dm.r0; y <= dm.r1; y++ {
			src := y*d.w + dm.c0
			dst := base + (p.y+y)*d.stride + p.x + dm.c0
			copy(d.pix[dst:dst+dm.c1-dm.c0+1], d.buf[src:src+dm.c1-dm.c0+1])
		}
		return
	}
	// Compare on the same mapped pixels used by the operation surface.
	for y := 0; y < d.yres; y++ {
		sy := d.srcY[y]
		if sy < 0 {
			continue
		}
		for x := 0; x < d.xres; x++ {
			sx := d.srcX[x]
			if sx < 0 {
				continue
			}
			var dx, dy int
			if d.rot == 90 || d.rot == 270 {
				dx, dy = int(sy), int(sx)
			} else {
				dx, dy = int(sx), int(sy)
			}
			if dx < dm.c0 || dx > dm.c1 || dy < dm.r0 || dy > dm.r1 {
				continue
			}
			d.pix[base+y*d.stride+x] = d.buf[d.xmap[x]+d.ystart[y]]
		}
	}
}

func (d *FBDisplayController) fillCenteredBackground(page int) {
	fillPixels(d.row[:d.xres], d.presentation.color)
	base := page * d.yres * d.stride
	for y := 0; y < d.yres; y++ {
		o := base + y*d.stride
		copy(d.pix[o:o+d.xres], d.row[:d.xres])
	}
}

// Stats returns the active configuration and the latest timings.
func (d *FBDisplayController) Stats() Stats {
	return Stats{
		Backend:     "fb",
		VSyncActive: d.vsyncActive,
		Copy:        d.lastCopy,
		Wait:        d.lastWait,
		Show:        d.lastShow,
		Skipped:     d.skipped,
	}
}

// Close unmaps the framebuffer and closes the device. Calling it twice is safe.
// DebugInfo describes the framebuffer geometry and software rotation.
func (d *FBDisplayController) DebugInfo() string {
	p := d.presentation
	return fmt.Sprintf("FB physical=%dx%d virtual=%dx%d stride=%d rotate=%d (%s) canvas=%dx%d viewport=(%d,%d)+%dx%d mode=%s pages=%v renderer=rotated-operations",
		d.xres, d.yres, d.vinfo.XresVirtual, d.vinfo.YresVirtual, d.stride,
		d.rot, d.rotSource, p.width, p.height, p.x, p.y, p.activeW, p.activeH,
		p.mode, d.pageIDs)
}

// Close releases /dev/fb0 and restores the initial pan when still owned.
func (d *FBDisplayController) Close() {
	if d.fd >= 0 && d.panChanged {
		var current fbVarScreeninfo
		if err := fbIoctl(d.fd, fbioGetVScreenInfo, unsafe.Pointer(&current)); err == nil &&
			current.Xres == d.originalVInfo.Xres && current.Yres == d.originalVInfo.Yres &&
			current.Xoffset == 0 && current.Yoffset == uint32(d.lastPage*d.yres) {
			original := d.originalVInfo
			_ = fbIoctl(d.fd, fbioPanDisplay, unsafe.Pointer(&original))
		}
	}
	if d.mem != nil {
		_ = syscall.Munmap(d.mem)
		d.mem, d.pix, d.buf, d.surface = nil, nil, nil, nil
	}
	if d.fd > 0 {
		_ = syscall.Close(d.fd)
		d.fd = -1
	}
}
