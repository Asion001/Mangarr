package imagecheck

import (
	"bytes"
	"encoding/binary"
)

// jxlSize reads the image size from a JPEG XL file: a bare codestream
// (FF 0A …) or an ISOBMFF container holding it in a jxlc/jxlp box.
func jxlSize(b []byte) (int, int) {
	if bytes.HasPrefix(b, []byte{0xFF, 0x0A}) {
		return jxlCodestreamSize(b[2:])
	}
	// container: walk the boxes
	for off := 0; off+8 <= len(b); {
		size := int(binary.BigEndian.Uint32(b[off : off+4]))
		typ := string(b[off+4 : off+8])
		hdr := 8
		if size == 1 && off+16 <= len(b) {
			size, hdr = int(binary.BigEndian.Uint64(b[off+8:off+16])), 16
		}
		if size == 0 {
			size = len(b) - off
		}
		if size < hdr || off+size > len(b) {
			size = len(b) - off // truncated box: read what we have
		}
		body := b[off+hdr : off+size]
		switch typ {
		case "jxlc":
			if bytes.HasPrefix(body, []byte{0xFF, 0x0A}) {
				return jxlCodestreamSize(body[2:])
			}
		case "jxlp":
			if len(body) > 4 && bytes.HasPrefix(body[4:], []byte{0xFF, 0x0A}) {
				return jxlCodestreamSize(body[6:])
			}
		}
		off += size
	}
	return 0, 0
}

type bitReader struct {
	b   []byte
	pos int // bit position
}

func (r *bitReader) u(n int) uint32 {
	var v uint32
	for i := 0; i < n; i++ {
		byteIdx := r.pos / 8
		if byteIdx >= len(r.b) {
			return v
		}
		bit := (r.b[byteIdx] >> (r.pos % 8)) & 1 // LSB first
		v |= uint32(bit) << i
		r.pos++
	}
	return v
}

// sizeDim reads one dimension of the SizeHeader.
func (r *bitReader) sizeDim(small bool) uint32 {
	if small {
		return 8 * (1 + r.u(5))
	}
	switch r.u(2) {
	case 0:
		return 1 + r.u(9)
	case 1:
		return 1 + r.u(13)
	case 2:
		return 1 + r.u(18)
	default:
		return 1 + r.u(30)
	}
}

var jxlRatios = [8][2]uint32{{0, 0}, {1, 1}, {12, 10}, {4, 3}, {3, 2}, {16, 9}, {5, 4}, {2, 1}}

// jxlCodestreamSize decodes the SizeHeader that follows the FF 0A signature.
func jxlCodestreamSize(b []byte) (int, int) {
	r := &bitReader{b: b}
	small := r.u(1) == 1
	h := r.sizeDim(small)
	ratio := r.u(3)
	var w uint32
	if ratio == 0 {
		w = r.sizeDim(small)
	} else {
		w = uint32(uint64(h) * uint64(jxlRatios[ratio][0]) / uint64(jxlRatios[ratio][1]))
	}
	return int(w), int(h)
}
