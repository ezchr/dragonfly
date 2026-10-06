package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// Optional cross-check against GeyserMC's Java -> Bedrock mappings of the same Java version. Geyser is never used
// to decide anything; every decision it disagrees with is reported.

type disagreement struct {
	what   string // Bedrock state / item / biome
	ours   string
	geyser []string
	status string // decision status (carried / new / ...)
	origin string
}

type geyserCheck struct {
	dir string

	blockChecked, blockNone int
	blocks                  []disagreement
	itemChecked, itemNone   int
	items                   []disagreement
	itemMissing             []string // Dragonfly items Geyser maps but the table does not
	biomeChecked            int
	biomes                  []disagreement
}

type soundCheck struct {
	checked int
	diff    []disagreement
	missing []string // Bedrock names Geyser maps to a Java event that the table lacks
}

func loadGeyserBlocks(path string, jd *javaData) map[string][]int {
	zr := must(gzip.NewReader(bytes.NewReader(must(os.ReadFile(path)))))
	var m struct {
		Mappings []struct {
			ID    string         `nbt:"bedrock_identifier"`
			State map[string]any `nbt:"state"`
		} `nbt:"bedrock_mappings"`
	}
	check(nbt.NewDecoderWithEncoding(zr, nbt.BigEndian).Decode(&m))
	if len(m.Mappings) != len(jd.states) {
		log.Fatalf("%s has %d entries, the Java reports %d states: Geyser mappings of another Java version", path, len(m.Mappings), len(jd.states))
	}
	rev := map[string][]int{}
	for i, e := range m.Mappings {
		if jd.states[i].wlogged {
			continue
		}
		id := e.ID
		if id == "" {
			id = strings.TrimPrefix(jd.states[i].block, "minecraft:")
		}
		k := stateKey("minecraft:"+id, e.State)
		rev[k] = append(rev[k], i)
	}
	return rev
}

func crossCheck(dir string, jd *javaData, reg javaRegistries, br *blocksResult, ir *itemsResult, bi *biomesResult, dfItems []nameMeta) *geyserCheck {
	gc := &geyserCheck{dir: dir}

	rev := loadGeyserBlocks(filepath.Join(dir, "blocks.nbt"), jd)
	for _, d := range br.decisions {
		cands := rev[d.df.key]
		if len(cands) == 0 {
			gc.blockNone++
			continue
		}
		gc.blockChecked++
		if slices.Contains(cands, d.java) {
			continue
		}
		var g []string
		for _, c := range cands {
			g = append(g, short(jd.str(c)))
		}
		gc.blocks = append(gc.blocks, disagreement{short(d.df.key), short(jd.str(d.java)), g, d.status, d.origin})
	}

	var items map[string]struct {
		Bedrock string `json:"bedrock_identifier"`
		Data    *int   `json:"bedrock_data"`
	}
	check(json.Unmarshal(must(os.ReadFile(filepath.Join(dir, "items.json"))), &items))
	byName := map[string][]string{}
	byMeta := map[string][]string{}
	for java, g := range items {
		meta := 0
		if g.Data != nil {
			meta = *g.Data
		}
		byName[g.Bedrock] = append(byName[g.Bedrock], java)
		k := fmt.Sprintf("%s@%d", g.Bedrock, meta)
		byMeta[k] = append(byMeta[k], java)
	}
	for _, e := range ir.entries {
		cands := byName[e.name]
		if e.hasMeta {
			cands = byMeta[itemLabel(e)]
		}
		if len(cands) == 0 {
			gc.itemNone++
			continue
		}
		gc.itemChecked++
		if !slices.Contains(cands, e.java) {
			slices.Sort(cands)
			gc.items = append(gc.items, disagreement{short(itemLabel(e)), short(e.java), shortAll(cands), e.status, e.origin})
		}
	}
	have := map[string]bool{}
	for _, e := range ir.entries {
		have[e.name] = true
	}
	for _, it := range dfItems {
		if !have[it.name] && len(byName[it.name]) > 0 {
			gc.itemMissing = append(gc.itemMissing, fmt.Sprintf("%s -> %s", short(it.name), strings.Join(shortAll(byName[it.name]), "/")))
		}
	}
	gc.itemMissing = slices.Compact(slices.Sorted(slices.Values(gc.itemMissing)))

	var biomes map[string]struct {
		ID int `json:"bedrock_id"`
	}
	check(json.Unmarshal(must(os.ReadFile(filepath.Join(dir, "biomes.json"))), &biomes))
	brev := map[int][]string{}
	for k, v := range biomes {
		brev[v.ID] = append(brev[v.ID], k)
	}
	for _, e := range bi.entries {
		cands := brev[e.id]
		if len(cands) == 0 {
			continue
		}
		gc.biomeChecked++
		if !slices.Contains(cands, e.java) {
			slices.Sort(cands)
			gc.biomes = append(gc.biomes, disagreement{fmt.Sprint(e.id), short(e.java), shortAll(cands), e.status, e.origin})
		}
	}
	return gc
}

func crossCheckSounds(dir string, reg javaRegistries, sr *soundsResult) *soundCheck {
	var m map[string]struct {
		Playsound string `json:"playsound_mapping"`
	}
	check(json.Unmarshal(must(os.ReadFile(filepath.Join(dir, "sounds.json"))), &m))
	rev := map[string][]string{}
	for j, v := range m {
		if v.Playsound == "" || v.Playsound == j {
			continue
		}
		if _, ok := reg.sounds["minecraft:"+j]; !ok {
			continue
		}
		rev[v.Playsound] = append(rev[v.Playsound], j)
	}
	sc := &soundCheck{}
	have := map[string]bool{}
	for _, p := range sr.pairs {
		have[p[0]] = true
		cands := rev[p[0]]
		if len(cands) == 0 {
			continue
		}
		sc.checked++
		if !slices.Contains(cands, p[1]) {
			slices.Sort(cands)
			sc.diff = append(sc.diff, disagreement{p[0], p[1], cands, "carried", ""})
		}
	}
	for _, b := range sortedKeys(rev) {
		if !have[b] {
			c := rev[b]
			slices.Sort(c)
			sc.missing = append(sc.missing, b+" -> "+c[0])
		}
	}
	return sc
}

func shortAll(s []string) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[i] = short(v)
	}
	return out
}
