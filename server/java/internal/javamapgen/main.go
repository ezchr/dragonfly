// Command javamapgen generates the Bedrock -> Java 26.3 id tables of package javamap.
//
// It reverses GeyserMC's Java -> Bedrock mappings (blocks.nbt, items.json, biomes.json) and resolves every
// state, item and biome Dragonfly registers, so the runtime only has to do table lookups. Run it through
// `go generate ./javamap` from the dfjava module root.
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"log"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	_ "github.com/df-mc/dragonfly/server"
	_ "github.com/df-mc/dragonfly/server/block"
	_ "github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	_ "github.com/df-mc/dragonfly/server/world/biome"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

var (
	refs = flag.String("refs", "../../refs", "directory holding geyser-mappings-26.3 and mojang-26.3")
	out  = flag.String("out", ".", "javamap package directory to write into")
	dump = flag.String("dump", "", "if set, write every Dragonfly state -> Java state decision to this file")
)

func main() {
	flag.Parse()
	log.SetFlags(0)

	js := loadJava(filepath.Join(*refs, "mojang-26.3/generated/reports/blocks.json"))
	gm := loadGeyserBlocks(filepath.Join(*refs, "geyser-mappings-26.3/blocks.nbt"), js)

	world.DefaultBlockRegistry.Finalize()
	br := genBlocks(js, gm)
	ir := genItems(filepath.Join(*refs, "geyser-mappings-26.3/items.json"), filepath.Join(*refs, "mojang-26.3/generated/reports/registries.json"))
	bi := genBiomes(filepath.Join(*refs, "geyser-mappings-26.3/biomes.json"))
	writeReport(br, ir, bi)
}

// ---------------------------------------------------------------------------------------------------------------
// Java block states (Mojang report)

type javaState struct {
	id      int
	block   string
	props   map[string]string
	def     bool
	wlogged bool // waterlogged=true
}

type javaBlock struct {
	name       string
	first, end int // state id range [first, end)
	def        int
	propNames  []string
}

type javaData struct {
	states []javaState
	blocks map[string]*javaBlock
}

func loadJava(path string) *javaData {
	raw := must(os.ReadFile(path))
	var rep map[string]struct {
		Properties map[string][]string `json:"properties"`
		States     []struct {
			ID         int               `json:"id"`
			Default    bool              `json:"default"`
			Properties map[string]string `json:"properties"`
		} `json:"states"`
	}
	check(json.Unmarshal(raw, &rep))
	jd := &javaData{blocks: map[string]*javaBlock{}}
	n := 0
	for _, b := range rep {
		n += len(b.States)
	}
	jd.states = make([]javaState, n)
	for name, b := range rep {
		jb := &javaBlock{name: name, first: math.MaxInt, def: -1}
		for k := range b.Properties {
			jb.propNames = append(jb.propNames, k)
		}
		sort.Strings(jb.propNames)
		for _, s := range b.States {
			if s.ID < 0 || s.ID >= n || jd.states[s.ID].block != "" {
				log.Fatalf("bad/duplicate java state id %d", s.ID)
			}
			jd.states[s.ID] = javaState{id: s.ID, block: name, props: s.Properties, def: s.Default, wlogged: s.Properties["waterlogged"] == "true"}
			jb.first = min(jb.first, s.ID)
			jb.end = max(jb.end, s.ID+1)
			if s.Default {
				jb.def = s.ID
			}
		}
		if jb.end-jb.first != len(b.States) || jb.def < 0 {
			log.Fatalf("java block %s: non-contiguous states or no default", name)
		}
		jd.blocks[name] = jb
	}
	return jd
}

// ---------------------------------------------------------------------------------------------------------------
// Geyser blocks.nbt: list indexed by Java state id.

type geyserState struct {
	name  string         // full bedrock name
	props map[string]any // bedrock states
	key   string
}

func loadGeyserBlocks(path string, jd *javaData) []geyserState {
	zr := must(gzip.NewReader(bytes.NewReader(must(os.ReadFile(path)))))
	var m struct {
		Mappings []struct {
			ID    string         `nbt:"bedrock_identifier"`
			State map[string]any `nbt:"state"`
		} `nbt:"bedrock_mappings"`
	}
	check(nbt.NewDecoderWithEncoding(zr, nbt.BigEndian).Decode(&m))
	if len(m.Mappings) != len(jd.states) {
		log.Fatalf("blocks.nbt has %d entries, blocks.json %d states", len(m.Mappings), len(jd.states))
	}
	gs := make([]geyserState, len(m.Mappings))
	for i, e := range m.Mappings {
		id := e.ID
		if id == "" {
			id = strings.TrimPrefix(jd.states[i].block, "minecraft:")
		}
		name := "minecraft:" + id
		gs[i] = geyserState{name: name, props: e.State, key: stateKey(name, e.State)}
	}
	return gs
}

// stateKey is the canonical Bedrock state identity: name plus properties sorted by key, values typed.
func stateKey(name string, props map[string]any) string {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(name)
	b.WriteByte('[')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(valString(props[k]))
	}
	b.WriteByte(']')
	return b.String()
}

func valString(v any) string {
	if s, ok := v.(string); ok {
		return fmt.Sprintf("%q", s)
	}
	if n, ok := intVal(v); ok {
		return fmt.Sprint(n)
	}
	log.Fatalf("unexpected property value type %T", v)
	return ""
}

func intVal(v any) (int64, bool) {
	switch v := v.(type) {
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	case uint8:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	}
	return 0, false
}

// ---------------------------------------------------------------------------------------------------------------
// Blocks

// Fallback reasons, in the order they are tried.
const (
	reasonExact     = "exact"
	reasonOverlap   = "same-bedrock-name-overlap"
	reasonAlias     = "alias-overlap"
	reasonJavaName  = "java-same-name-default"
	reasonManual    = "manual"
	reasonInvisible = "invisible-air"
	reasonStone     = "stone"
)

// aliases adds the Java candidates of other Bedrock names to the overlap match of a Bedrock name. Bedrock splits
// still/flowing liquids into two names (Geyser uses water for level 0 and flowing_water for 1-15, so a still
// water state with depth 3 must match flowing_water). Names starting with hard_ (Education hardened glass)
// alias to the name without the prefix.
var aliases = map[string][]string{
	"minecraft:water":                     {"minecraft:flowing_water"},
	"minecraft:flowing_water":             {"minecraft:water"},
	"minecraft:lava":                      {"minecraft:flowing_lava"},
	"minecraft:flowing_lava":              {"minecraft:lava"},
	"minecraft:deprecated_anvil":          {"minecraft:anvil"},
	"minecraft:deprecated_purpur_block_1": {"minecraft:purpur_block"},
	"minecraft:deprecated_purpur_block_2": {"minecraft:purpur_block"},
	"minecraft:underwater_tnt":            {"minecraft:tnt"},
}

func aliasesOf(name string) []string {
	if a, ok := aliases[name]; ok {
		return a
	}
	if rest, ok := strings.CutPrefix(name, "minecraft:hard_"); ok {
		return []string{"minecraft:" + rest}
	}
	return nil
}

// manual maps Bedrock-only block names that have no counterpart with a similar state layout to a Java block
// (its default state is used).
var manual = map[string]string{
	"minecraft:border_block":                     "minecraft:barrier",
	"minecraft:chalkboard":                       "minecraft:oak_wall_sign",
	"minecraft:allow":                            "minecraft:bedrock",
	"minecraft:deny":                             "minecraft:bedrock",
	"minecraft:netherreactor":                    "minecraft:iron_block",
	"minecraft:glowingobsidian":                  "minecraft:crying_obsidian",
	"minecraft:stonecutter":                      "minecraft:stonecutter",
	"minecraft:frame":                            "minecraft:air",
	"minecraft:glow_frame":                       "minecraft:air",
	"minecraft:client_request_placeholder_block": "minecraft:air",
	"minecraft:camera":                           "minecraft:observer",
	"minecraft:invisible_bedrock":                "minecraft:barrier",
	"minecraft:chemical_heat":                    "minecraft:furnace",
	"minecraft:reserved6":                        "minecraft:stone",
	"minecraft:info_update":                      "minecraft:stone",
	"minecraft:info_update2":                     "minecraft:stone",
	"minecraft:underwater_torch":                 "minecraft:torch",
	"minecraft:colored_torch_red":                "minecraft:redstone_torch",
	"minecraft:colored_torch_green":              "minecraft:torch",
	"minecraft:colored_torch_blue":               "minecraft:soul_torch",
	"minecraft:colored_torch_purple":             "minecraft:soul_torch",
	"minecraft:compound_creator":                 "minecraft:crafting_table",
	"minecraft:material_reducer":                 "minecraft:crafting_table",
	"minecraft:element_constructor":              "minecraft:crafting_table",
	"minecraft:lab_table":                        "minecraft:crafting_table",
}

// invisible lists technical Bedrock blocks that render as nothing (or are never placed in worlds).
var invisible = map[string]bool{
	"minecraft:unknown":            true,
	"minecraft:moving_block":       true,
	"minecraft:portal_placeholder": true,
}

// crossBlockPreference picks the Java block for Bedrock states shared by several Java blocks when no Java block
// has the Bedrock name itself.
var crossBlockPreference = map[string]string{
	"minecraft:bed":        "minecraft:red_bed",
	"minecraft:flower_pot": "minecraft:flower_pot",
}

type blockDecision struct {
	rid    uint32
	hash   uint32
	key    string
	name   string
	java   int
	reason string
	ncands int
}

type blocksResult struct {
	nStates, nJava, nGeyserKeys int
	reasons                     map[string]int
	reasonNames                 map[string]map[string]int // reason -> bedrock name -> states
	ambiguousStates             int
	ambiguousProps              map[string]map[string]bool // java block -> props that vary
	crossBlock                  map[string][]string        // bedrock name -> java blocks
	geyserMissing               []string                   // geyser bedrock keys dragonfly lacks
	waterlogRanges              int
	deps                        []dep
}

func genBlocks(jd *javaData, gs []geyserState) *blocksResult {
	reg := world.DefaultBlockRegistry
	res := &blocksResult{reasons: map[string]int{}, reasonNames: map[string]map[string]int{},
		ambiguousProps: map[string]map[string]bool{}, crossBlock: map[string][]string{}, nJava: len(jd.states)}

	rev := map[string][]int{}    // bedrock key -> java ids (waterlogged=true excluded)
	byName := map[string][]int{} // bedrock name -> java ids
	for id, g := range gs {
		if jd.states[id].wlogged {
			continue
		}
		rev[g.key] = append(rev[g.key], id)
		byName[g.name] = append(byName[g.name], id)
	}
	res.nGeyserKeys = len(rev)

	// distance between a java state and its block's default state, ignoring waterlogged
	defDiff := func(id int) int {
		s := jd.states[id]
		d := jd.states[jd.blocks[s.block].def]
		n := 0
		for k, v := range s.props {
			if k != "waterlogged" && d.props[k] != v {
				n++
			}
		}
		return n
	}
	pickCanonical := func(name string, cands []int) int {
		blocks := map[string][]int{}
		var order []string
		for _, id := range cands {
			b := jd.states[id].block
			if _, ok := blocks[b]; !ok {
				order = append(order, b)
			}
			blocks[b] = append(blocks[b], id)
		}
		chosen := order[0]
		if len(order) > 1 {
			if _, ok := blocks[name]; ok {
				chosen = name
			} else if p, ok := crossBlockPreference[name]; ok {
				if _, ok := blocks[p]; ok {
					chosen = p
				}
			}
			if _, seen := res.crossBlock[name]; !seen {
				res.crossBlock[name] = order
			}
		}
		ids := blocks[chosen]
		if len(ids) > 1 {
			props := res.ambiguousProps[chosen]
			if props == nil {
				props = map[string]bool{}
				res.ambiguousProps[chosen] = props
			}
			for _, id := range ids[1:] {
				for k, v := range jd.states[id].props {
					if jd.states[ids[0]].props[k] != v {
						props[k] = true
					}
				}
			}
		}
		return slices.MinFunc(ids, func(a, b int) int {
			if c := defDiff(a) - defDiff(b); c != 0 {
				return c
			}
			return a - b
		})
	}
	overlap := func(props map[string]any, cands []int) int {
		best, bestScore := -1, -1.0
		for _, id := range cands {
			gp := gs[id].props
			score := 0.0
			for k, v := range props {
				gv, ok := gp[k]
				if !ok {
					continue
				}
				if valString(gv) == valString(v) {
					score++
					continue
				}
				a, ok1 := intVal(v)
				b, ok2 := intVal(gv)
				if ok1 && ok2 && magnitude(k) {
					score += 0.5 / float64(1+abs(a-b))
				}
			}
			// Exact-score ties: prefer states closest to their block default, then lowest id.
			if score > bestScore || (score == bestScore && (defDiff(id) < defDiff(best) || (defDiff(id) == defDiff(best) && id < best))) {
				best, bestScore = id, score
			}
		}
		return best
	}

	n := reg.BlockCount()
	res.nStates = n
	dfKeys := map[string]bool{}
	decisions := make([]blockDecision, 0, n)
	for rid := uint32(0); rid < uint32(n); rid++ {
		name, props, _ := reg.RuntimeIDToState(rid)
		hash, ok := reg.RuntimeIDToHash(rid)
		if !ok {
			log.Fatalf("no network hash for rid %d", rid)
		}
		key := stateKey(name, props)
		dfKeys[key] = true
		d := blockDecision{rid: rid, hash: hash, key: key, name: name}
		if invisible[name] {
			d.java, d.reason = jd.blocks["minecraft:air"].def, reasonInvisible
		} else if cands := rev[key]; len(cands) > 0 {
			d.java, d.reason, d.ncands = pickCanonical(name, cands), reasonExact, len(cands)
			if len(cands) > 1 {
				res.ambiguousStates++
			}
		} else if cands := withAliases(byName, name); len(cands) > 0 {
			d.java, d.reason = overlap(props, cands), reasonOverlap
			if len(byName[name]) == 0 {
				d.reason = reasonAlias
			}
		} else if jb, ok := jd.blocks[name]; ok {
			d.java, d.reason = jb.def, reasonJavaName
		} else if m, ok := manual[name]; ok {
			d.java, d.reason = jd.blocks[m].def, reasonManual
		} else {
			d.java, d.reason = jd.blocks["minecraft:stone"].def, reasonStone
		}
		res.reasons[d.reason]++
		if d.reason != reasonExact {
			if res.reasonNames[d.reason] == nil {
				res.reasonNames[d.reason] = map[string]int{}
			}
			res.reasonNames[d.reason][name]++
		}
		decisions = append(decisions, d)
	}
	for k := range rev {
		if !dfKeys[k] {
			res.geyserMissing = append(res.geyserMissing, k)
		}
	}
	sort.Strings(res.geyserMissing)

	// Per-name defaults for runtime ids that are not in the hash table (custom registries).
	nameDefault := map[string]int{}
	for _, d := range decisions {
		cur, ok := nameDefault[d.name]
		if !ok || (jd.states[d.java].def && !jd.states[cur].def) {
			nameDefault[d.name] = d.java
		}
	}

	// Waterlog ranges.
	type wrange struct{ first, end, delta int }
	var wr []wrange
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
			// find the waterlogged twin
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
		// check parity rule used at runtime: waterlogged=false iff ((id-first)/delta)%2 == 1
		for id := jb.first; id < jb.end; id++ {
			if ((id-jb.first)/delta)%2 == 1 == jd.states[id].wlogged {
				log.Fatalf("waterlog parity rule fails for %s", jb.name)
			}
		}
		wr = append(wr, wrange{jb.first, jb.end, delta})
	}
	sort.Slice(wr, func(i, j int) bool { return wr[i].first < wr[j].first })
	res.waterlogRanges = len(wr)

	// Emit blocks_gen.go
	slices.SortFunc(decisions, func(a, b blockDecision) int { return int(int64(a.hash) - int64(b.hash)) })
	var b bytes.Buffer
	b.WriteString(header)
	fmt.Fprintf(&b, "// JavaStateCount is the number of Java 26.3 block states (the direct palette size).\nconst JavaStateCount = %d\n\n", len(jd.states))
	fmt.Fprintf(&b, "// Java state ids of a few blocks the runtime needs.\nconst (\n\tJavaAir int32 = %d\n\tJavaStone int32 = %d\n\tJavaWater int32 = %d\n)\n\n",
		jd.blocks["minecraft:air"].def, jd.blocks["minecraft:stone"].def, jd.blocks["minecraft:water"].def)
	b.WriteString("// stateTable holds hash<<32 | javaState for every Bedrock state Dragonfly registers, sorted by hash.\n" +
		"// hash is the Bedrock network block hash (FNV-1a of the name + sorted states NBT), i.e. the state identity.\n")
	b.WriteString("var stateTable = [...]uint64{\n")
	for i, d := range decisions {
		if i > 0 && d.hash == decisions[i-1].hash {
			log.Fatalf("duplicate network hash %x", d.hash)
		}
		fmt.Fprintf(&b, "0x%08x%08x,", d.hash, uint32(d.java))
		if i%6 == 5 {
			b.WriteByte('\n')
		}
	}
	b.WriteString("\n}\n\n")
	b.WriteString("// nameDefault maps a Bedrock block name to a Java state, for states missing from stateTable.\nvar nameDefault = map[string]int32{\n")
	for _, k := range sortedKeys(nameDefault) {
		fmt.Fprintf(&b, "%q: %d,\n", k, nameDefault[k])
	}
	b.WriteString("}\n\n")
	b.WriteString("// waterlogRanges lists Java blocks with a waterlogged property: [first, end) state ids and the id distance\n" +
		"// between a waterlogged=true state and its waterlogged=false twin.\nvar waterlogRanges = [...][3]int32{\n")
	for _, w := range wr {
		fmt.Fprintf(&b, "{%d, %d, %d},\n", w.first, w.end, w.delta)
	}
	b.WriteString("}\n\n")

	deps := classifyDeps(res)
	b.WriteString("// neighbourDependent lists Java blocks whose state the Bedrock state does not fully determine.\nvar neighbourDependent = []Dependency{\n")
	for _, d := range deps {
		fmt.Fprintf(&b, "{Block: %q, Properties: %#v, Source: %s},\n", d.block, d.props, d.source)
	}
	b.WriteString("}\n")
	writeGo("blocks_gen.go", b.Bytes())
	res.deps = deps

	if *dump != "" {
		var o bytes.Buffer
		slices.SortFunc(decisions, func(a, b blockDecision) int { return strings.Compare(a.key, b.key) })
		for _, d := range decisions {
			s := jd.states[d.java]
			fmt.Fprintf(&o, "%s\t%s\t%d %s%s\t%d\n", d.key, d.reason, d.java, s.block, propString(s.props), d.ncands)
		}
		check(os.WriteFile(*dump, o.Bytes(), 0o644))
	}
	return res
}

func withAliases(byName map[string][]int, name string) []int {
	c := slices.Clone(byName[name])
	for _, a := range aliasesOf(name) {
		c = append(c, byName[a]...)
	}
	return c
}

// magnitude reports whether an integer Bedrock property is a quantity (depth, growth, signal...) where a near
// value is a better match than a far one, rather than an enumeration such as a direction.
func magnitude(prop string) bool {
	for _, s := range []string{"direction", "facing", "_bits", "rotation", "_type"} {
		if strings.Contains(prop, s) {
			return false
		}
	}
	return true
}

func propString(p map[string]string) string {
	if len(p) == 0 {
		return ""
	}
	var parts []string
	for _, k := range sortedKeys(p) {
		parts = append(parts, k+"="+p[k])
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func sameExcept(a, b map[string]string, skip string) bool {
	for k, v := range a {
		if k != skip && b[k] != v {
			return false
		}
	}
	return true
}

type dep struct {
	block  string
	props  []string
	source string
}

// Where the missing Java property comes from.
var propSource = map[string]string{
	"north": "SourceNeighbours", "east": "SourceNeighbours", "south": "SourceNeighbours", "west": "SourceNeighbours",
	"up": "SourceNeighbours", "down": "SourceNeighbours", "shape": "SourceNeighbours", "snowy": "SourceNeighbours",
	"type": "SourceNeighbours", "instrument": "SourceNeighbours", "signal_fire": "SourceNeighbours",
	"locked": "SourceNeighbours", "attached": "SourceNeighbours", "in_wall": "SourceNeighbours",
	"powered": "SourceRedstone", "power": "SourceRedstone", "triggered": "SourceRedstone", "enabled": "SourceRedstone",
	"note": "SourceBlockEntity", "has_book": "SourceBlockEntity", "rotation": "SourceBlockEntity",
	"copper_golem_pose": "SourceBlockEntity", "facing": "SourceBlockEntity", "has_record": "SourceBlockEntity",
	"cracked": "SourceBlockEntity", "extended": "SourceBlockEntity", "mode": "SourceBlockEntity",
	"bottom":   "SourceNeighbours",
	"distance": "SourceNone", "stage": "SourceNone", "short": "SourceNone",
}

// crossBlockSource says how to pick the Java block for Bedrock states several Java blocks share (default: the
// block entity, e.g. bed and banner colour, flower pot contents).
var crossBlockSource = map[string]string{
	"minecraft:air":            "SourceNone",       // air / cave_air / void_air
	"minecraft:unknown":        "SourceNone",       // mapped to air
	"minecraft:kelp":           "SourceNeighbours", // kelp (top) vs kelp_plant: kelp above?
	"minecraft:cave_vines":     "SourceNeighbours", // cave_vines (tip) vs cave_vines_plant
	"minecraft:twisting_vines": "SourceNeighbours",
	"minecraft:weeping_vines":  "SourceNeighbours",
}

// blockPropSource overrides propSource for one block.
func blockPropSource(block, prop string) (string, bool) {
	if prop == "facing" && (strings.HasSuffix(block, "_button") || block == "minecraft:lever") {
		// floor/ceiling buttons and levers: Bedrock keeps two orientations at most, Java four
		return "SourceNone", true
	}
	if prop == "type" && block == "minecraft:moving_piston" {
		return "SourceBlockEntity", true
	}
	src, ok := propSource[prop]
	return src, ok
}

func classifyDeps(res *blocksResult) []dep {
	var deps []dep
	for _, blk := range sortedKeys(res.ambiguousProps) {
		props := sortedKeys(res.ambiguousProps[blk])
		// group per source so each entry has one source
		bySrc := map[string][]string{}
		for _, p := range props {
			src, ok := blockPropSource(blk, p)
			if !ok {
				src = "SourceUnknown"
			}
			bySrc[src] = append(bySrc[src], p)
		}
		for _, src := range sortedKeys(bySrc) {
			deps = append(deps, dep{blk, bySrc[src], src})
		}
	}
	for _, name := range sortedKeys(res.crossBlock) {
		src, ok := crossBlockSource[name]
		if !ok {
			src = "SourceBlockEntity"
		}
		deps = append(deps, dep{name, []string{"<block>"}, src})
	}
	return deps
}

// ---------------------------------------------------------------------------------------------------------------
// Items

type itemsResult struct {
	nJava, nDragonfly int
	exact, nameOnly   int
	fallbacks         map[string][]string // reason -> items
	ambiguous         map[string][]string // bedrock name/meta -> java items
	metaNames         []string
}

// itemAliases maps Bedrock item names with no Geyser entry to a Java item.
var itemAliases = map[string]string{
	"minecraft:glow_frame":           "minecraft:glow_item_frame",
	"minecraft:frame":                "minecraft:item_frame",
	"minecraft:fireworks":            "minecraft:firework_rocket",
	"minecraft:firework_star":        "minecraft:firework_star",
	"minecraft:netherreactor":        "minecraft:iron_block",
	"minecraft:glowingobsidian":      "minecraft:crying_obsidian",
	"minecraft:border_block":         "minecraft:barrier",
	"minecraft:camera":               "minecraft:spyglass",
	"minecraft:chalkboard":           "minecraft:oak_sign",
	"minecraft:allow":                "minecraft:bedrock",
	"minecraft:deny":                 "minecraft:bedrock",
	"minecraft:underwater_torch":     "minecraft:torch",
	"minecraft:colored_torch_red":    "minecraft:redstone_torch",
	"minecraft:colored_torch_green":  "minecraft:torch",
	"minecraft:colored_torch_blue":   "minecraft:soul_torch",
	"minecraft:colored_torch_purple": "minecraft:soul_torch",
	"minecraft:compound_creator":     "minecraft:crafting_table",
	"minecraft:material_reducer":     "minecraft:crafting_table",
	"minecraft:element_constructor":  "minecraft:crafting_table",
	"minecraft:lab_table":            "minecraft:crafting_table",
	"minecraft:invisible_bedrock":    "minecraft:barrier",
}

// itemPreference picks the Java item for Bedrock (name, meta) pairs several Java items map to.
var itemPreference = map[string]string{
	"minecraft:filled_map":      "minecraft:filled_map",
	"minecraft:arrow":           "minecraft:arrow",
	"minecraft:book":            "minecraft:book",
	"minecraft:stick":           "minecraft:stick",
	"minecraft:hopper_minecart": "minecraft:hopper_minecart",
	"minecraft:unknown":         "minecraft:air",
}

func genItems(itemsPath, regPath string) *itemsResult {
	var geyser map[string]struct {
		Bedrock string `json:"bedrock_identifier"`
		Data    *int   `json:"bedrock_data"`
	}
	check(json.Unmarshal(must(os.ReadFile(itemsPath)), &geyser))
	var reg map[string]struct {
		Entries map[string]struct {
			ID int `json:"protocol_id"`
		} `json:"entries"`
	}
	check(json.Unmarshal(must(os.ReadFile(regPath)), &reg))
	ids := map[string]int{}
	for k, v := range reg["minecraft:item"].Entries {
		ids[k] = v.ID
	}
	res := &itemsResult{nJava: len(ids), fallbacks: map[string][]string{}, ambiguous: map[string][]string{}}

	type nm = nameMeta
	byNM := map[nm][]string{}
	byName := map[string]map[string]bool{}
	for java, g := range geyser {
		if _, ok := ids[java]; !ok {
			log.Fatalf("geyser item %s not in registries.json", java)
		}
		meta := 0
		if g.Data != nil {
			meta = *g.Data
		}
		byNM[nm{g.Bedrock, meta}] = append(byNM[nm{g.Bedrock, meta}], java)
		if byName[g.Bedrock] == nil {
			byName[g.Bedrock] = map[string]bool{}
		}
		byName[g.Bedrock][java] = true
	}
	pick := func(key string, javas []string) string {
		slices.Sort(javas)
		if len(javas) == 1 {
			return javas[0]
		}
		res.ambiguous[key] = javas
		if p, ok := itemPreference[strings.SplitN(key, "@", 2)[0]]; ok {
			return p
		}
		if slices.Contains(javas, strings.SplitN(key, "@", 2)[0]) {
			return strings.SplitN(key, "@", 2)[0]
		}
		return javas[0]
	}
	// name-level table: names whose Java item does not depend on meta.
	nameTable := map[string]int{}
	metaTable := map[string]int{} // "name@meta"
	for name, javas := range byName {
		if len(javas) == 1 || allSameNM(byNM, name) {
			var list []string
			for j := range javas {
				list = append(list, j)
			}
			nameTable[name] = ids[pick(name, list)]
			continue
		}
		res.metaNames = append(res.metaNames, name)
		lowest := math.MaxInt
		for k, v := range byNM {
			if k.name != name {
				continue
			}
			key := fmt.Sprintf("%s@%d", name, k.meta)
			metaTable[key] = ids[pick(key, v)]
			if k.meta < lowest {
				lowest = k.meta
				nameTable[name] = metaTable[key] // meta fallback: lowest meta
			}
		}
	}
	slices.Sort(res.metaNames)

	// Resolve every Dragonfly item.
	for _, it := range world.Items() {
		name, meta := it.EncodeItem()
		res.nDragonfly++
		desc := fmt.Sprintf("%s:%d", name, meta)
		if _, ok := metaTable[fmt.Sprintf("%s@%d", name, meta)]; ok {
			res.exact++
			continue
		}
		if _, ok := nameTable[name]; ok {
			if slices.Contains(res.metaNames, name) {
				res.fallbacks["meta-unknown-lowest-meta"] = append(res.fallbacks["meta-unknown-lowest-meta"], desc)
			} else {
				res.nameOnly++
			}
			continue
		}
		if id, ok := ids[name]; ok {
			nameTable[name] = id
			res.fallbacks["java-same-name"] = append(res.fallbacks["java-same-name"], desc)
			continue
		}
		if strings.HasPrefix(name, "minecraft:light_block_") {
			nameTable[name] = ids["minecraft:light"]
			res.fallbacks["alias"] = append(res.fallbacks["alias"], desc)
			continue
		}
		if a, ok := itemAliases[name]; ok {
			nameTable[name] = ids[a]
			res.fallbacks["alias"] = append(res.fallbacks["alias"], desc)
			continue
		}
		res.fallbacks["unmapped-barrier"] = append(res.fallbacks["unmapped-barrier"], desc)
	}
	for _, v := range res.fallbacks {
		slices.Sort(v)
	}

	var b bytes.Buffer
	b.WriteString(header)
	fmt.Fprintf(&b, "// JavaItemCount is the number of Java 26.3 items.\nconst JavaItemCount = %d\n\n", len(ids))
	fmt.Fprintf(&b, "// Java item ids the runtime needs.\nconst (\n\tJavaItemAir int32 = %d\n\tJavaItemBarrier int32 = %d\n)\n\n", ids["minecraft:air"], ids["minecraft:barrier"])
	b.WriteString("// itemByName maps a Bedrock item name to a Java item id when the meta does not select the item.\nvar itemByName = map[string]int32{\n")
	for _, k := range sortedKeys(nameTable) {
		fmt.Fprintf(&b, "%q: %d,\n", k, nameTable[k])
	}
	b.WriteString("}\n\n// itemByMeta maps Bedrock items whose meta selects the Java item (banners, beds, ...).\nvar itemByMeta = map[itemKey]int32{\n")
	for _, k := range sortedKeys(metaTable) {
		parts := strings.SplitN(k, "@", 2)
		fmt.Fprintf(&b, "{%q, %s}: %d,\n", parts[0], parts[1], metaTable[k])
	}
	b.WriteString("}\n")
	writeGo("items_gen.go", b.Bytes())
	return res
}

type nameMeta struct {
	name string
	meta int
}

func allSameNM(byNM map[nameMeta][]string, name string) bool {
	// true when every Java item for this Bedrock name shares one meta (meta can't distinguish them anyway)
	metas := map[int]bool{}
	for k := range byNM {
		if k.name == name {
			metas[k.meta] = true
		}
	}
	return len(metas) == 1
}

// ---------------------------------------------------------------------------------------------------------------
// Biomes

type biomesResult struct {
	nGeyser, nDragonfly int
	exact               int
	ambiguous           map[int][]string
	fallbacks           []string
}

var biomePreference = map[int]string{
	9: "minecraft:the_end",
	7: "minecraft:river",
}

// legacyBiomes maps Bedrock biomes Java removed in 1.18 to the biome Java converts them to.
var legacyBiomes = map[string]string{
	"legacy_frozen_ocean":              "minecraft:frozen_ocean",
	"ice_mountains":                    "minecraft:snowy_plains",
	"mushroom_island_shore":            "minecraft:mushroom_fields",
	"desert_hills":                     "minecraft:desert",
	"forest_hills":                     "minecraft:forest",
	"taiga_hills":                      "minecraft:taiga",
	"extreme_hills_edge":               "minecraft:windswept_hills",
	"jungle_hills":                     "minecraft:jungle",
	"birch_forest_hills":               "minecraft:birch_forest",
	"cold_taiga_hills":                 "minecraft:snowy_taiga",
	"mega_taiga_hills":                 "minecraft:old_growth_pine_taiga",
	"mesa_plateau":                     "minecraft:badlands",
	"deep_warm_ocean":                  "minecraft:warm_ocean",
	"bamboo_jungle_hills":              "minecraft:bamboo_jungle",
	"desert_mutated":                   "minecraft:desert",
	"taiga_mutated":                    "minecraft:taiga",
	"swampland_mutated":                "minecraft:swamp",
	"jungle_mutated":                   "minecraft:jungle",
	"jungle_edge_mutated":              "minecraft:sparse_jungle",
	"birch_forest_hills_mutated":       "minecraft:old_growth_birch_forest",
	"roofed_forest_mutated":            "minecraft:dark_forest",
	"cold_taiga_mutated":               "minecraft:snowy_taiga",
	"redwood_taiga_hills_mutated":      "minecraft:old_growth_spruce_taiga",
	"extreme_hills_plus_trees_mutated": "minecraft:windswept_forest",
	"savanna_plateau_mutated":          "minecraft:windswept_savanna",
	"mesa_plateau_stone_mutated":       "minecraft:wooded_badlands",
	"mesa_plateau_mutated":             "minecraft:badlands",
}

func genBiomes(path string) *biomesResult {
	var geyser map[string]struct {
		ID int `json:"bedrock_id"`
	}
	check(json.Unmarshal(must(os.ReadFile(path)), &geyser))
	res := &biomesResult{nGeyser: len(geyser), ambiguous: map[int][]string{}}
	rev := map[int][]string{}
	javaNames := map[string]bool{}
	for k, v := range geyser {
		rev[v.ID] = append(rev[v.ID], k)
		javaNames[k] = true
	}
	table := map[int]string{}
	for id, names := range rev {
		slices.Sort(names)
		table[id] = names[0]
		if len(names) > 1 {
			res.ambiguous[id] = names
			if p, ok := biomePreference[id]; ok {
				table[id] = p
			}
		}
	}
	bs := world.Biomes()
	sort.Slice(bs, func(i, j int) bool { return bs[i].EncodeBiome() < bs[j].EncodeBiome() })
	for _, bi := range bs {
		res.nDragonfly++
		id := bi.EncodeBiome()
		if _, ok := table[id]; ok {
			res.exact++
			continue
		}
		if javaNames["minecraft:"+bi.String()] {
			table[id] = "minecraft:" + bi.String()
			res.fallbacks = append(res.fallbacks, fmt.Sprintf("%s (%d) -> same name", bi.String(), id))
			continue
		}
		if l, ok := legacyBiomes[bi.String()]; ok {
			if !javaNames[l] {
				log.Fatalf("legacy biome target %s is not a Java biome", l)
			}
			table[id] = l
			res.fallbacks = append(res.fallbacks, fmt.Sprintf("%s (%d) -> %s (legacy)", bi.String(), id, l))
			continue
		}
		table[id] = "minecraft:plains"
		res.fallbacks = append(res.fallbacks, fmt.Sprintf("%s (%d) -> plains", bi.String(), id))
	}
	var b bytes.Buffer
	b.WriteString(header)
	b.WriteString("// biomeNames maps a Bedrock biome id to the Java biome key.\nvar biomeNames = map[int]string{\n")
	keys := make([]int, 0, len(table))
	for k := range table {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%d: %q,\n", k, table[k])
	}
	b.WriteString("}\n")
	writeGo("biomes_gen.go", b.Bytes())
	return res
}

// ---------------------------------------------------------------------------------------------------------------
// Output helpers

const header = "// Code generated by javamapgen from GeyserMC mappings (Java 26.3 <-> Bedrock 1.26.50) and the Mojang 26.3 reports. DO NOT EDIT.\n\npackage javamap\n\n"

func writeGo(name string, src []byte) {
	f, err := format.Source(src)
	if err != nil {
		os.WriteFile(filepath.Join(*out, name+".broken"), src, 0o644)
		log.Fatalf("format %s: %v", name, err)
	}
	check(os.WriteFile(filepath.Join(*out, name), f, 0o644))
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func must[T any](v T, err error) T {
	check(err)
	return v
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// ---------------------------------------------------------------------------------------------------------------
// REPORT.md

func writeReport(br *blocksResult, ir *itemsResult, bi *biomesResult) {
	var b bytes.Buffer
	p := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	p("# javamap mapping report\n\n")
	p("Generated by `internal/javamapgen` (do not edit by hand; run `go generate ./javamap`).\n")
	p("Sources: GeyserMC mappings for Java 26.3 <-> Bedrock 1.26.50, Mojang 26.3 reports, Dragonfly's default registries.\n\n")

	p("## Blocks\n\n")
	p("| | count |\n|---|---:|\n")
	p("| Dragonfly Bedrock states | %d |\n", br.nStates)
	p("| Java 26.3 states | %d |\n", br.nJava)
	p("| distinct Bedrock states in Geyser's table (waterlogged=true excluded) | %d |\n", br.nGeyserKeys)
	p("| Geyser Bedrock states Dragonfly does not register | %d |\n", len(br.geyserMissing))
	order := []string{reasonExact, reasonOverlap, reasonAlias, reasonJavaName, reasonManual, reasonInvisible, reasonStone}
	for _, r := range order {
		p("| Dragonfly states resolved by `%s` | %d |\n", r, br.reasons[r])
	}
	p("| ... of the exact ones, Bedrock states with more than one Java candidate | %d |\n", br.ambiguousStates)
	p("| Java blocks with waterlogged ranges | %d |\n\n", br.waterlogRanges)

	p("Policy, in order:\n\n")
	p("1. `exact`: the Bedrock state appears in Geyser's Java->Bedrock table. With several Java candidates the Java block\n")
	p("   whose name equals the Bedrock name wins (else a preference table, else the lowest id block), and within\n")
	p("   the block the state with the fewest properties differing from the Java default state (then lowest id).\n")
	p("   waterlogged=true Java states are never chosen; use `Waterlogged` when layer 1 holds water.\n")
	p("2. `same-bedrock-name-overlap`: the Bedrock name is in Geyser's table but this exact state is not (unreachable\n")
	p("   Bedrock permutations). Pick the Java state whose Bedrock state shares the most property values;\n")
	p("   integer properties that differ score by closeness; ties go to the Java default state.\n")
	p("   Candidates include those of alias names: water <-> flowing_water and lava <-> flowing_lava (Geyser maps\n")
	p("   level 0 to water and levels 1-15 to flowing_water, Dragonfly emits still water with any depth).\n")
	p("3. `alias-overlap`: Bedrock-only names matched against another name's candidates (hard_* glass -> *,\n")
	p("   deprecated_anvil -> anvil, deprecated_purpur_block_* -> purpur_block, underwater_tnt -> tnt).\n")
	p("4. `java-same-name-default`: a Java block with the same name exists: its default state.\n")
	p("5. `manual`: hand table for Bedrock/Education-only blocks. frame/glow_frame map to air: Java item frames\n")
	p("   are entities, so a Java session has to spawn one per frame block.\n")
	p("6. `invisible-air`: technical blocks (unknown, moving_block, portal_placeholder), checked first.\n")
	p("7. `stone`: anything left. At runtime, states not in the generated table (custom registries) use the\n")
	p("   per-name default, else stone.\n\n")

	for _, r := range order[1:] {
		names := br.reasonNames[r]
		if len(names) == 0 {
			continue
		}
		p("### Fallback `%s` (%d states, %d names)\n\n", r, br.reasons[r], len(names))
		keys := sortedKeys(names)
		slices.SortStableFunc(keys, func(a, b string) int { return names[b] - names[a] })
		var parts []string
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s (%d)", strings.TrimPrefix(k, "minecraft:"), names[k]))
		}
		p("%s\n\n", strings.Join(parts, ", "))
	}
	if len(br.geyserMissing) > 0 {
		p("### Geyser Bedrock states Dragonfly does not register\n\n")
		for i, k := range br.geyserMissing {
			if i == 50 {
				p("- ... %d more\n", len(br.geyserMissing)-50)
				break
			}
			p("- `%s`\n", k)
		}
		p("\n")
	}

	p("### Bedrock states shared by several Java blocks\n\n")
	for _, k := range sortedKeys(br.crossBlock) {
		v := br.crossBlock[k]
		short := make([]string, len(v))
		for i, s := range v {
			short[i] = strings.TrimPrefix(s, "minecraft:")
		}
		p("- `%s` -> %s\n", k, strings.Join(short, ", "))
	}
	p("\n### Java state not determined by the Bedrock state (future fixers)\n\n")
	p("Exported as `javamap.NeighbourDependent()`. Source: Neighbours = derive from adjacent blocks at chunk encode and\n")
	p("on block updates (+6 neighbours); Redstone = derive from redstone power (usually Bedrock does not store it);\n")
	p("BlockEntity = from the block entity (Java block/property chosen per block entity); None = Bedrock does not keep it and the\n")
	p("default renders acceptably (or the client never sees it).\n\n")
	p("Not listed, because Bedrock 1.26.50 carries the state and Dragonfly computes it on placement and neighbour\n")
	p("updates: fence/pane/bars connections (`minecraft:connection_*`), wall connections, stair shape\n")
	p("(`minecraft:corner`), tripwire connections. These map exactly; only states stored before Dragonfly computed\n")
	p("them (imported worlds, structures pasted without neighbour updates) show what is stored.\n\n")
	bySrc := map[string][]string{}
	for _, d := range br.deps {
		bySrc[d.source] = append(bySrc[d.source], fmt.Sprintf("%s [%s]", strings.TrimPrefix(d.block, "minecraft:"), strings.Join(d.props, ",")))
	}
	for _, src := range sortedKeys(bySrc) {
		p("**%s** (%d): %s\n\n", strings.TrimPrefix(src, "Source"), len(bySrc[src]), strings.Join(bySrc[src], "; "))
	}

	p("## Items\n\n")
	p("| | count |\n|---|---:|\n")
	p("| Java 26.3 items | %d |\n| Dragonfly items (name, meta) | %d |\n", ir.nJava, ir.nDragonfly)
	p("| exact (name, meta) | %d |\n| by name (meta does not select the Java item) | %d |\n", ir.exact, ir.nameOnly)
	for _, k := range sortedKeys(ir.fallbacks) {
		p("| fallback `%s` | %d |\n", k, len(ir.fallbacks[k]))
	}
	p("\nBedrock names where meta selects the Java item: %s.\n\n", strings.Join(ir.metaNames, ", "))
	p("Ambiguous reverse mappings (first listed = chosen unless the preference table says otherwise):\n\n")
	for _, k := range sortedKeys(ir.ambiguous) {
		p("- `%s` -> %s\n", k, strings.Join(ir.ambiguous[k], ", "))
	}
	for _, k := range sortedKeys(ir.fallbacks) {
		p("\nFallback `%s`: %s\n", k, strings.Join(ir.fallbacks[k], ", "))
	}

	p("\n## Biomes\n\n")
	p("Geyser biomes: %d. Dragonfly biomes: %d, exact: %d, fallbacks: %d.\n\n", bi.nGeyser, bi.nDragonfly, bi.exact, len(bi.fallbacks))
	for _, id := range slices.Sorted(maps.Keys(bi.ambiguous)) {
		p("- Bedrock biome %d is shared by %s\n", id, strings.Join(bi.ambiguous[id], ", "))
	}
	for _, f := range bi.fallbacks {
		p("- fallback: %s\n", f)
	}
	check(os.WriteFile(filepath.Join(*out, "REPORT.md"), b.Bytes(), 0o644))
}
