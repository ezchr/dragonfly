package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDescriptor(t *testing.T) {
	cases := []struct{ from, to, desc string }{
		{"minecraft:oak_log", "minecraft:oak_log", "="},
		{"minecraft:oak_double_slab", "minecraft:oak_slab", "double>"},
		{"minecraft:redstone_torch", "minecraft:redstone_wall_torch", ">wall"},
		{"minecraft:silver_glazed_terracotta", "minecraft:light_gray_glazed_terracotta", "silver>light_gray"},
		{"minecraft:darkoak_wall_sign", "minecraft:dark_oak_wall_sign", "darkoak>dark_oak"},
	}
	for _, c := range cases {
		if d, _ := descriptor(c.from, c.to); d != c.desc {
			t.Errorf("descriptor(%s, %s) = %q, want %q", c.from, c.to, d, c.desc)
		}
	}
	java := map[string]bool{"minecraft:soul_wall_torch": true, "minecraft:cherry_slab": true, "minecraft:beetroots": true}
	ok := func(n string) bool { return java[n] }
	if got := applyDescriptor("minecraft:soul_torch", ">wall", 1, ok); !slices.Equal(got, []string{"minecraft:soul_wall_torch"}) {
		t.Errorf("apply >wall: %v", got)
	}
	if got := applyDescriptor("minecraft:cherry_double_slab", "double>", 1, ok); !slices.Equal(got, []string{"minecraft:cherry_slab"}) {
		t.Errorf("apply double>: %v", got)
	}
	if got := applyDescriptor("minecraft:beetroot", suffixDescriptor("minecraft:carrot", "minecraft:carrots"), 0, ok); !slices.Equal(got, []string{"minecraft:beetroots"}) {
		t.Errorf("apply ~>s: %v", got)
	}
}

func TestStateKeyRoundTrip(t *testing.T) {
	props := map[string]any{"pillar_axis": "y", "age": int32(3), "open_bit": uint8(1), "odd": "a,b c"}
	key := stateKey("minecraft:x", props)
	s, err := parseKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if s.name != "minecraft:x" || s.props["pillar_axis"] != "y" || s.props["age"] != "3" || s.props["open_bit"] != "1" || s.props["odd"] != "a,b c" {
		t.Fatalf("parseKey(%s) = %+v", key, s)
	}
	r, err := parseKeyRaw(key)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.keys, []string{"age", "odd", "open_bit", "pillar_axis"}) || !slices.Equal(r.vals, []string{"3", `"a,b c"`, "1", `"y"`}) {
		t.Fatalf("parseKeyRaw(%s) = %+v", key, r)
	}
}

func TestDecisionsRoundTrip(t *testing.T) {
	in := &prevTables{java: "26.3", blocks: []prevBlock{
		{stateKey("minecraft:torch", map[string]any{"torch_facing_direction": "top"}), "minecraft:torch", "geyser"},
		{stateKey("minecraft:torch", map[string]any{"torch_facing_direction": "north"}), "minecraft:wall_torch[facing=south]", "geyser"},
		{stateKey("minecraft:torch", map[string]any{"torch_facing_direction": "south"}), "minecraft:wall_torch[facing=north]", "auto:analog"},
		{stateKey("minecraft:odd", map[string]any{"v": "two words"}), "minecraft:stone", "miss"},
		{stateKey("minecraft:stone", nil), "minecraft:stone", "geyser"},
	}, items: []prevItem{
		{name: "minecraft:bed", meta: 14, hasMeta: true, java: "minecraft:red_bed", origin: "geyser"},
		{name: "minecraft:snow", java: "minecraft:snow_block", origin: "auto:block"},
	}, biomes: []prevBiome{{1, "minecraft:plains", "geyser"}},
		deps: []dep{{"minecraft:bed", []string{"<block>"}, "SourceBlockEntity"}, {"minecraft:lever", []string{"facing", "face"}, "SourceNone"}}}
	path := filepath.Join(t.TempDir(), decisionsFile)
	writeDecisions(path, in)
	out := readDecisions(path)
	sortBlocks := func(b []prevBlock) { slices.SortFunc(b, func(x, y prevBlock) int { return cmpStr(x.key, y.key) }) }
	sortBlocks(in.blocks)
	sortBlocks(out.blocks)
	if !slices.Equal(in.blocks, out.blocks) || !slices.Equal(in.items, out.items) || !slices.Equal(in.biomes, out.biomes) || out.java != in.java {
		t.Fatalf("round trip:\n in %+v\nout %+v", in, out)
	}
	if len(out.deps) != 2 || out.deps[1].block != "minecraft:lever" || !slices.Equal(out.deps[1].props, []string{"facing", "face"}) {
		t.Fatalf("deps: %+v", out.deps)
	}
}

func cmpStr(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func TestXform(t *testing.T) {
	m := map[string]map[string]int{"0": {"false": 1}, "5": {"true": 1}, "4": {"false": 1}, "7": {"true": 1}}
	x, ok := findXform(m)
	if !ok || x.kind != "bit" || x.bit != 0 {
		t.Fatalf("findXform = %+v %v, want bit 0", x, ok)
	}
	if looPurity(m, x) != 1 {
		t.Fatalf("looPurity with xform = %v, want 1", looPurity(m, x))
	}
	if looPurity(m, xform{}) != 0 {
		t.Fatalf("looPurity without xform = %v, want 0", looPurity(m, xform{}))
	}
	id, ok := findXform(map[string]map[string]int{"1": {"1": 1}, "2": {"2": 1}})
	if !ok || id.kind != "identity" {
		t.Fatalf("identity not found: %+v", id)
	}
}

// testJava writes and loads a minimal Mojang blocks.json: block -> property values, states in cartesian order.
func testJava(t *testing.T, blocks map[string]map[string][]string) *javaData {
	type st struct {
		ID         int               `json:"id"`
		Default    bool              `json:"default,omitempty"`
		Properties map[string]string `json:"properties,omitempty"`
	}
	type bl struct {
		Properties map[string][]string `json:"properties,omitempty"`
		States     []st                `json:"states"`
	}
	rep := map[string]bl{}
	id := 0
	for _, name := range sortedKeys(blocks) {
		props := blocks[name]
		keys := sortedKeys(props)
		combos := []map[string]string{{}}
		for _, k := range keys {
			var next []map[string]string
			for _, c := range combos {
				for _, v := range props[k] {
					n := map[string]string{}
					for a, b := range c {
						n[a] = b
					}
					n[k] = v
					next = append(next, n)
				}
			}
			combos = next
		}
		b := bl{Properties: props}
		for i, c := range combos {
			b.States = append(b.States, st{ID: id, Default: i == 0, Properties: c})
			id++
		}
		rep[name] = b
	}
	path := filepath.Join(t.TempDir(), "blocks.json")
	raw, _ := json.Marshal(rep)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return loadJava(path)
}

func TestLearner(t *testing.T) {
	axis := map[string][]string{"axis": {"x", "y", "z"}}
	jd := testJava(t, map[string]map[string][]string{
		"minecraft:stone":       {},
		"minecraft:oak_log":     axis,
		"minecraft:birch_log":   axis,
		"minecraft:spruce_log":  axis,
		"minecraft:basalt":      axis,
		"minecraft:kelp":        {"age": {"0", "1", "2", "3", "4", "5"}},
		"minecraft:copper_bulb": {"lit": {"false", "true"}, "powered": {"false", "true"}},
	})
	var pairs []*pair
	add := func(name string, bp map[string]string, java string, jp map[string]string) {
		pairs = append(pairs, newPair(bstate{name, bp}, java, jp))
	}
	for _, w := range []string{"oak", "birch"} {
		for _, a := range []string{"x", "y", "z"} {
			add("minecraft:"+w+"_log", map[string]string{"pillar_axis": a}, "minecraft:"+w+"_log", map[string]string{"axis": a})
		}
	}
	for _, a := range []string{"x", "y"} { // basalt z is missing: siblings + identity must recover it
		add("minecraft:basalt", map[string]string{"pillar_axis": a}, "minecraft:basalt", map[string]string{"axis": a})
	}
	for _, a := range []string{"0", "1", "2", "4"} {
		add("minecraft:kelp", map[string]string{"kelp_age": a}, "minecraft:kelp", map[string]string{"age": a})
	}
	l := newLearner(jd, pairs, true)
	check := func(name string, bp map[string]string, want, strategy string) {
		t.Helper()
		pr := l.predict(bstate{name, bp})
		if got := jd.str(pr.java); got != want || pr.strategy != strategy {
			t.Errorf("predict %s %v = %s by %s, want %s by %s", name, bp, got, pr.strategy, want, strategy)
		}
	}
	check("minecraft:spruce_log", map[string]string{"pillar_axis": "z"}, "minecraft:spruce_log[axis=z]", stratAnalog)
	check("minecraft:basalt", map[string]string{"pillar_axis": "z"}, "minecraft:basalt[axis=z]", stratSiblings)
	check("minecraft:kelp", map[string]string{"kelp_age": "3"}, "minecraft:kelp[age=3]", stratSiblings)
	// no evidence for its properties: the Java default, flagged
	check("minecraft:copper_bulb", map[string]string{"lit": "1", "powered_bit": "0"}, "minecraft:copper_bulb[lit=false,powered=false]", stratSameName)
	if pr := l.predict(bstate{"minecraft:copper_bulb", map[string]string{"lit": "1"}}); len(pr.low) == 0 {
		t.Error("copper_bulb: no low-confidence reason")
	}
	check("minecraft:element_7", nil, "minecraft:stone", stratMiss)
}
