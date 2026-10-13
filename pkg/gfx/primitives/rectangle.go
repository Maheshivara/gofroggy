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
	if rect.Width <= 0 || rect.Height <= 0 {
		return
	}

	x := rect.Origin.X
	y := rect.Origin.Y
	w := rect.Width
	h := rect.Height

	if config.IsFilled {
		inset := 0
		if config.BorderWidth > 0 {
			inset = config.BorderWidth
		}

		fillX := x + inset
		fillY := y + inset
		fillW := w - (2 * inset)
		fillH := h - (2 * inset)

		for fy := fillY; fy < fillY+fillH; fy++ {
			for fx := fillX; fx < fillX+fillW; fx++ {
				d.dc.SetPixel(fx, fy, config.FillColor)
			}
		}
	}

	if config.BorderWidth > 0 {
		bw := config.BorderWidth

		for row := 0; row < bw; row++ {
			for col := 0; col < w; col++ {
				d.dc.SetPixel(x+col, y+row, config.Color)
			}
		}

		for row := 0; row < bw; row++ {
			for col := 0; col < w; col++ {
				d.dc.SetPixel(x+col, y+h-1-row, config.Color)
			}
		}

		for row := bw; row < h-bw; row++ {
			for col := 0; col < bw; col++ {
				d.dc.SetPixel(x+col, y+row, config.Color)
			}
		}

		for row := bw; row < h-bw; row++ {
			for col := 0; col < bw; col++ {
				d.dc.SetPixel(x+w-1-col, y+row, config.Color)
			}
		}
	}
}
