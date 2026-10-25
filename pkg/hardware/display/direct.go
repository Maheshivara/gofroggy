//go:build cgo

package display

/*
#cgo LDFLAGS: -ldl
#include "direct_driver.h"
*/
import "C"

import (
	"fmt"
	"os"
	"runtime"
	"time"
	"unsafe"
)

// init registers the direct backend with NewDisplayController.
func init() {
	directCtor = func(cfg Config) (DisplayController, error) {
		d, err := NewDirectDisplayController(cfg)
		if err != nil {
			return nil, err
		}
		return d, nil
	}
}

// DirectDisplayController loads the vendor display driver into this process
// with cgo (C side: direct_driver.c).
type DirectDisplayController struct {
	frame

	vsyncActive      bool
	updateScreen     bool
	frameSize        int
	outputW, outputH int
	stagePrimed      [2]bool

	stage [2]unsafe.Pointer // C-allocated staging buffers
	next  int               // staging buffer for the next frame
	pend  [2]damage         // changes each staging buffer has not received yet

	check   bool   // compare frames, skipping or shrinking unchanged ones
	skipped uint64 // frames skipped because they were identical

	pending bool
	req     chan int          // staging buffer index to present; closed to stop
	done    chan directTiming // one result per presented frame
	quit    chan struct{}     // closed when the driver thread has exited

	lastCopy, lastWait, lastShow time.Duration
	lastDetail                   DirectTimings
}

// directTiming is the timing of one presented frame.
type directTiming struct {
	wait, show, update time.Duration
}

// DirectTimings separates frame comparison, staging and vendor calls.
// PipelineWait is time spent waiting to reuse a staging buffer.
type DirectTimings struct {
	Compare, PipelineWait, StageCopy   time.Duration
	VSync, DriverDisplay, DriverUpdate time.Duration
}

var _ DisplayController = (*DirectDisplayController)(nil)

// NewDirectDisplayController loads the driver and allocates its buffers.
// Only available in cgo builds.
func NewDirectDisplayController(cfg Config) (*DirectDisplayController, error) {
	panelW, panelH := C.int(854), C.int(480)
	C.dd_fb_open(&panelW, &panelH)

	var baseFrame frame
	if err := baseFrame.configure(cfg, int(panelW), int(panelH)); err != nil {
		C.dd_fb_close()
		return nil, err
	}
	outputW, outputH := baseFrame.presentation.width, baseFrame.presentation.height
	frameSize := outputW * outputH * 2
	pitch := C.int(outputW * 2)

	// Probe the vsync ioctl once so the caller learns whether it really works.
	vsyncOK := false
	if cfg.VSync {
		vsyncOK = C.dd_fb_wait_vsync() == 0
	}

	d := &DirectDisplayController{
		vsyncActive:  vsyncOK,
		updateScreen: cfg.UpdateScreen,
		frameSize:    frameSize,
		outputW:      outputW, outputH: outputH,
		pend:  [2]damage{noDamage, noDamage},
		check: !cfg.DisableFrameCheck,
		req:   make(chan int, 1),
		done:  make(chan directTiming, 1),
		quit:  make(chan struct{}),
	}
	d.frame = baseFrame

	ready := make(chan error, 1)
	update := C.int(0)
	if cfg.UpdateScreen {
		update = 1
	}

	go func() {
		runtime.LockOSThread()
		defer close(d.quit)

		path := cfg.DriverPath
		if path == "" {
			var pathErr error
			path, pathErr = getDriverPath()
			if pathErr != nil {
				path = "/usr/lib/driver.so"
			}
			if _, err := os.Stat(path); err != nil {
				path = "/mnt/sdcard/cubegm/driver_sf3000.so"
			}
		}
		cpath := C.CString(path)
		rc := C.dd_load(cpath)
		C.dd_free(unsafe.Pointer(cpath))
		if rc != C.DD_OK {
			ready <- fmt.Errorf("display driver %q: %s", path, C.GoString(C.dd_error()))
			return
		}

		if ret := C.dd_init(); ret <= 0 {
			fmt.Fprintf(os.Stderr, "display: video_drivers_init returned %d\n", int(ret))
		}

		defer C.dd_unload()
		var stage [2]unsafe.Pointer
		defer func() {
			for _, ptr := range stage {
				if ptr != nil {
					C.dd_free(ptr)
				}
			}
		}()
		for i := range stage {
			p := C.dd_alloc(C.size_t(frameSize))
			if p == nil {
				ready <- fmt.Errorf("cannot allocate staging buffer %d", i)
				return
			}
			stage[i] = p
		}
		d.stage = stage
		ready <- nil

		for idx := range d.req {
			var tm directTiming
			if vsyncOK {
				t := time.Now()
				C.dd_fb_wait_vsync()
				tm.wait = time.Since(t)
			}
			t := time.Now()
			C.dd_present(stage[idx], C.int(outputW), C.int(outputH), pitch)
			tm.show = time.Since(t)
			if update != 0 {
				t = time.Now()
				C.dd_update()
				tm.update = time.Since(t)
			}
			d.done <- tm
		}
	}()

	if err := <-ready; err != nil {
		C.dd_fb_close()
		return nil, err
	}
	return d, nil
}

// Update stages changed pixels and presents them on the vendor thread.
// It waits only if the previous frame is still presenting.
func (d *DirectDisplayController) Update() error {
	if d.req == nil || len(d.buf) == 0 {
		return nil
	}

	// The driver owns a staging buffer until it completes the vendor call.
	// Compare first to overlap CPU work with the previous presentation.
	t := time.Now()
	dm := d.changed(d.check)
	compareTime := time.Since(t)
	if d.check && dm.empty() {
		d.lastDetail.Compare = compareTime
		d.lastDetail.PipelineWait = 0
		d.lastDetail.StageCopy = 0
		d.lastCopy = compareTime
		d.skipped++
		return nil
	}

	var pipelineWait time.Duration
	if d.pending {
		t = time.Now()
		tm := <-d.done
		pipelineWait = time.Since(t)
		d.lastWait, d.lastShow = tm.wait, tm.show+tm.update
		d.lastDetail.VSync = tm.wait
		d.lastDetail.DriverDisplay = tm.show
		d.lastDetail.DriverUpdate = tm.update
		d.pending = false
	}

	i := d.next
	d.next ^= 1
	d.pend[0] = d.pend[0].union(dm)
	d.pend[1] = d.pend[1].union(dm)
	rows := d.pend[i]
	if !d.stagePrimed[i] {
		rows = d.fullDamage()
	}

	t = time.Now()
	if d.presentation.native(d.w, d.h) {
		// One libc memcpy for a contiguous span; include complete rows so the
		// staging copy needs no per-row Go loop or multiple cgo transitions.
		if !rows.empty() {
			start := rows.r0 * d.w
			n := (rows.r1 - rows.r0 + 1) * d.w
			C.dd_copy(unsafe.Add(d.stage[i], uintptr(start*2)),
				unsafe.Pointer(&d.buf[start]), C.size_t(n*2))
			runtime.KeepAlive(d.buf)
		}
	} else {
		dst := unsafe.Slice((*uint16)(d.stage[i]), d.outputW*d.outputH)
		if _, err := d.presentation.compose(dst, d.buf, d.w, d.h, rows, !d.stagePrimed[i]); err != nil {
			return err
		}
	}
	stageTime := time.Since(t)
	d.stagePrimed[i] = true
	d.pend[i] = noDamage
	d.lastCopy = compareTime + stageTime
	d.lastDetail.Compare = compareTime
	d.lastDetail.PipelineWait = pipelineWait
	d.lastDetail.StageCopy = stageTime

	d.req <- i
	d.pending = true
	return nil
}

// Timings returns the latest Direct frame's individual costs.
// Vendor-call values describe the last completed presentation.
func (d *DirectDisplayController) Timings() DirectTimings {
	return d.lastDetail
}

// Stats returns the active configuration and the latest timings.
func (d *DirectDisplayController) Stats() Stats {
	return Stats{
		Backend:     "direct",
		VSyncActive: d.vsyncActive,
		Copy:        d.lastCopy,
		Wait:        d.lastWait,
		Show:        d.lastShow,
		Skipped:     d.skipped,
	}
}

// Close stops the driver thread and frees the buffers. Calling it twice is safe.
func (d *DirectDisplayController) Close() {
	if d.req == nil {
		return
	}
	if d.pending {
		<-d.done
		d.pending = false
	}
	close(d.req)
	<-d.quit // the driver thread frees the buffers; don't touch them after this
	C.dd_fb_close()
	d.req = nil
	d.buf = nil
}
