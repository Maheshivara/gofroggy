package primitives

import (
	"math"

	"github.com/Maheshivara/gofroggy/pkg/hardware/display"
)

// Point is a position in logical pixels.
type Point struct {
	X int
	Y int
}

// DrawConfig sets the colors and border of a shape.
type DrawConfig struct {
	// Color is the border color (RGB565).
	Color uint16

	// FillColor is the fill color (RGB565).
	FillColor uint16

	// IsFilled fills the shape.
	IsFilled bool

	// BorderWidth is the border thickness in logical pixels; 0 draws no border.
	BorderWidth int
}

// PrimitivesDrawer draws shapes in logical (panel) pixels and scales them to
// the display's render size.
type PrimitivesDrawer struct {
	dc display.DisplayController
}

// NewPrimitivesDrawer returns a drawer that draws on dc.
func NewPrimitivesDrawer(dc display.DisplayController) *PrimitivesDrawer {
	return &PrimitivesDrawer{dc: dc}
}

// scale returns render pixels per logical pixel on each axis.
func (d *PrimitivesDrawer) scale() (sx, sy float64) {
	return d.dc.Scale()
}

// scaleCoord converts a logical coordinate to render pixels.
func scaleCoord(v int, s float64) int {
	return int(math.Round(float64(v) * s))
}

// scaleLen converts a length to render pixels, never below 1 if v is positive.
func scaleLen(v int, s float64) int {
	if v <= 0 {
		return v
	}
	r := int(math.Round(float64(v) * s))
	if r < 1 {
		r = 1
	}
	return r
}

// scalePoint converts a logical point to render pixels.
func scalePoint(p Point, sx, sy float64) Point {
	return Point{X: scaleCoord(p.X, sx), Y: scaleCoord(p.Y, sy)}
}

// avgScale returns the mean of the two axis scales, for lengths and radii.
func avgScale(sx, sy float64) float64 {
	return (sx + sy) / 2
}
