package item

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ezchr/go-mc/java/wire"
)

// NBT tag types.
const (
	nbtEnd = iota
	nbtByte
	nbtShort
	nbtInt
	nbtLong
	nbtFloat
	nbtDouble
	nbtByteArray
	nbtString
	nbtList
	nbtCompound
	nbtIntArray
	nbtLongArray
)

// maxNBTDepth is vanilla's NBT nesting limit.
const maxNBTDepth = 512

// skipNBT skips one network NBT tag (a type byte, no name, the payload).
func skipNBT(r *wire.Reader) { skipPayload(r, r.Byte(), 0) }

func skipPayload(r *wire.Reader, typ byte, depth int) {
	if depth > maxNBTDepth {
		fail(r, fmt.Errorf("%w: NBT nested too deep", ErrInvalid))
		return
	}
	switch typ {
	case nbtEnd:
	case nbtByte:
		take(r, 1)
	case nbtShort:
		take(r, 2)
	case nbtInt, nbtFloat:
		take(r, 4)
	case nbtLong, nbtDouble:
		take(r, 8)
	case nbtByteArray:
		take(r, nbtLen(r, 1))
	case nbtIntArray:
		take(r, nbtLen(r, 4)*4)
	case nbtLongArray:
		take(r, nbtLen(r, 8)*8)
	case nbtString:
		take(r, int(r.Uint16()))
	case nbtList:
		et := r.Byte()
		n := nbtLen(r, 0)
		if et == nbtEnd && n > 0 {
			fail(r, fmt.Errorf("%w: NBT list of TAG_End", ErrInvalid))
			return
		}
		for i := 0; i < n && r.Err == nil; i++ {
			skipPayload(r, et, depth+1)
		}
	case nbtCompound:
		for r.Err == nil {
			t := r.Byte()
			if t == nbtEnd {
				return
			}
			take(r, int(r.Uint16()))
			skipPayload(r, t, depth+1)
		}
	default:
		fail(r, fmt.Errorf("%w: NBT tag type %d", ErrInvalid, typ))
	}
}

// nbtLen reads an NBT int32 length and checks it against the bytes left.
func nbtLen(r *wire.Reader, elem int) int {
	n := r.Int32()
	if r.Err != nil {
		return 0
	}
	if n < 0 || int(n)*max(elem, 1) > r.Len() {
		fail(r, fmt.Errorf("%w: NBT length %d", ErrInvalid, n))
		return 0
	}
	return int(n)
}

// writeMUTF8 writes Java's DataOutput.writeUTF form: u16 length, then UTF-8 with NUL as 0xC0 0x80 and
// supplementary characters as surrogate pairs.
func writeMUTF8(w *wire.Writer, s string) {
	start := len(w.B)
	w.Uint16(0)
	for i := 0; i < len(s); i++ {
		if c := s[i]; c != 0 && c < 0x80 {
			w.B = append(w.B, c)
			continue
		}
		r, n := utf8.DecodeRuneInString(s[i:])
		i += n - 1
		switch {
		case r == 0:
			w.B = append(w.B, 0xc0, 0x80)
		case r < 0x800:
			w.B = append(w.B, 0xc0|byte(r>>6), 0x80|byte(r&0x3f))
		case r < 0x10000:
			w.B = append(w.B, 0xe0|byte(r>>12), 0x80|byte(r>>6&0x3f), 0x80|byte(r&0x3f))
		default:
			r -= 0x10000
			hi, lo := 0xd800+r>>10, 0xdc00+r&0x3ff
			w.B = append(w.B, 0xe0|byte(hi>>12), 0x80|byte(hi>>6&0x3f), 0x80|byte(hi&0x3f),
				0xe0|byte(lo>>12), 0x80|byte(lo>>6&0x3f), 0x80|byte(lo&0x3f))
		}
	}
	n := len(w.B) - start - 2
	w.B[start] = byte(n >> 8)
	w.B[start+1] = byte(n)
}

// readMUTF8 reads a u16-length modified UTF-8 string.
func readMUTF8(r *wire.Reader) string { return mutf8(take(r, int(r.Uint16()))) }

func mutf8(b []byte) string {
	plain := true
	for _, c := range b {
		if c >= 0x80 {
			plain = false
			break
		}
	}
	if plain || (utf8.Valid(b) && !hasMUTF8Escapes(b)) {
		return string(b)
	}
	var sb strings.Builder
	sb.Grow(len(b))
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c < 0x80:
			sb.WriteByte(c)
			i++
		case c&0xe0 == 0xc0 && i+1 < len(b):
			sb.WriteRune(rune(c&0x1f)<<6 | rune(b[i+1]&0x3f))
			i += 2
		case c&0xf0 == 0xe0 && i+2 < len(b):
			u := rune(c&0x0f)<<12 | rune(b[i+1]&0x3f)<<6 | rune(b[i+2]&0x3f)
			i += 3
			if u >= 0xd800 && u < 0xdc00 && i+2 < len(b) && b[i]&0xf0 == 0xe0 {
				lo := rune(b[i]&0x0f)<<12 | rune(b[i+1]&0x3f)<<6 | rune(b[i+2]&0x3f)
				if lo >= 0xdc00 && lo < 0xe000 {
					u = 0x10000 + (u-0xd800)<<10 + (lo - 0xdc00)
					i += 3
				}
			}
			sb.WriteRune(u)
		default:
			sb.WriteRune(utf8.RuneError)
			i++
		}
	}
	return sb.String()
}

// hasMUTF8Escapes reports whether b has an encoded NUL or a surrogate (which standard UTF-8 rejects
// anyway, so this only looks for 0xC0 0x80).
func hasMUTF8Escapes(b []byte) bool {
	for i := 0; i+1 < len(b); i++ {
		if b[i] == 0xc0 && b[i+1] == 0x80 {
			return true
		}
	}
	return false
}

// PlainText returns the plain text of a chat component in network NBT: a string tag, or a compound's
// "text" (or "translate" key) followed by its "extra" components'. Styles are dropped.
func PlainText(nbt []byte) string {
	r := wire.Reader{B: nbt}
	var sb strings.Builder
	plainText(&r, r.Byte(), &sb, 0)
	return sb.String()
}

func plainText(r *wire.Reader, typ byte, sb *strings.Builder, depth int) {
	switch typ {
	case nbtString:
		sb.WriteString(readMUTF8(r))
	case nbtList:
		et := r.Byte()
		n := nbtLen(r, 0)
		for i := 0; i < n && r.Err == nil; i++ {
			plainText(r, et, sb, depth+1)
		}
	case nbtCompound:
		var extra []byte
		var extraType byte
		for r.Err == nil && depth < maxNBTDepth {
			t := r.Byte()
			if t == nbtEnd {
				break
			}
			name := take(r, int(r.Uint16()))
			switch {
			case t == nbtString && (string(name) == "text" || string(name) == "translate" || string(name) == ""):
				sb.WriteString(readMUTF8(r))
			case string(name) == "extra":
				start := r.Off
				skipPayload(r, t, depth+1)
				extra, extraType = r.B[start:r.Off], t
			default:
				skipPayload(r, t, depth+1)
			}
		}
		if extra != nil {
			plainText(&wire.Reader{B: extra}, extraType, sb, depth+1)
		}
	default:
		skipPayload(r, typ, depth)
	}
}
