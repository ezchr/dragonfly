package main

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// The learner predicts the Java state of a Bedrock state from evidence: Bedrock -> Java pairs already decided.
// Property names and value conventions are not written by hand; they are read off the pairs.
//
// The Java block is a name rewrite ("descriptor") of the Bedrock name: "=" (same name), "double>" (oak_double_slab
// -> oak_slab), ">wall" (redstone_torch -> redstone_wall_torch), "silver>light_gray", or a character-level suffix
// rewrite "~>s" (beetroot -> beetroots).
//
// Strategies, in order (see predict):
//
//	analog:   a Bedrock name of the same family (same last word) with the same property names and a decision for
//	          the same values: its Java state, with its name rewrite applied to this name (new hanging sign wood,
//	          new slab, new mushroom block...).
//	siblings: other states of the same Bedrock name pick the Java block.
//	hand:     handrules.go.
//	shape:    a name rewrite learned from Bedrock states with the same property names.
//	same-name, rewrite, miss.
//
// Every Java property is a target predicted from the Bedrock properties: for each source (no property, one Bedrock
// property, or a pair) the evidence gives a value table, scored by how many examples it predicts with each left
// out. A source whose evidence is a value-preserving mapping (identity, 0/1 -> false/true, bit k of an integer)
// also predicts values it has not seen. The best-scoring source whose table knows the Bedrock value wins, at the most
// specific evidence level that scores at least 0.9:
//
//	name:       pairs of the same Bedrock name and Java block
//	shape+name: pairs with the same Bedrock and Java property names and the same name rewrite (keeps oak_slab and
//	            oak_double_slab apart)
//	shape:      pairs with the same Bedrock and Java property names
//	java:       pairs with the same Java block
//	domain:     pairs whose Java property has the same values (6-way facing apart from 4-way facing)
//	global:     every pair with the Java property
//
// A source is only used while it scores within 0.05 of the level's best: when the best source has not seen the
// Bedrock value, a broader level that has is more trustworthy than a weaker source.

const blockTarget = "<block>"

type jstate struct {
	block string
	props map[string]string
}

type pair struct {
	b    bstate
	j    jstate
	desc string
	suf  int // tokens after the rewritten part (where to apply the descriptor)
}

type rewrite struct {
	desc  string
	names int
	suf   int
}

type learner struct {
	jd       *javaData
	pairs    []*pair
	byName   map[string][]*pair
	byNameJ  map[string][]*pair
	byBSig   map[string][]*pair
	byFSig   map[string][]*pair
	byFDesc  map[string][]*pair
	byJ      map[string][]*pair
	byVals   map[string][]*pair // Bedrock property names and values (no name) -> pairs
	byDomain map[string][]*pair
	squash   map[string]string // Java block name without underscores -> name ("" when ambiguous)
	cache    map[string]*table
	rules    []rewrite
	consts   map[string]map[string]string // descriptor -> Java props constant over its pairs
	hand     bool
}

func newLearner(jd *javaData, pairs []*pair, hand bool) *learner {
	l := &learner{jd: jd, pairs: pairs, hand: hand, byName: map[string][]*pair{}, byNameJ: map[string][]*pair{},
		byBSig: map[string][]*pair{}, byFSig: map[string][]*pair{}, byFDesc: map[string][]*pair{}, byJ: map[string][]*pair{},
		byVals: map[string][]*pair{}, byDomain: map[string][]*pair{}, squash: map[string]string{}, cache: map[string]*table{}, consts: map[string]map[string]string{}}
	for b := range jd.blocks {
		k := strings.ReplaceAll(b, "_", "")
		if _, ok := l.squash[k]; ok {
			l.squash[k] = ""
		} else {
			l.squash[k] = b
		}
	}
	for _, p := range pairs {
		l.byName[p.b.name] = append(l.byName[p.b.name], p)
		l.byNameJ[p.b.name+"\x00"+p.j.block] = append(l.byNameJ[p.b.name+"\x00"+p.j.block], p)
		bs, fs := bsig(p.b.props), bsig(p.b.props)+"|"+bsig(p.j.props)
		l.byBSig[bs] = append(l.byBSig[bs], p)
		l.byFSig[fs] = append(l.byFSig[fs], p)
		l.byFDesc[fs+"|"+p.desc] = append(l.byFDesc[fs+"|"+p.desc], p)
		l.byJ[p.j.block] = append(l.byJ[p.j.block], p)
		l.byVals[valsKey(p.b.props)] = append(l.byVals[valsKey(p.b.props)], p)
	}
	l.rules = learnRewrites(namePairs(pairs))
	// Java properties a rewrite always comes with (double> -> type=double, lit> -> lit=true), seen on at least two
	// Bedrock names.
	byDesc := map[string][]*pair{}
	names := map[string]map[string]bool{}
	for _, p := range pairs {
		if p.desc == "=" {
			continue
		}
		byDesc[p.desc] = append(byDesc[p.desc], p)
		if names[p.desc] == nil {
			names[p.desc] = map[string]bool{}
		}
		names[p.desc][p.b.name] = true
	}
	for d, ps := range byDesc {
		if len(names[d]) < 2 {
			continue
		}
		c := map[string]string{}
		for k, v := range ps[0].j.props {
			c[k] = v
		}
		for _, p := range ps[1:] {
			for k, v := range c {
				if p.j.props[k] != v {
					delete(c, k)
				}
			}
		}
		if len(c) > 0 {
			l.consts[d] = c
		}
	}
	return l
}

func bsig(p map[string]string) string { return strings.Join(sortedKeys(p), ",") }

func valsKey(p map[string]string) string {
	var b strings.Builder
	for _, k := range sortedKeys(p) {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(p[k])
		b.WriteByte(',')
	}
	return b.String()
}

func namePairs(ps []*pair) [][2]string {
	seen := map[[2]string]bool{}
	var out [][2]string
	for _, p := range ps {
		k := [2]string{p.b.name, p.j.block}
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// ---------------------------------------------------------------------------------------------------------------
// Name descriptors

func tokens(name string) []string {
	s := short(name)
	if s == "" {
		return nil
	}
	return strings.Split(s, "_")
}

// descriptor describes how the name to is made from from: "=" or "<removed tokens>><inserted tokens>", and the
// number of tokens after the rewritten part.
func descriptor(from, to string) (string, int) {
	if from == to {
		return "=", 0
	}
	a, b := tokens(from), tokens(to)
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	return strings.Join(a[p:len(a)-s], "_") + ">" + strings.Join(b[p:len(b)-s], "_"), s
}

// suffixDescriptor is the character-level rewrite of the end of a name: "~<removed>><added>".
func suffixDescriptor(from, to string) string {
	a, b := short(from), short(to)
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	if p == 0 {
		return ""
	}
	return "~" + a[p:] + ">" + b[p:]
}

// applyDescriptor rewrites name by desc wherever the removed tokens occur (anywhere, for a pure insertion) and
// returns the results for which ok holds, those with suffix length suf first.
func applyDescriptor(name, desc string, suf int, ok func(string) bool) []string {
	if desc == "=" {
		if ok(name) {
			return []string{name}
		}
		return nil
	}
	if rest, isSuffix := strings.CutPrefix(desc, "~"); isSuffix {
		from, to, _ := strings.Cut(rest, ">")
		if base, found := strings.CutSuffix(name, from); found && len(short(base)) > 0 && ok(base+to) && base+to != name {
			return []string{base + to}
		}
		return nil
	}
	from, to, _ := strings.Cut(desc, ">")
	var ft, tt []string
	if from != "" {
		ft = strings.Split(from, "_")
	}
	if to != "" {
		tt = strings.Split(to, "_")
	}
	nt := tokens(name)
	ns := name[:len(name)-len(short(name))]
	type cand struct {
		name string
		suf  int
	}
	var cs []cand
	for i := 0; i+len(ft) <= len(nt); i++ {
		if !slices.Equal(nt[i:i+len(ft)], ft) {
			continue
		}
		out := slices.Concat(nt[:i], tt, nt[i+len(ft):])
		if len(out) == 0 {
			continue
		}
		c := ns + strings.Join(out, "_")
		if c != name && ok(c) && !slices.ContainsFunc(cs, func(x cand) bool { return x.name == c }) {
			cs = append(cs, cand{c, len(nt) - i - len(ft)})
		}
	}
	slices.SortStableFunc(cs, func(a, b cand) int { return abs(a.suf-suf) - abs(b.suf-suf) })
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.name
	}
	return out
}

// learnRewrites collects the token and character-level name rewrites of name pairs, most names first.
func learnRewrites(pairs [][2]string) []rewrite {
	support := map[string]map[string]bool{}
	suf := map[string]map[int]int{}
	add := func(d, name string, s int) {
		if support[d] == nil {
			support[d], suf[d] = map[string]bool{}, map[int]int{}
		}
		support[d][name] = true
		suf[d][s]++
	}
	for _, p := range pairs {
		d, s := descriptor(p[0], p[1])
		if d == "=" {
			continue
		}
		add(d, p[0], s)
		if sd := suffixDescriptor(p[0], p[1]); sd != "" {
			add(sd, p[0], 0)
		}
	}
	var rules []rewrite
	for d, names := range support {
		best, bestN := 0, -1
		for s, c := range suf[d] {
			if c > bestN || (c == bestN && s < best) {
				best, bestN = s, c
			}
		}
		rules = append(rules, rewrite{d, len(names), best})
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].names != rules[j].names {
			return rules[i].names > rules[j].names
		}
		// token rewrites before character rewrites, then shorter first
		if ci, cj := strings.HasPrefix(rules[i].desc, "~"), strings.HasPrefix(rules[j].desc, "~"); ci != cj {
			return cj
		}
		if len(rules[i].desc) != len(rules[j].desc) {
			return len(rules[i].desc) < len(rules[j].desc)
		}
		return rules[i].desc < rules[j].desc
	})
	return rules
}

// ---------------------------------------------------------------------------------------------------------------
// Value tables

// xform is a value-preserving mapping a source follows over all its evidence; it predicts values not seen yet.
type xform struct {
	kind string // "identity" or "bit"
	bit  int
}

func (x xform) apply(v string) []string {
	switch x.kind {
	case "identity":
		return []string{v, boolName(v)}
	case "bit":
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil
		}
		return []string{strconv.FormatBool(n>>x.bit&1 == 1)}
	}
	return nil
}

type table struct {
	obs    map[string]map[string]map[string]int // source -> source value -> target value -> count
	purity map[string]float64
	xf     map[string]xform
}

func targetVal(p *pair, target string) (string, bool) {
	if target == blockTarget {
		return p.desc, true
	}
	v, ok := p.j.props[target]
	return v, ok
}

func srcVal(src string, bp map[string]string) (string, bool) {
	if src == "" {
		return "", true
	}
	if a, b, ok := strings.Cut(src, "+"); ok {
		va, ok1 := bp[a]
		vb, ok2 := bp[b]
		return va + "|" + vb, ok1 && ok2
	}
	v, ok := bp[src]
	return v, ok
}

func buildTable(ps []*pair, target string, joint bool) *table {
	t := &table{obs: map[string]map[string]map[string]int{}, purity: map[string]float64{}, xf: map[string]xform{}}
	add := func(src, sv, tv string) {
		m := t.obs[src]
		if m == nil {
			m = map[string]map[string]int{}
			t.obs[src] = m
		}
		c := m[sv]
		if c == nil {
			c = map[string]int{}
			m[sv] = c
		}
		c[tv]++
	}
	for _, p := range ps {
		tv, ok := targetVal(p, target)
		if !ok {
			continue
		}
		add("", "", tv)
		keys := sortedKeys(p.b.props)
		for i, k := range keys {
			add(k, p.b.props[k], tv)
			if !joint {
				continue
			}
			for _, k2 := range keys[i+1:] {
				add(k+"+"+k2, p.b.props[k]+"|"+p.b.props[k2], tv)
			}
		}
	}
	for src, m := range t.obs {
		if src != "" && !strings.Contains(src, "+") && target != blockTarget {
			if x, ok := findXform(m); ok {
				t.xf[src] = x
			}
		}
		t.purity[src] = looPurity(m, t.xf[src])
	}
	return t
}

// findXform finds a value-preserving mapping every observation of a source follows (at least two values seen).
func findXform(m map[string]map[string]int) (xform, bool) {
	if len(m) < 2 {
		return xform{}, false
	}
	follows := func(x xform) bool {
		for sv, c := range m {
			ok := x.apply(sv)
			for tv := range c {
				if !slices.Contains(ok, tv) {
					return false
				}
			}
		}
		return true
	}
	if x := (xform{kind: "identity"}); follows(x) {
		return x, true
	}
	for b := range 16 {
		if x := (xform{kind: "bit", bit: b}); follows(x) {
			return x, true
		}
	}
	return xform{}, false
}

func boolName(v string) string {
	switch v {
	case "1":
		return "true"
	case "0":
		return "false"
	}
	return v
}

// looPurity is the fraction of examples a table predicts correctly when each example is left out of it: by the
// other examples with the same source value, or, for the only example of a value, by the source's xform.
func looPurity(m map[string]map[string]int, x xform) float64 {
	correct, total := 0, 0
	for sv, c := range m {
		top, second, sum := 0, 0, 0
		for _, n := range c {
			sum += n
			if n > top {
				second, top = top, n
			} else if n > second {
				second = n
			}
		}
		total += sum
		switch {
		case top-1 > second:
			correct += top
		case sum == 1 && x.kind != "":
			for tv := range c {
				if slices.Contains(x.apply(sv), tv) {
					correct++
				}
			}
		}
	}
	if total == 0 {
		return 0
	}
	return float64(correct) / float64(total)
}

type choice struct {
	val    string
	src    string
	purity float64
	level  string
}

// choose predicts the target value for the Bedrock properties bp. valid filters values; def breaks ties.
func (t *table) choose(bp map[string]string, valid func(string) bool, prefer, def string) (choice, bool) {
	type cand struct {
		src  string
		pur  float64
		rank int
	}
	var cs []cand
	for src := range t.obs {
		if _, ok := srcVal(src, bp); !ok {
			continue
		}
		rank := 1
		if src == "" {
			rank = 0
		} else if strings.Contains(src, "+") {
			rank = 2
		}
		cs = append(cs, cand{src, t.purity[src], rank})
	}
	sort.Slice(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if a.pur != b.pur {
			return a.pur > b.pur
		}
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if (a.src == prefer) != (b.src == prefer) {
			return a.src == prefer
		}
		return a.src < b.src
	})
	for _, c := range cs {
		// Only sources about as good as the best one: when the best source has not seen this value, a broader
		// level that has is better than a weaker source here.
		if c.pur < cs[0].pur-0.05 {
			break
		}
		sv, _ := srcVal(c.src, bp)
		if m, ok := t.obs[c.src][sv]; ok {
			if v, ok := argmax(m, def, valid); ok {
				return choice{val: v, src: c.src, purity: c.pur}, true
			}
			continue
		}
		if x, ok := t.xf[c.src]; ok {
			for _, v := range x.apply(sv) {
				if valid(v) {
					name := x.kind
					if x.kind == "bit" {
						name = fmt.Sprintf("bit %d", x.bit)
					}
					return choice{val: v, src: c.src + " (" + name + ")", purity: c.pur}, true
				}
			}
		}
	}
	return choice{}, false
}

func argmax(m map[string]int, def string, valid func(string) bool) (string, bool) {
	best, bestN := "", 0
	for _, v := range sortedKeys(m) {
		n := m[v]
		if !valid(v) {
			continue
		}
		if n > bestN || (n == bestN && v == def) {
			best, bestN = v, n
		}
	}
	return best, bestN > 0
}

type level struct {
	name  string
	key   string
	pairs []*pair
	min   float64
}

func (l *learner) table(key, target string, ps []*pair) *table {
	k := key + "\x00" + target
	if t, ok := l.cache[k]; ok {
		return t
	}
	// Pairs of Bedrock properties as sources only on the smaller levels: on the large ones the tables get big and
	// single properties carry the conventions anyway.
	joint := !strings.HasPrefix(key, "g:") && !strings.HasPrefix(key, "j:")
	t := buildTable(ps, target, joint)
	l.cache[k] = t
	return t
}

// decide walks the levels and returns the first choice that meets its level's threshold, else the best choice seen.
func (l *learner) decide(levels []level, target string, bp map[string]string, valid func(string) bool, prefer, def string) (choice, bool) {
	var best choice
	found := false
	for _, lv := range levels {
		if len(lv.pairs) == 0 {
			continue
		}
		c, ok := l.table(lv.key, target, lv.pairs).choose(bp, valid, prefer, def)
		if !ok {
			continue
		}
		c.level = lv.name
		if c.purity >= lv.min {
			return c, true
		}
		if !found || c.purity > best.purity {
			best, found = c, true
		}
	}
	return best, found
}

// ---------------------------------------------------------------------------------------------------------------
// Prediction

type prediction struct {
	java     int
	strategy string            // how the Java block was chosen
	props    map[string]string // Java property -> how its value was chosen
	low      []string          // reasons the decision is a low-confidence guess
}

const (
	stratAnalog    = "analog"   // same-family Bedrock name with the same state
	stratSiblings  = "siblings" // other states of the Bedrock name
	stratHand      = "hand"     // handrules.go
	stratHandAlias = "hand-alias"
	stratShape     = "shape"     // name rewrite predicted from Bedrock states with the same property names
	stratSameName  = "same-name" // Java block with the Bedrock name (ignoring underscores)
	stratRewrite   = "rewrite"   // name rewrite learned from other names
	stratMiss      = "miss"
)

// domain returns the pairs whose Java block has property P with exactly these values (6-way vs 4-way facing).
func (l *learner) domain(P string, values []string) []*pair {
	k := P + "=" + strings.Join(values, ",")
	if ps, ok := l.byDomain[k]; ok {
		return ps
	}
	var ps []*pair
	for _, p := range l.pairs {
		if jb := l.jd.blocks[p.j.block]; jb != nil && slices.Equal(jb.values[P], values) {
			ps = append(ps, p)
		}
	}
	l.byDomain[k] = ps
	return ps
}

func (l *learner) isBlock(n string) bool { _, ok := l.jd.blocks[n]; return ok }

// analog looks for Bedrock names of the same family (sharing the most trailing words, at least one) with a decision
// for exactly these property values, and applies their Java state and name rewrite to this name. allowed, when not
// nil, limits the Java blocks.
func (l *learner) analog(s bstate, allowed map[string]bool) (string, int, int, string) {
	nt := tokens(s.name)
	type vote struct {
		state string
		from  string
	}
	bestScore := 0
	var votes []vote
	for _, p := range l.byVals[valsKey(s.props)] {
		if p.b.name == s.name {
			continue
		}
		mt := tokens(p.b.name)
		score := 0
		for score < len(nt) && score < len(mt) && nt[len(nt)-1-score] == mt[len(mt)-1-score] {
			score++
		}
		if score == 0 || score < bestScore {
			continue
		}
		cands := applyDescriptor(s.name, p.desc, p.suf, l.isBlock)
		if len(cands) == 0 {
			continue
		}
		J := cands[0]
		if allowed != nil && !allowed[J] {
			continue
		}
		jb := l.jd.blocks[J]
		props := map[string]string{}
		fits := true
		for k, v := range p.j.props {
			if !slices.Contains(jb.values[k], v) {
				fits = false
				break
			}
			props[k] = v
		}
		if !fits {
			continue
		}
		for _, k := range jb.propNames {
			if _, ok := props[k]; !ok {
				props[k] = l.jd.defaultValue(J, k)
				if k == "waterlogged" {
					props[k] = "false"
				}
			}
		}
		if score > bestScore {
			bestScore, votes = score, nil
		}
		votes = append(votes, vote{stateString(J, props), p.b.name})
	}
	if len(votes) == 0 {
		return "", 0, 0, ""
	}
	count := map[string]int{}
	for _, v := range votes {
		count[v.state]++
	}
	best := mostCommon(count)
	from := ""
	for _, v := range votes {
		if v.state == best {
			from = v.from
			break
		}
	}
	return best, count[best], len(votes), from
}

func (l *learner) predict(s bstate) prediction {
	return l.predictAs(s, 0)
}

func (l *learner) predictAs(s bstate, depth int) prediction {
	jd := l.jd
	pr := prediction{props: map[string]string{}}
	sib := l.byName[s.name]
	var allowed map[string]bool
	if len(sib) > 0 {
		allowed = map[string]bool{}
		for _, p := range sib {
			allowed[p.j.block] = true
		}
	}
	if st, n, total, from := l.analog(s, allowed); st != "" && 2*n > total {
		id, _ := jd.id(st)
		pr.java, pr.strategy = id, stratAnalog
		pr.props["*"] = fmt.Sprintf("as %s (%d of %d family names agree)", short(from), n, total)
		if n < total {
			pr.low = append(pr.low, fmt.Sprintf("only %d of %d same-family names agree", n, total))
		}
		return pr
	}

	J, desc := "", ""
	if len(sib) > 0 {
		valid := func(d string) bool { return siblingJava(sib, d) != "" }
		if c, ok := l.decide([]level{{"name", "n:" + s.name, sib, 0}}, blockTarget, s.props, valid, "", "="); ok {
			J, desc, pr.strategy = siblingJava(sib, c.val), c.val, stratSiblings
			if c.purity < 0.9 {
				pr.low = append(pr.low, fmt.Sprintf("Java block from siblings via %s (score %.2f)", srcName(c.src), c.purity))
			}
		}
	}
	if J == "" && l.hand && depth == 0 {
		if h, ok := handBlocks[s.name]; ok {
			name, props := parseStateString(h)
			id, _ := jd.closest(name, props)
			pr.java, pr.strategy = id, stratHand
			return pr
		}
		if a, ok := handBlockAliases[s.name]; ok {
			p := l.predictAs(bstate{a, s.props}, depth+1)
			if p.strategy != stratMiss {
				p.strategy = stratHandAlias + "/" + p.strategy
				return p
			}
		}
	}
	if J == "" {
		ps := l.byBSig[bsig(s.props)]
		valid := func(d string) bool { return len(applyDescriptor(s.name, d, l.suf(d), l.isBlock)) > 0 }
		if c, ok := l.decide([]level{{"shape", "s:" + bsig(s.props), ps, 0.95}}, blockTarget, s.props, valid, "", "="); ok && c.purity >= 0.95 {
			J, desc, pr.strategy = applyDescriptor(s.name, c.val, l.suf(c.val), l.isBlock)[0], c.val, stratShape
			if c.val != "=" {
				pr.low = append(pr.low, "Java block "+short(J)+" by a name rewrite ("+c.val+") learned from blocks with the same Bedrock properties")
			}
		}
	}
	if J == "" {
		if l.isBlock(s.name) {
			J, desc, pr.strategy = s.name, "=", stratSameName
		} else if b := l.squash[strings.ReplaceAll(s.name, "_", "")]; b != "" {
			J, pr.strategy = b, stratSameName
			desc, _ = descriptor(s.name, b)
			pr.low = append(pr.low, "Java block "+short(b)+": same name ignoring underscores")
		}
	}
	if J == "" {
		for _, r := range l.rules {
			if c := applyDescriptor(s.name, r.desc, r.suf, l.isBlock); len(c) > 0 {
				J, desc, pr.strategy = c[0], r.desc, stratRewrite
				pr.low = append(pr.low, fmt.Sprintf("Java block %s by the name rewrite %q (seen on %d names)", short(J), r.desc, r.names))
				break
			}
		}
	}
	if J == "" {
		pr.java, pr.strategy = jd.blocks["minecraft:stone"].def, stratMiss
		return pr
	}
	var fixed map[string]string // properties the name rewrite always comes with
	if desc != "=" && len(l.byNameJ[s.name+"\x00"+J]) == 0 {
		fixed = l.consts[desc]
	}

	jb := jd.blocks[J]
	out := map[string]string{}
	unseenBlock := len(l.byJ[J]) == 0
	fs := bsig(s.props) + "|" + strings.Join(slices.DeleteFunc(slices.Clone(jb.propNames), func(p string) bool { return p == "waterlogged" }), ",")
	for _, P := range jb.propNames {
		def := jd.defaultValue(J, P)
		if P == "waterlogged" {
			out[P] = "false"
			continue
		}
		if v, ok := fixed[P]; ok && slices.Contains(jb.values[P], v) {
			out[P], pr.props[P] = v, "rewrite-constant"
			continue
		}
		valid := func(v string) bool { return slices.Contains(jb.values[P], v) }
		levels := []level{
			{"name", "nj:" + s.name + "\x00" + J, l.byNameJ[s.name+"\x00"+J], 0.9},
			{"shape+name", "fd:" + fs + "|" + desc, l.byFDesc[fs+"|"+desc], 0.9},
			{"shape", "f:" + fs, l.byFSig[fs], 0.9},
			{"java", "j:" + J, l.byJ[J], 0.9},
			{"domain", "d:" + P + "=" + strings.Join(jb.values[P], ","), l.domain(P, jb.values[P]), 0.9},
			{"global", "g:", l.pairs, 0.9},
		}
		c, ok := l.decide(levels, P, s.props, valid, P, def)
		if !ok || c.purity < 0.5 {
			out[P], pr.props[P] = def, "default"
			if len(jb.values[P]) > 1 && unseenBlock && len(s.props) > 0 {
				pr.low = append(pr.low, P+": Java default (no evidence)")
			}
			continue
		}
		out[P], pr.props[P] = c.val, fmt.Sprintf("%s:%s %.2f", c.level, srcName(c.src), c.purity)
		if c.purity < 0.9 {
			pr.low = append(pr.low, fmt.Sprintf("%s from %s (%s level, score %.2f)", P, srcName(c.src), c.level, c.purity))
		}
	}
	id, ok := jd.id(stateString(J, out))
	if !ok {
		panic("no state " + stateString(J, out))
	}
	pr.java = id
	return pr
}

func srcName(src string) string {
	if src == "" {
		return "constant"
	}
	return src
}

func siblingJava(sib []*pair, desc string) string {
	for _, p := range sib {
		if p.desc == desc {
			return p.j.block
		}
	}
	return ""
}

func (l *learner) suf(desc string) int {
	for _, r := range l.rules {
		if r.desc == desc {
			return r.suf
		}
	}
	return 0
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// newPair builds an evidence pair (waterlogged is not evidence: it is always false).
func newPair(b bstate, javaBlock string, javaProps map[string]string) *pair {
	d, s := descriptor(b.name, javaBlock)
	p := map[string]string{}
	for k, v := range javaProps {
		if k != "waterlogged" {
			p[k] = v
		}
	}
	return &pair{b: b, j: jstate{javaBlock, p}, desc: d, suf: s}
}

// ---------------------------------------------------------------------------------------------------------------
// Names (items and biomes): the same rewrite learning on plain names.

type nameLearner struct {
	rules []rewrite
}

func newNameLearner(pairs [][2]string) *nameLearner {
	return &nameLearner{rules: learnRewrites(pairs)}
}

// rewrite returns the first rewrite of name (most supported rule first) that ok accepts.
func (nl *nameLearner) rewrite(name string, ok func(string) bool) (string, rewrite, bool) {
	for _, r := range nl.rules {
		if c := applyDescriptor(name, r.desc, r.suf, ok); len(c) > 0 {
			return c[0], r, true
		}
	}
	return "", rewrite{}, false
}
