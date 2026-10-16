package sprite

import "time"

// LoopMode says what an animation does after its last frame.
type LoopMode int

const (
	// LoopForever starts again from the first frame.
	LoopForever LoopMode = iota

	// LoopOnce stops on the last frame.
	LoopOnce

	// LoopPingPong plays forwards, then backwards, and so on.
	LoopPingPong
)

// Animation is a sequence of sheet frames with a duration each.
type Animation struct {
	// Sheet holds the frames.
	Sheet *Sheet

	// Frames are the sheet frame numbers to play, in order.
	Frames []int

	// Durations has one entry per element of Frames.
	Durations []time.Duration

	// Mode is what happens at the end.
	Mode LoopMode
}

// NewAnimation plays the given frames of sheet, each for frameTime.
func NewAnimation(sheet *Sheet, frames []int, frameTime time.Duration, mode LoopMode) *Animation {
	d := make([]time.Duration, len(frames))
	for i := range d {
		d[i] = frameTime
	}
	return &Animation{Sheet: sheet, Frames: frames, Durations: d, Mode: mode}
}

// Range returns the frame numbers from..to, inclusive.
func Range(from, to int) []int {
	step := 1
	if to < from {
		step = -1
	}

	var out []int
	for i := from; i != to+step; i += step {
		out = append(out, i)
	}
	return out
}

// Animator plays an Animation. Call Update once per frame of your game loop.
type Animator struct {
	// Speed scales time: 1 is normal, 2 is twice as fast. NewAnimator sets 1.
	Speed float64

	anim    *Animation
	index   int // position in anim.Frames
	dir     int // +1 or -1, for ping-pong
	elapsed time.Duration
	done    bool
	paused  bool
}

// NewAnimator returns an animator that starts a at its first frame.
func NewAnimator(a *Animation) *Animator {
	return &Animator{Speed: 1, anim: a, dir: 1}
}

// Play switches to a and restarts. Playing the animation that is already
// running does nothing, so it is safe to call every frame.
func (p *Animator) Play(a *Animation) {
	if p.anim == a && !p.done {
		return
	}
	p.anim = a
	p.Reset()
}

// Reset goes back to the first frame.
func (p *Animator) Reset() {
	p.index, p.dir, p.elapsed, p.done = 0, 1, 0, false
}

// Pause stops time; Resume continues.
func (p *Animator) Pause()  { p.paused = true }
func (p *Animator) Resume() { p.paused = false }

// Done reports whether a LoopOnce animation reached its end.
func (p *Animator) Done() bool { return p.done }

// Sheet returns the sheet of the current animation.
func (p *Animator) Sheet() *Sheet {
	if p.anim == nil {
		return nil
	}
	return p.anim.Sheet
}

// Frame returns the sheet frame number to draw now.
func (p *Animator) Frame() int {
	if p.anim == nil || len(p.anim.Frames) == 0 {
		return 0
	}
	return p.anim.Frames[p.index]
}

// Update moves the animation forward by dt.
func (p *Animator) Update(dt time.Duration) {
	if p.paused || p.done || p.anim == nil || len(p.anim.Frames) == 0 {
		return
	}

	p.elapsed += time.Duration(float64(dt) * p.Speed)

	for guard := 0; guard < 1000; guard++ {
		d := max(p.anim.Durations[p.index], time.Millisecond)
		if p.elapsed < d {
			return
		}
		p.elapsed -= d
		p.advance()
		if p.done {
			p.elapsed = 0
			return
		}
	}
	p.elapsed = 0
}

func (p *Animator) advance() {
	n := len(p.anim.Frames)

	switch p.anim.Mode {
	case LoopOnce:
		if p.index+1 >= n {
			p.done = true
			return
		}
		p.index++

	case LoopPingPong:
		if n == 1 {
			return
		}
		next := p.index + p.dir
		if next < 0 || next >= n {
			p.dir = -p.dir
			next = p.index + p.dir
		}
		p.index = next

	default:
		p.index = (p.index + 1) % n
	}
}
