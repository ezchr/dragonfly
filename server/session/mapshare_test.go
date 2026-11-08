package session

import (
	"image/color"
	"testing"
)

func TestMapPixelsShared(t *testing.T) {
	pic := make([]color.RGBA, MapSize*MapSize)
	pic[5] = color.RGBA{R: 200, A: 255}
	SetMapPixels(-9001, pic)
	SetMapPixels(-9002, pic)
	a, _ := MapPixels(-9001)
	b, _ := MapPixels(-9002)
	if &a[0] != &b[0] {
		t.Fatal("the same picture must be kept once")
	}
	other := make([]color.RGBA, MapSize*MapSize)
	SetMapPixels(-9002, other)
	if c, _ := MapPixels(-9002); c[5] == pic[5] {
		t.Fatal("a new picture must replace the old one")
	}
	RemoveMap(-9001)
	RemoveMap(-9002)
	mapsMu.Lock()
	defer mapsMu.Unlock()
	if len(sharedPixels) != 0 {
		t.Fatalf("pictures no map shows must be let go, %d left", len(sharedPixels))
	}
}
