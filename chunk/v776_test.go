package chunk

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ezchr/go-mcjava/wire"
)

// 26.2 (protocol 776) chunks: the same layout as 26.3 except the light masks, which are long
// arrays. Vanilla 26.2 captures decode and encode back to the same bytes.
func TestV776Captures(t *testing.T) {
	files, _ := filepath.Glob("testdata/v776/*.bin")
	if len(files) == 0 {
		t.Fatal("no 26.2 captures")
	}
	const states776, biomes776 = 32366, 66
	dec := NewDecoder(states776, biomes776)
	dec.LongMasks = true
	enc := NewEncoder(states776, biomes776)
	enc.LongMasks = true
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var c Column
		r := wire.NewReader(body)
		if err := dec.Decode(r, &c); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if r.Len() != 0 {
			t.Fatalf("%s: %d bytes left", f, r.Len())
		}
		var w wire.Writer
		if err := enc.Encode(&w, &c); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(w.B, body) {
			t.Errorf("%s: re-encoded body differs (%d vs %d bytes)", f, len(w.B), len(body))
		}
	}
}
