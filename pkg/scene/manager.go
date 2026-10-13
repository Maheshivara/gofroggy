package scene

import (
	"fmt"
	"sync"

	"github.com/Maheshivara/gofroggy/pkg/gfx"
	"github.com/Maheshivara/gofroggy/pkg/hardware/input"
	"github.com/Maheshivara/gofroggy/pkg/hardware/input/mapping"
)

// SceneManager runs a stack of scenes; the top one is the active scene.
type SceneManager struct {
	stack []Scene
	mu    sync.RWMutex
}

// NewSceneManager returns an empty manager.
func NewSceneManager() *SceneManager {
	return &SceneManager{
		stack: make([]Scene, 0, 4),
	}
}

// Push pauses the current scene and puts s on top. If s.Init fails, s is
// destroyed and removed, and the error is returned.
func (sm *SceneManager) Push(s Scene) error {
	sm.mu.Lock()
	if len(sm.stack) > 0 {
		sm.stack[len(sm.stack)-1].Pause()
	}
	sm.stack = append(sm.stack, s)
	sm.mu.Unlock()

	if err := s.Init(sm); err != nil {
		sm.Pop()
		return fmt.Errorf("falha ao inicializar a cena: %w", err)
	}
	return nil
}

// Pop removes and destroys the top scene, resumes the one below and returns
// it (nil if the stack is empty).
func (sm *SceneManager) Pop() Scene {
	sm.mu.Lock()
	n := len(sm.stack)
	if n == 0 {
		sm.mu.Unlock()
		return nil
	}

	top := sm.stack[n-1]
	sm.stack = sm.stack[:n-1]

	if len(sm.stack) > 0 {
		sm.stack[len(sm.stack)-1].Resume()
	}
	sm.mu.Unlock()

	top.Destroy()
	return top
}

// Switch destroys all scenes, top first, and pushes s as the only scene.
func (sm *SceneManager) Switch(s Scene) error {
	sm.mu.Lock()
	oldStack := make([]Scene, len(sm.stack))
	copy(oldStack, sm.stack)
	sm.stack = sm.stack[:0]
	sm.mu.Unlock()

	for i := len(oldStack) - 1; i >= 0; i-- {
		oldStack[i].Destroy()
	}

	return sm.Push(s)
}

// Update updates the top scene only and returns its error.
func (sm *SceneManager) Update(dt float64, in *input.InputController) error {
	sm.mu.RLock()
	n := len(sm.stack)
	if n == 0 {
		sm.mu.RUnlock()
		return nil
	}
	activeScene := sm.stack[n-1]
	sm.mu.RUnlock()

	return activeScene.Update(dt, in)
}

// Draw draws all scenes, bottom first, so upper scenes cover lower ones.
func (sm *SceneManager) Draw(d *gfx.Drawer) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for _, s := range sm.stack {
		s.Draw(d)
	}
}

// HandleInput passes a button event to the top scene and reports whether it
// was handled.
func (sm *SceneManager) HandleInput(btn mapping.InputButton, pressed bool) bool {
	sm.mu.RLock()
	n := len(sm.stack)
	if n == 0 {
		sm.mu.RUnlock()
		return false
	}
	top := sm.stack[n-1]
	sm.mu.RUnlock()

	return top.HandleInput(btn, pressed)
}
