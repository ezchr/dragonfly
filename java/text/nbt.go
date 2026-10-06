package text

import (
	"github.com/ezchr/go-mc/java/wire"
)

// NBT tag types.
const (
	TagEnd       = 0
	TagByte      = 1
	TagShort     = 2
	TagInt       = 3
	TagLong      = 4
	TagFloat     = 5
	TagDouble    = 6
	TagByteArray = 7
	TagString    = 8
	TagList      = 9
	TagCompound  = 10
	TagIntArray  = 11
	TagLongArray = 12
)

// Small helpers for writing network NBT by hand. A compound is written as its tag type (or a Key),
// then the entries, then TagEnd.

// String writes an NBT string payload: Java's DataOutput.writeUTF, a u16 byte length and modified
// UTF-8 (NUL as C0 80, supplementary characters as surrogate pairs). Longer strings are cut at
// 65535 bytes on a character boundary.
func String(w *wire.Writer, s string) {
	start := len(w.B)
	w.B = append(w.B, 0, 0)
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] == 0 || s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii && len(s) <= 0xffff {
		w.B = append(w.B, s...)
	} else {
		for _, r := range s {
			before := len(w.B)
			switch {
			case r == 0:
				w.B = append(w.B, 0xc0, 0x80)
			case r < 0x80:
				w.B = append(w.B, byte(r))
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
			if len(w.B)-start-2 > 0xffff {
				w.B = w.B[:before]
				break
			}
		}
	}
	n := len(w.B) - start - 2
	w.B[start], w.B[start+1] = byte(n>>8), byte(n)
}

// Key starts a named entry of a compound: the tag type and the name.
func Key(w *wire.Writer, tag byte, name string) {
	w.Byte(tag)
	String(w, name)
}

// StringTag writes a named string entry.
func StringTag(w *wire.Writer, name, v string) {
	Key(w, TagString, name)
	String(w, v)
}

// ByteTag writes a named byte entry (booleans are bytes).
func ByteTag(w *wire.Writer, name string, v byte) {
	Key(w, TagByte, name)
	w.Byte(v)
}

// BoolTag writes a named boolean (a byte, 1 or 0).
func BoolTag(w *wire.Writer, name string, v bool) {
	b := byte(0)
	if v {
		b = 1
	}
	ByteTag(w, name, b)
}

// IntTag writes a named int entry.
func IntTag(w *wire.Writer, name string, v int32) {
	Key(w, TagInt, name)
	w.Int32(v)
}

// FloatTag writes a named float entry.
func FloatTag(w *wire.Writer, name string, v float32) {
	Key(w, TagFloat, name)
	w.Float32(v)
}

// ComponentTag writes a named component entry (a string or a compound).
func ComponentTag(w *wire.Writer, name string, c *Component) {
	if c.isString() {
		StringTag(w, name, c.Text)
		return
	}
	Key(w, TagCompound, name)
	c.writeCompound(w)
}

// ListStart starts a named list of n elements of the tag type elem; the n payloads follow.
func ListStart(w *wire.Writer, name string, elem byte, n int) {
	Key(w, TagList, name)
	w.Byte(elem)
	w.Int32(int32(n))
}

// javaHash is java.lang.String.hashCode (over UTF-16 code units; keys here are ASCII).
func javaHash(s string) int32 {
	var h int32
	for _, r := range s {
		if r >= 0x10000 {
			r -= 0x10000
			h = 31*h + (0xd800 + r>>10)
			h = 31*h + (0xdc00 + r&0x3ff)
			continue
		}
		h = 31*h + r
	}
	return h
}

// javaBucket is the bucket of key s in a java.util.HashMap with n buckets: HashMap iterates
// buckets in order, which is the order vanilla writes a CompoundTag's entries.
func javaBucket(s string, n int) int {
	h := uint32(javaHash(s))
	return int((h ^ h>>16) & uint32(n-1))
}

// sortKeys orders keys (inserted in codec order) as a 16-bucket HashMap iterates them: by bucket,
// ties in insertion order (a stable insertion sort; at most 15 keys).
func sortKeys(keys []uint8, bucket *[numKeys]uint8) {
	for i := 1; i < len(keys); i++ {
		k := keys[i]
		j := i
		for j > 0 && bucket[keys[j-1]] > bucket[k] {
			keys[j] = keys[j-1]
			j--
		}
		keys[j] = k
	}
}
