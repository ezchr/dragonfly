package wire

import (
	"bytes"
	"compress/zlib"
	"errors"
	"io"
	"math"
	"net"
	"strings"
	"testing"
	"time"
)

// TestAngleFloor checks Angle against vanilla's Mth.packDegrees: (byte) floor(deg * 256f / 360f).
func TestAngleFloor(t *testing.T) {
	for deg, want := range map[float32]byte{0: 0, -0.5: 255, 0.5: 0, 1.40625: 1, -1.40625: 255, -1.5: 254,
		90: 64, -90: 192, 180: 128, 359.9: 255, 360: 0, -360: 0, 720.5: 0, -720.5: 255} {
		if got := AngleByte(deg); got != want {
			t.Errorf("%v: %d, want %d", deg, got, want)
		}
	}
	// Against the formula for a sweep of values (Java's floor then the low 8 bits).
	for d := float32(-2000); d < 2000; d += 0.37 {
		f := math.Floor(float64(d * 256 / 360))
		want := byte(int64(f))
		if got := AngleByte(d); got != want {
			t.Fatalf("%v: %d, want %d", d, got, want)
		}
	}
	// Huge, infinite and NaN angles must not depend on an implementation-defined conversion.
	for _, d := range []float32{1e20, -1e20, float32(math.Inf(1)), float32(math.Inf(-1)), float32(math.NaN())} {
		var w Writer
		w.Angle(d)
		if len(w.B) != 1 {
			t.Fatalf("%v: %d bytes", d, len(w.B))
		}
	}
	if AngleByte(float32(math.NaN())) != 0 {
		t.Error("NaN not 0")
	}
	// 2^24 steps is a multiple of 256: a large but exact angle still wraps to 0.
	if got := AngleByte(float32(1<<24) * 360 / 256); got != 0 {
		t.Errorf("2^24 steps: %d", got)
	}
}

// TestStringUTF16 checks that string limits count UTF-16 units, like Java's String.length().
func TestStringUTF16(t *testing.T) {
	emoji := "\U0001F600" // 2 UTF-16 units, 4 UTF-8 bytes
	var w Writer
	w.String(strings.Repeat(emoji, 128))
	if s := NewReader(w.B).String(256); s == "" {
		t.Fatal("128 emoji (256 units) rejected with limit 256")
	}
	r := NewReader(w.B)
	if r.String(255); r.Err == nil {
		t.Fatal("128 emoji (256 units) accepted with limit 255")
	}
	w.Reset()
	w.String(strings.Repeat(emoji, 129))
	r = NewReader(w.B)
	if r.String(256); r.Err == nil {
		t.Fatal("129 emoji (258 units) accepted with limit 256")
	}
	for s, n := range map[string]int{"": 0, "abc": 3, "é": 1, "€": 1, emoji: 2, "a" + emoji + "é": 4} {
		if got := UTF16Len([]byte(s)); got != n {
			t.Errorf("UTF16Len(%q) = %d, want %d", s, got, n)
		}
	}
}

// fakeConn is a net.Conn reading from a fixed byte stream.
type fakeConn struct {
	net.Conn
	r io.Reader
}

func (f *fakeConn) Read(b []byte) (int, error) { return f.r.Read(b) }
func (f *fakeConn) Close() error               { return nil }

func readerConn(b []byte) *Conn { return NewConn(&fakeConn{r: bytes.NewReader(b)}) }

// TestFrameNotPreallocated: a frame length alone (the attack: "ff ff 7f" and nothing else) must not
// make the reader allocate the whole frame.
func TestFrameNotPreallocated(t *testing.T) {
	c := readerConn([]byte{0xff, 0xff, 0x7f, 0x00, 1, 2, 3})
	if _, _, err := c.ReadPacket(); err == nil {
		t.Fatal("truncated frame accepted")
	}
	if cap(c.frame) > readChunk {
		t.Fatalf("frame buffer of %d bytes for 4 bytes of data", cap(c.frame))
	}
	// readGrowing on its own: a 100 MB claim with 10 bytes behind it.
	buf, err := readGrowing(bytes.NewReader(make([]byte, 10)), nil, 100<<20)
	if !errors.Is(err, io.ErrUnexpectedEOF) || cap(buf) > readChunk || len(buf) != 10 {
		t.Fatalf("readGrowing: err %v, len %d cap %d", err, len(buf), cap(buf))
	}
	// A real big frame still reads.
	body := bytes.Repeat([]byte{9}, 300000)
	var w Writer
	w.VarInt(int32(len(body) + 1))
	w.VarInt(0x42)
	w.Raw(body)
	c = readerConn(w.B)
	id, got, err := c.ReadPacket()
	if err != nil || id != 0x42 || !bytes.Equal(got, body) {
		t.Fatalf("big frame: id %#x len %d err %v", id, len(got), err)
	}
}

// compressedFrame builds a compressed frame that claims dataLen bytes and holds data.
func compressedFrame(dataLen int, data []byte) []byte {
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write(data)
	zw.Close()
	var inner Writer
	inner.VarInt(int32(dataLen))
	inner.Raw(z.Bytes())
	var w Writer
	w.VarInt(int32(len(inner.B)))
	w.Raw(inner.B)
	return w.B
}

func TestInflateExactAndBounded(t *testing.T) {
	// 8 MiB of zeros compresses to about 8 KB: the claim is honest, it must work.
	big := make([]byte, MaxDecompressed)
	big[0] = 0x33 // packet id
	small := []byte{0x34, 1, 2, 3}
	stream := append(compressedFrame(len(big), big), compressedFrame(300, append(small, make([]byte, 296)...))...)
	c := readerConn(stream)
	c.SetThreshold(256)
	id, body, err := c.ReadPacket()
	if err != nil || id != 0x33 || len(body) != len(big)-1 {
		t.Fatalf("8 MiB packet: id %#x len %d err %v", id, len(body), err)
	}
	if cap(c.inflated) > 2*MaxDecompressed {
		t.Fatalf("inflate buffer %d bytes for %d", cap(c.inflated), len(big))
	}
	// The next packet drops the big buffer.
	if id, _, err = c.ReadPacket(); err != nil || id != 0x34 {
		t.Fatalf("small packet: %#x %v", id, err)
	}
	if cap(c.inflated) > keepBuffer {
		t.Fatalf("big inflate buffer kept: %d bytes", cap(c.inflated))
	}

	// Claims 8 MiB, holds 300 bytes: rejected, and the buffer only grew as far as the data.
	c = readerConn(compressedFrame(MaxDecompressed, make([]byte, 300)))
	c.SetThreshold(256)
	if _, _, err := c.ReadPacket(); err == nil {
		t.Fatal("short packet accepted")
	}
	if cap(c.inflated) > readChunk {
		t.Fatalf("inflate buffer of %d bytes for 300 bytes of data", cap(c.inflated))
	}
	// Inflates to more than it claims: rejected (vanilla requires the exact size).
	c = readerConn(compressedFrame(300, make([]byte, 600)))
	c.SetThreshold(256)
	if _, _, err := c.ReadPacket(); err == nil {
		t.Fatal("packet longer than its claim accepted")
	}
	// Exactly as claimed: fine.
	c = readerConn(compressedFrame(600, make([]byte, 600)))
	c.SetThreshold(256)
	if _, b, err := c.ReadPacket(); err != nil || len(b) != 599 {
		t.Fatalf("exact packet: %d %v", len(b), err)
	}
	// Corrupt checksum: rejected.
	f := compressedFrame(600, make([]byte, 600))
	f[len(f)-1] ^= 0xff
	c = readerConn(f)
	c.SetThreshold(256)
	if _, _, err := c.ReadPacket(); err == nil {
		t.Fatal("bad checksum accepted")
	}
}

func TestSetMaxPacket(t *testing.T) {
	c := readerConn(compressedFrame(100000, make([]byte, 100000)))
	c.SetThreshold(256)
	c.SetMaxPacket(1 << 16)
	if _, _, err := c.ReadPacket(); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("100 KB packet with a 64 KiB limit: %v", err)
	}
	var w Writer
	w.VarInt(70000)
	c = readerConn(append(w.B, make([]byte, 70000)...))
	c.SetMaxPacket(1 << 16)
	if _, _, err := c.ReadPacket(); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("70 KB frame with a 64 KiB limit: %v", err)
	}
	c = readerConn(append(w.B, make([]byte, 70000)...))
	c.SetMaxPacket(1 << 16)
	c.SetMaxPacket(0)
	if _, _, err := c.ReadPacket(); err != nil {
		t.Fatalf("limit not lifted: %v", err)
	}
}

// TestWriteFrameLimit: an uncompressed frame must fit a 3-byte length, or the client disconnects.
func TestWriteFrameLimit(t *testing.T) {
	for _, tc := range []struct {
		threshold, size int
		ok              bool
	}{
		{-1, MaxFrame - 1, true}, // + 1 byte id = MaxFrame
		{-1, MaxFrame, false},
		{-1, 5 << 20, false},
		{8 << 20, MaxFrame - 2, true}, // + id + the 0 data length
		{8 << 20, MaxFrame - 1, false},
		{256, 5 << 20, true}, // compressed: zeros fit easily
	} {
		a, b := net.Pipe()
		go io.Copy(io.Discard, b)
		c := NewConn(a)
		c.SetThreshold(tc.threshold)
		err := c.WritePacket(0x01, make([]byte, tc.size))
		if tc.ok != (err == nil) || err != nil && !errors.Is(err, ErrTooLarge) {
			t.Errorf("threshold %d size %d: %v", tc.threshold, tc.size, err)
		}
		a.SetDeadline(time.Now().Add(5 * time.Second))
		c.Flush()
		a.Close()
		b.Close()
	}
}

// FuzzReadPacket feeds arbitrary bytes to ReadPacket with and without compression.
func FuzzReadPacket(f *testing.F) {
	f.Add([]byte{0xff, 0xff, 0x7f, 0x00}, false)
	f.Add(compressedFrame(300, make([]byte, 300)), true)
	f.Add(compressedFrame(MaxDecompressed, make([]byte, 30)), true)
	f.Fuzz(func(t *testing.T, b []byte, compressed bool) {
		c := readerConn(b)
		if compressed {
			c.SetThreshold(256)
		}
		for i := 0; i < 8; i++ {
			if _, _, err := c.ReadPacket(); err != nil {
				return
			}
		}
	})
}
