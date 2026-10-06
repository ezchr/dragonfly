// Package wire has the Java Edition protocol's data types and packet framing.
//
// Writer appends to a byte slice and never allocates beyond growing it; packet code writes its
// fields one after another, the way gophertunnel's generated packets do, instead of boxing each
// field in an interface. Reader is bounds-checked: a malformed or hostile packet (negative or huge
// lengths, truncated data) sets an error instead of panicking, and every later read returns zero
// values, so packet code can read all fields and check Err once at the end.
package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"
)

// Writer builds a packet body.
type Writer struct {
	B []byte
}

// Reset empties the writer, keeping its buffer.
func (w *Writer) Reset() { w.B = w.B[:0] }

func (w *Writer) Byte(v byte)   { w.B = append(w.B, v) }
func (w *Writer) Int8(v int8)   { w.B = append(w.B, byte(v)) }
func (w *Writer) Raw(b []byte)  { w.B = append(w.B, b...) }
func (w *Writer) Int16(v int16) { w.B = binary.BigEndian.AppendUint16(w.B, uint16(v)) }
func (w *Writer) Uint16(v uint16) {
	w.B = binary.BigEndian.AppendUint16(w.B, v)
}
func (w *Writer) Int32(v int32)     { w.B = binary.BigEndian.AppendUint32(w.B, uint32(v)) }
func (w *Writer) Int64(v int64)     { w.B = binary.BigEndian.AppendUint64(w.B, uint64(v)) }
func (w *Writer) Uint64(v uint64)   { w.B = binary.BigEndian.AppendUint64(w.B, v) }
func (w *Writer) Float32(v float32) { w.Int32(int32(math.Float32bits(v))) }
func (w *Writer) Float64(v float64) { w.Int64(int64(math.Float64bits(v))) }

func (w *Writer) Bool(v bool) {
	if v {
		w.B = append(w.B, 1)
	} else {
		w.B = append(w.B, 0)
	}
}

// VarInt writes a LEB128-style variable length int32 (two's complement, at most 5 bytes).
func (w *Writer) VarInt(v int32) { w.B = AppendVarInt(w.B, v) }

// AppendVarInt appends v as a VarInt.
func AppendVarInt(b []byte, v int32) []byte {
	u := uint32(v)
	for u >= 0x80 {
		b = append(b, byte(u)|0x80)
		u >>= 7
	}
	return append(b, byte(u))
}

// VarIntSize is the number of bytes v takes as a VarInt.
func VarIntSize(v int32) int {
	u := uint32(v)
	n := 1
	for u >= 0x80 {
		u >>= 7
		n++
	}
	return n
}

// VarLong writes a variable length int64 (at most 10 bytes).
func (w *Writer) VarLong(v int64) {
	u := uint64(v)
	for u >= 0x80 {
		w.B = append(w.B, byte(u)|0x80)
		u >>= 7
	}
	w.B = append(w.B, byte(u))
}

// String writes a VarInt byte length and the UTF-8 bytes.
func (w *Writer) String(s string) {
	w.VarInt(int32(len(s)))
	w.B = append(w.B, s...)
}

// ByteArray writes a VarInt length and the bytes.
func (w *Writer) ByteArray(b []byte) {
	w.VarInt(int32(len(b)))
	w.B = append(w.B, b...)
}

// UUID writes 16 bytes, most significant first.
func (w *Writer) UUID(u [16]byte) { w.B = append(w.B, u[:]...) }

// Position packs a block position into one long: x 26 bits, z 26 bits, y 12 bits.
func (w *Writer) Position(x, y, z int) {
	w.Int64(int64(x&0x3FFFFFF)<<38 | int64(z&0x3FFFFFF)<<12 | int64(y&0xFFF))
}

// Angle writes an angle in degrees as 1/256 steps of a turn, rounded down like vanilla's
// Mth.packDegrees (floor, so -0.5 degrees is step 255, not 0). Angles of any size wrap; NaN and
// infinities are written as 0.
func (w *Writer) Angle(deg float32) {
	w.B = append(w.B, AngleByte(deg))
}

// AngleByte is the byte Angle writes for deg.
func AngleByte(deg float32) byte {
	f := math.Floor(float64(deg * 256 / 360)) // the multiply and divide in float32, like vanilla
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	f = math.Mod(f, 256) // exact for any float64 integer; keeps huge angles meaningful
	if f < 0 {
		f += 256
	}
	return byte(int(f))
}

// BitSet writes a VarInt count of longs and the longs.
func (w *Writer) BitSet(longs []uint64) {
	w.VarInt(int32(len(longs)))
	for _, l := range longs {
		w.Uint64(l)
	}
}

// Errors returned by Reader.
var (
	ErrShort    = errors.New("wire: packet ended early")
	ErrVarInt   = errors.New("wire: VarInt too long")
	ErrTooLarge = errors.New("wire: length out of range")
	ErrUTF8     = errors.New("wire: string is not valid UTF-8")
)

// Reader reads a packet body. The first failure is kept in Err and later reads return zero values.
type Reader struct {
	B   []byte
	Off int
	Err error
}

// NewReader reads b.
func NewReader(b []byte) *Reader { return &Reader{B: b} }

// Len is the number of unread bytes.
func (r *Reader) Len() int { return len(r.B) - r.Off }

func (r *Reader) fail(err error) {
	if r.Err == nil {
		r.Err = err
	}
	r.Off = len(r.B)
}

// take returns the next n bytes, or nil (and an error) if there aren't n left.
func (r *Reader) take(n int) []byte {
	if r.Err != nil {
		return nil
	}
	if n < 0 || n > r.Len() {
		r.fail(ErrShort)
		return nil
	}
	b := r.B[r.Off : r.Off+n]
	r.Off += n
	return b
}

func (r *Reader) Byte() byte {
	if b := r.take(1); b != nil {
		return b[0]
	}
	return 0
}
func (r *Reader) Int8() int8 { return int8(r.Byte()) }
func (r *Reader) Bool() bool { return r.Byte() != 0 }
func (r *Reader) Int16() int16 {
	if b := r.take(2); b != nil {
		return int16(binary.BigEndian.Uint16(b))
	}
	return 0
}
func (r *Reader) Uint16() uint16 { return uint16(r.Int16()) }
func (r *Reader) Int32() int32 {
	if b := r.take(4); b != nil {
		return int32(binary.BigEndian.Uint32(b))
	}
	return 0
}
func (r *Reader) Int64() int64 {
	if b := r.take(8); b != nil {
		return int64(binary.BigEndian.Uint64(b))
	}
	return 0
}
func (r *Reader) Uint64() uint64   { return uint64(r.Int64()) }
func (r *Reader) Float32() float32 { return math.Float32frombits(uint32(r.Int32())) }
func (r *Reader) Float64() float64 { return math.Float64frombits(uint64(r.Int64())) }

// VarInt reads a VarInt.
func (r *Reader) VarInt() int32 {
	var u uint32
	for i := 0; i < 5; i++ {
		b := r.Byte()
		if r.Err != nil {
			return 0
		}
		u |= uint32(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return int32(u)
		}
	}
	r.fail(ErrVarInt)
	return 0
}

// VarLong reads a VarLong.
func (r *Reader) VarLong() int64 {
	var u uint64
	for i := 0; i < 10; i++ {
		b := r.Byte()
		if r.Err != nil {
			return 0
		}
		u |= uint64(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return int64(u)
		}
	}
	r.fail(ErrVarInt)
	return 0
}

// length reads a VarInt length and checks it against max and the bytes left (each element is at
// least minElem bytes), so a hostile length can't make the caller allocate gigabytes.
func (r *Reader) length(max, minElem int) int {
	n := r.VarInt()
	if r.Err != nil {
		return 0
	}
	if n < 0 || int(n) > max || int(n)*minElem > r.Len() {
		r.fail(fmt.Errorf("%w: %d", ErrTooLarge, n))
		return 0
	}
	return int(n)
}

// String reads a string of at most maxChars characters (the protocol's limit for that field).
// Characters are counted like Java's String.length(): in UTF-16 units, so a character outside the
// Basic Multilingual Plane (an emoji) counts as 2.
func (r *Reader) String(maxChars int) string {
	n := r.length(maxChars*3, 1)
	b := r.take(n)
	if b == nil {
		return ""
	}
	if !utf8.Valid(b) {
		r.fail(ErrUTF8)
		return ""
	}
	if UTF16Len(b) > maxChars {
		r.fail(fmt.Errorf("%w: string longer than %d", ErrTooLarge, maxChars))
		return ""
	}
	return string(b)
}

// UTF16Len is the length of the valid UTF-8 b in UTF-16 units (Java's String.length()).
func UTF16Len(b []byte) int {
	n := 0
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c < 0x80:
			i++
		case c < 0xe0:
			i += 2
		case c < 0xf0:
			i += 3
		default: // 4-byte sequence: a supplementary character, a surrogate pair in UTF-16
			i += 4
			n++
		}
		n++
	}
	return n
}

// ByteArray reads a VarInt length and that many bytes (at most max). The result aliases the
// packet buffer: copy it to keep it.
func (r *Reader) ByteArray(max int) []byte { return r.take(r.length(max, 1)) }

// Rest returns the unread bytes (aliasing the packet buffer).
func (r *Reader) Rest() []byte { return r.take(r.Len()) }

// UUID reads 16 bytes.
func (r *Reader) UUID() (u [16]byte) {
	if b := r.take(16); b != nil {
		copy(u[:], b)
	}
	return
}

// Position reads a packed block position.
func (r *Reader) Position() (x, y, z int) {
	v := r.Int64()
	return int(v >> 38), int(v << 52 >> 52), int(v << 26 >> 38)
}

// Angle reads an angle into degrees.
func (r *Reader) Angle() float32 { return float32(r.Int8()) * 360 / 256 }

// BitSet reads a VarInt count of longs (at most max) and the longs.
func (r *Reader) BitSet(max int) []uint64 {
	n := r.length(max, 8)
	if n == 0 {
		return nil
	}
	out := make([]uint64, n)
	for i := range out {
		out[i] = r.Uint64()
	}
	return out
}
