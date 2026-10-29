package textcolour

import "testing"

func TestNearestPicksTheCodeOfAnExactColour(t *testing.T) {
	for code, c := range Bedrock {
		if got := Nearest(c); got != code {
			t.Errorf("Nearest(%s) = %c, want %c", c.Hex(), got, code)
		}
	}
}

func TestParseStop(t *testing.T) {
	for in, want := range map[string]RGB{
		"#FF2020": {0xFF, 0x20, 0x20}, "&6": Bedrock['6'], "§g": Bedrock['g'], "c": Bedrock['c'], "&C": Bedrock['c'],
	} {
		if got, ok := ParseStop(in); !ok || got != want {
			t.Errorf("ParseStop(%q) = %v %v, want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"#FF20", "#GGGGGG", "&l", "&z", "red", ""} {
		if _, ok := ParseStop(in); ok {
			t.Errorf("ParseStop(%q) parsed", in)
		}
	}
}

func TestExpandBedrock(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		// The stops' own colours at the ends, a code only where the colour changes, spaces skipped.
		{"<gradient:&c:&c>AB C</gradient>", "§cAB C"},
		{"<gradient:#FF2020:#FFD000>THE SUNSET</gradient>", "§mT§cH§vE SU§6NS§pE§gT"},
		// Each line of a name tag gets its colour again.
		{"<gradient:&c:&c>a\nb</gradient>", "§ca\n§cb"},
		// The colour before the gradient applies again after it; with none, a reset and the styles.
		{"§7a <gradient:&c:&6>xy</gradient> b", "§7a §cx§6y§7 b"},
		{"§la<gradient:&c:&6>xy</gradient>b", "§la§cx§6y§r§lb"},
		// No closing tag: to the end. Bold inside the gradient is kept.
		{"<gradient:&c:&6>§lxy", "§l§cx§6y"},
		// Not a gradient: left as written.
		{"<gradient:&c>x</gradient>", "<gradient:&c>x</gradient>"},
		{"<gradient:&c:nope>x", "<gradient:&c:nope>x"},
		{"plain §ctext", "plain §ctext"},
	} {
		if got := Expand(tc.in, false); got != tc.want {
			t.Errorf("Expand(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestExpandJavaIsExact(t *testing.T) {
	got := Expand("<gradient:#FF0000:#0000FF>abc</gradient>!", true)
	want := "§#FF0000a§#" + toLab(RGB{0xFF, 0, 0}).mix(toLab(RGB{0, 0, 0xFF}), 0.5).rgb().Hex()[1:] + "b§#0000FFc§r!"
	if got != want {
		t.Errorf("Expand = %q, want %q", got, want)
	}
}

// mix is c blended with o by f (for the test's middle letter).
func (c lab) mix(o lab, f float64) lab {
	for j := range c {
		c[j] += (o[j] - c[j]) * f
	}
	return c
}
