// Package display shows RGB565 frames on the SF3000 screen via the vendor driver.
// Draw in logical pixels (the panel size); the frame buffer may be smaller.
package display

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
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
	// Backend is "direct" or "bridge".
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
}

// Backend selects the implementation behind NewDisplayController.
type Backend int

const (
	// BackendAuto picks Direct in cgo builds and Bridge otherwise.
	BackendAuto Backend = iota

	// BackendBridge uses the driver_bridge helper process (no cgo).
	BackendBridge

	// BackendDirect loads the driver in-process (needs cgo).
	BackendDirect
)

// String returns "auto", "bridge" or "direct".
func (b Backend) String() string {
	switch b {
	case BackendBridge:
		return "bridge"
	case BackendDirect:
		return "direct"
	default:
		return "auto"
	}
}

// ParseBackend parses "auto", "bridge" or "direct" ("cgo" also means direct).
func ParseBackend(s string) (Backend, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return BackendAuto, nil
	case "bridge":
		return BackendBridge, nil
	case "direct", "cgo":
		return BackendDirect, nil
	}
	return BackendAuto, fmt.Errorf("unknown display backend %q (want auto, bridge or direct)", s)
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
}

// ErrCGODisabled means BackendDirect was requested in a build without cgo.
var ErrCGODisabled = errors.New("the direct display backend needs cgo (build with CGO_ENABLED=1)")

// directCtor is set by direct_cgo.go; nil without cgo.
var directCtor func(cfg Config) (DisplayController, error)

// NewDisplayController starts the backend chosen in cfg. Call Close when done.
func NewDisplayController(cfg Config) (DisplayController, error) {
	useDirect := false
	switch cfg.Backend {
	case BackendDirect:
		useDirect = true
	case BackendBridge:
		useDirect = false
	default:
		useDirect = directCtor != nil
	}

	if useDirect {
		if directCtor == nil {
			return nil, ErrCGODisabled
		}
		return directCtor(cfg)
	}
	d, err := NewBridgeDisplayController(cfg)
	if err != nil {
		return nil, err
	}
	return d, nil
}

// Largest render size the driver accepts.
const (
	maxRenderWidth  = 1280
	maxRenderHeight = 720
)

// resolveSize returns the even render size for cfg (Width x Height, or the
// panel size divided by Downscale), checked against the driver's range.
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
		if !(fw >= 2 && fw <= maxRenderWidth && fh >= 2 && fh <= maxRenderHeight) {
			return 0, 0, fmt.Errorf("downscale %v gives an unsupported size for a %dx%d panel (driver range 2x2 to %dx%d)",
				cfg.Downscale, panelW, panelH, maxRenderWidth, maxRenderHeight)
		}
		w, h = int(fw), int(fh)
	}

	// Even dimensions only: odd source widths wedge some driver builds.
	w &^= 1
	h &^= 1
	if w < 2 || h < 2 || w > maxRenderWidth || h > maxRenderHeight {
		return 0, 0, fmt.Errorf("unsupported render size %dx%d (driver range 2x2 to %dx%d)",
			w, h, maxRenderWidth, maxRenderHeight)
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

	defer func() {
		err := syscall.Close(fd)
		if err != nil {
			panic(err)
		}
	}()

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

// frame is the frame buffer and drawing code shared by both backends.
type frame struct {
	w, h   int // render size
	lw, lh int // logical size (the panel size)
	buf    []uint16
}

// LogicalSize returns the panel size.
func (f *frame) LogicalSize() (int, int) {
	if f.lw <= 0 || f.lh <= 0 {
		return f.w, f.h
	}
	return f.lw, f.lh
}

// Scale returns render pixels per logical pixel.
func (f *frame) Scale() (sx, sy float64) {
	if f.lw <= 0 || f.lh <= 0 {
		return 1, 1
	}
	return float64(f.w) / float64(f.lw), float64(f.h) / float64(f.lh)
}

// Width returns the render width.
func (f *frame) Width() int { return f.w }

// Height returns the render height.
func (f *frame) Height() int { return f.h }

// Buffer returns the frame buffer.
func (f *frame) Buffer() []uint16 { return f.buf }

// Set buffer directly
func (f *frame) SetBuffer(b []uint16) { f.buf = b }

// Clear fills the frame buffer with c.
func (f *frame) Clear(c uint16) {
	if c == 0 {
		clear(f.buf)
		return
	}
	if len(f.buf) == 0 {
		return
	}
	row := f.buf[:f.w]
	for i := range row {
		row[i] = c
	}
	for y := 1; y < f.h; y++ {
		copy(f.buf[y*f.w:(y+1)*f.w], row)
	}
}

// SetPixel sets a pixel in render pixels.
func (f *frame) SetPixel(x, y int, c uint16) {
	if x < 0 || x >= f.w || y < 0 || y >= f.h {
		return
	}
	f.buf[y*f.w+x] = c
}

// GetPixel returns a pixel in render pixels.
func (f *frame) GetPixel(x, y int) uint16 {
	if x < 0 || x >= f.w || y < 0 || y >= f.h {
		return _BLACK_COLOR
	}
	return f.buf[y*f.w+x]
}

// SaveScreenshot writes the frame buffer as a PNG.
func (f *frame) SaveScreenshot(outputDir string) error {
	bufCopy := make([]uint16, len(f.buf))
	copy(bufCopy, f.buf)
	return saveBufferToFile(bufCopy, f.w, f.h, outputDir)
}

// SaveScreenshotAsync writes the PNG in the background.
func (f *frame) SaveScreenshotAsync(outputDir string) {
	bufCopy := make([]uint16, len(f.buf))
	copy(bufCopy, f.buf)
	w, h := f.w, f.h

	go func() {
		_ = saveBufferToFile(bufCopy, w, h, outputDir)
	}()
}

// saveBufferToFile writes buf as the next NNNN.png in outputDir.
func saveBufferToFile(buf []uint16, width, height int, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return fmt.Errorf("failed to read output directory: %w", err)
	}

	pngCount := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".png") {
			pngCount++
		}
	}

	filePath := filepath.Join(outputDir, fmt.Sprintf("%04d.png", pngCount+1))

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			p := buf[y*width+x]

			r := uint8(((p >> 11) & 0x1F) * 255 / 31)
			g := uint8(((p >> 5) & 0x3F) * 255 / 63)
			b := uint8((p & 0x1F) * 255 / 31)

			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	defer func() {
		err = file.Close()
	}()

	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(file, img); err != nil {
		return fmt.Errorf("failed to encode png: %w", err)
	}
	return err
}
