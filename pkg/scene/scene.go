package scene

import (
	"github.com/Maheshivara/gofroggy/pkg/gfx"
	"github.com/Maheshivara/gofroggy/pkg/hardware/input"
	"github.com/Maheshivara/gofroggy/pkg/hardware/input/mapping"
)

// Scene is one screen of the application, managed by a SceneManager.
type Scene interface {
	// Init is called once when the scene is pushed. mgr lets it push or pop
	// other scenes.
	Init(mgr *SceneManager) error

	// Update advances the scene by dt seconds. It is called only while the
	// scene is on top; in gives the button state.
	Update(dt float64, in *input.InputController) error

	// Draw draws the scene, even when it is not on top. The manager is locked
	// during the call: do not push or pop from it.
	Draw(d *gfx.Drawer)

	// HandleInput handles a button event and returns true if it consumed it.
	// Only the top scene receives events.
	HandleInput(btn mapping.InputButton, pressed bool) bool

	// Pause is called when another scene is pushed on top. Do not call the
	// manager from it.
	Pause()

	// Resume is called when the scene becomes the top one again. Do not call
	// the manager from it.
	Resume()

	// Destroy is called when the scene is removed; release its resources here.
	Destroy()
}
