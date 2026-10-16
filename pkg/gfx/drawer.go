package gfx

import (
	"github.com/Maheshivara/gofroggy/pkg/gfx/primitives"
	"github.com/Maheshivara/gofroggy/pkg/gfx/sprite"
	"github.com/Maheshivara/gofroggy/pkg/gfx/text"
	"github.com/Maheshivara/gofroggy/pkg/hardware/display"
)

// Drawer groups the drawing helpers for one display.
type Drawer struct {
	DisplayController display.DisplayController

	// Text draws text; positions are in logical pixels.
	Text *text.TextDrawer

	// Primitives draws shapes; coordinates are in logical pixels.
	Primitives *primitives.PrimitivesDrawer

	// Sprite draw sprites from images.
	Sprite *sprite.SpriteDrawer
}

// NewDrawer returns a Drawer that draws on d.
func NewDrawer(d display.DisplayController) *Drawer {
	textDrawer := text.NewTextDrawer(d)
	primitivesDrawer := primitives.NewPrimitivesDrawer(d)
	spriteDrawer := sprite.NewSpriteDrawer(d)

	return &Drawer{
		Text:              textDrawer,
		Primitives:        primitivesDrawer,
		Sprite:            spriteDrawer,
		DisplayController: d,
	}
}
