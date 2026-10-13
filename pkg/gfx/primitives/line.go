package primitives

// DrawLine draws a line from p1 to p2; thickness is in logical pixels.
func (d *PrimitivesDrawer) DrawLine(p1, p2 Point, color uint16, thickness int) {
	if thickness <= 0 {
		return
	}

	sx, sy := d.scale()
	d.drawLine(
		scalePoint(p1, sx, sy),
		scalePoint(p2, sx, sy),
		color,
		scaleLen(thickness, avgScale(sx, sy)),
	)
}

// drawLine draws in render pixels.
func (d *PrimitivesDrawer) drawLine(p1, p2 Point, color uint16, thickness int) {
	if thickness <= 0 {
		return
	}

	x0, y0 := p1.X, p1.Y
	x1, y1 := p2.X, p2.Y

	dx := abs(x1 - x0)
	dy := abs(y1 - y0)

	sx := -1
	if x0 < x1 {
		sx = 1
	}

	sy := -1
	if y0 < y1 {
		sy = 1
	}

	err := dx - dy

	for {
		d.drawPointThickness(x0, y0, thickness, color)

		if x0 == x1 && y0 == y1 {
			break
		}

		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

// drawPointThickness draws a square dot centered on (cx, cy).
func (d *PrimitivesDrawer) drawPointThickness(cx, cy, thickness int, color uint16) {
	if thickness == 1 {
		d.dc.SetPixel(cx, cy, color)
		return
	}

	radius := thickness / 2
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			d.dc.SetPixel(x, y, color)
		}
	}
}

// abs returns the absolute value of v.
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
