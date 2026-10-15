package primitives

// RoundedRect is an axis-aligned rectangle with rounded corners, in logical
// pixels.
type RoundedRect struct {
	// Origin is the top-left corner.
	Origin Point

	// Width and Height are in logical pixels.
	Width, Height int

	// Radius is the corner radius in logical pixels. It is clamped to half of
	// the shorter side, so a large value gives a pill or a circle.
	Radius int
}

// DrawRoundedRect draws a rect with rounded borders; nothing if its Width or Height is 0 or less.
func (d *PrimitivesDrawer) DrawRoundedRect(rect RoundedRect, config DrawConfig) {
	if rect.Width <= 0 || rect.Height <= 0 {
		return
	}

	sx, sy := d.scale()
	k := avgScale(sx, sy)

	// Scale both edges so neighboring shapes still touch without gaps.
	x0 := scaleCoord(rect.Origin.X, sx)
	y0 := scaleCoord(rect.Origin.Y, sy)
	x1 := scaleCoord(rect.Origin.X+rect.Width, sx)
	y1 := scaleCoord(rect.Origin.Y+rect.Height, sy)
	if x1 <= x0 {
		x1 = x0 + 1
	}
	if y1 <= y0 {
		y1 = y0 + 1
	}

	config.BorderWidth = scaleLen(config.BorderWidth, k)

	d.drawRoundedRect(RoundedRect{
		Origin: Point{X: x0, Y: y0},
		Width:  x1 - x0,
		Height: y1 - y0,
		Radius: scaleLen(rect.Radius, k),
	}, config)
}

// drawRoundedRect draws in render pixels, one span per row.
func (d *PrimitivesDrawer) drawRoundedRect(rect RoundedRect, config DrawConfig) {
	x, y := rect.Origin.X, rect.Origin.Y
	w, h := rect.Width, rect.Height
	if w <= 0 || h <= 0 {
		return
	}

	r := min(max(rect.Radius, 0), min(w, h)/2)
	bw := max(config.BorderWidth, 0)

	// The inner shape is the outer one inset by the border width. With no
	// border it is identical to the outer shape, which is what the fill uses.
	ix, iy := x+bw, y+bw
	iw, ih := w-2*bw, h-2*bw
	ir := max(r-bw, 0)
	hasInner := iw > 0 && ih > 0

	if config.IsFilled && hasInner {
		for py := iy; py < iy+ih; py++ {
			x0, x1 := roundRectRow(py, ix, iy, iw, ih, ir)
			d.span(x0, x1, py, config.FillColor)
		}
	}

	if bw > 0 {
		for py := y; py < y+h; py++ {
			ox0, ox1 := roundRectRow(py, x, y, w, h, r)
			if !hasInner || py < iy || py >= iy+ih {
				d.span(ox0, ox1, py, config.Color)
				continue
			}
			ix0, ix1 := roundRectRow(py, ix, iy, iw, ih, ir)
			d.span(ox0, ix0-1, py, config.Color)
			d.span(ix1+1, ox1, py, config.Color)
		}
	}
}

// roundRectRow returns the first and last pixel of row py inside the w by h
// rounded rectangle at (x, y) with corner radius r; py must be inside it.
func roundRectRow(py, x, y, w, h, r int) (x0, x1 int) {
	dy := 0
	if top := y + r; py < top {
		dy = top - py
	} else if bottom := y + h - 1 - r; py > bottom {
		dy = py - bottom
	}

	ext := isqrt(int64(r*r - dy*dy))
	return x + r - ext, x + w - 1 - r + ext
}
