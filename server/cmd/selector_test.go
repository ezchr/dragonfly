package cmd

import (
	"testing"

	"github.com/go-gl/mathgl/mgl64"
)

type fakeTarget struct {
	name string
	pos  mgl64.Vec3
}

func (f fakeTarget) Position() mgl64.Vec3 { return f.pos }
func (f fakeTarget) Name() string         { return f.name }

func TestParseSelector(t *testing.T) {
	src := mgl64.Vec3{10, 64, 10}
	kind, sel, err := parseSelector("@E[type=!zid:gubby, r=5,c=-2,x=~1,y=70,dx=3]", src)
	if err != nil || kind != "@e" {
		t.Fatalf("kind %q err %v", kind, err)
	}
	if sel.origin != (mgl64.Vec3{11, 70, 10}) || !sel.hasR || sel.r != 5 || sel.count != -2 || !sel.hasVol || sel.volume[0] != 3 {
		t.Errorf("parsed %+v", sel)
	}
	if len(sel.types) != 1 || !sel.types[0].negate || sel.types[0].value != "zid:gubby" {
		t.Errorf("types %+v", sel.types)
	}
	for _, bad := range []string{"@e[type=gubby", "@e[tag=x]", "@e[c=two]", "@e[r]"} {
		if _, _, err := parseSelector(bad, src); err == nil {
			t.Errorf("%q parsed without error", bad)
		}
	}
}

func TestTypeMatches(t *testing.T) {
	for _, c := range []struct {
		id, want string
		ok       bool
	}{
		{"zid:gubby", "gubby", true}, {"zid:gubby", "zid:gubby", true}, {"zid:gubby", "ZID:Gubby", true},
		{"minecraft:item", "item", true}, {"zid:gubby", "minecraft:gubby", false}, {"minecraft:player", "gubby", false},
	} {
		if typeMatches(c.id, c.want) != c.ok {
			t.Errorf("typeMatches(%q, %q) != %v", c.id, c.want, c.ok)
		}
	}
}

func TestSelectTargets(t *testing.T) {
	src := mgl64.Vec3{}
	pool := []Target{
		fakeTarget{"far", mgl64.Vec3{20, 0, 0}},
		fakeTarget{"near", mgl64.Vec3{2, 0, 0}},
		fakeTarget{"mid", mgl64.Vec3{8, 0, 0}},
	}
	names := func(s string) (out []string) {
		kind, sel, err := parseSelector(s, src)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		for _, t := range sel.selectTargets(kind, pool) {
			out = append(out, t.(fakeTarget).name)
		}
		return
	}
	check := func(s string, want ...string) {
		got := names(s)
		if len(got) != len(want) {
			t.Errorf("%s = %v, want %v", s, got, want)
			return
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%s = %v, want %v", s, got, want)
				return
			}
		}
	}
	check("@e", "far", "near", "mid")
	check("@p", "near")
	check("@e[c=2]", "near", "mid")
	check("@e[c=-1]", "far")
	check("@e[r=10]", "near", "mid")
	check("@e[rm=5]", "far", "mid")
	check("@e[name=!mid,c=5]", "near", "far")
	check("@e[x=19,y=0,z=0,dx=2,dy=0,dz=0]", "far")
	check("@r[name=mid]", "mid")
}
