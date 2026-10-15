package primitives

// Rectangle is a box in logical pixels.
type Rectangle struct {
	// Origin is the top-left corner.
	Origin Point

	// Width and Height are the size; 0 or less draws nothing.
	Width  int
	Height int
}

// DrawRectangle draws rect; nothing if its Width or Height is 0 or less.
func (d *PrimitivesDrawer) DrawRectangle(rect Rectangle, config DrawConfig) {
	if rect.Width <= 0 || rect.Height <= 0 {
		return
	}

	sx, sy := d.scale()

	// Scale both edges so neighboring rectangles still touch without gaps.
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

	config.BorderWidth = scaleLen(config.BorderWidth, avgScale(sx, sy))

	d.drawRectangle(Rectangle{
		Origin: Point{X: x0, Y: y0},
		Width:  x1 - x0,
		Height: y1 - y0,
	}, config)
}

// drawRectangle draws in render pixels.
func (d *PrimitivesDrawer) drawRectangle(rect Rectangle, config DrawConfig) {
	x, y := rect.Origin.X, rect.Origin.Y
	w, h := rect.Width, rect.Height
	if w <= 0 || h <= 0 {
		return
	}

	// Clamped so a wide border never draws outside the rectangle.
	bw := min(max(config.BorderWidth, 0), (min(w, h)+1)/2)

	if config.IsFilled {
		for fy := y + bw; fy < y+h-bw; fy++ {
			d.span(x+bw, x+w-1-bw, fy, config.FillColor)
		}
	}

	if bw > 0 {
		for row := 0; row < bw; row++ {
			d.span(x, x+w-1, y+row, config.Color)
			d.span(x, x+w-1, y+h-1-row, config.Color)
		}
		for row := bw; row < h-bw; row++ {
			d.span(x, x+bw-1, y+row, config.Color)
			d.span(x+w-bw, x+w-1, y+row, config.Color)
		}
	}
}
