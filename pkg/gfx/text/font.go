package text

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"strings"
	"sync"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// AlphaMask is the coverage of a glyph, one byte per pixel.
type AlphaMask struct {
	// Width and Height are in pixels.
	Width  int
	Height int

	// Pixels is the coverage (0 to 255), row by row.
	Pixels []byte
}

// Bounds is where a glyph mask sits relative to the glyph origin.
type Bounds struct {
	// MinX is the offset of the left edge.
	MinX int

	// MinY is the offset of the top edge (negative is above the baseline).
	MinY int
}

// CachedGlyph is a rasterized glyph kept for reuse.
type CachedGlyph struct {
	// Mask is the glyph coverage; nil if the glyph has no pixels.
	Mask *AlphaMask

	// Bounds is the mask offset from the glyph origin.
	Bounds Bounds
}

// Font is a loaded font, or the built-in 8x8 bitmap font if loading failed.
// Sizes, metrics and measurements are in logical pixels.
type Font struct {
	face       *font.Face
	baseFont   *font.Font
	size       float64 // logical size, as requested by the caller
	scale      float64 // render pixels per logical pixel (0 or 1 = native)
	shaper     shaping.HarfbuzzShaper
	glyphCache map[font.GID]CachedGlyph
	cacheMutex sync.RWMutex
}

// parseScriptSafe parses a script tag, returning the zero script on error.
func parseScriptSafe(str string) language.Script {
	scr, err := language.ParseScript(str)
	if err != nil {
		return language.Script(0)
	}
	return scr
}

// parsedFonts keeps parsed font files, so loading the same file again (for
// another size) skips the file read and the parse.
var parsedFonts = struct {
	sync.Mutex
	m map[string]*font.Font
}{m: make(map[string]*font.Font)}

// ClearFontCache drops the parsed font files kept by LoadFont. Fonts already
// loaded keep working; call it to free memory after switching fonts.
func ClearFontCache() {
	parsedFonts.Lock()
	clear(parsedFonts.m)
	parsedFonts.Unlock()
}

// loadParsed returns the parsed font file, reading it only on first use.
func loadParsed(path string) (*font.Font, error) {
	parsedFonts.Lock()
	defer parsedFonts.Unlock()

	if f, ok := parsedFonts.m[path]; ok {
		return f, nil
	}

	// One sequential read; the parser then works from memory instead of
	// seeking around the file on a slow SD card.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	loader, err := opentype.NewLoader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	f, err := font.NewFont(loader)
	if err != nil {
		return nil, err
	}

	parsedFonts.m[path] = f
	return f, nil
}

// LoadFont loads a TTF or OTF file at the given logical size. If that fails it
// returns a Font that uses the bitmap font. The parsed file is kept, so loading
// the same path again at another size is cheap.
func LoadFont(path string, size float64) (*Font, error) {
	loadedFont, err := loadParsed(path)
	if err != nil {
		return &Font{face: nil, size: size, scale: 1}, err
	}

	return &Font{
		face:       font.NewFace(loadedFont),
		baseFont:   loadedFont,
		size:       size,
		scale:      1,
		shaper:     shaping.HarfbuzzShaper{},
		glyphCache: make(map[font.GID]CachedGlyph),
	}, nil
}

// Warmup string.
const ASCII = " !\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~"

// Warmup shapes str and rasterizes its glyphs now, so the first DrawText that
// uses them doesn't pay for it. Call SetScale first (a scale change clears the
// glyph cache) and don't call it while another goroutine draws with f.
func (f *Font) Warmup(str string) {
	runes := []rune(str)
	if f == nil || f.face == nil || len(runes) == 0 {
		return
	}

	output := f.shaper.Shape(shaping.Input{
		Text:      runes,
		RunStart:  0,
		RunEnd:    len(runes),
		Direction: di.DirectionLTR,
		Script:    parseScriptSafe("Latn"),
		Face:      f.face,
		Size:      fixedSize(f.renderSize()),
	})

	for _, glyph := range output.Glyphs {
		_, _, _ = f.getCachedGlyph(glyph.GlyphID)
	}
}

// effScale returns the render scale, treating unset as 1.
func (f *Font) effScale() float64 {
	if f == nil || f.scale <= 0 {
		return 1
	}
	return f.scale
}

// renderSize returns the font size in render pixels.
func (f *Font) renderSize() float64 {
	return f.size * f.effScale()
}

// bitmapScale returns the whole-pixel multiplier of the 8x8 bitmap font.
func (f *Font) bitmapScale() int {
	if f == nil || f.size <= 0 {
		return 1
	}
	return int(math.Max(1, math.Floor(f.renderSize()/8.0)))
}

// SetScale sets the render pixels per logical pixel (the display Scale).
// A change clears the glyph cache.
func (f *Font) SetScale(s float64) {
	if f == nil || s <= 0 {
		return
	}

	f.cacheMutex.Lock()
	defer f.cacheMutex.Unlock()

	if f.scale == s {
		return
	}

	f.scale = s
	f.glyphCache = make(map[font.GID]CachedGlyph)
}

// getCachedGlyph returns the glyph mask, rasterizing it on first use.
func (f *Font) getCachedGlyph(glyphID font.GID) (*AlphaMask, Bounds, error) {
	f.cacheMutex.RLock()
	cached, exists := f.glyphCache[glyphID]
	f.cacheMutex.RUnlock()

	if exists {
		return cached.Mask, cached.Bounds, nil
	}

	mask, bounds, err := rasterizeGlyph(f.face, glyphID, f.renderSize())

	f.cacheMutex.Lock()
	f.glyphCache[glyphID] = CachedGlyph{Mask: mask, Bounds: bounds}
	f.cacheMutex.Unlock()

	return mask, bounds, err
}

// rasterizeGlyph renders a glyph at size (render pixels) into an alpha mask.
// The outline is shifted by the fractional part of its position, so the mask
// holds the exact shape and Bounds is its whole-pixel offset from the glyph
// origin (the point on the baseline).
func rasterizeGlyph(face *font.Face, glyphID font.GID, size float64) (*AlphaMask, Bounds, error) {
	var b Bounds

	extents, ok := face.GlyphExtents(glyphID)
	if !ok {
		return nil, b, nil
	}

	scale := size / float64(face.Upem())

	// The glyph's box in pixels from its origin, with y pointing down.
	left := float64(extents.XBearing) * scale
	top := -float64(extents.YBearing) * scale
	right := left + float64(extents.Width)*scale
	bottom := top - float64(extents.Height)*scale // Height is negative

	b.MinX = int(math.Floor(left))
	b.MinY = int(math.Floor(top))
	width := int(math.Ceil(right)) - b.MinX
	height := int(math.Ceil(bottom)) - b.MinY

	if width <= 0 || height <= 0 {
		return nil, b, nil
	}

	outline, ok := face.GlyphDataOutline(glyphID)
	if !ok {
		return nil, b, nil
	}

	ras := vector.NewRasterizer(width, height)
	ras.DrawOp = draw.Over

	s := float32(scale)
	ox, oy := float32(b.MinX), float32(b.MinY)
	tx := func(x float32) float32 { return x*s - ox }
	ty := func(y float32) float32 { return -y*s - oy }

	for _, segment := range outline.Segments {
		switch segment.Op {
		case opentype.SegmentOpMoveTo:
			ras.MoveTo(tx(float32(segment.Args[0].X)), ty(float32(segment.Args[0].Y)))

		case opentype.SegmentOpLineTo:
			ras.LineTo(tx(float32(segment.Args[0].X)), ty(float32(segment.Args[0].Y)))

		case opentype.SegmentOpQuadTo:
			ras.QuadTo(
				tx(float32(segment.Args[0].X)), ty(float32(segment.Args[0].Y)),
				tx(float32(segment.Args[1].X)), ty(float32(segment.Args[1].Y)))

		case opentype.SegmentOpCubeTo:
			ras.CubeTo(
				tx(float32(segment.Args[0].X)), ty(float32(segment.Args[0].Y)),
				tx(float32(segment.Args[1].X)), ty(float32(segment.Args[1].Y)),
				tx(float32(segment.Args[2].X)), ty(float32(segment.Args[2].Y)))
		}
	}

	dst := image.NewAlpha(image.Rect(0, 0, width, height))
	ras.Draw(dst, image.Rect(0, 0, width, height), image.NewUniform(color.Alpha{A: 255}), image.Point{})

	return &AlphaMask{
		Width:  width,
		Height: height,
		Pixels: dst.Pix,
	}, b, nil
}

// fixedSize converts pixels to 26.6 fixed point, keeping fractions.
func fixedSize(px float64) fixed.Int26_6 {
	return fixed.Int26_6(math.Round(px * 64))
}

// MeasureText returns the size of str in logical pixels.
func (f *Font) MeasureText(str string) (width, height int) {
	if str == "" {
		return 0, 0
	}

	if f == nil || f.face == nil {
		bs := f.bitmapScale()
		k := f.effScale()

		lines := strings.Split(str, "\n")
		maxLen := 0
		for _, line := range lines {
			if len(line) > maxLen {
				maxLen = len(line)
			}
		}
		w := float64(maxLen*8*bs) / k
		h := float64(len(lines)*8*bs) / k
		return int(math.Round(w)), int(math.Round(h))
	}

	runes := []rune(str)

	dir := di.DirectionLTR
	scr := parseScriptSafe("Latn")

	for _, r := range runes {
		if r >= 0x0600 && r <= 0x06FF {
			dir = di.DirectionRTL
			scr = parseScriptSafe("Arab")
			break
		} else if r >= 0x0590 && r <= 0x05FF {
			dir = di.DirectionRTL
			scr = parseScriptSafe("Hebr")
			break
		}
	}

	input := shaping.Input{
		Text:      runes,
		RunStart:  0,
		RunEnd:    len(runes),
		Direction: dir,
		Script:    scr,
		Face:      f.face,
		Size:      fixedSize(f.renderSize()),
	}

	output := f.shaper.Shape(input)

	totalWidth := int(math.Round(float64(output.Advance) / 64 / f.effScale()))

	ascent, descent := f.GetMetrics()
	totalHeight := ascent + descent

	return totalWidth, totalHeight
}

// GetMetrics returns the ascent and descent in logical pixels.
func (f *Font) GetMetrics() (ascent, descent int) {
	if f == nil || f.face == nil {
		bs := f.bitmapScale()
		return int(math.Round(float64(8*bs) / f.effScale())), 0
	}

	asc := f.size * 0.80
	desc := f.size * 0.20

	return int(math.Round(asc)), int(math.Round(desc))
}

// SetSize sets the logical font size and clears the glyph cache.
func (f *Font) SetSize(size float64) {
	if size <= 0 || f == nil {
		return
	}

	f.cacheMutex.Lock()
	defer f.cacheMutex.Unlock()

	if f.size == size {
		return
	}

	f.size = size
	f.glyphCache = make(map[font.GID]CachedGlyph)
}
