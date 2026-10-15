package display

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"unsafe"
)

// frame is the frame buffer and drawing code shared by both backends.
type frame struct {
	w, h   int // render size
	lw, lh int // logical size (the panel size)
	buf    []uint16

	prev   []uint16 // the frame as last compared (see framecheck.go)
	primed bool     // prev holds a frame
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
	fillPixels(f.buf, c)
}

// fillPixels sets every element of dst to c. It fills a few pixels, then
// doubles the filled part with copy, so long fills run at memmove speed.
func fillPixels(dst []uint16, c uint16) {
	n := min(len(dst), 16)
	for i := 0; i < n; i++ {
		dst[i] = c
	}
	for k := n; k < len(dst); k *= 2 {
		copy(dst[k:], dst[:k])
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

// HLine fills the pixels x0..x1 (inclusive) of row y, clipped to the frame
func (f *frame) HLine(x0, x1, y int, color uint16) {
	if y < 0 || y >= f.h {
		return
	}
	x0, x1 = max(x0, 0), min(x1, f.w-1)
	if x0 > x1 {
		return
	}
	fillPixels(f.buf[y*f.w+x0:y*f.w+x1+1], color)
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
func saveBufferToFile(buf []uint16, width, height int, outputDir string) (err error) {
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
	var filePath string
	for i := pngCount + 1; ; i++ {
		filePath = filepath.Join(outputDir, fmt.Sprintf("%04d.png", i))

		_, err := os.Stat(filePath)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return err
		}
	}

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
		if cerr := file.Close(); err == nil {
			err = cerr
		}
	}()

	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(file, img); err != nil {
		return fmt.Errorf("failed to encode png: %w", err)
	}
	return nil
}

// damage is a bounding box of render-buffer pixels, inclusive. It is empty
// when r1 < r0.
type damage struct{ r0, r1, c0, c1 int }

var noDamage = damage{r0: 0, r1: -1}

func (a damage) empty() bool { return a.r1 < a.r0 }

func (a damage) union(b damage) damage {
	if a.empty() {
		return b
	}
	if b.empty() {
		return a
	}
	return damage{min(a.r0, b.r0), max(a.r1, b.r1), min(a.c0, b.c0), max(a.c1, b.c1)}
}

// fullDamage covers the whole frame.
func (f *frame) fullDamage() damage {
	return damage{0, f.h - 1, 0, f.w - 1}
}

// diff compares buf with the previous frame, refreshes prev for what changed
// and returns the bounding box of the changes (empty if there are none). The
// first call counts as a full change, so the first frame is always shown.
func (f *frame) diff() damage {
	w, h := f.w, f.h

	if len(f.prev) != len(f.buf) {
		f.prev = make([]uint16, len(f.buf))
		f.primed = false
	}
	if !f.primed || w&1 != 0 || uintptr(unsafe.Pointer(&f.buf[0]))&3 != 0 {
		copy(f.prev, f.buf)
		f.primed = true
		return f.fullDamage()
	}

	n := w / 2
	a32 := unsafe.Slice((*uint32)(unsafe.Pointer(&f.buf[0])), len(f.buf)/2)
	b32 := unsafe.Slice((*uint32)(unsafe.Pointer(&f.prev[0])), len(f.prev)/2)

	r0, r1, c0, c1 := 0, -1, n, -1
	for r := 0; r < h; r++ {
		a := a32[r*n : (r+1)*n]
		b := b32[r*n : (r+1)*n]

		i := 0
		for i < n && a[i] == b[i] {
			i++
		}
		if i == n {
			continue
		}
		j := n - 1
		for a[j] == b[j] {
			j--
		}
		copy(b[i:j+1], a[i:j+1])

		if r1 < 0 {
			r0 = r
		}
		r1 = r
		c0 = min(c0, i)
		c1 = max(c1, j)
	}

	if r1 < 0 {
		return noDamage
	}
	return damage{r0, r1, 2 * c0, 2*c1 + 1}
}
