package javasession

import "testing"

func TestGradientAroundName(t *testing.T) {
	b, a := gradientAroundName("§f[§cWOLVES§r] Owner <gradient:c:6>", "</gradient>")
	if b != "§f[§cWOLVES§r] Owner §c" || a != "" {
		t.Fatalf("got %q %q", b, a)
	}
	b, a = gradientAroundName("§7x ", " §ey")
	if b != "§7x " || a != " §ey" {
		t.Fatalf("plain changed: %q %q", b, a)
	}
}

func TestTeamColourOfBedrockShades(t *testing.T) {
	for name, want := range map[string]int32{"red": 12, "#FF5454": 12, "#C5C5C5": 7, "#447FFF": 9, "#EE222C": 12, "#EFCE16": 6, "nope": -1} {
		if got := teamColour(name); got != want {
			t.Errorf("teamColour(%q) = %d, want %d", name, got, want)
		}
	}
}
