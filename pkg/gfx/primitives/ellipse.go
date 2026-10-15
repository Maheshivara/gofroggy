package primitives

// Circle is a circle (bruh) in logical pixels.
type Circle struct {
	// Center is the middle of the circle.
	Center Point

	// Radius is in logical pixels.
	Radius int
}

// Ellipse is an axis-aligned ellipse in logical pixels.
type Ellipse struct {
	// Center is the middle of the ellipse.
	Center Point

	// RadiusX is the horizontal radius in logical pixels.
	RadiusX int

	// RadiusY is the vertical radius in logical pixels.
	RadiusY int
}

// DrawCircle draws circle; nothing if its Radius is 0 or less.
func (d *PrimitivesDrawer) DrawCircle(circle Circle, config DrawConfig) {
	if circle.Radius <= 0 {
		return
	}

	sx, sy := d.scale()
	k := avgScale(sx, sy)

	config.BorderWidth = scaleLen(config.BorderWidth, k)

	d.drawCircle(Circle{
		Center: scalePoint(circle.Center, sx, sy),
		Radius: scaleLen(circle.Radius, k),
	}, config)
}

// drawCircle draws in render pixels.
func (d *PrimitivesDrawer) drawCircle(circle Circle, config DrawConfig) {
	d.drawEllipse(Ellipse{
		Center:  circle.Center,
		RadiusX: circle.Radius,
		RadiusY: circle.Radius,
	}, config)
}

// DrawEllipse draws ellipse; nothing if either radius is 0 or less.
func (d *PrimitivesDrawer) DrawEllipse(ellipse Ellipse, config DrawConfig) {
	if ellipse.RadiusX <= 0 || ellipse.RadiusY <= 0 {
		return
	}

	sx, sy := d.scale()
	k := avgScale(sx, sy)

	config.BorderWidth = scaleLen(config.BorderWidth, k)

	d.drawEllipse(Ellipse{
		Center:  scalePoint(ellipse.Center, sx, sy),
		RadiusX: scaleLen(ellipse.RadiusX, sx),
		RadiusY: scaleLen(ellipse.RadiusY, sy),
	}, config)
}

// drawEllipse draws in render pixels, one span per row.
func (d *PrimitivesDrawer) drawEllipse(ellipse Ellipse, config DrawConfig) {
	rx, ry := ellipse.RadiusX, ellipse.RadiusY
	if rx <= 0 || ry <= 0 {
		return
	}

	cx, cy := ellipse.Center.X, ellipse.Center.Y
	bw := max(config.BorderWidth, 0)

	if config.IsFilled {
		fillRx, fillRy := rx-bw, ry-bw
		if fillRx > 0 && fillRy > 0 {
			for dy := -fillRy; dy <= fillRy; dy++ {
				hw := ellipseHalfWidth(dy, fillRx, fillRy)
				d.span(cx-hw, cx+hw, cy+dy, config.FillColor)
			}
		}
	}

	if bw > 0 {
		// Inner radii clamp to 0, so a wide border fills the whole shape.
		innerRx, innerRy := max(rx-bw, 0), max(ry-bw, 0)

		for dy := -ry; dy <= ry; dy++ {
			outer := ellipseHalfWidth(dy, rx, ry)
			inner := ellipseInnerHalfWidth(dy, innerRx, innerRy)
			if inner < 0 {
				d.span(cx-outer, cx+outer, cy+dy, config.Color)
				continue
			}
			d.span(cx-outer, cx-inner-1, cy+dy, config.Color)
			d.span(cx+inner+1, cx+outer, cy+dy, config.Color)
		}
	}
}

// ellipseHalfWidth returns the largest x with (x, dy) inside or on the
// ellipse, or -1 if row dy misses it. Integer-only, so it can't overflow.
func ellipseHalfWidth(dy, rx, ry int) int {
	rx2, ry2, dy2 := int64(rx)*int64(rx), int64(ry)*int64(ry), int64(dy)*int64(dy)
	if dy2 > ry2 {
		return -1
	}
	return isqrt(rx2 * (ry2 - dy2) / ry2)
}

// ellipseInnerHalfWidth is like ellipseHalfWidth but strictly inside the
// ellipse, so the edge is excluded; -1 if no pixel of row dy qualifies.
func ellipseInnerHalfWidth(dy, rx, ry int) int {
	rx2, ry2, dy2 := int64(rx)*int64(rx), int64(ry)*int64(ry), int64(dy)*int64(dy)
	t := rx2 * (ry2 - dy2)
	if ry2 == 0 || t <= 0 {
		return -1
	}
	return isqrt((t - 1) / ry2)
}
