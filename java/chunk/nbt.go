package chunk

import (
	"fmt"

	"github.com/ezchr/go-mc/java/wire"
)

const (
	tagCompound = 10
	maxNBTDepth = 512 // NbtAccounter's default depth limit
)

// validCompound reports whether b is exactly one network NBT compound (0x0a + payload).
func validCompound(b []byte) bool {
	if len(b) == 0 || b[0] != tagCompound {
		return false
	}
	r := wire.NewReader(b)
	r.Off = 1
	skipPayload(r, tagCompound, 0)
	return r.Err == nil && r.Len() == 0
}

// skipPayload skips the payload of a tag of type t, failing r on malformed or too deep data.
func skipPayload(r *wire.Reader, t byte, depth int) {
	if depth > maxNBTDepth {
		fail(r, fmt.Errorf("%w: nested deeper than %d", ErrNBT, maxNBTDepth))
		return
	}
	switch t {
	case 1:
		skip(r, 1)
	case 2:
		skip(r, 2)
	case 3, 5:
		skip(r, 4)
	case 4, 6:
		skip(r, 8)
	case 7:
		skip(r, int(r.Int32()))
	case 8:
		skip(r, int(r.Uint16()))
	case 9:
		et := r.Byte()
		n := int(r.Int32())
		if r.Err != nil {
			return
		}
		if n < 0 || (et == 0 && n > 0) {
			fail(r, fmt.Errorf("%w: list of %d tags of type %d", ErrNBT, n, et))
			return
		}
		for i := 0; i < n && r.Err == nil; i++ {
			skipPayload(r, et, depth+1)
		}
	case tagCompound:
		for r.Err == nil {
			tt := r.Byte()
			if r.Err != nil || tt == 0 {
				return
			}
			skip(r, int(r.Uint16()))
			skipPayload(r, tt, depth+1)
		}
	case 11:
		n := int(r.Int32())
		if n < 0 || n > r.Len()/4 {
			fail(r, ErrNBT)
			return
		}
		skip(r, n*4)
	case 12:
		n := int(r.Int32())
		if n < 0 || n > r.Len()/8 {
			fail(r, ErrNBT)
			return
		}
		skip(r, n*8)
	default:
		fail(r, fmt.Errorf("%w: tag type %d", ErrNBT, t))
	}
}
