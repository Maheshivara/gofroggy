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

	downscale        float64 // effective factor: panel width / render width
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
}

// directTiming is the timing of one presented frame.
type directTiming struct {
	wait, show time.Duration
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
		downscale:    float64(panelW) / float64(baseFrame.w),
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

		var stage [2]unsafe.Pointer
		for i := range stage {
			p := C.dd_alloc(C.size_t(frameSize))
			if p == nil {
				for j := 0; j < i; j++ {
					C.dd_free(stage[j])
				}
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
			C.dd_show(stage[idx], C.int(outputW), C.int(outputH), pitch, update)
			tm.show = time.Since(t)
			d.done <- tm
		}

		C.dd_free(stage[0])
		C.dd_free(stage[1])
		C.dd_unload()
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

	// Compare before waiting for the preceding frame.
	t := time.Now()
	dm := d.changed(d.check)
	if dm.empty() {
		d.lastCopy = time.Since(t)
		d.skipped++
		return nil
	}
	compareTime := time.Since(t)
	if d.pending {
		tm := <-d.done
		d.lastWait, d.lastShow = tm.wait, tm.show
		d.pending = false
	}
	i := d.next
	d.next ^= 1
	d.pend[0] = d.pend[0].union(dm)
	d.pend[1] = d.pend[1].union(dm)
	rows := d.pend[i]
	d.pend[i] = noDamage
	t = time.Now()
	dst := unsafe.Slice((*uint16)(d.stage[i]), d.outputW*d.outputH)
	if _, err := d.presentation.compose(dst, d.buf, d.w, d.h, rows, !d.stagePrimed[i]); err != nil {
		return err
	}
	d.stagePrimed[i] = true
	d.lastCopy = compareTime + time.Since(t)

	d.req <- i
	d.pending = true
	return nil
}

// Stats returns the active configuration and the latest timings.
func (d *DirectDisplayController) Stats() Stats {
	return Stats{
		Backend:     "direct",
		Downscale:   d.downscale,
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
