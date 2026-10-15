package text

import (
	"math"

	"github.com/Maheshivara/gofroggy/pkg/gfx/utils"
	"github.com/Maheshivara/gofroggy/pkg/hardware/display"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

// TextDrawer draws text on a display, in logical pixels.
type TextDrawer struct {
	displayController display.DisplayController
}

// NewTextDrawer returns a drawer that draws on d.
func NewTextDrawer(d display.DisplayController) *TextDrawer {
	return &TextDrawer{displayController: d}
}

// DrawText draws str with its baseline at (startX, startY), in logical pixels.
// A nil font, or one that failed to load, uses the 8x8 bitmap font.
func (r *TextDrawer) DrawText(f *Font, str string, startX, startY int, color uint16) {
	sx, sy := r.displayController.Scale()
	f.SetScale(sy)

	devX := int(math.Round(float64(startX) * sx))
	devY := int(math.Round(float64(startY) * sy))

	if f == nil || f.face == nil {
		// Built-in 8x8 bitmap font: the baseline is 8*scale pixels below the top.
		bs := f.bitmapScale()
		r.drawBitmapText(str, devX, devY-8*bs, color, bs)
		return
	}

	runes := []rune(str)
	if len(runes) == 0 {
		return
	}

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

	penX := fixed.I(devX)
	penY := fixed.I(devY)

	for _, glyph := range output.Glyphs {
		mask, bounds, err := f.getCachedGlyph(glyph.GlyphID)
		if err == nil && mask != nil {

			gx := (penX + glyph.XOffset).Round() + bounds.MinX
			gy := (penY - glyph.YOffset).Round() + bounds.MinY

			for y := 0; y < mask.Height; y++ {
				for x := 0; x < mask.Width; x++ {
					alpha := mask.Pixels[y*mask.Width+x]
					if alpha == 0 {
						continue
					}

					pixelX := gx + x
					pixelY := gy + y

					bg := r.displayController.GetPixel(pixelX, pixelY)
					blended := utils.BlendRGB565(color, bg, alpha)
					r.displayController.SetPixel(pixelX, pixelY, blended)
				}
			}
		}

		penX += glyph.Advance
	}
}

// drawBitmapText draws text with the 8x8 bitmap font, in render pixels.
func (r *TextDrawer) drawBitmapText(text string, startX, startY int, color uint16, scale int) {
	if scale < 1 {
		scale = 1
	}

	curX := startX
	curY := startY

	for _, char := range text {
		if char == '\n' {
			curX = startX
			curY += 8 * scale
			continue
		}

		glyph, exists := font8x8[char]
		if !exists {
			glyph = font8x8['?']
		}

		for row := 0; row < 8; row++ {
			line := glyph[row]
			for col := 0; col < 8; col++ {
				if (line & (0x80 >> col)) != 0 {
					for sy := 0; sy < scale; sy++ {
						for sx := 0; sx < scale; sx++ {
							r.displayController.SetPixel(curX+(col*scale)+sx, curY+(row*scale)+sy, color)
						}
					}
				}
			}
		}

		curX += 8 * scale
	}
}
