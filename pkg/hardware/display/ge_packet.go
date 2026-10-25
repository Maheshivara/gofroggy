package display

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

// Captured 232-byte SF3000 command packet validated by the hardware tests.
// Only its framebuffer addresses are relocated for each frame.
const (
	gePacketBytes = 0xe8
	gePacketCRC   = 0x9cbb453d
	geRingStart   = 0x420
	geRingLength  = 0x3c000
	gePanelWidth  = 854
	gePanelHeight = 480
	geFrameBytes  = gePanelWidth * gePanelHeight * 2
)

var geCapturedWords = [...]uint32{
	0x0206870f, 0x00a03009, 0x0bc14800, 0x000061e0,
	0x0bc14800, 0x000061e0, 0x0bb4c580, 0x40006356,
	0x00000000, 0x035601e0, 0x00000000, 0x00000000,
	0x01e00356, 0x00004000, 0x00000000, 0x00000000,
	0x80000000, 0x0000ffff, 0x00000000, 0x8000ffff,
	0x80000000, 0x00000000, 0x8100c002, 0x80020000,
	0x0b1ea780, 0x02000307, 0x00000001, 0x10100000,
	0x00001002, 0x10100000, 0x00001002, 0x00000000,
	0x00010002, 0x00000000, 0x0206c70f, 0x10a03009,
	0x0b1ea780, 0x00006356, 0x0b1ea780, 0x00006356,
	0x0bc14800, 0x400061e0, 0x00000000, 0x01e00358,
	0x00000000, 0x00000000, 0x035601e0, 0x00000000,
	0x01e00356, 0x00004000, 0x00000000, 0x00000080,
	0x00008fe3, 0x00000000, 0x00f08fe3, 0x00000000,
	0x0001c777, 0x01ab0000,
}

// These offsets are absolute in the command ring (not relative to the packet).
var geReloc = [...]int{0x428, 0x430, 0x438, 0x480, 0x4b0, 0x4b8, 0x4c0}

func geTemplate() ([]byte, error) {
	p := make([]byte, len(geCapturedWords)*4)
	for i, w := range geCapturedWords {
		binary.LittleEndian.PutUint32(p[i*4:], w)
	}
	if len(p) != gePacketBytes || crc32.ChecksumIEEE(p) != gePacketCRC {
		return nil, fmt.Errorf("SF3000 GE template integrity check failed")
	}
	return p, nil
}

// Relocate the two-operation hardware sequence: source -> scratch -> output.
func geRelocatedPacket(template []byte, source, scratch, output uint32) []byte {
	p := append([]byte(nil), template...)
	for _, off := range [...]int{0x428, 0x430, 0x4c0} {
		gePut(p, off-geRingStart, scratch)
	}
	gePut(p, 0x438-geRingStart, source)
	for _, off := range [...]int{0x480, 0x4b0, 0x4b8} {
		gePut(p, off-geRingStart, output)
	}
	return p
}

func geGet(p []byte, off int) uint32    { return binary.LittleEndian.Uint32(p[off:]) }
func gePut(p []byte, off int, w uint32) { binary.LittleEndian.PutUint32(p[off:], w) }

// Check a previously submitted packet before accepting an idle GE queue left
// by an earlier test. Unrecognized GE streams must not be overwritten.
func geCompatiblePacket(packet, reference []byte, base, size uint32) error {
	if len(packet) != gePacketBytes || len(reference) != gePacketBytes {
		return fmt.Errorf("invalid packet length")
	}
	for off := 0; off < gePacketBytes; off += 4 {
		absolute := off + geRingStart
		a, b := geGet(packet, off), geGet(reference, off)
		relocated := false
		for _, item := range geReloc {
			if item == absolute {
				relocated = true
				break
			}
		}
		if relocated {
			if a < base || uint64(a) >= uint64(base)+uint64(size) || (a-base)%geFrameBytes != 0 {
				return fmt.Errorf("invalid framebuffer address %08x at packet+0x%x", a, off)
			}
		} else if a != b {
			return fmt.Errorf("unknown HCGE packet word+0x%x: %08x != %08x", off, a, b)
		}
	}
	return nil
}
