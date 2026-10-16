// Package sprite loads bitmaps and sprite sheets, draws them on a display and
// animates them.
package sprite

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif" // register the GIF decoder
	_ "image/jpeg"
	_ "image/png"
	"os"
)

// Bitmap is an RGB565 image with an optional alpha channel.
type Bitmap struct {
	// W and H are the size in pixels.
	W, H int

	// Stride is the number of pixels from one row to the next in Pix and Alpha.
	Stride int

	// Pix holds the pixels (RGB565), row by row.
	Pix []uint16

	// Alpha holds 0 (transparent) to 255 (opaque) per pixel; nil when every
	// pixel is opaque.
	Alpha []uint8
}

func rgb565(r, g, b uint8) uint16 {
	return uint16(r>>3)<<11 | uint16(g>>2)<<5 | uint16(b>>3)
}

// FromImage converts any decoded image to a Bitmap.
func FromImage(img image.Image) *Bitmap {
	r := img.Bounds()
	w, h := r.Dx(), r.Dy()

	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(src, src.Bounds(), img, r.Min, draw.Src)

	bm := &Bitmap{W: w, H: h, Stride: w, Pix: make([]uint16, w*h)}
	opaque := true
	for i := range bm.Pix {
		p := src.Pix[i*4 : i*4+4]
		bm.Pix[i] = rgb565(p[0], p[1], p[2])
		if p[3] != 255 {
			opaque = false
		}
	}

	if !opaque {
		bm.Alpha = make([]uint8, w*h)
		for i := range bm.Alpha {
			bm.Alpha[i] = src.Pix[i*4+3]
		}
	}
	return bm
}

// DecodeBitmap decodes PNG, GIF (first frame) or JPEG data.
func DecodeBitmap(data []byte) (*Bitmap, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return FromImage(img), nil
}

// LoadBitmap decodes a PNG, GIF (first frame) or JPEG file.
func LoadBitmap(path string) (*Bitmap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	bm, err := DecodeBitmap(data)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return bm, nil
}

// SetColorKey makes every pixel of colour key (RGB565) transparent. Use it
// for images without an alpha channel, before cutting them into frames.
func (b *Bitmap) SetColorKey(key uint16) {
	if b.Alpha == nil {
		b.Alpha = make([]uint8, len(b.Pix))
		for i := range b.Alpha {
			b.Alpha[i] = 255
		}
	}
	for i, p := range b.Pix {
		if p == key {
			b.Alpha[i] = 0
		}
	}
}

// Sub returns the w x h part at (x, y). It shares the pixels with b.
func (b Bitmap) Sub(x, y, w, h int) Bitmap {
	off := y*b.Stride + x
	s := Bitmap{W: w, H: h, Stride: b.Stride, Pix: b.Pix[off:]}
	if b.Alpha != nil {
		s.Alpha = b.Alpha[off:]
	}
	return s
}

// scaled returns b resized to w x h with nearest-neighbour sampling.
func (b Bitmap) scaled(w, h int) Bitmap {
	if w == b.W && h == b.H {
		return b
	}

	out := Bitmap{W: w, H: h, Stride: w, Pix: make([]uint16, w*h)}
	if b.Alpha != nil {
		out.Alpha = make([]uint8, w*h)
	}

	xs := make([]int, w)
	for x := range xs {
		xs[x] = x * b.W / w
	}

	for y := 0; y < h; y++ {
		src := (y * b.H / h) * b.Stride
		dst := y * w
		for x, sx := range xs {
			out.Pix[dst+x] = b.Pix[src+sx]
			if out.Alpha != nil {
				out.Alpha[dst+x] = b.Alpha[src+sx]
			}
		}
	}
	return out
}
