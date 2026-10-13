package utils

// BlendRGB565 blends srcColor over dstColor with the given alpha (0 gives
// dstColor, 255 gives srcColor). Colors are RGB565.
func BlendRGB565(srcColor uint16, dstColor uint16, alpha byte) uint16 {
	if alpha == 0 {
		return dstColor
	}
	if alpha == 255 {
		return srcColor
	}

	a := uint32(alpha) + (uint32(alpha) >> 7)
	invA := 256 - a

	srcR := uint32((srcColor >> 11) & 0x1F)
	srcG := uint32((srcColor >> 5) & 0x3F)
	srcB := uint32(srcColor & 0x1F)

	dstR := uint32((dstColor >> 11) & 0x1F)
	dstG := uint32((dstColor >> 5) & 0x3F)
	dstB := uint32(dstColor & 0x1F)

	outR := (srcR*a + dstR*invA) >> 8
	outG := (srcG*a + dstG*invA) >> 8
	outB := (srcB*a + dstB*invA) >> 8

	return uint16((outR << 11) | (outG << 5) | outB)
}
