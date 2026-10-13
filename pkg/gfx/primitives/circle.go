package primitives

// Circle is a circle (bruh) in logical pixels.
type Circle struct {
	// Center is the middle of the circle.
	Center Point

	// Radius is in logical pixels.
	Radius int
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
	if circle.Radius <= 0 {
		return
	}

	cx := circle.Center.X
	cy := circle.Center.Y
	r := circle.Radius

	if config.IsFilled {
		fillRadius := r
		if config.BorderWidth > 0 {
			fillRadius = r - config.BorderWidth
		}

		if fillRadius > 0 {
			for y := -fillRadius; y <= fillRadius; y++ {
				for x := -fillRadius; x <= fillRadius; x++ {
					if x*x+y*y <= fillRadius*fillRadius {
						d.dc.SetPixel(cx+x, cy+y, config.FillColor)
					}
				}
			}
		}
	}

	if config.BorderWidth > 0 {
		outerR := r
		innerR := r - config.BorderWidth
		if innerR < 0 {
			innerR = 0
		}

		for y := -outerR; y <= outerR; y++ {
			for x := -outerR; x <= outerR; x++ {
				distSq := x*x + y*y
				if distSq <= outerR*outerR && distSq >= innerR*innerR {
					d.dc.SetPixel(cx+x, cy+y, config.Color)
				}
			}
		}
	}
}
