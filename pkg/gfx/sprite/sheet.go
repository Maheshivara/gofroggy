package sprite

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"math"
	"os"
	"time"
)

// Sheet is a set of equally sized frames. It is not safe for concurrent use.
type Sheet struct {
	// OriginX and OriginY are the pivot of every frame, in logical pixels from
	// its top-left corner. Draw puts the pivot at the position you give it.
	OriginX, OriginY int

	frames []Bitmap
	fw, fh int // frame size in logical pixels

	scaleX, scaleY float64
	scaled         []Bitmap // frames at the display scale; W == 0 means not made yet
}

// NewSheet cuts bm into frameW x frameH frames, left to right, top to bottom.
func NewSheet(bm *Bitmap, frameW, frameH int) (*Sheet, error) {
	if frameW <= 0 || frameH <= 0 {
		frameW, frameH = bm.W, bm.H
	}

	cols, rows := bm.W/frameW, bm.H/frameH
	if cols == 0 || rows == 0 {
		return nil, fmt.Errorf("image %dx%d is smaller than a %dx%d frame", bm.W, bm.H, frameW, frameH)
	}

	frames := make([]Bitmap, 0, cols*rows)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			frames = append(frames, bm.Sub(c*frameW, r*frameH, frameW, frameH))
		}
	}
	return NewSheetFromFrames(frames)
}

// NewSheetFromFrames builds a sheet from frames that all have the same size.
func NewSheetFromFrames(frames []Bitmap) (*Sheet, error) {
	if len(frames) == 0 {
		return nil, fmt.Errorf("a sheet needs at least one frame")
	}
	for _, f := range frames {
		if f.W != frames[0].W || f.H != frames[0].H {
			return nil, fmt.Errorf("all frames must have the same size")
		}
	}
	return &Sheet{frames: frames, fw: frames[0].W, fh: frames[0].H}, nil
}

// LoadSheet loads an image file and cuts it into frames. A frame size of 0
// makes the whole image one frame.
func LoadSheet(path string, frameW, frameH int) (*Sheet, error) {
	bm, err := LoadBitmap(path)
	if err != nil {
		return nil, err
	}
	return NewSheet(bm, frameW, frameH)
}

// Len returns the number of frames.
func (s *Sheet) Len() int { return len(s.frames) }

// FrameSize returns the size of a frame in logical pixels.
func (s *Sheet) FrameSize() (w, h int) { return s.fw, s.fh }

// frame returns frame i resized for the display scale, making it on first use.
func (s *Sheet) frame(i int, sx, sy float64) Bitmap {
	if s.scaled == nil || s.scaleX != sx || s.scaleY != sy {
		s.scaleX, s.scaleY = sx, sy
		s.scaled = make([]Bitmap, len(s.frames))
	}

	if s.scaled[i].W == 0 {
		w := max(1, int(math.Round(float64(s.fw)*sx)))
		h := max(1, int(math.Round(float64(s.fh)*sy)))
		s.scaled[i] = s.frames[i].scaled(w, h)
	}
	return s.scaled[i]
}

// LoadGIF loads an animated GIF as a sheet and an animation that follows the
// file's frame delays.
func LoadGIF(path string) (*Sheet, *Animation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if len(g.Image) == 0 {
		return nil, nil, fmt.Errorf("%s has no frames", path)
	}

	canvas := image.NewNRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
	frames := make([]Bitmap, len(g.Image))
	durations := make([]time.Duration, len(g.Image))
	order := make([]int, len(g.Image))

	for i, im := range g.Image {
		disposal := byte(gif.DisposalNone)
		if i < len(g.Disposal) {
			disposal = g.Disposal[i]
		}

		var saved []uint8
		if disposal == gif.DisposalPrevious {
			saved = append(saved, canvas.Pix...)
		}

		draw.Draw(canvas, im.Bounds(), im, im.Bounds().Min, draw.Over)
		frames[i] = *FromImage(canvas)

		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, im.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			copy(canvas.Pix, saved)
		}

		durations[i] = time.Duration(g.Delay[i]) * 10 * time.Millisecond
		if durations[i] <= 0 {
			durations[i] = 100 * time.Millisecond
		}
		order[i] = i
	}

	sheet, err := NewSheetFromFrames(frames)
	if err != nil {
		return nil, nil, err
	}

	mode := LoopForever
	if g.LoopCount == -1 {
		mode = LoopOnce
	}
	return sheet, &Animation{Sheet: sheet, Frames: order, Durations: durations, Mode: mode}, nil
}
