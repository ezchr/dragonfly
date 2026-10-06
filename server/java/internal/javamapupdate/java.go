package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"math"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------------------------------------------
// Java block states (Mojang blocks.json report)

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
	values     map[string][]string
}

type javaData struct {
	states []javaState
	blocks map[string]*javaBlock
	index  map[string]int // stateString -> id
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
	jd := &javaData{blocks: map[string]*javaBlock{}, index: map[string]int{}}
	n := 0
	for _, b := range rep {
		n += len(b.States)
	}
	jd.states = make([]javaState, n)
	for name, b := range rep {
		jb := &javaBlock{name: name, first: math.MaxInt, def: -1, values: b.Properties}
		for k := range b.Properties {
			jb.propNames = append(jb.propNames, k)
		}
		sort.Strings(jb.propNames)
		for _, s := range b.States {
			if s.ID < 0 || s.ID >= n || jd.states[s.ID].block != "" {
				log.Fatalf("bad/duplicate java state id %d", s.ID)
			}
			jd.states[s.ID] = javaState{id: s.ID, block: name, props: s.Properties, def: s.Default, wlogged: s.Properties["waterlogged"] == "true"}
			jd.index[stateString(name, s.Properties)] = s.ID
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

// stateString is the id-independent name of a Java state: block[k=v,...] with sorted keys.
func stateString(block string, props map[string]string) string {
	if len(props) == 0 {
		return block
	}
	var b strings.Builder
	b.WriteString(block)
	b.WriteByte('[')
	for i, k := range sortedKeys(props) {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(props[k])
	}
	b.WriteByte(']')
	return b.String()
}

func parseStateString(s string) (string, map[string]string) {
	name, rest, ok := strings.Cut(s, "[")
	props := map[string]string{}
	if ok {
		for _, kv := range strings.Split(strings.TrimSuffix(rest, "]"), ",") {
			k, v, _ := strings.Cut(kv, "=")
			props[k] = v
		}
	}
	return name, props
}

// id returns the state id of a Java state string.
func (jd *javaData) id(s string) (int, bool) {
	id, ok := jd.index[s]
	return id, ok
}

func (jd *javaData) str(id int) string {
	return stateString(jd.states[id].block, jd.states[id].props)
}

// closest returns the state of block that keeps as many of props as the block still has; other properties take
// the default state's values. dropped lists properties (or values) that could not be kept.
func (jd *javaData) closest(block string, props map[string]string) (int, []string) {
	jb := jd.blocks[block]
	want := map[string]string{}
	var dropped []string
	for k, v := range jd.states[jb.def].props {
		want[k] = v
	}
	for _, k := range sortedKeys(props) {
		v := props[k]
		if slices.Contains(jb.values[k], v) {
			want[k] = v
		} else {
			dropped = append(dropped, k+"="+v)
		}
	}
	id, ok := jd.index[stateString(block, want)]
	if !ok {
		log.Fatalf("closest: %s has no state %v", block, want)
	}
	return id, dropped
}

// defaultValue is the value of prop in block's default state.
func (jd *javaData) defaultValue(block, prop string) string {
	return jd.states[jd.blocks[block].def].props[prop]
}

// ---------------------------------------------------------------------------------------------------------------
// registries.json: items and sound events

type javaRegistries struct {
	items  map[string]int
	sounds map[string]int
}

func loadRegistries(path string) javaRegistries {
	var reg map[string]struct {
		Entries map[string]struct {
			ID int `json:"protocol_id"`
		} `json:"entries"`
	}
	check(json.Unmarshal(must(os.ReadFile(path)), &reg))
	r := javaRegistries{items: map[string]int{}, sounds: map[string]int{}}
	for k, v := range reg["minecraft:item"].Entries {
		r.items[k] = v.ID
	}
	for k, v := range reg["minecraft:sound_event"].Entries {
		r.sounds[k] = v.ID
	}
	if len(r.items) == 0 {
		log.Fatalf("%s: no minecraft:item registry", path)
	}
	return r
}

func itemNames(ids map[string]int) map[int]string {
	m := make(map[int]string, len(ids))
	for k, v := range ids {
		m[v] = k
	}
	return m
}

// loadBiomeNames reads the minecraft:worldgen/biome entries of a javagen registries.go (protocol/vNNN). The
// worldgen registries are data driven, so Mojang's registries.json report does not list them.
func loadBiomeNames(path string) []string {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	check(err)
	var names []string
	ast.Inspect(f, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		if k, ok := kv.Key.(*ast.BasicLit); ok && unquote(k.Value) == "minecraft:worldgen/biome" {
			for _, e := range kv.Value.(*ast.CompositeLit).Elts {
				names = append(names, unquote(e.(*ast.BasicLit).Value))
			}
			return false
		}
		return true
	})
	if len(names) == 0 {
		log.Fatalf("%s: no minecraft:worldgen/biome registry", path)
	}
	return names
}

func unquote(s string) string {
	u, err := strconv.Unquote(s)
	check(err)
	return u
}
