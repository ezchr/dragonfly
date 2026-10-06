package main

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

	_ "github.com/df-mc/dragonfly/server"
	_ "github.com/df-mc/dragonfly/server/block"
	_ "github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	_ "github.com/df-mc/dragonfly/server/world/biome"
)

// bstate is a Bedrock block state: full name and properties with values in string form (strings as is,
// integers and booleans as decimal numbers).
type bstate struct {
	name  string
	props map[string]string
}

// stateKey is the canonical Bedrock state identity: name plus properties sorted by key, string values quoted.
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
		return strconv.Quote(s)
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

// parseKey turns a stateKey back into a bstate.
func parseKey(key string) (bstate, error) {
	name, rest, ok := strings.Cut(key, "[")
	if !ok || !strings.HasSuffix(rest, "]") {
		return bstate{}, fmt.Errorf("bad state key %q", key)
	}
	rest = strings.TrimSuffix(rest, "]")
	s := bstate{name: name, props: map[string]string{}}
	for rest != "" {
		k, v, ok := strings.Cut(rest, "=")
		if !ok {
			return bstate{}, fmt.Errorf("bad state key %q", key)
		}
		if strings.HasPrefix(v, `"`) {
			q, err := strconv.QuotedPrefix(v)
			if err != nil {
				return bstate{}, fmt.Errorf("bad state key %q: %v", key, err)
			}
			s.props[k] = must(strconv.Unquote(q))
			rest = strings.TrimPrefix(v[len(q):], ",")
			continue
		}
		val, after, _ := strings.Cut(v, ",")
		s.props[k] = val
		rest = after
	}
	return s, nil
}

func stringProps(props map[string]any) map[string]string {
	m := make(map[string]string, len(props))
	for k, v := range props {
		if s, ok := v.(string); ok {
			m[k] = s
		} else {
			n, _ := intVal(v)
			m[k] = fmt.Sprint(n)
		}
	}
	return m
}

// ---------------------------------------------------------------------------------------------------------------
// Dragonfly's registries

type dfState struct {
	rid  uint32
	hash uint32
	key  string
	b    bstate
}

func dragonflyStates() []dfState {
	reg := world.DefaultBlockRegistry
	reg.Finalize()
	n := reg.BlockCount()
	out := make([]dfState, 0, n)
	for rid := uint32(0); rid < uint32(n); rid++ {
		name, props, _ := reg.RuntimeIDToState(rid)
		hash, ok := reg.RuntimeIDToHash(rid)
		if !ok {
			log.Fatalf("no network hash for rid %d", rid)
		}
		out = append(out, dfState{rid: rid, hash: hash, key: stateKey(name, props), b: bstate{name, stringProps(props)}})
	}
	return out
}

type nameMeta struct {
	name string
	meta int
}

func dragonflyItems() []nameMeta {
	var out []nameMeta
	for _, it := range world.Items() {
		name, meta := it.EncodeItem()
		out = append(out, nameMeta{name, int(meta)})
	}
	return out
}

type dfBiome struct {
	id   int
	name string
}

func dragonflyBiomes() []dfBiome {
	var out []dfBiome
	for _, b := range world.Biomes() {
		out = append(out, dfBiome{b.EncodeBiome(), b.String()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}
