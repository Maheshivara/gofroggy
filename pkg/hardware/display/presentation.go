package display

import "fmt"

// presentation maps RGB565 drawing pixels onto the device-sized source image.
// Coordinates are resolved once, so partial updates never divide per pixel.
type presentation struct {
	mode             ScalingMode
	width, height    int
	x, y             int
	activeW, activeH int
	centered         bool
	scaled           bool
	color            uint16
	xmap, ymap       []int // output position -> source pixel, -1 for background
}

func newPresentation(cfg Config, sw, sh, panelW, panelH int) presentation {
	p := presentation{width: panelW, height: panelH, color: cfg.BackgroundColor, mode: cfg.Scaling}
	p.activeW, p.activeH = panelW, panelH
	switch cfg.Scaling {
	case ScalingCenter:
		p.activeW, p.activeH = sw, sh
	case ScalingFit:
		// Use integer arithmetic to avoid aspect-ratio rounding drift.
		if int64(sw)*int64(panelH) > int64(sh)*int64(panelW) {
			p.activeH = max(1, int(int64(panelW)*int64(sh)/int64(sw)))
		} else {
			p.activeW = max(1, int(int64(panelH)*int64(sw)/int64(sh)))
		}
	}
	p.x = (panelW - p.activeW) / 2
	p.y = (panelH - p.activeH) / 2
	p.centered = p.activeW != panelW || p.activeH != panelH
	p.scaled = sw != p.activeW || sh != p.activeH
	p.xmap = make([]int, panelW)
	p.ymap = make([]int, panelH)
	for x := range p.xmap {
		p.xmap[x] = -1
		if x >= p.x && x < p.x+p.activeW {
			p.xmap[x] = (x - p.x) * sw / p.activeW
		}
	}
	for y := range p.ymap {
		p.ymap[y] = -1
		if y >= p.y && y < p.y+p.activeH {
			p.ymap[y] = (y - p.y) * sh / p.activeH
		}
	}
	return p
}

// compose copies only pixels affected by damage, applying nearest-neighbor fit.
// The first update initializes the background, if any.
func (p presentation) compose(dst, src []uint16, sw, sh int, dm damage, first bool) (damage, error) {
	if len(src) != sw*sh || len(dst) != p.width*p.height {
		return noDamage, fmt.Errorf("invalid presentation buffers: source %d/%d, output %d/%d", len(src), sw*sh, len(dst), p.width*p.height)
	}
	if !first && dm.empty() {
		return noDamage, nil
	}
	if first {
		dm = damage{0, sh - 1, 0, sw - 1}
		if p.centered {
			fillPixels(dst, p.color)
		}
	}
	if !p.scaled {
		width := dm.c1 - dm.c0 + 1
		for y := dm.r0; y <= dm.r1; y++ {
			from := y*sw + dm.c0
			to := (y+p.y)*p.width + p.x + dm.c0
			copy(dst[to:to+width], src[from:from+width])
		}
		if first {
			return damage{0, p.height - 1, 0, p.width - 1}, nil
		}
		return damage{dm.r0 + p.y, dm.r1 + p.y, dm.c0 + p.x, dm.c1 + p.x}, nil
	}

	// Inverse maps bound the changed destination region without repainting bars.
	x0, x1 := p.width, -1
	for x, sx := range p.xmap {
		if sx >= dm.c0 && sx <= dm.c1 {
			x0 = min(x0, x)
			x1 = x
		}
	}
	y0, y1 := p.height, -1
	for y, sy := range p.ymap {
		if sy >= dm.r0 && sy <= dm.r1 {
			y0 = min(y0, y)
			y1 = y
		}
	}
	if x1 < 0 || y1 < 0 {
		return noDamage, nil
	}
	for y := y0; y <= y1; y++ {
		from := p.ymap[y] * sw
		out := dst[y*p.width:]
		for x := x0; x <= x1; x++ {
			out[x] = src[from+p.xmap[x]]
		}
	}
	if first {
		return damage{0, p.height - 1, 0, p.width - 1}, nil
	}
	return damage{y0, y1, x0, x1}, nil
}

func (p presentation) native(w, h int) bool {
	return !p.centered && !p.scaled && w == p.width && h == p.height
}

func (f *frame) configure(cfg Config, panelW, panelH int) error {
	w, h, err := resolveSize(cfg, panelW, panelH)
	if err != nil {
		return err
	}
	f.w, f.h = w, h
	f.lw, f.lh = resolveLogicalSize(cfg, w, h, panelW, panelH)
	f.presentation = newPresentation(cfg, w, h, panelW, panelH)
	f.buf = make([]uint16, w*h)
	f.trackDrawDamage = cfg.TrackDrawDamage
	f.dirty = f.fullDamage()
	return nil
}

// changed returns net pixel changes when raw buffers are accessible.
// Draw-helper dirty marks are authoritative only before Buffer() is exposed.
func (f *frame) changed(check bool) damage {
	if !check {
		f.dirty = noDamage
		f.exposurePending = false
		return f.fullDamage()
	}
	if !f.trackDrawDamage {
		return f.diff()
	}
	if f.exposed || f.forceCompare {
		result := f.diff()
		if f.exposurePending {
			// The first Buffer() snapshot can occur after helper writes.
			result = result.union(f.dirty)
		}
		f.exposurePending = false
		f.dirty = noDamage
		return result
	}
	result := f.dirty
	f.dirty = noDamage
	return result
}
