// Package display shows RGB565 frames on the SF3000 screen via the vendor driver.
// Draw in logical pixels (the panel size); the frame buffer may be smaller.
package display

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const _BLACK_COLOR uint16 = 0x0000

// DisplayController is the screen you draw on. Not safe for concurrent use.
type DisplayController interface {
	// Width returns the render width in pixels.
	Width() int

	// Height returns the render height in pixels.
	Height() int

	// LogicalSize returns the panel size; lay out your screen in it.
	LogicalSize() (w, h int)

	// Scale returns render pixels per logical pixel (1, 1 at native size).
	Scale() (sx, sy float64)

	// Buffer returns the frame buffer (Width*Height RGB565 pixels, row by
	// row). The slice stays the same until Close.
	Buffer() []uint16

	// Update shows the frame. It returns once the driver has it, not when it
	// is on screen.
	Update() error

	// Clear fills the frame buffer with color c.
	Clear(c uint16)

	// SetPixel sets a pixel in render pixels; out of range is ignored.
	SetPixel(x, y int, c uint16)

	// GetPixel returns a pixel in render pixels; out of range gives black.
	GetPixel(x, y int) uint16

	// HLine fills the pixels x0..x1 (inclusive) of row y, clipped to the frame
	HLine(x0, x1, y int, color uint16)

	// SaveScreenshot writes the frame buffer to outputDir as the next NNNN.png.
	SaveScreenshot(outputDir string) error

	// SaveScreenshotAsync is SaveScreenshot in the background; errors are
	// ignored.
	SaveScreenshotAsync(outputDir string)

	// Stats returns the active configuration and the latest timings.
	Stats() Stats

	// Close releases the driver. Calling it twice is safe.
	Close()
}

// Stats holds the active configuration and the latest timings.
type Stats struct {
	// Backend is "direct", "bridge" or "fb".
	Backend string

	// Downscale is panel width divided by render width.
	Downscale float64

	// VSyncActive reports whether the vsync wait works.
	VSyncActive bool

	// Copy is the time to copy the latest frame to the driver.
	Copy time.Duration

	// Wait is the vsync wait of the previous frame.
	Wait time.Duration

	// Show is the driver call time of the previous frame.
	Show time.Duration

	// Skipped counts the frames not shown because they were identical to the
	// previous one (see Config.DisableFrameCheck).
	Skipped uint64
}

// Backend selects the implementation behind NewDisplayController.
type Backend int

const (
	// BackendAuto picks Direct in cgo builds and FB otherwise.
	BackendAuto Backend = iota

	// BackendBridge uses the driver_bridge helper process (no cgo).
	BackendBridge

	// BackendDirect loads the driver in-process (needs cgo).
	BackendDirect

	// BackendFB writes straight to /dev/fb0 and bypasses the vendor driver.
	BackendFB
)

// String returns "auto", "bridge", "direct" or "fb".
func (b Backend) String() string {
	switch b {
	case BackendBridge:
		return "bridge"
	case BackendDirect:
		return "direct"
	case BackendFB:
		return "fb"
	default:
		return "auto"
	}
}

// ParseBackend parses "auto", "bridge", "direct" ("cgo" also means direct) or "fb".
func ParseBackend(s string) (Backend, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return BackendAuto, nil
	case "bridge":
		return BackendBridge, nil
	case "direct", "cgo":
		return BackendDirect, nil
	case "fb":
		return BackendFB, nil
	}
	return BackendAuto, fmt.Errorf("unknown display backend %q (want auto, bridge, direct or fb)", s)
}

// Config controls how the display is driven. The zero value is valid.
type Config struct {
	// Width and Height set the render size and override Downscale. Rounded
	// down to even numbers, at most 1280x720.
	Width, Height int

	// Downscale divides the panel size (fractional is fine, e.g. 1.9).
	// 0 or 1 is native size.
	Downscale float64

	// VSync waits for the vertical blank before each frame: no tearing, but
	// about 30 fps.
	VSync bool

	// DriverPath is the driver library to load. Empty searches ./driver_sf3000.so,
	// then /mnt/sdcard/cubegm/driver_sf3000.so.
	DriverPath string

	// BridgePath is the driver bridge path. Empty searches ./driver_bridge,
	// then /mnt/sdcard/cubegm/driver_bridge.
	BridgePath string

	// UpdateScreen also calls the driver's update_screen: about 20 fps, so
	// it is off by default.
	UpdateScreen bool

	// Backend picks the implementation (default BackendAuto).
	Backend Backend

	// DisableFrameCheck turns off the comparison with the previous frame.
	// By default a frame identical to the last one is not shown again, and
	// only what changed is copied.
	DisableFrameCheck bool

	// Rotation forces the render to be rotated clockwise by this angle on its
	// way to the screen, instead of the rotation the device reports. Only
	// the fb backend uses it; the others leave rotation to the vendor
	// driver. RotationNone (the default) keeps the device's own rotation.
	Rotation Rotation
}

// Rotation is a clockwise rotation of the render.
type Rotation int

const (
	// RotationNone keeps the rotation the device reports.
	RotationNone Rotation = iota

	// RotationC90 rotates 90 degrees clockwise.
	RotationC90

	// RotationC180 rotates 180 degrees.
	RotationC180

	// RotationC270 rotates 270 degrees clockwise.
	RotationC270
)

// String returns "none", "c90", "c180" or "c270".
func (r Rotation) String() string {
	switch r {
	case RotationC90:
		return "c90"
	case RotationC180:
		return "c180"
	case RotationC270:
		return "c270"
	default:
		return "none"
	}
}

// Degrees returns the clockwise angle: 0, 90, 180 or 270.
func (r Rotation) Degrees() int {
	switch r {
	case RotationC90:
		return 90
	case RotationC180:
		return 180
	case RotationC270:
		return 270
	default:
		return 0
	}
}

// ParseRotation parses "none", "c90", "c180" or "c270" (also "90", "180", "270").
func ParseRotation(s string) (Rotation, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "none":
		return RotationNone, nil
	case "c90", "90":
		return RotationC90, nil
	case "c180", "180":
		return RotationC180, nil
	case "c270", "270":
		return RotationC270, nil
	}
	return RotationNone, fmt.Errorf("unknown rotation %q (want none, c90, c180 or c270)", s)
}

// ErrCGODisabled means BackendDirect was requested in a build without cgo.
var ErrCGODisabled = errors.New("the direct display backend needs cgo (build with CGO_ENABLED=1)")

// directCtor is set by direct.go; nil without cgo.
var directCtor func(cfg Config) (DisplayController, error)

// NewDisplayController starts the backend chosen in cfg. Call Close when done.
func NewDisplayController(cfg Config) (DisplayController, error) {
	backend := BackendFB
	switch cfg.Backend {
	case BackendDirect:
		backend = BackendDirect
	case BackendBridge:
		backend = BackendBridge
	default:
		if directCtor != nil {
			backend = BackendDirect
		}
	}

	if backend == BackendDirect {
		if directCtor == nil {
			return nil, ErrCGODisabled
		}
		return directCtor(cfg)
	}

	if backend == BackendBridge {
		d, err := NewBridgeDisplayController(cfg)
		if err != nil {
			return nil, err
		}
		return d, nil
	}
	return NewFBDisplayController(cfg)
}

// resolveSize returns the even render size for cfg (Width x Height, or the
// panel size divided by Downscale).
func resolveSize(cfg Config, panelW, panelH int) (w, h int, err error) {
	if cfg.Width > 0 && cfg.Height > 0 {
		w, h = cfg.Width, cfg.Height
	} else {
		f := cfg.Downscale
		if f == 0 {
			f = 1
		}
		if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
			return 0, 0, fmt.Errorf("invalid downscale factor %v", cfg.Downscale)
		}
		fw := math.Floor(float64(panelW) / f)
		fh := math.Floor(float64(panelH) / f)
		w, h = int(fw), int(fh)
	}

	// Even dimensions only: odd source widths wedge some driver builds.
	w &^= 1
	h &^= 1

	if w < 2 || h < 2 {
		return 0, 0, fmt.Errorf("render size %dx%d is too small", w, h)
	}
	if w > 1280 || h > 720 {
		return 0, 0, fmt.Errorf("render size %dx%d is larger than 1280x720", w, h)
	}

	return w, h, nil
}

// readPanelSize returns the panel resolution reported by /dev/fb0 before the
// driver is started (854x480 if it cannot be read).
func readPanelSize() (w, h int) {
	w, h = 854, 480
	fd, err := syscall.Open("/dev/fb0", syscall.O_RDONLY, 0)
	if err != nil {
		return w, h
	}

	defer func() { _ = syscall.Close(fd) }()

	// struct fb_var_screeninfo is 160 bytes; xres and yres are its first two
	// uint32 fields. FBIOGET_VSCREENINFO = 0x4600.
	var vinfo [40]uint32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), 0x4600,
		uintptr(unsafe.Pointer(&vinfo[0])))
	if errno == 0 && vinfo[0] > 0 && vinfo[1] > 0 && vinfo[0] <= 4096 && vinfo[1] <= 4096 {
		w, h = int(vinfo[0]), int(vinfo[1])
	}
	return w, h
}
