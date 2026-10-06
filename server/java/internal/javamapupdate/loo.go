package main

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
)

// Leave-one-out evaluation: hide a random fraction of the known decisions and let the rules recover them from the
// rest. Two block modes: random states (a Dragonfly update adding states to known blocks) and random whole Bedrock
// names (new blocks).

type looCase struct {
	key   string
	b     bstate
	truth int
}

type looStats struct {
	n, exact, block int
	byCat           map[string]int
	byFamily        map[string]int
	examples        map[string][]string
	byStrategy      map[string][2]int // strategy -> {n, exact}
	byProp          map[string]int    // Java property -> wrong states
}

func newLooStats() *looStats {
	return &looStats{byCat: map[string]int{}, byFamily: map[string]int{}, examples: map[string][]string{}, byStrategy: map[string][2]int{}, byProp: map[string]int{}}
}

func (s *looStats) fail(cat, family, example string) {
	s.byCat[cat]++
	s.byFamily[family]++
	if len(s.examples[cat]) < 3 {
		s.examples[cat] = append(s.examples[cat], example)
	}
}

func family(name string) string {
	t := tokens(name)
	return t[len(t)-1]
}

func runLOO(jd *javaData, prev *prevTables, df []dfState, dfItems []nameMeta, dfBio []dfBiome, javaBiomes []string, reg javaRegistries, frac float64, seed uint64, hand bool) string {
	var out bytes.Buffer
	p := func(f string, a ...any) { fmt.Fprintf(&out, f, a...) }
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))

	inDF := map[string]bool{}
	for _, s := range df {
		inDF[s.key] = true
	}
	var cases []looCase
	var allPairs []*pair
	pairOf := map[string]*pair{}
	for _, pb := range prev.blocks {
		ps := blockEvidence(jd, []prevBlock{pb})
		if len(ps) == 0 {
			continue
		}
		allPairs = append(allPairs, ps[0])
		pairOf[pb.key] = ps[0]
		id, ok := jd.id(pb.java)
		if ok && inDF[pb.key] {
			cases = append(cases, looCase{pb.key, ps[0].b, id})
		}
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].key < cases[j].key })

	p("# javamapupdate leave-one-out evaluation\n\n")
	p("Hidden fraction %.0f%%, seed %d, hand rules %v. Truth: the previous decisions (%s). A hidden decision counts as\n", frac*100, seed, onOff(hand), prev.source)
	p("recovered when the rules give exactly the same Java state.\n\n")

	evalBlocks := func(title string, hidden map[string]bool) {
		var ev []*pair
		for _, pb := range prev.blocks {
			if pr, ok := pairOf[pb.key]; ok && !hidden[pb.key] {
				ev = append(ev, pr)
			}
		}
		l := newLearner(jd, ev, hand)
		st := newLooStats()
		for _, c := range cases {
			if !hidden[c.key] {
				continue
			}
			pr := l.predict(c.b)
			st.n++
			bs := st.byStrategy[pr.strategy]
			bs[0]++
			got, want := jd.states[pr.java], jd.states[c.truth]
			ex := fmt.Sprintf("`%s`: got `%s`, want `%s`", short(c.key), short(jd.str(pr.java)), short(jd.str(c.truth)))
			switch {
			case pr.java == c.truth:
				st.exact++
				st.block++
				bs[1]++
			case pr.strategy == stratMiss:
				st.fail("miss", family(c.b.name), ex)
			case got.block != want.block:
				st.fail("wrong Java block ("+pr.strategy+")", family(c.b.name), ex)
			default:
				st.block++
				for _, k := range sortedKeys(want.props) {
					if got.props[k] != want.props[k] {
						st.byProp[k]++
					}
				}
				st.fail("wrong properties ("+pr.strategy+")", family(c.b.name), ex)
			}
			st.byStrategy[pr.strategy] = bs
		}
		p("## %s\n\n", title)
		p("| | |\n|---|---:|\n| hidden states | %d |\n| recovered exactly | %d (%.1f%%) |\n| right Java block | %d (%.1f%%) |\n\n",
			st.n, st.exact, pct(st.exact, st.n), st.block, pct(st.block, st.n))
		p("By how the Java block was chosen:\n\n| strategy | states | exact |\n|---|---:|---:|\n")
		for _, k := range sortedKeys(st.byStrategy) {
			v := st.byStrategy[k]
			p("| %s | %d | %d (%.1f%%) |\n", k, v[0], v[1], pct(v[1], v[0]))
		}
		writeFailures(p, st)
	}

	// Mode 1: random states.
	hidden := map[string]bool{}
	for _, c := range cases {
		if rng.Float64() < frac {
			hidden[c.key] = true
		}
	}
	evalBlocks(fmt.Sprintf("Blocks: %.0f%% of states hidden", frac*100), hidden)

	// Mode 2: random Bedrock names, all their states.
	names := map[string]bool{}
	for _, c := range cases {
		names[c.b.name] = true
	}
	hiddenNames := map[string]bool{}
	for _, n := range sortedKeys(names) {
		if rng.Float64() < frac {
			hiddenNames[n] = true
		}
	}
	hidden = map[string]bool{}
	for _, c := range cases {
		if hiddenNames[c.b.name] {
			hidden[c.key] = true
		}
	}
	evalBlocks(fmt.Sprintf("Blocks: %.0f%% of Bedrock names hidden (%d of %d names, new blocks)", frac*100, len(hiddenNames), len(names)), hidden)

	// Items: whole names (itemByName entries of Dragonfly items).
	dfNames := map[string]bool{}
	for _, it := range dfItems {
		dfNames[it.name] = true
	}
	var icases []prevItem
	for _, it := range prev.items {
		if !it.hasMeta && dfNames[it.name] {
			if _, ok := reg.items[it.java]; ok {
				icases = append(icases, it)
			}
		}
	}
	hiddenItems := map[string]bool{}
	for _, it := range icases {
		if rng.Float64() < frac {
			hiddenItems[it.name] = true
		}
	}
	var iev []itemDecision
	for _, it := range prev.items {
		if !hiddenItems[it.name] {
			iev = append(iev, itemDecision{name: it.name, java: it.java})
		}
	}
	ip := newItemPredictor(reg.items, iev, allPairs, hand)
	ist := newLooStats()
	for _, it := range icases {
		if !hiddenItems[it.name] {
			continue
		}
		got, strategy, _ := ip.predict(it.name)
		ist.n++
		bs := ist.byStrategy[strategy]
		bs[0]++
		ex := fmt.Sprintf("`%s`: got `%s`, want `%s`", short(it.name), short(got), short(it.java))
		switch {
		case got == it.java:
			ist.exact++
			bs[1]++
		case strategy == "miss":
			ist.fail("miss", family(it.name), ex)
		default:
			ist.fail("wrong item ("+strategy+")", family(it.name), ex)
		}
		ist.byStrategy[strategy] = bs
	}
	p("## Items: %.0f%% of Dragonfly item names hidden\n\n", frac*100)
	p("| | |\n|---|---:|\n| hidden items | %d |\n| recovered exactly | %d (%.1f%%) |\n\n", ist.n, ist.exact, pct(ist.exact, ist.n))
	p("By strategy:\n\n| strategy | items | exact |\n|---|---:|---:|\n")
	for _, k := range sortedKeys(ist.byStrategy) {
		v := ist.byStrategy[k]
		p("| %s | %d | %d (%.1f%%) |\n", k, v[0], v[1], pct(v[1], v[0]))
	}
	writeFailures(p, ist)
	p("Meta-selected items (banner, bed colours) are not part of this test: a new meta of a known item falls back to the\n")
	p("lowest meta's Java item, and the colour order differs per item, so they need a carried decision or a hand rule.\n\n")

	// Biomes.
	java := map[string]bool{}
	for _, b := range javaBiomes {
		java[b] = true
	}
	dfName := map[int]string{}
	for _, b := range dfBio {
		dfName[b.id] = b.name
	}
	var bcases []prevBiome
	for _, b := range prev.biomes {
		if _, ok := dfName[b.id]; ok {
			bcases = append(bcases, b)
		}
	}
	hiddenB := map[int]bool{}
	for _, b := range bcases {
		if rng.Float64() < frac {
			hiddenB[b.id] = true
		}
	}
	var bp [][2]string
	for _, b := range bcases {
		if !hiddenB[b.id] {
			bp = append(bp, [2]string{"minecraft:" + dfName[b.id], b.java})
		}
	}
	nl := newNameLearner(bp)
	bst := newLooStats()
	for _, b := range bcases {
		if !hiddenB[b.id] {
			continue
		}
		got, strategy, _ := biomePredict(dfName[b.id], java, nl, hand)
		bst.n++
		ex := fmt.Sprintf("`%s` (%d): got `%s`, want `%s`", dfName[b.id], b.id, short(got), short(b.java))
		if got == b.java {
			bst.exact++
		} else {
			bst.fail("wrong biome ("+strategy+")", "biome", ex)
		}
	}
	p("## Biomes: %.0f%% of Dragonfly biomes hidden\n\n", frac*100)
	p("| | |\n|---|---:|\n| hidden biomes | %d |\n| recovered exactly | %d (%.1f%%) |\n\n", bst.n, bst.exact, pct(bst.exact, bst.n))
	writeFailures(p, bst)
	return out.String()
}

func writeFailures(p func(string, ...any), st *looStats) {
	if len(st.byCat) == 0 {
		p("\nNo failures.\n\n")
		return
	}
	p("\nFailures by category:\n\n| category | count | examples |\n|---|---:|---|\n")
	cats := sortedKeys(st.byCat)
	slices.SortStableFunc(cats, func(a, b string) int { return st.byCat[b] - st.byCat[a] })
	for _, c := range cats {
		p("| %s | %d | %s |\n", c, st.byCat[c], strings.Join(st.examples[c], "<br>"))
	}
	if len(st.byProp) > 0 {
		props := sortedKeys(st.byProp)
		slices.SortStableFunc(props, func(a, b string) int { return st.byProp[b] - st.byProp[a] })
		var parts []string
		for _, k := range props {
			parts = append(parts, fmt.Sprintf("%s (%d)", k, st.byProp[k]))
		}
		p("\nWrong Java properties (states): %s\n", strings.Join(parts, ", "))
	}
	fams := sortedKeys(st.byFamily)
	slices.SortStableFunc(fams, func(a, b string) int { return st.byFamily[b] - st.byFamily[a] })
	var parts []string
	for i, f := range fams {
		if i == 20 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s (%d)", f, st.byFamily[f]))
	}
	p("\nFailures by Bedrock name family (last word): %s\n\n", strings.Join(parts, ", "))
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return 100 * float64(a) / float64(b)
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
