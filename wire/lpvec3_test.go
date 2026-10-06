package wire

import (
	"bytes"
	"encoding/hex"
	"math"
	"testing"
)

// A zombie's velocity from a vanilla 26.3 add_entity packet.
func TestLpVec3Vanilla(t *testing.T) {
	raw, _ := hex.DecodeString("f9ff7ffeebed")
	r := NewReader(raw)
	x, y, z := r.LpVec3()
	if r.Err != nil || r.Len() != 0 {
		t.Fatalf("decode: err %v, %d left", r.Err, r.Len())
	}
	var w Writer
	w.LpVec3(x, y, z)
	if !bytes.Equal(w.B, raw) {
		t.Fatalf("re-encoded % x, want % x (decoded %v %v %v)", w.B, raw, x, y, z)
	}
}

func TestLpVec3RoundTrip(t *testing.T) {
	for _, v := range [][3]float64{{0, 0, 0}, {0.1, -0.0784, 0}, {0.4, 0.36, -0.4}, {3.9, 0, 0}, {-12.5, 7, 0.25}, {1e-6, 0, 0}} {
		var w Writer
		w.LpVec3(v[0], v[1], v[2])
		x, y, z := NewReader(w.B).LpVec3()
		tol := math.Ceil(math.Max(math.Abs(v[0]), math.Max(math.Abs(v[1]), math.Abs(v[2])))) * 2 / 32766
		if math.Abs(x-v[0]) > tol+lpMin || math.Abs(y-v[1]) > tol+lpMin || math.Abs(z-v[2]) > tol+lpMin {
			t.Fatalf("%v: got %v %v %v", v, x, y, z)
		}
	}
}
