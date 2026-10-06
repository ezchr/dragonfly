package main

import (
	"fmt"
	"log"
	"slices"
	"sort"
	"strings"
)

// Decision statuses.
const (
	statusCarried        = "carried"            // same Java name and properties as before
	statusCarriedClosest = "carried-closest"    // Java block kept, some properties changed in the new Java version
	statusCarriedRenamed = "carried-renamed"    // Java block or item renamed (javaRenames)
	statusCarriedSubst   = "carried-substitute" // Java block or item missing in the target version: a stand-in
	statusNew            = "new"                // decided by the rules
)

// substituteBlock finds a stand-in for a Java block the target version does not have: javaRenames, then the
// javaTokenSubstitutes of handrules.go, then the block with the same properties (and the values the decisions use) that shares the most
// trailing name words (at least one), then the most words, then the first by name.
func substituteBlock(jd *javaData, layout map[string][]string, name string) (string, string, bool) {
	if r, ok := javaRenames[name]; ok && jd.blocks[r] != nil {
		return r, "renamed", true
	}
	if r, ok := substituteTokens(name, func(n string) bool { return jd.blocks[n] != nil }, false); ok {
		return r, "name words", true
	}
	if layout == nil {
		return "", "", false
	}
	nt := tokens(name)
	best, bestTail, bestAll := "", 0, 0
	for _, b := range sortedKeys(jd.blocks) {
		jb := jd.blocks[b]
		if len(jb.values) != len(layout) {
			continue
		}
		same := true
		for k, vs := range layout {
			for _, v := range vs {
				if !slices.Contains(jb.values[k], v) {
					same = false
				}
			}
		}
		if !same {
			continue
		}
		bt := tokens(b)
		tail := 0
		for tail < len(nt) && tail < len(bt) && nt[len(nt)-1-tail] == bt[len(bt)-1-tail] {
			tail++
		}
		all := 0
		for _, t := range bt {
			if slices.Contains(nt, t) {
				all++
			}
		}
		if tail > bestTail || (tail == bestTail && tail > 0 && all > bestAll) {
			best, bestTail, bestAll = b, tail, all
		}
	}
	if best == "" {
		return "", "", false
	}
	return best, "same properties and last word", true
}

// substituteTokens applies javaTokenSubstitutes, then drops leading words (orange_poplar_leaves -> birch_leaves),
// returning the first name ok accepts. With trailing, dropping the last word is tried first (items: keep the
// colour or material, black_concrete_slab -> black_concrete).
func substituteTokens(name string, ok func(string) bool, trailing bool) (string, bool) {
	t := tokens(name)
	for i, w := range t {
		if r, found := javaTokenSubstitutes[w]; found {
			t[i] = r
		}
	}
	ns := name[:len(name)-len(short(name))]
	var cands []string
	if n := ns + strings.Join(t, "_"); n != name {
		cands = append(cands, n)
	}
	if trailing && len(t) > 1 {
		// one word only: light_blue_cushion must not become light
		cands = append(cands, ns+strings.Join(t[:len(t)-1], "_"))
	}
	for i := 1; i < len(t); i++ {
		cands = append(cands, ns+strings.Join(t[i:], "_"))
	}
	for _, c := range cands {
		if ok(c) {
			return c, true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------------------------------------------
// Blocks

type blockDecision struct {
	df     dfState
	java   int
	origin string
	status string
	pred   *prediction
	note   string
}

type blocksResult struct {
	decisions     []blockDecision // runtime id order
	dropped       []prevBlock     // previous decisions for states this Dragonfly does not register
	nameDefault   map[string]int
	waterlog      [][3]int
	deps          []dep
	depsDropped   []string
	depsAdded     []dep
	depsSuggested []dep
	unreachable   []string
	evidence      int
}

// blockEvidence turns previous decisions into learner pairs, with Java names moved to the target version.
func blockEvidence(jd *javaData, blocks []prevBlock) []*pair {
	var out []*pair
	for _, p := range blocks {
		b, err := parseKey(p.key)
		check(err)
		name, props := parseStateString(p.java)
		if _, ok := jd.blocks[name]; !ok {
			r, ok := javaRenames[name]
			if _, ok2 := jd.blocks[r]; !ok || !ok2 {
				continue
			}
			name = r
		}
		out = append(out, newPair(b, name, props))
	}
	return out
}

func updateBlocks(jd *javaData, prev *prevTables, df []dfState, hand bool) *blocksResult {
	res := &blocksResult{nameDefault: map[string]int{}}
	prevByKey := map[string]prevBlock{}
	for _, p := range prev.blocks {
		prevByKey[p.key] = p
	}
	// Property layout of Java blocks the target version lacks, from the previous decisions' states.
	prevLayout := map[string]map[string][]string{}
	for _, p := range prev.blocks {
		name, props := parseStateString(p.java)
		if jd.blocks[name] != nil {
			continue
		}
		l := prevLayout[name]
		if l == nil {
			l = map[string][]string{}
			prevLayout[name] = l
		}
		for k, v := range props {
			if !slices.Contains(l[k], v) {
				l[k] = append(l[k], v)
			}
		}
	}
	pairs := blockEvidence(jd, prev.blocks)
	res.evidence = len(pairs)
	l := newLearner(jd, pairs, hand)

	dfKeys := map[string]bool{}
	for _, s := range df {
		dfKeys[s.key] = true
		d := blockDecision{df: s}
		if p, ok := prevByKey[s.key]; ok {
			name, props := parseStateString(p.java)
			d.origin = p.origin
			if id, ok := jd.id(p.java); ok {
				d.java, d.status = id, statusCarried
			} else if _, ok := jd.blocks[name]; ok {
				var dropped []string
				d.java, dropped = jd.closest(name, props)
				d.status, d.note = statusCarriedClosest, "kept "+short(name)+", changed "+strings.Join(dropped, ",")
			} else if r, how, ok := substituteBlock(jd, prevLayout[name], name); ok {
				var dropped []string
				d.java, dropped = jd.closest(r, props)
				d.status, d.note = statusCarriedSubst, short(name)+" -> "+short(r)+" ("+how+")"
				if how == "renamed" {
					d.status = statusCarriedRenamed
				}
				if len(dropped) > 0 {
					d.note += ", changed " + strings.Join(dropped, ",")
				}
			} else {
				d.note = "Java block " + short(name) + " does not exist in this version"
			}
		}
		if d.status == "" {
			pr := l.predict(s.b)
			d.java, d.status, d.pred = pr.java, statusNew, &pr
			d.origin = originOf(pr.strategy)
		}
		res.decisions = append(res.decisions, d)
	}
	for _, p := range prev.blocks {
		if !dfKeys[p.key] {
			res.dropped = append(res.dropped, p)
		}
	}

	// Per-name defaults for runtime ids that are not in the hash table (custom registries).
	for _, d := range res.decisions {
		cur, ok := res.nameDefault[d.df.b.name]
		if !ok || (jd.states[d.java].def && !jd.states[cur].def) {
			res.nameDefault[d.df.b.name] = d.java
		}
	}
	res.waterlog = waterlogRanges(jd)
	updateDeps(jd, prev, res)
	return res
}

func originOf(strategy string) string {
	switch strategy {
	case stratHand:
		return "hand"
	case stratMiss:
		return "miss"
	}
	return "auto:" + strategy
}

func waterlogRanges(jd *javaData) [][3]int {
	var wr [][3]int
	for _, jb := range jd.blocks {
		if !slices.Contains(jb.propNames, "waterlogged") {
			continue
		}
		delta := -1
		for id := jb.first; id < jb.end; id++ {
			s := jd.states[id]
			if s.wlogged {
				continue
			}
			twin := -1
			for t := jb.first; t < jb.end; t++ {
				if jd.states[t].wlogged && sameExcept(jd.states[t].props, s.props, "waterlogged") {
					twin = t
					break
				}
			}
			if twin < 0 || (delta >= 0 && id-twin != delta) || id-twin <= 0 {
				log.Fatalf("waterlog layout of %s is not regular", jb.name)
			}
			delta = id - twin
		}
		// the runtime's parity rule: waterlogged=false iff ((id-first)/delta)%2 == 1
		for id := jb.first; id < jb.end; id++ {
			if ((id-jb.first)/delta)%2 == 1 == jd.states[id].wlogged {
				log.Fatalf("waterlog parity rule fails for %s", jb.name)
			}
		}
		wr = append(wr, [3]int{jb.first, jb.end, delta})
	}
	sort.Slice(wr, func(i, j int) bool { return wr[i][0] < wr[j][0] })
	return wr
}

func sameExcept(a, b map[string]string, skip string) bool {
	for k, v := range a {
		if k != skip && b[k] != v {
			return false
		}
	}
	return true
}

// updateDeps carries NeighbourDependent over and derives entries for Java blocks the previous tables never produced:
// a property is undetermined when every Bedrock state mapping to the block leaves it at one value.
func updateDeps(jd *javaData, prev *prevTables, res *blocksResult) {
	dfNames := map[string]bool{}
	image := map[string]map[string]map[string]bool{} // java block -> prop -> values
	for _, d := range res.decisions {
		dfNames[d.df.b.name] = true
		s := jd.states[d.java]
		m := image[s.block]
		if m == nil {
			m = map[string]map[string]bool{}
			image[s.block] = m
		}
		for k, v := range s.props {
			if m[k] == nil {
				m[k] = map[string]bool{}
			}
			m[k][v] = true
		}
	}
	prevImage := map[string]bool{}
	for _, p := range prev.blocks {
		name, _ := parseStateString(p.java)
		if r, ok := javaRenames[name]; ok && jd.blocks[name] == nil {
			name = r
		}
		prevImage[name] = true
	}
	carried := map[string]bool{} // block\x00prop
	for _, d := range prev.deps {
		if len(d.props) == 1 && d.props[0] == "<block>" {
			if dfNames[d.block] {
				res.deps = append(res.deps, d)
			} else {
				res.depsDropped = append(res.depsDropped, short(d.block)+" [<block>]: Dragonfly no longer has the Bedrock name")
			}
			continue
		}
		b := d.block
		if jd.blocks[b] == nil {
			if r, ok := javaRenames[b]; ok && jd.blocks[r] != nil {
				b = r
			} else {
				res.depsDropped = append(res.depsDropped, short(d.block)+": no such Java block")
				continue
			}
		}
		var props []string
		for _, p := range d.props {
			if slices.Contains(jd.blocks[b].propNames, p) {
				props = append(props, p)
				carried[b+"\x00"+p] = true
			} else {
				res.depsDropped = append(res.depsDropped, short(b)+" ["+p+"]: no such Java property")
			}
		}
		if len(props) > 0 {
			res.deps = append(res.deps, dep{b, props, d.source})
		}
	}
	for _, b := range sortedKeys(image) {
		jb := jd.blocks[b]
		bySrc := map[string][]string{}
		for _, p := range jb.propNames {
			if p == "waterlogged" || len(jb.values[p]) < 2 || len(image[b][p]) > 1 || carried[b+"\x00"+p] {
				continue
			}
			src, ok := propSource[p]
			if !ok {
				src = "SourceUnknown"
			}
			bySrc[src] = append(bySrc[src], p)
		}
		for _, src := range sortedKeys(bySrc) {
			d := dep{b, bySrc[src], src}
			if prevImage[b] {
				res.depsSuggested = append(res.depsSuggested, d)
			} else {
				res.depsAdded = append(res.depsAdded, d)
				res.deps = append(res.deps, d)
			}
		}
	}
	isCross := func(d dep) bool { return len(d.props) == 1 && d.props[0] == "<block>" }
	sort.SliceStable(res.deps, func(i, j int) bool {
		a, b := res.deps[i], res.deps[j]
		if isCross(a) != isCross(b) {
			return !isCross(a)
		}
		if a.block != b.block {
			return a.block < b.block
		}
		return a.source < b.source
	})
	for _, b := range sortedKeys(jd.blocks) {
		if image[b] == nil {
			res.unreachable = append(res.unreachable, b)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------
// Items

type itemDecision struct {
	name    string
	meta    int
	hasMeta bool
	java    string
	origin  string
	status  string
	note    string
}

type itemsResult struct {
	entries     []itemDecision // the table: itemByName (hasMeta false) and itemByMeta entries
	newOnes     []itemDecision
	substituted []itemDecision // carried, Java item renamed or missing in the target version
	misses      []string
	dropped     []string
	dfCount     int
	dfByMeta    int
	dfByName    int
	javaCount   int
}

// itemPredictor decides Bedrock items without a carried decision.
type itemPredictor struct {
	ids       map[string]int
	blockJava map[string]string // Bedrock block name -> Java block most of its states map to
	items     *nameLearner
	blocks    *nameLearner
	hand      bool
}

func newItemPredictor(ids map[string]int, evidence []itemDecision, blockDecisions []*pair, hand bool) *itemPredictor {
	ip := &itemPredictor{ids: ids, blockJava: map[string]string{}, hand: hand}
	var ipairs, bpairs [][2]string
	for _, e := range evidence {
		ipairs = append(ipairs, [2]string{e.name, e.java})
	}
	votes := map[string]map[string]int{}
	for _, p := range blockDecisions {
		if votes[p.b.name] == nil {
			votes[p.b.name] = map[string]int{}
		}
		votes[p.b.name][p.j.block]++
	}
	for n, v := range votes {
		best, bestN := "", 0
		for _, j := range sortedKeys(v) {
			if v[j] > bestN || (v[j] == bestN && j == n) {
				best, bestN = j, v[j]
			}
		}
		ip.blockJava[n] = best
		bpairs = append(bpairs, [2]string{n, best})
	}
	ip.items, ip.blocks = newNameLearner(ipairs), newNameLearner(bpairs)
	return ip
}

func (ip *itemPredictor) predict(name string) (java, strategy, note string) {
	isItem := func(n string) bool { _, ok := ip.ids[n]; return ok }
	if ip.hand {
		if j, ok := handItems[name]; ok && isItem(j) {
			return j, "hand", ""
		}
		for _, p := range sortedKeys(handItemPrefixes) {
			if strings.HasPrefix(name, p) && isItem(handItemPrefixes[p]) {
				return handItemPrefixes[p], "hand", ""
			}
		}
	}
	// The block first: Bedrock item names of blocks are the Bedrock block names (snow -> snow_block, nether_brick
	// -> nether_bricks), so the block decision beats a same-name Java item that is something else.
	if j, ok := ip.blockJava[name]; ok && isItem(j) {
		return j, "auto:block", ""
	}
	if isItem(name) {
		return name, "auto:same-name", ""
	}
	if j, r, ok := ip.items.rewrite(name, isItem); ok {
		return j, "auto:rewrite", fmt.Sprintf("item name rewrite %q (seen on %d items)", r.desc, r.names)
	}
	if j, r, ok := ip.blocks.rewrite(name, isItem); ok {
		return j, "auto:rewrite", fmt.Sprintf("block name rewrite %q (seen on %d blocks)", r.desc, r.names)
	}
	return "", "miss", ""
}

func updateItems(reg javaRegistries, prev *prevTables, dfItems []nameMeta, blockPairs []*pair, hand bool) *itemsResult {
	res := &itemsResult{dfCount: len(dfItems), javaCount: len(reg.items)}
	dfNames := map[string]bool{}
	for _, it := range dfItems {
		dfNames[it.name] = true
	}
	type k struct {
		name string
		meta int
	}
	byName := map[string]bool{}
	byMeta := map[k]bool{}
	for _, p := range prev.items {
		d := itemDecision{name: p.name, meta: p.meta, hasMeta: p.hasMeta, java: p.java, origin: p.origin, status: statusCarried}
		if _, ok := reg.items[p.java]; !ok {
			if r, ok := renamedItem(reg, p.java); ok {
				d.java, d.status, d.note = r, statusCarriedSubst, short(p.java)+" -> "+short(r)
				if javaRenames[p.java] == r {
					d.status = statusCarriedRenamed
				}
				res.substituted = append(res.substituted, d)
			} else {
				res.dropped = append(res.dropped, fmt.Sprintf("%s -> %s (no such Java item)", short(itemLabel(d)), short(p.java)))
				continue
			}
		}
		res.entries = append(res.entries, d)
		if d.hasMeta {
			byMeta[k{d.name, d.meta}] = true
		} else {
			byName[d.name] = true
		}
	}
	ip := newItemPredictor(reg.items, res.entries, blockPairs, hand)
	for _, it := range dfItems {
		if byMeta[k{it.name, it.meta}] {
			res.dfByMeta++
			continue
		}
		if byName[it.name] {
			res.dfByName++
			continue
		}
		java, strategy, note := ip.predict(it.name)
		if java == "" {
			res.misses = append(res.misses, fmt.Sprintf("%s:%d", it.name, it.meta))
			continue
		}
		d := itemDecision{name: it.name, java: java, origin: strategy, status: statusNew, note: note}
		res.entries = append(res.entries, d)
		res.newOnes = append(res.newOnes, d)
		byName[it.name] = true
		res.dfByName++
	}
	sort.Slice(res.entries, func(i, j int) bool { return itemLabel(res.entries[i]) < itemLabel(res.entries[j]) })
	slices.Sort(res.misses)
	return res
}

func renamedItem(reg javaRegistries, name string) (string, bool) {
	isItem := func(n string) bool { _, ok := reg.items[n]; return ok }
	if r, ok := javaRenames[name]; ok && isItem(r) {
		return r, true
	}
	return substituteTokens(name, isItem, true)
}

func itemLabel(d itemDecision) string {
	if d.hasMeta {
		return fmt.Sprintf("%s@%d", d.name, d.meta)
	}
	return d.name
}

// ---------------------------------------------------------------------------------------------------------------
// Biomes

type biomeDecision struct {
	id     int
	java   string
	origin string
	status string
	note   string
}

type biomesResult struct {
	entries   []biomeDecision
	newOnes   []biomeDecision
	dropped   []string
	dfCount   int
	javaCount int
}

func biomePredict(name string, java map[string]bool, nl *nameLearner, hand bool) (string, string, string) {
	if hand {
		if j, ok := handBiomes[name]; ok && java[j] {
			return j, "hand", ""
		}
	}
	if java["minecraft:"+name] {
		return "minecraft:" + name, "auto:same-name", ""
	}
	if j, r, ok := nl.rewrite("minecraft:"+name, func(n string) bool { return java[n] }); ok {
		return j, "auto:rewrite", fmt.Sprintf("name rewrite %q (seen on %d biomes)", r.desc, r.names)
	}
	return "minecraft:plains", "miss", ""
}

func updateBiomes(javaBiomes []string, prev *prevTables, df []dfBiome, hand bool) *biomesResult {
	java := map[string]bool{}
	for _, b := range javaBiomes {
		java[b] = true
	}
	res := &biomesResult{dfCount: len(df), javaCount: len(java)}
	dfName := map[int]string{}
	for _, b := range df {
		dfName[b.id] = b.name
	}
	have := map[int]bool{}
	var pairs [][2]string
	for _, p := range prev.biomes {
		d := biomeDecision{id: p.id, java: p.java, origin: p.origin, status: statusCarried}
		if !java[p.java] {
			if r, ok := javaRenames[p.java]; ok && java[r] {
				d.java, d.status, d.note = r, statusCarriedRenamed, short(p.java)+" -> "+short(r)
			} else {
				res.dropped = append(res.dropped, fmt.Sprintf("%d -> %s (Java biome no longer exists)", p.id, short(p.java)))
				continue
			}
		}
		res.entries = append(res.entries, d)
		have[d.id] = true
		if n, ok := dfName[d.id]; ok {
			pairs = append(pairs, [2]string{"minecraft:" + n, d.java})
		}
	}
	nl := newNameLearner(pairs)
	for _, b := range df {
		if have[b.id] {
			continue
		}
		j, strategy, note := biomePredict(b.name, java, nl, hand)
		d := biomeDecision{id: b.id, java: j, origin: strategy, status: statusNew, note: b.name + ": " + note}
		res.entries = append(res.entries, d)
		res.newOnes = append(res.newOnes, d)
	}
	sort.Slice(res.entries, func(i, j int) bool { return res.entries[i].id < res.entries[j].id })
	return res
}
