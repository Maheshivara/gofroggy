//go:build linux && mipsle

package display

// Pure-Go SF3000 /dev/ge backend. This is a hardware-specific implementation:
// the 232-byte HCGE instruction sequence was captured from the vendor driver
// and successfully executed in the SF3000 device.
//
// It is NOT a generic HCGE implementation. Do not use with another framebuffer
// layout, another rotation, or simultaneously with another display client.

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

const (
	geQueryIoctl       = 0x20002305
	geWaitIoctl        = 0x20002304
	geRegisterBytes    = 0x2000
	geQueueMmapOffset  = 0x2000
	geMaximumFBLen     = 32 << 20
	geMipsVsyncIoctl   = 0x80044620
	geVirtualPages     = 15
	gePhysBaseObserved = 0x0af92000
)

// GEDisplayController renders RGB565 using the SF3000 graphics engine.
// It requires exclusive framebuffer and GE access.
type GEDisplayController struct {
	frame
	fbfd, gefd                        int
	lock                              *os.File
	fb, ring, regs                    []byte
	originalPage, visiblePage         uint32
	fbBase, fbLen, geBase, frameBytes uint32
	srcPage, tmpPage                  uint32
	outputPages                       [2]uint32
	backups                           [4][]byte
	backupPages                       [4]uint32
	packet                            []byte
	nextOutput                        int
	check                             bool
	vsync                             bool
	vsyncActive                       bool

	lastCopy, lastWait, lastShow time.Duration
	skipped                      uint64
	closed, faulty               bool
	sourcePrimed                 bool
}

var _ DisplayController = (*GEDisplayController)(nil)

func geIoctl(fd int, cmd, arg uintptr) (uintptr, error) {
	r, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), cmd, arg)
	if errno != 0 {
		return r, errno
	}
	return r, nil
}
func geQuery(fd int, cmd uintptr, buf []byte) error {
	_, err := geIoctl(fd, cmd, uintptr(unsafe.Pointer(&buf[0])))
	runtime.KeepAlive(buf)
	return err
}
func geMMIORead32(b []byte, off int) uint32 {
	v := *(*uint32)(unsafe.Pointer(&b[off]))
	runtime.KeepAlive(b)
	return v
}
func geMMIOWrite32(b []byte, off int, v uint32) {
	*(*uint32)(unsafe.Pointer(&b[off])) = v
	runtime.KeepAlive(b)
}
func geCacheFlush(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	_, _, err := syscall.Syscall(syscall.SYS_CACHEFLUSH,
		uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 3)
	runtime.KeepAlive(b)
	if err != 0 {
		return err
	}
	return nil
}

// NewGEDisplayController opens the SF3000 GE without cgo or vendor libraries.
// Presentation scales or centers the image before hardware rotation.
func NewGEDisplayController(cfg Config) (d *GEDisplayController, err error) {
	if cfg.Rotation != RotationNone && cfg.Rotation != RotationC90 {
		return nil, fmt.Errorf("SF3000 GE supports only its established 90-degree clockwise path, got %s", cfg.Rotation)
	}
	d = &GEDisplayController{fbfd: -1, gefd: -1, check: !cfg.DisableFrameCheck, vsync: cfg.VSync}
	// Capture the allocated controller *before* any return statement. With
	// named results, `return nil, err` sets d=nil before defers execute.
	// Closing the named result would therefore panic and hide the real error.
	resourceOwner := d
	defer func() {
		if err != nil {
			resourceOwner.closeResources()
		}
	}()

	// An advisory lock protects against other copies of this backend; it
	// cannot lock out a vendor/third-party framebuffer client.
	d.lock, err = os.OpenFile("/tmp/sf3000-ge.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("GE lockfile: %w", err)
	}
	if err = syscall.Flock(int(d.lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, fmt.Errorf("another BackendGE process holds the GE lock: %w", err)
	}
	d.fbfd, err = syscall.Open("/dev/fb0", syscall.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/fb0: %w", err)
	}
	var fix [80]byte
	var vari [160]byte
	if err = geQuery(d.fbfd, fbioGetFScreenInfo, fix[:]); err != nil {
		return nil, fmt.Errorf("FB FIX: %w", err)
	}
	if err = geQuery(d.fbfd, fbioGetVScreenInfo, vari[:]); err != nil {
		return nil, fmt.Errorf("FB VAR: %w", err)
	}
	d.fbBase, d.fbLen = geGet(fix[:], 16), geGet(fix[:], 20)
	stride := geGet(fix[:], 44)
	x, y := geGet(vari[:], 0), geGet(vari[:], 4)
	xv, yv := geGet(vari[:], 8), geGet(vari[:], 12)
	yoff, bpp := geGet(vari[:], 20), geGet(vari[:], 24)
	// Preserve all safety constraints and report exactly which one failed.
	// Do NOT attempt GE command submission on an unrecognized FB geometry.
	var layoutIssues []string
	if x != gePanelWidth || y != gePanelHeight {
		layoutIssues = append(layoutIssues, "panel dimensions")
	}
	if xv != x || yv < geVirtualPages*y {
		layoutIssues = append(layoutIssues, "virtual dimensions")
	}
	if stride != gePanelWidth*2 {
		layoutIssues = append(layoutIssues, "stride")
	}
	if bpp != 16 {
		layoutIssues = append(layoutIssues, "bits per pixel")
	}
	if y == 0 || yoff%gePanelHeight != 0 {
		layoutIssues = append(layoutIssues, "display offset")
	}
	if d.fbBase != gePhysBaseObserved {
		layoutIssues = append(layoutIssues, "physical base")
	}
	if d.fbLen < 17*geFrameBytes || d.fbLen > geMaximumFBLen {
		layoutIssues = append(layoutIssues, "physical length")
	}
	if len(layoutIssues) != 0 {
		return nil, fmt.Errorf("unsupported SF3000 GE FB layout (failed: %v): base=%08x len=%d %dx%d virt=%dx%d stride=%d bpp=%d yoff=%d", layoutIssues, d.fbBase, d.fbLen, x, y, xv, yv, stride, bpp, yoff)
	}
	d.frameBytes = geFrameBytes
	d.originalPage = yoff / y
	d.visiblePage = d.originalPage
	if d.originalPage >= geVirtualPages {
		return nil, fmt.Errorf("display page %d is outside virtual framebuffer", d.originalPage)
	}

	chosen, chooseErr := geChoosePages(d.originalPage)
	if chooseErr != nil {
		return nil, chooseErr
	}
	d.srcPage, d.tmpPage = chosen[0], chosen[1]
	d.outputPages = [2]uint32{chosen[2], chosen[3]}
	d.backupPages = chosen
	if e := d.frame.configure(cfg, gePanelWidth, gePanelHeight); e != nil {
		return nil, e
	}

	d.gefd, err = syscall.Open("/dev/ge", syscall.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/ge: %w", err)
	}
	var ge [8]byte
	if err = geQuery(d.gefd, geQueryIoctl, ge[:]); err != nil {
		return nil, fmt.Errorf("GE query: %w", err)
	}
	d.geBase = geGet(ge[:], 0)
	geLen := geGet(ge[:], 4)
	if d.geBase == 0 || geLen != geRingLength {
		return nil, fmt.Errorf("unexpected GE base/length: %08x/%x", d.geBase, geLen)
	}
	d.ring, err = syscall.Mmap(d.gefd, geQueueMmapOffset, geRingLength, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("GE queue mmap: %w", err)
	}
	d.regs, err = syscall.Mmap(d.gefd, 0, geRegisterBytes, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("GE regs mmap: %w", err)
	}
	d.fb, err = syscall.Mmap(d.fbfd, 0, int(d.fbLen), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("FB mmap: %w", err)
	}
	if d.packet, err = geTemplate(); err != nil {
		return nil, err
	}
	if err = d.validateGEIdle(); err != nil {
		return nil, err
	}
	// Ensure the display has not moved while the GE device was opened.
	if err = d.expectDisplayedPage(d.originalPage); err != nil {
		return nil, err
	}
	// Preserve pre-existing pixels; display pages are NOT cleared at startup.
	// Restored by Close only after confirmed GE completion and original pan.
	for i, page := range d.backupPages {
		d.backups[i] = append([]byte(nil), d.page(page)...)
	}

	if d.presentation.native(d.w, d.h) {
		src := d.page(d.srcPage)
		d.frame.buf = unsafe.Slice((*uint16)(unsafe.Pointer(&src[0])), gePanelWidth*gePanelHeight)
		runtime.KeepAlive(src)
	}
	return d, nil
}

func geChoosePages(active uint32) ([4]uint32, error) {
	for _, p := range [...][4]uint32{{11, 12, 13, 8}, {5, 6, 7, 8}, {1, 2, 3, 4}, {8, 9, 10, 14}} {
		found := false
		for _, q := range p {
			if q == active {
				found = true
			}
		}
		if !found {
			return p, nil
		}
	}
	return [4]uint32{}, fmt.Errorf("no safe scratch/output page set (visible page %d)", active)
}
func (d *GEDisplayController) page(page uint32) []byte {
	off := int(page * d.frameBytes)
	return d.fb[off : off+int(d.frameBytes)]
}
func (d *GEDisplayController) phys(page uint32) uint32 { return d.fbBase + page*d.frameBytes }

func (d *GEDisplayController) validateGEIdle() error {
	start, end, cursor := geGet(d.ring, 0), geGet(d.ring, 4), geGet(d.ring, 0x18)
	regCursor, regEnd := geMMIORead32(d.regs, 0x10), geMMIORead32(d.regs, 0x14)
	ctrl0, ctrl4, status := geMMIORead32(d.regs, 0), geMMIORead32(d.regs, 4), geMMIORead32(d.regs, 8)
	if start != d.geBase+geRingStart || end != d.geBase+geRingLength || ctrl0 != 0x80020000 || status != 0 {
		return fmt.Errorf("unexpected GE ring/control/status start=%08x end=%08x ctrl=%08x status=%08x", start, end, ctrl0, status)
	}
	header8, headerC, header10 := geGet(d.ring, 8), geGet(d.ring, 0xc), geGet(d.ring, 0x10)
	reset := cursor == start && regCursor == start && regEnd == start && ((ctrl4&3) == 0 || (ctrl4&3) == 2) && header8 == 0 && headerC == 0 && header10 == 1
	idle := cursor >= start+gePacketBytes && cursor <= end && regCursor == cursor && regEnd == cursor-4 &&
		(ctrl4&3) == 2 && header8 == 1 && header10 == 0
	if reset {
		return nil
	}
	if !idle {
		return fmt.Errorf("unknown/busy GE queue cursor=%08x registers=%08x/%08x ctrl=%08x header=%x/%x/%x", cursor, regCursor, regEnd, ctrl4, header8, headerC, header10)
	}
	off := int(cursor-d.geBase) - gePacketBytes
	if off < geRingStart || off+gePacketBytes > len(d.ring) {
		return fmt.Errorf("GE last packet out of bounds")
	}
	if err := geCompatiblePacket(d.ring[off:off+gePacketBytes], d.packet, d.fbBase, d.fbLen); err != nil {
		return fmt.Errorf("unknown previous GE packet; reboot/stop other GE clients before BackendGE: %w", err)
	}
	return nil
}

// geRewind resets idle queue pointers when the command ring fills.
// This reset-on-wrap path has not yet been tested on hardware.
func (d *GEDisplayController) geRewind() error {
	old := geGet(d.ring, 0x18)
	if geMMIORead32(d.regs, 8) != 0 || geMMIORead32(d.regs, 0x10) != old ||
		(old != d.geBase+geRingStart && geMMIORead32(d.regs, 0x14) != old-4) {
		return fmt.Errorf("GE not idle; refusing command ring rewind")
	}
	start := d.geBase + geRingStart
	geMMIOWrite32(d.ring, 0, start)
	geMMIOWrite32(d.ring, 4, d.geBase+geRingLength)
	geMMIOWrite32(d.ring, 8, 0)
	geMMIOWrite32(d.ring, 0xc, 0)
	geMMIOWrite32(d.ring, 0x10, 1)
	geMMIOWrite32(d.ring, 0x18, start)
	geMMIOWrite32(d.regs, 0x10, start)
	geMMIOWrite32(d.regs, 0x14, start)
	return nil
}

func (d *GEDisplayController) expectDisplayedPage(page uint32) error {
	var v [160]byte
	if err := geQuery(d.fbfd, fbioGetVScreenInfo, v[:]); err != nil {
		return err
	}
	if geGet(v[:], 0) != gePanelWidth || geGet(v[:], 4) != gePanelHeight ||
		geGet(v[:], 20) != page*gePanelHeight || geGet(v[:], 16) != 0 {
		return fmt.Errorf("FB page/mode changed externally (expected %d, got x=%d yoff=%d)", page, geGet(v[:], 16), geGet(v[:], 20))
	}
	return nil
}
func (d *GEDisplayController) pan(page uint32) error {
	var v [160]byte
	if err := geQuery(d.fbfd, fbioGetVScreenInfo, v[:]); err != nil {
		return err
	}
	gePut(v[:], 16, 0)
	gePut(v[:], 20, page*gePanelHeight)
	_, err := geIoctl(d.fbfd, fbioPanDisplay, uintptr(unsafe.Pointer(&v[0])))
	runtime.KeepAlive(v)
	if err != nil {
		return fmt.Errorf("FBIOPAN_DISPLAY(%d): %w", page, err)
	}
	return d.expectDisplayedPage(page)
}

// ZeroCopyEnabled reports whether the drawing buffer maps the GE source page.
// Other buffers use the shared RGB565 compositor.
func (d *GEDisplayController) ZeroCopyEnabled() bool {
	return len(d.buf) == gePanelWidth*gePanelHeight &&
		d.presentation.native(d.w, d.h) &&
		len(d.fb) != 0 &&
		unsafe.Pointer(&d.buf[0]) == unsafe.Pointer(&d.page(d.srcPage)[0])
}

func (d *GEDisplayController) copySource(dm damage) error {
	if d.ZeroCopyEnabled() {
		d.sourcePrimed = true
		return nil
	}
	src := d.page(d.srcPage)
	pixels := unsafe.Slice((*uint16)(unsafe.Pointer(&src[0])), gePanelWidth*gePanelHeight)
	_, err := d.presentation.compose(pixels, d.buf, d.w, d.h, dm, !d.sourcePrimed)
	runtime.KeepAlive(src)
	if err == nil {
		d.sourcePrimed = true
	}
	return err
}

// submit executes a two-pass hardware rotation packet and waits for completion.
// Display panning occurs only after the engine is idle.
func (d *GEDisplayController) submit(dst uint32) (flushTime, waitTime time.Duration, err error) {
	cursor := geGet(d.ring, 0x18)
	if status := geMMIORead32(d.regs, 8); status != 0 || geMMIORead32(d.regs, 0x10) != cursor {
		return 0, 0, fmt.Errorf("GE not idle: status=%08x cursor=%08x", status, cursor)
	}
	if cursor+gePacketBytes > d.geBase+geRingLength-0x20 {
		if err := d.geRewind(); err != nil {
			return 0, 0, err
		}
		cursor = geGet(d.ring, 0x18)
	}
	packet := geRelocatedPacket(d.packet, d.phys(d.srcPage), d.phys(d.tmpPage), d.phys(dst))
	begin := int(cursor - d.geBase)
	if begin < geRingStart || begin+len(packet) > len(d.ring) {
		return 0, 0, fmt.Errorf("GE command write out of queue bounds")
	}
	t := time.Now()
	for _, p := range [...]uint32{d.srcPage, d.tmpPage, dst} {
		if err := geCacheFlush(d.page(p)); err != nil {
			return 0, 0, fmt.Errorf("FB cacheflush page %d: %w", p, err)
		}
	}
	copy(d.ring[begin:begin+len(packet)], packet)
	if err := geCacheFlush(d.ring[begin : begin+len(packet)]); err != nil {
		return 0, 0, fmt.Errorf("GE command cacheflush: %w", err)
	}
	flushTime = time.Since(t)
	end := cursor + uint32(len(packet))
	// Exact hcge_feed_nodes transition observed in the vendor code
	geMMIOWrite32(d.regs, 0x10, cursor)
	geMMIOWrite32(d.regs, 0x14, end-4)
	geMMIOWrite32(d.ring, 0x10, 0)
	geMMIOWrite32(d.ring, 0x08, 0)
	geMMIOWrite32(d.ring, 0x18, end)
	geMMIOWrite32(d.regs, 0, geMMIORead32(d.regs, 0)|(1<<31)|(1<<17))
	geMMIOWrite32(d.regs, 4, (geMMIORead32(d.regs, 4)&^uint32(3))|2)

	t = time.Now()
	for attempt := 0; attempt < 3; attempt++ {
		_, e := geIoctl(d.gefd, geWaitIoctl, 80)
		status, got := geMMIORead32(d.regs, 8), geMMIORead32(d.regs, 0x10)
		if e != nil {
			return flushTime, time.Since(t), fmt.Errorf("GE WAIT ioctl: %w (status=%08x cursor=%08x)", e, status, got)
		}
		if status&0x80000000 != 0 {
			return flushTime, time.Since(t), fmt.Errorf("GE hardware fault status=%08x cursor=%08x", status, got)
		}
		if status == 0 && got == end {
			return flushTime, time.Since(t), nil
		}
	}
	return flushTime, time.Since(t), fmt.Errorf("GE completion not confirmed: status=%08x cursor=%08x expected=%08x", geMMIORead32(d.regs, 8), geMMIORead32(d.regs, 0x10), end)
}

// Update copies the application's RGB565 buffer to the GE source page,
// hardware-rotates/scales it, then pans to the completed output page.
func (d *GEDisplayController) Update() error {
	if d.closed {
		return fmt.Errorf("GE display controller closed")
	}
	if d.faulty {
		return fmt.Errorf("GE previously faulted; reboot before retry")
	}
	if len(d.buf) != d.w*d.h {
		return fmt.Errorf("invalid GE buffer length: got %d pixels, need %d", len(d.buf), d.w*d.h)
	}
	if err := d.expectDisplayedPage(d.visiblePage); err != nil {
		d.faulty = true
		return err
	}
	dm := d.changed(d.check)
	if dm.empty() {
		d.skipped++
		return nil
	}
	t := time.Now()
	if err := d.copySource(dm); err != nil {
		return err
	}
	copyTime := time.Since(t)
	dst := d.outputPages[d.nextOutput]
	flushTime, waitTime, err := d.submit(dst)
	// Treat ALL submission errors as potentially active DMA; never restore
	// captured pages after a failed/timeout GE submission.
	if err != nil {
		d.faulty = true
		return err
	}
	d.lastCopy = copyTime + flushTime
	d.lastWait = waitTime
	t = time.Now()
	if err = d.pan(dst); err != nil {
		d.faulty = true
		return err
	}
	d.visiblePage = dst
	d.nextOutput ^= 1
	if d.vsync {
		var zero uint32
		if err = fbIoctl(d.fbfd, geMipsVsyncIoctl, unsafe.Pointer(&zero)); err != nil {
			return fmt.Errorf("GE vsync ioctl: %w", err)
		}
		d.vsyncActive = true
	}
	d.lastShow = time.Since(t)
	return nil
}

// DebugInfo returns read-only GE queue, framebuffer and sample-pixel state.
// It does not reset, submit, or otherwise modify the graphics engine.
func (d *GEDisplayController) DebugInfo() string {
	if d.closed || len(d.ring) == 0 || len(d.regs) == 0 || len(d.fb) == 0 {
		return "GE closed or not initialized"
	}
	fbState := "ok"
	if err := d.expectDisplayedPage(d.visiblePage); err != nil {
		fbState = err.Error()
	}
	page := d.page(d.visiblePage)
	pixel := func(x, y int) uint16 {
		off := (y*gePanelWidth + x) * 2
		return uint16(page[off]) | uint16(page[off+1])<<8
	}
	return fmt.Sprintf("GE visible=%d original=%d src=%d tmp=%d out=%v queue=%08x hwcursor=%08x hwstatus=%08x ctrl=%08x pan=%s pixels[top-left,center,bottom-right]=[%04x,%04x,%04x] faulty=%v",
		d.visiblePage, d.originalPage, d.srcPage, d.tmpPage, d.outputPages,
		geGet(d.ring, 0x18), geMMIORead32(d.regs, 0x10), geMMIORead32(d.regs, 8),
		geMMIORead32(d.regs, 0), fbState,
		pixel(0, 0), pixel(gePanelWidth/2, gePanelHeight/2), pixel(gePanelWidth-1, gePanelHeight-1), d.faulty)
}

// Stats implements DisplayController. Timings refer to the latest Update.
func (d *GEDisplayController) Stats() Stats {
	return Stats{Backend: "ge", VSyncActive: d.vsyncActive,
		Copy: d.lastCopy, Wait: d.lastWait, Show: d.lastShow, Skipped: d.skipped}
}

func (d *GEDisplayController) closeResources() {
	if d.regs != nil {
		_ = syscall.Munmap(d.regs)
		d.regs = nil
	}
	if d.ring != nil {
		_ = syscall.Munmap(d.ring)
		d.ring = nil
	}
	if d.fb != nil {
		_ = syscall.Munmap(d.fb)
		d.fb = nil
	}
	if d.gefd >= 0 {
		_ = syscall.Close(d.gefd)
		d.gefd = -1
	}
	if d.fbfd >= 0 {
		_ = syscall.Close(d.fbfd)
		d.fbfd = -1
	}
	if d.lock != nil {
		_ = syscall.Flock(int(d.lock.Fd()), syscall.LOCK_UN)
		_ = d.lock.Close()
		d.lock = nil
	}
}

// Close restores saved pages only after confirmed GE completion.
// It avoids racing potentially active DMA after a fault.
func (d *GEDisplayController) Close() {
	if d.closed {
		return
	}
	d.closed = true
	if !d.faulty && len(d.fb) != 0 && len(d.backups[0]) != 0 && d.fbfd >= 0 {
		if d.expectDisplayedPage(d.visiblePage) == nil && d.pan(d.originalPage) == nil {
			// Do not race another GE client that moved the queue since Update.
			cursor := geGet(d.ring, 0x18)
			if geMMIORead32(d.regs, 8) == 0 && geMMIORead32(d.regs, 0x10) == cursor {
				for i, page := range d.backupPages {
					copy(d.page(page), d.backups[i])
					_ = geCacheFlush(d.page(page))
				}
			}
		}
	}
	// Do not retain a slice into unmapped framebuffer memory after Close.
	d.buf, d.prev = nil, nil
	d.closeResources()
	for i := range d.backups {
		d.backups[i] = nil
	}
}
