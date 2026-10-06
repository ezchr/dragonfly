package wire

import (
	"bytes"
	"math"
	"math/rand"
	"net"
	"testing"
)

func TestVarInt(t *testing.T) {
	for _, v := range []int32{0, 1, 127, 128, 255, 25565, 2097151, math.MaxInt32, -1, math.MinInt32} {
		var w Writer
		w.VarInt(v)
		if len(w.B) != VarIntSize(v) {
			t.Fatalf("%d: size %d, VarIntSize %d", v, len(w.B), VarIntSize(v))
		}
		r := NewReader(w.B)
		if got := r.VarInt(); got != v || r.Err != nil || r.Len() != 0 {
			t.Fatalf("%d: got %d err %v left %d", v, got, r.Err, r.Len())
		}
	}
	// Known encodings from the wiki.
	for v, want := range map[int32][]byte{0: {0}, 128: {0x80, 1}, 25565: {0xdd, 0xc7, 1}, -1: {0xff, 0xff, 0xff, 0xff, 0x0f}} {
		if got := AppendVarInt(nil, v); !bytes.Equal(got, want) {
			t.Fatalf("%d: % x, want % x", v, got, want)
		}
	}
}

func TestVarLong(t *testing.T) {
	for _, v := range []int64{0, 1, 2147483648, math.MaxInt64, -1, math.MinInt64} {
		var w Writer
		w.VarLong(v)
		r := NewReader(w.B)
		if got := r.VarLong(); got != v || r.Err != nil {
			t.Fatalf("%d: got %d err %v", v, got, r.Err)
		}
	}
}

func TestPosition(t *testing.T) {
	for _, p := range [][3]int{{0, 0, 0}, {18357644, 831, -20882616}, {-1, -64, -1}, {33554431, 2047, -33554432}} {
		var w Writer
		w.Position(p[0], p[1], p[2])
		x, y, z := NewReader(w.B).Position()
		if x != p[0] || y != p[1] || z != p[2] {
			t.Fatalf("%v: got %d %d %d", p, x, y, z)
		}
	}
}

func TestReaderNeverPanics(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	buf := make([]byte, 64)
	for i := 0; i < 100000; i++ {
		rng.Read(buf[:rng.Intn(64)])
		r := NewReader(buf[:rng.Intn(64)])
		r.String(32767)
		r.ByteArray(1 << 20)
		r.BitSet(1 << 10)
		r.VarLong()
		r.UUID()
		r.Position()
	}
	// Hostile lengths: negative and huge.
	for _, l := range []int32{-1, math.MinInt32, math.MaxInt32, 1 << 30} {
		var w Writer
		w.VarInt(l)
		r := NewReader(w.B)
		if s := r.String(32767); s != "" || r.Err == nil {
			t.Fatalf("length %d accepted", l)
		}
	}
}

func TestStringLimit(t *testing.T) {
	var w Writer
	w.String("héllo")
	if s := NewReader(w.B).String(5); s != "héllo" {
		t.Fatalf("got %q", s)
	}
	r := NewReader(w.B)
	if r.String(4); r.Err == nil {
		t.Fatal("5-char string accepted with limit 4")
	}
}

// pair is two Conns talking to each other over a pipe.
func pair() (*Conn, *Conn) {
	a, b := net.Pipe()
	return NewConn(a), NewConn(b)
}

func TestConnRoundTrip(t *testing.T) {
	big := bytes.Repeat([]byte("chunk data "), 20000) // 220 KB, compresses well
	payloads := [][]byte{{}, {1, 2, 3}, bytes.Repeat([]byte{7}, 300), big}
	for _, mode := range []struct {
		name      string
		threshold int
		encrypt   bool
	}{{"plain", -1, false}, {"compressed", 256, false}, {"encrypted", -1, true}, {"both", 256, true}} {
		t.Run(mode.name, func(t *testing.T) {
			s, c := pair()
			defer s.Close()
			defer c.Close()
			secret := []byte("0123456789abcdef")
			s.SetThreshold(mode.threshold)
			c.SetThreshold(mode.threshold)
			if mode.encrypt {
				s.EnableEncryption(secret)
				c.EnableEncryption(secret)
			}
			keep := append([]byte(nil), big...)
			go func() {
				for i, p := range payloads {
					s.WritePacket(int32(i+0x20), p)
				}
				s.Flush()
			}()
			for i, p := range payloads {
				id, body, err := c.ReadPacket()
				if err != nil {
					t.Fatal(err)
				}
				if id != int32(i+0x20) || !bytes.Equal(body, p) {
					t.Fatalf("packet %d: id %#x, %d bytes (want %d)", i, id, len(body), len(p))
				}
			}
			if !bytes.Equal(big, keep) {
				t.Fatal("writing changed the caller's buffer")
			}
		})
	}
}

func BenchmarkWriteSmallPacket(b *testing.B) {
	s, c := pair()
	defer s.Close()
	defer c.Close()
	go func() {
		for {
			if _, _, err := c.ReadPacket(); err != nil {
				return
			}
		}
	}()
	s.SetThreshold(256)
	c.SetThreshold(256)
	var w Writer
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.Reset()
		w.VarInt(int32(i))
		w.Float64(1.5)
		w.Float64(64)
		w.Float64(-3.25)
		w.Bool(true)
		s.WritePacket(0x2f, w.B)
		if i%64 == 0 {
			s.Flush()
		}
	}
	s.Flush()
}
