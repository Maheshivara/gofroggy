// Package display shows RGB565 frames on the SF3000 screen via the vendor driver.
// Draw in logical pixels (the panel size); the frame buffer may be smaller.
package display

import (
	"errors"
	"fmt"
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

	// LogicalSize returns the configured size when Width and Height are set,
	// or the panel size otherwise; lay out your screen in it.
	LogicalSize() (w, h int)

	// Scale returns render pixels per logical pixel (1, 1 when the
	// configured Width and Height are used).
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

// Stats holds active display settings and recent frame timings.
type Stats struct {
	// Backend identifies "direct", "bridge", "fb", or "ge".
	Backend string

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

	// BackendGE uses pure-Go SF3000 graphics-engine hardware.
	BackendGE
)

// String returns the backend name.
func (b Backend) String() string {
	switch b {
	case BackendBridge:
		return "bridge"
	case BackendDirect:
		return "direct"
	case BackendFB:
		return "fb"
	case BackendGE:
		return "ge"
	default:
		return "auto"
	}
}

// ParseBackend parses the backend name ("cgo" also selects direct).
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
	case "ge":
		return BackendGE, nil
	}
	return BackendAuto, fmt.Errorf("unknown display backend %q (want auto, bridge, direct, fb or ge)", s)
}

// ScalingMode chooses how render pixels occupy the physical display.
type ScalingMode uint8

const (
	// ScalingCenter places pixels at native size with colored surrounding bars.
	ScalingCenter ScalingMode = iota
	// ScalingStretch resamples the image to cover the entire display.
	ScalingStretch

	// ScalingFit maximizes the image while preserving its aspect ratio.
	// Remaining space uses BackgroundColor.
	ScalingFit
)

// String returns the scaling mode's name.
func (m ScalingMode) String() string {
	switch m {
	case ScalingCenter:
		return "center"
	case ScalingStretch:
		return "stretch"
	case ScalingFit:
		return "fit"
	default:
		return fmt.Sprintf("unknown(%d)", m)
	}
}

// ParseScalingMode parses "center", "stretch" and "fit".
// "contain" is an alias for "fit".
func ParseScalingMode(s string) (ScalingMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "center", "fill":
		return ScalingCenter, nil
	case "stretch":
		return ScalingStretch, nil
	case "fit", "contain", "letterbox":
		return ScalingFit, nil
	default:
		return 0, fmt.Errorf("unknown scaling mode %q (want center, fit or stretch)", s)
	}
}

// Config controls how the display is driven. The zero value is valid.
type Config struct {
	// Width and Height set the RGB565 drawing resolution; zero uses panel size.
	Width, Height int

	// Scaling selects native centering, aspect-preserving fit or stretching.
	Scaling ScalingMode

	// BackgroundColor colors Center/Fit bars; zero means black.
	BackgroundColor uint16

	// VSync requests hardware vertical synchronization when supported.
	VSync bool

	// DriverPath overrides the vendor driver library location.
	DriverPath string

	// BridgePath overrides the driver_bridge helper executable location.
	BridgePath string

	// UpdateScreen calls the optional vendor update-screen function.
	UpdateScreen bool

	// Backend selects the implementation (zero uses BackendAuto).
	Backend Backend

	// TrackDrawDamage tracks helper writes; Clear and raw Buffer writes
	// use final-frame comparison so unchanged pixels are not resent.
	TrackDrawDamage bool

	// DisableFrameCheck presents frames even when pixels have not changed.
	DisableFrameCheck bool

	// Rotation overrides FB rotation; Direct and Bridge use the vendor driver.
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

	// RotationC0 forces no software rotation (unlike RotationNone, which detects).
	RotationC0
)

// String returns the requested clockwise rotation or "none" for detection.
func (r Rotation) String() string {
	switch r {
	case RotationC0:
		return "c0"
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

// ParseRotation accepts automatic detection or an explicit clockwise rotation.
func ParseRotation(s string) (Rotation, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "none", "auto":
		return RotationNone, nil
	case "c0", "0":
		return RotationC0, nil
	case "c90", "90":
		return RotationC90, nil
	case "c180", "180":
		return RotationC180, nil
	case "c270", "270":
		return RotationC270, nil
	}
	return RotationNone, fmt.Errorf("unknown rotation %q (want none, c0, c90, c180 or c270)", s)
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
	case BackendGE:
		backend = BackendGE
	case BackendFB:
		backend = BackendFB
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

	if backend == BackendGE {
		return NewGEDisplayController(cfg)
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

// resolveSize validates the app's drawing resolution for the device.
func resolveSize(cfg Config, panelW, panelH int) (w, h int, err error) {
	if cfg.Scaling != ScalingCenter && cfg.Scaling != ScalingStretch && cfg.Scaling != ScalingFit {
		return 0, 0, fmt.Errorf("invalid scaling mode %d", cfg.Scaling)
	}
	if (cfg.Width == 0) != (cfg.Height == 0) {
		return 0, 0, fmt.Errorf("Width and Height must both be set or both zero")
	}
	if cfg.Width == 0 && cfg.Height == 0 {
		w, h = panelW, panelH
	} else {
		w, h = cfg.Width, cfg.Height
	}
	if w < 2 || h < 2 || w > 4096 || h > 4096 || int64(w)*int64(h) > 4096*2048 {
		return 0, 0, fmt.Errorf("invalid render size %dx%d (minimum 2x2; maximum 4096 per dimension and 8M pixels)", w, h)
	}
	if w%2 != 0 || h%2 != 0 {
		return 0, 0, fmt.Errorf("render dimensions must be even: got %dx%d", w, h)
	}
	if cfg.Scaling == ScalingCenter && (w > panelW || h > panelH) {
		return 0, 0, fmt.Errorf("centered %dx%d image exceeds the %dx%d panel; use ScalingFit or ScalingStretch", w, h, panelW, panelH)
	}
	return w, h, nil
}

// resolveLogicalSize returns the explicit render dimensions as the logical
// dimensions too. With no explicit size, layout uses the physical panel size.
func resolveLogicalSize(cfg Config, renderW, renderH, panelW, panelH int) (int, int) {
	if cfg.Width > 0 && cfg.Height > 0 {
		return renderW, renderH
	}
	return panelW, panelH
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
