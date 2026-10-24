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
// driver. Explicitly smaller images are centered on a colored background.
type FBDisplayController struct {
	frame

	downscale   float64
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

	rot int // clockwise degrees the frame is rotated by on the way out

	row []uint16 // scratch row, so the framebuffer is never read

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

	rot, _ := fbDetectRotation(panelW, panelH)
	if cfg.Rotation != RotationNone {
		rot = cfg.Rotation.Degrees() // forced, instead of the device's
	}

	// The app always sees the landscape panel, whatever the fb mode is.
	lw, lh := max(panelW, panelH), min(panelW, panelH)

	var baseFrame frame
	if err := baseFrame.configure(cfg, lw, lh); err != nil {
		return fail(err)
	}
	w := baseFrame.w

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
		downscale:   float64(lw) / float64(w),
		vsyncActive: vsyncOK,
		fd:          fd,
		mem:         mem,
		pix:         unsafe.Slice((*uint16)(unsafe.Pointer(&mem[0])), len(mem)/2),
		vinfo:       v,
		xres:        xres,
		yres:        yres,
		stride:      stride,
		pages:       pages,
		pageIDs:     pageIDs,
		back:        0, // first selected inactive page
		check:       !cfg.DisableFrameCheck,
		pend:        [2]damage{noDamage, noDamage},
		rot:         rot,
		row:         make([]uint16, xres),
	}
	d.frame = baseFrame

	d.initMaps()

	return d, nil
}

// initMaps precomputes where every panel pixel comes from. A panel pixel's
// source index in buf is xmap[x] + ystart[y], for any rotation.
func (d *FBDisplayController) initMaps() {
	if d.presentation.centered {
		d.initCenteredMaps()
		return
	}
	d.xmap = make([]int32, d.xres)
	d.ystart = make([]int32, d.yres)
	d.srcX = make([]int32, d.xres)
	d.srcY = make([]int32, d.yres)

	if d.rot == 90 || d.rot == 270 {
		for x := range d.xmap {
			row := x * d.h / d.xres
			if d.rot == 90 {
				row = d.h - 1 - row
			}
			d.xmap[x] = int32(row * d.w)
			d.srcX[x] = int32(row)
		}
		for y := range d.ystart {
			col := y * d.w / d.yres
			if d.rot == 270 {
				col = d.w - 1 - col
			}
			d.ystart[y] = int32(col)
			d.srcY[y] = int32(col)
		}
		return
	}

	for x := range d.xmap {
		col := x * d.w / d.xres
		if d.rot == 180 {
			col = d.w - 1 - col
		}
		d.xmap[x] = int32(col)
		d.srcX[x] = int32(col)
	}
	for y := range d.ystart {
		row := y * d.h / d.yres
		if d.rot == 180 {
			row = d.h - 1 - row
		}
		d.ystart[y] = int32(row * d.w)
		d.srcY[y] = int32(row)
	}
}

// Update copies what changed into the idle page and flips to it. A frame
// identical to the last one costs only the comparison.
func (d *FBDisplayController) Update() error {
	if d.mem == nil || len(d.buf) == 0 {
		return nil
	}

	var wait time.Duration
	t := time.Now()

	dm := d.changed(d.check)
	if d.check {
		if dm.empty() {
			d.lastCopy = time.Since(t)
			d.skipped++
			return nil
		}
	}
	d.pend[0] = d.pend[0].union(dm)
	d.pend[1] = d.pend[1].union(dm)
	// Each selected page must receive a complete first frame.
	if !d.pagePrimed[d.back] {
		d.pend[d.back] = d.fullDamage()
	}

	if d.pages == 1 && d.vsyncActive {
		w := time.Now()
		d.waitVSync()
		wait = time.Since(w)
	}
	d.blit(d.pageIDs[d.back], d.pend[d.back])
	d.pagePrimed[d.back] = true
	d.pend[d.back] = noDamage

	d.lastCopy = time.Since(t) - wait
	d.lastWait = wait

	t = time.Now()
	d.vinfo.Xoffset = 0
	d.vinfo.Yoffset = uint32(d.pageIDs[d.back] * d.yres)
	if err := fbIoctl(d.fd, fbioPanDisplay, unsafe.Pointer(&d.vinfo)); err != nil {
		return fmt.Errorf("FBIOPAN_DISPLAY: %w", err)
	}
	if d.pages == 2 {
		d.back ^= 1
		if d.vsyncActive {
			w := time.Now()
			d.waitVSync()
			d.lastWait = time.Since(w)
		}
	}
	d.lastShow = time.Since(t)
	return nil
}

func (d *FBDisplayController) waitVSync() {
	var zero uint32
	_ = fbIoctl(d.fd, fbioWaitForVSync, unsafe.Pointer(&zero))
}

// initCenteredMaps maps panel pixels to the active rectangle once.
// Invalid coordinates use -1 so background pixels are never sampled.
func (d *FBDisplayController) initCenteredMaps() {
	p := d.presentation
	d.xmap = make([]int32, d.xres)
	d.ystart = make([]int32, d.yres)
	d.srcX = make([]int32, d.xres)
	d.srcY = make([]int32, d.yres)

	for x := 0; x < d.xres; x++ {
		var coordinate, offset, extent int
		if d.rot == 90 || d.rot == 270 {
			coordinate = x * p.height / d.xres
			if d.rot == 90 {
				coordinate = p.height - 1 - coordinate
			}
			offset, extent = p.y, d.h
		} else {
			coordinate = x * p.width / d.xres
			if d.rot == 180 {
				coordinate = p.width - 1 - coordinate
			}
			offset, extent = p.x, d.w
		}
		index := coordinate - offset
		if index < 0 || index >= extent {
			d.srcX[x], d.xmap[x] = -1, -1
			continue
		}
		d.srcX[x] = int32(index)
		if d.rot == 90 || d.rot == 270 {
			d.xmap[x] = int32(index * d.w)
		} else {
			d.xmap[x] = int32(index)
		}
	}

	for y := 0; y < d.yres; y++ {
		var coordinate, offset, extent int
		if d.rot == 90 || d.rot == 270 {
			coordinate = y * p.width / d.yres
			if d.rot == 270 {
				coordinate = p.width - 1 - coordinate
			}
			offset, extent = p.x, d.w
		} else {
			coordinate = y * p.height / d.yres
			if d.rot == 180 {
				coordinate = p.height - 1 - coordinate
			}
			offset, extent = p.y, d.h
		}
		index := coordinate - offset
		if index < 0 || index >= extent {
			d.srcY[y], d.ystart[y] = -1, -1
			continue
		}
		d.srcY[y] = int32(index)
		if d.rot == 90 || d.rot == 270 {
			d.ystart[y] = int32(index)
		} else {
			d.ystart[y] = int32(index * d.w)
		}
	}
}

// fillCenteredBackground initializes one output page's margins.
// Subsequent frames only touch pixels in the changed rectangle.
func (d *FBDisplayController) fillCenteredBackground(page int) {
	fillPixels(d.row[:d.xres], d.presentation.color)
	base := page * d.yres * d.stride
	for y := 0; y < d.yres; y++ {
		o := base + y*d.stride
		copy(d.pix[o:o+d.xres], d.row[:d.xres])
	}
}

// blit copies changed pixels into the inactive framebuffer page.
// Precomputed coordinate maps keep division out of the render loop.
func (d *FBDisplayController) blit(page int, dm damage) {
	if dm.empty() {
		return
	}
	if d.presentation.centered && !d.pagePrimed[d.back] {
		d.fillCenteredBackground(page)
	}
	base := page * d.yres * d.stride

	if d.rot == 0 && d.w == d.xres && d.h == d.yres && d.stride == d.xres &&
		dm.r0 == 0 && dm.r1 == d.h-1 && dm.c0 == 0 && dm.c1 == d.w-1 {
		copy(d.pix[base:base+d.xres*d.yres], d.buf)
		return
	}

	// A native, nonrotated centered rectangle can copy source rows directly.
	p := d.presentation
	if p.centered && d.rot == 0 && p.width == d.xres && p.height == d.yres {
		n := dm.c1 - dm.c0 + 1
		for y := dm.r0; y <= dm.r1; y++ {
			src := y*d.w + dm.c0
			dst := base + (p.y+y)*d.stride + p.x + dm.c0
			copy(d.pix[dst:dst+n], d.buf[src:src+n])
		}
		return
	}

	// Rotated output columns map to source rows; otherwise to columns.
	xlo, xhi, ylo, yhi := dm.c0, dm.c1, dm.r0, dm.r1
	if d.rot == 90 || d.rot == 270 {
		xlo, xhi, ylo, yhi = dm.r0, dm.r1, dm.c0, dm.c1
	}

	x0, x1 := d.xres, -1
	for x, s := range d.srcX {
		if int(s) >= xlo && int(s) <= xhi {
			x0 = min(x0, x)
			x1 = x
		}
	}
	if x1 < 0 {
		return
	}

	buf, xmap, row := d.buf, d.xmap, d.row
	prev := int32(-1)
	for y, start := range d.ystart {
		if s := int(d.srcY[y]); s < ylo || s > yhi {
			continue
		}
		if start != prev {
			prev = start
			for x := x0; x <= x1; x++ {
				row[x] = buf[xmap[x]+start]
			}
		}
		o := base + y*d.stride
		copy(d.pix[o+x0:o+x1+1], row[x0:x1+1])
	}
}

// Stats returns the active configuration and the latest timings.
func (d *FBDisplayController) Stats() Stats {
	return Stats{
		Backend:     "fb",
		Downscale:   d.downscale,
		VSyncActive: d.vsyncActive,
		Copy:        d.lastCopy,
		Wait:        d.lastWait,
		Show:        d.lastShow,
		Skipped:     d.skipped,
	}
}

// Close unmaps the framebuffer and closes the device. Calling it twice is safe.
func (d *FBDisplayController) Close() {
	if d.mem != nil {
		_ = syscall.Munmap(d.mem)
		d.mem, d.pix, d.buf = nil, nil, nil
	}
	if d.fd > 0 {
		_ = syscall.Close(d.fd)
		d.fd = -1
	}
}
