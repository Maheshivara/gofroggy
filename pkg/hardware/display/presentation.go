package display

import "fmt"

// presentation maps the logical RGB565 image onto the device-sized source frame.
// An explicitly smaller image is centered without scaling.
type presentation struct {
	width, height int
	x, y          int
	centered      bool
	color         uint16
}

func newPresentation(cfg Config, width, height, panelW, panelH int) presentation {
	p := presentation{width: panelW, height: panelH, color: cfg.BackgroundColor}
	p.centered = cfg.Width > 0 && cfg.Height > 0 && width <= panelW && height <= panelH && (width < panelW || height < panelH)
	if p.centered {
		p.x = (panelW - width) / 2
		p.y = (panelH - height) / 2
	}
	return p
}

// compose writes changed logical pixels into a physical-sized RGB565 canvas.
// The first call initializes the full background and content.
func (p presentation) compose(dst, src []uint16, sw, sh int, dm damage, first bool) (damage, error) {
	if len(src) != sw*sh || len(dst) != p.width*p.height {
		return noDamage, fmt.Errorf("invalid presentation buffers: source %d/%d, output %d/%d", len(src), sw*sh, len(dst), p.width*p.height)
	}
	if !first && dm.empty() {
		return noDamage, nil
	}
	if first {
		dm = damage{0, sh - 1, 0, sw - 1}
	}
	if p.centered {
		if first {
			fillPixels(dst, p.color)
		}
		for y := dm.r0; y <= dm.r1; y++ {
			from := y*sw + dm.c0
			to := (y+p.y)*p.width + p.x + dm.c0
			copy(dst[to:to+dm.c1-dm.c0+1], src[from:from+dm.c1-dm.c0+1])
		}
		if first {
			return damage{0, p.height - 1, 0, p.width - 1}, nil
		}
		return damage{dm.r0 + p.y, dm.r1 + p.y, dm.c0 + p.x, dm.c1 + p.x}, nil
	}
	if sw == p.width && sh == p.height {
		for y := dm.r0; y <= dm.r1; y++ {
			start := y*p.width + dm.c0
			copy(dst[start:start+dm.c1-dm.c0+1], src[start:start+dm.c1-dm.c0+1])
		}
		return dm, nil
	}
	// Downscale without explicit geometry retains the original nearest-neighbor upscaling.
	for y := 0; y < p.height; y++ {
		from := src[(y*sh/p.height)*sw:]
		out := dst[y*p.width : (y+1)*p.width]
		for x := range out {
			out[x] = from[x*sw/p.width]
		}
	}
	return damage{0, p.height - 1, 0, p.width - 1}, nil
}

func (p presentation) native(w, h int) bool { return !p.centered && w == p.width && h == p.height }

func (f *frame) configure(cfg Config, panelW, panelH int) error {
	w, h, err := resolveSize(cfg, panelW, panelH)
	if err != nil {
		return err
	}
	f.w, f.h = w, h
	f.lw, f.lh = resolveLogicalSize(cfg, w, h, panelW, panelH)
	f.presentation = newPresentation(cfg, w, h, panelW, panelH)
	f.buf = make([]uint16, w*h)
	return nil
}

func (f *frame) changed(check bool) damage {
	if check {
		return f.diff()
	}
	return f.fullDamage()
}
