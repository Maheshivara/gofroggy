package sprite

import (
	"math"

	"github.com/Maheshivara/gofroggy/pkg/gfx/utils"
	"github.com/Maheshivara/gofroggy/pkg/hardware/display"
)

// Flip mirrors a sprite when drawn.
type Flip uint8

const (
	// FlipNone draws the sprite as it is.
	FlipNone Flip = 0

	// FlipH mirrors it left to right.
	FlipH Flip = 1

	// FlipV mirrors it top to bottom.
	FlipV Flip = 2
)

// SpriteDrawer draws sprites on a display, in logical pixels.
type SpriteDrawer struct {
	dc   display.DisplayController
	cols []int // scratch for DrawScaled
}

// NewDrawer returns a drawer that draws on dc.
func NewSpriteDrawer(dc display.DisplayController) *SpriteDrawer {
	return &SpriteDrawer{dc: dc}
}

// Prepare resizes every frame of s for the display now, so the first Draw of
// each frame doesn't pay for it.
func (d *SpriteDrawer) Prepare(s *Sheet) {
	sx, sy := d.dc.Scale()
	for i := range s.frames {
		s.frame(i, sx, sy)
	}
}

// DrawAnimator draws the current frame of a with its pivot at (x, y).
func (d *SpriteDrawer) DrawAnimator(a *Animator, x, y int, flip Flip) {
	d.Draw(a.Sheet(), a.Frame(), x, y, flip)
}

// Draw draws frame of s with the sheet's pivot at (x, y), in logical pixels.
// Anything outside the screen is clipped.
func (d *SpriteDrawer) Draw(s *Sheet, frame, x, y int, flip Flip) {
	if s == nil || frame < 0 || frame >= len(s.frames) {
		return
	}

	sx, sy := d.dc.Scale()
	bm := s.frame(frame, sx, sy)

	dx := int(math.Round(float64(x-s.OriginX) * sx))
	dy := int(math.Round(float64(y-s.OriginY) * sy))

	screenW, screenH := d.dc.Width(), d.dc.Height()
	x0, x1 := max(dx, 0), min(dx+bm.W, screenW)
	y0, y1 := max(dy, 0), min(dy+bm.H, screenH)
	if x0 >= x1 || y0 >= y1 {
		return
	}

	buf := d.dc.Buffer()
	flipH, flipV := flip&FlipH != 0, flip&FlipV != 0

	for y := y0; y < y1; y++ {
		row := y - dy
		if flipV {
			row = bm.H - 1 - row
		}
		src := row * bm.Stride

		col, step := x0-dx, 1
		if flipH {
			col, step = bm.W-1-(x0-dx), -1
		}

		dst := buf[y*screenW+x0 : y*screenW+x1]

		// Opaque and not mirrored: whole rows at memmove speed.
		if bm.Alpha == nil && !flipH {
			copy(dst, bm.Pix[src+col:src+col+len(dst)])
			continue
		}

		for i := range dst {
			p := src + col
			col += step

			a := uint8(255)
			if bm.Alpha != nil {
				a = bm.Alpha[p]
			}

			switch a {
			case 0:
			case 255:
				dst[i] = bm.Pix[p]
			default:
				dst[i] = utils.BlendRGB565(bm.Pix[p], dst[i], a)
			}
		}
	}
}

// DrawAnimatorScaled draws the current frame of a like DrawScaled.
func (d *SpriteDrawer) DrawAnimatorScaled(a *Animator, x, y int, scale float64, flip Flip) {
	d.DrawScaled(a.Sheet(), a.Frame(), x, y, scale, flip)
}

// DrawScaled draws frame of s like Draw, enlarged or shrunk by scale (1 keeps
// the sheet's own size). The pivot stays at (x, y), so the sprite grows around
// it. Frames are resized as they are drawn, so any scale works without extra
// memory, but drawing costs more than Draw.
func (d *SpriteDrawer) DrawScaled(s *Sheet, frame, x, y int, scale float64, flip Flip) {
	d.DrawScaledXY(s, frame, x, y, scale, scale, flip)
}

// DrawScaledXY is DrawScaled with a separate scale for width and height.
func (d *SpriteDrawer) DrawScaledXY(s *Sheet, frame, x, y int, scaleX, scaleY float64, flip Flip) {
	if s == nil || frame < 0 || frame >= len(s.frames) || scaleX <= 0 || scaleY <= 0 {
		return
	}
	if scaleX == 1 && scaleY == 1 {
		d.Draw(s, frame, x, y, flip)
		return
	}

	sx, sy := d.dc.Scale()
	src := s.frames[frame]

	dw := max(1, int(math.Round(float64(s.fw)*scaleX*sx)))
	dh := max(1, int(math.Round(float64(s.fh)*scaleY*sy)))
	dx := int(math.Round((float64(x) - float64(s.OriginX)*scaleX) * sx))
	dy := int(math.Round((float64(y) - float64(s.OriginY)*scaleY) * sy))

	screenW, screenH := d.dc.Width(), d.dc.Height()
	x0, x1 := max(dx, 0), min(dx+dw, screenW)
	y0, y1 := max(dy, 0), min(dy+dh, screenH)
	if x0 >= x1 || y0 >= y1 {
		return
	}

	flipH, flipV := flip&FlipH != 0, flip&FlipV != 0

	if cap(d.cols) < x1-x0 {
		d.cols = make([]int, x1-x0)
	}
	cols := d.cols[:x1-x0]
	for i := range cols {
		c := (x0 + i - dx) * src.W / dw
		if flipH {
			c = src.W - 1 - c
		}
		cols[i] = c
	}

	buf := d.dc.Buffer()
	for y := y0; y < y1; y++ {
		r := (y - dy) * src.H / dh
		if flipV {
			r = src.H - 1 - r
		}
		base := r * src.Stride
		dst := buf[y*screenW+x0 : y*screenW+x1]

		for i, c := range cols {
			p := base + c

			a := uint8(255)
			if src.Alpha != nil {
				a = src.Alpha[p]
			}

			switch a {
			case 0:
			case 255:
				dst[i] = src.Pix[p]
			default:
				dst[i] = utils.BlendRGB565(src.Pix[p], dst[i], a)
			}
		}
	}
}
