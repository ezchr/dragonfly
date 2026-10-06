package cmd

import (
	"math"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

type fakeSource struct {
	pos mgl64.Vec3
	rot cube.Rotation
}

func (f fakeSource) Position() mgl64.Vec3      { return f.pos }
func (f fakeSource) Rotation() cube.Rotation   { return f.rot }
func (f fakeSource) SendCommandOutput(*Output) {}

func TestVec3Coordinates(t *testing.T) {
	// Yaw 0 faces south (+z), yaw 90 faces west (-x).
	south := fakeSource{pos: mgl64.Vec3{10, 64, 10}}
	west := fakeSource{pos: mgl64.Vec3{10, 64, 10}, rot: cube.Rotation{90, 0}}
	for _, c := range []struct {
		src  fakeSource
		args []string
		want mgl64.Vec3
		rest int
	}{
		{south, []string{"1", "2.5", "-3"}, mgl64.Vec3{1, 2.5, -3}, 0},
		{south, []string{"~", "~5", "~-2", "stone"}, mgl64.Vec3{10, 69, 8}, 1},
		{south, []string{"~~1~", "stone"}, mgl64.Vec3{10, 65, 10}, 1},
		{south, []string{"~", "70", "~"}, mgl64.Vec3{10, 70, 10}, 0},
		{south, []string{"^", "^", "^2"}, mgl64.Vec3{10, 64, 12}, 0},
		{south, []string{"^1", "^", "^"}, mgl64.Vec3{11, 64, 10}, 0}, // left of south is east
		{west, []string{"^^^3"}, mgl64.Vec3{7, 64, 10}, 0},
		{west, []string{"^", "^2", "^"}, mgl64.Vec3{10, 66, 10}, 0},
	} {
		line := &Line{args: c.args, src: c.src}
		var got mgl64.Vec3
		if err := (parser{}).vec3(line, reflect.ValueOf(&got).Elem()); err != nil {
			t.Errorf("%v: %v", c.args, err)
			continue
		}
		line.RemoveNext() // parseArgument removes the last one
		for i := range got {
			if math.Abs(got[i]-c.want[i]) > 1e-9 {
				t.Errorf("%v = %v, want %v", c.args, got, c.want)
				break
			}
		}
		if line.Len() != c.rest {
			t.Errorf("%v left %d arguments, want %d", c.args, line.Len(), c.rest)
		}
	}
	for _, bad := range [][]string{{"~", "^", "~"}, {"1", "x", "3"}, {"~", "~"}, {"~a", "0", "0"}} {
		var got mgl64.Vec3
		if err := (parser{}).vec3(&Line{args: bad, src: south}, reflect.ValueOf(&got).Elem()); err == nil {
			t.Errorf("%v parsed as %v", bad, got)
		}
	}
}
