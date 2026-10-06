package wire

import "math"

// Low-precision vectors (net.minecraft.network.LpVec3), used for entity velocity: a zero vector is
// one byte; otherwise 6 bytes hold a 2-bit scale (plus a continuation flag) and three 15-bit
// components in [-1, 1] multiplied by the scale, with a VarInt for the rest of a scale over 3.

const (
	lpMin = 3.051944088384301e-5
	lpMax = 1.7179869183e10
)

func lpSanitize(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(-lpMax, math.Min(lpMax, v))
}

func lpPack(v float64) uint64 { return uint64(math.Floor((v*0.5+0.5)*32766 + 0.5)) }

func lpUnpack(v uint64) float64 { return math.Min(float64(v&32767), 32766)*2/32766 - 1 }

// LpVec3 writes a low-precision vector.
func (w *Writer) LpVec3(x, y, z float64) {
	x, y, z = lpSanitize(x), lpSanitize(y), lpSanitize(z)
	m := math.Max(math.Abs(x), math.Max(math.Abs(y), math.Abs(z)))
	if m < lpMin {
		w.Byte(0)
		return
	}
	scale := int64(math.Ceil(m))
	partial := scale&3 != scale
	markers := uint64(scale)
	if partial {
		markers = uint64(scale&3) | 4
	}
	s := float64(scale)
	buf := markers | lpPack(x/s)<<3 | lpPack(y/s)<<18 | lpPack(z/s)<<33
	w.Byte(byte(buf))
	w.Byte(byte(buf >> 8))
	w.Int32(int32(buf >> 16))
	if partial {
		w.VarInt(int32(scale >> 2))
	}
}

// LpVec3 reads a low-precision vector.
func (r *Reader) LpVec3() (x, y, z float64) {
	b0 := r.Byte()
	if b0 == 0 || r.Err != nil {
		return 0, 0, 0
	}
	b1 := r.Byte()
	hi := uint32(r.Int32())
	buf := uint64(b0) | uint64(b1)<<8 | uint64(hi)<<16
	scale := buf & 3
	if buf&4 != 0 {
		scale |= (uint64(uint32(r.VarInt())) & 0xffffffff) << 2
	}
	s := float64(scale)
	return lpUnpack(buf>>3) * s, lpUnpack(buf>>18) * s, lpUnpack(buf>>33) * s
}
