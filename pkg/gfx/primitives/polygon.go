package primitives

import "sort"

// Polygon is a closed shape made of points in logical pixels.
type Polygon struct {
	// Points are the corners in order; the last one connects to the first.
	Points []Point
}

// DrawPolygon draws poly; nothing if it has fewer than 3 points.
func (d *PrimitivesDrawer) DrawPolygon(poly Polygon, config DrawConfig) {
	n := len(poly.Points)
	if n < 3 {
		return
	}

	sx, sy := d.scale()

	pts := make([]Point, n)
	for i, p := range poly.Points {
		pts[i] = scalePoint(p, sx, sy)
	}
	config.BorderWidth = scaleLen(config.BorderWidth, avgScale(sx, sy))

	d.drawPolygon(Polygon{Points: pts}, config)
}

// drawPolygon draws in render pixels.
func (d *PrimitivesDrawer) drawPolygon(poly Polygon, config DrawConfig) {
	n := len(poly.Points)
	if n < 3 {
		return
	}

	if config.IsFilled {
		d.fillPolygon(poly.Points, config.FillColor)
	}

	if config.BorderWidth > 0 {
		for i := 0; i < n; i++ {
			p1 := poly.Points[i]
			p2 := poly.Points[(i+1)%n] // Loop back to start point
			d.drawLine(p1, p2, config.Color, config.BorderWidth)
		}
	}
}

// fillPolygon fills the polygon with color, one line at a time.
func (d *PrimitivesDrawer) fillPolygon(points []Point, color uint16) {
	n := len(points)
	if n < 3 {
		return
	}

	minY := points[0].Y
	maxY := points[0].Y
	for _, p := range points {
		if p.Y < minY {
			minY = p.Y
		}
		if p.Y > maxY {
			maxY = p.Y
		}
	}

	for y := minY; y <= maxY; y++ {
		var nodes []int

		for i := 0; i < n; i++ {
			p1 := points[i]
			p2 := points[(i+1)%n]

			if (p1.Y < y && p2.Y >= y) || (p2.Y < y && p1.Y >= y) {
				x := p1.X + (y-p1.Y)*(p2.X-p1.X)/(p2.Y-p1.Y)
				nodes = append(nodes, x)
			}
		}

		sort.Ints(nodes)

		for i := 0; i < len(nodes)-1; i += 2 {
			xStart := nodes[i]
			xEnd := nodes[i+1]

			for x := xStart; x <= xEnd; x++ {
				d.dc.SetPixel(x, y, color)
			}
		}
	}
}
