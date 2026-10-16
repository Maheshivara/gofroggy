package main

import (
	"embed"
	"fmt"
	"time"

	"github.com/Maheshivara/gofroggy/pkg/gfx"
	"github.com/Maheshivara/gofroggy/pkg/gfx/sprite"
	"github.com/Maheshivara/gofroggy/pkg/gfx/text"
	"github.com/Maheshivara/gofroggy/pkg/hardware/input"
	"github.com/Maheshivara/gofroggy/pkg/hardware/input/mapping"
	"github.com/Maheshivara/gofroggy/pkg/scene"
)

const (
	frameSize = 150
	frameTime = 100 * time.Millisecond
	deathHold = 1500 * time.Millisecond

	minScale  = 0.25
	maxScale  = 3.0
	scaleStep = 0.25

	colorYellow uint16 = 0xFFE0
	colorGray   uint16 = 0x7BEF
)

var entries = []struct {
	label string
	file  string
	mode  sprite.LoopMode
}{
	{"Idle", "Idle.png", sprite.LoopForever},
	{"Move", "Move.png", sprite.LoopForever},
	{"Attack", "Attack.png", sprite.LoopForever},
	{"Take Hit", "Take_Hit.png", sprite.LoopForever},
	{"Death", "Death.png", sprite.LoopOnce},
}

// SpriteDemo is a scene that owns its sprites: it loads them in Init, advances
// the animation in Update, draws it in Draw and drops it in Destroy.
type SpriteDemo struct {
	files         embed.FS
	width, height int

	font     *text.Font
	anims    []*sprite.Animation
	animator *sprite.Animator
	current  int
	scale    float64
	hold     time.Duration
	prepared bool
}

func NewSpriteDemo(files embed.FS, screenW, screenH int) *SpriteDemo {
	return &SpriteDemo{files: files, width: screenW, height: screenH, scale: 2}
}

func (s *SpriteDemo) Init(mgr *scene.SceneManager) error {
	anims := make([]*sprite.Animation, len(entries))

	for i, e := range entries {
		data, err := s.files.ReadFile("img/" + e.file)
		if err != nil {
			return err
		}

		bm, err := sprite.DecodeBitmap(data)
		if err != nil {
			return fmt.Errorf("%s: %w", e.file, err)
		}

		sheet, err := sprite.NewSheet(bm, frameSize, frameSize)
		if err != nil {
			return fmt.Errorf("%s: %w", e.file, err)
		}
		sheet.OriginX, sheet.OriginY = frameSize/2, frameSize/2

		anims[i] = sprite.NewAnimation(sheet, sprite.Range(0, sheet.Len()-1), frameTime, e.mode)
	}

	s.anims = anims
	s.animator = sprite.NewAnimator(anims[0])
	s.font, _ = text.LoadFont("", 20)
	return nil
}

func (s *SpriteDemo) Update(dt float64, in *input.InputController) error {
	s.animator.Update(time.Duration(dt * float64(time.Second)))

	if s.animator.Done() {
		if s.hold += time.Duration(dt * float64(time.Second)); s.hold >= deathHold {
			s.animator.Reset()
			s.hold = 0
		}
	}
	return nil
}

func (s *SpriteDemo) Draw(d *gfx.Drawer) {
	if !s.prepared {
		for _, a := range s.anims {
			d.Sprite.Prepare(a.Sheet)
		}
		s.prepared = true
	}

	cx, cy := s.width/2, s.height/2-30
	d.Sprite.DrawAnimatorScaled(s.animator, cx, cy, s.scale, sprite.FlipNone)

	s.drawCentered(d, fmt.Sprintf("< %s >   x%.2f", entries[s.current].label, s.scale), s.height-56, colorYellow)
	s.drawCentered(d, "LEFT/RIGHT: SPRITE   UP/DOWN: SCALE", s.height-34, colorGray)
	s.drawCentered(d, "SELECT + START: EXIT", s.height-12, colorGray)
}

func (s *SpriteDemo) drawCentered(d *gfx.Drawer, str string, y int, color uint16) {
	w, _ := s.font.MeasureText(str)
	d.Text.DrawText(s.font, str, (s.width-w)/2, y, color)
}

func (s *SpriteDemo) HandleInput(btn mapping.InputButton, pressed bool) bool {
	if !pressed {
		return false
	}

	switch btn {
	case mapping.RIGHT, mapping.A:
		s.change(1)
	case mapping.LEFT:
		s.change(-1)
	case mapping.UP:
		s.scale = min(s.scale+scaleStep, maxScale)
	case mapping.DOWN:
		s.scale = max(s.scale-scaleStep, minScale)
	default:
		return false
	}
	return true
}

func (s *SpriteDemo) change(dir int) {
	s.current = (s.current + dir + len(s.anims)) % len(s.anims)
	s.animator.Play(s.anims[s.current])
	s.hold = 0
}

func (s *SpriteDemo) Pause() { s.animator.Pause() }

func (s *SpriteDemo) Resume() { s.animator.Resume() }

func (s *SpriteDemo) Destroy() {
	s.anims, s.animator, s.font = nil, nil, nil
}
