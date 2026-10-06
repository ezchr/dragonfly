package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"path/filepath"
	"strconv"
	"strings"
)

// decisions.txt is the id-independent form of the tables: every Bedrock -> Java decision by name and properties,
// with where it came from. It is what an update carries over; the _gen.go files are compiled from it.
const decisionsFile = "decisions.txt"

type prevBlock struct {
	key    string // Bedrock stateKey
	java   string // Java stateString
	origin string
}

type prevItem struct {
	name    string
	meta    int
	hasMeta bool // itemByMeta entry
	java    string
	origin  string
}

type prevBiome struct {
	id     int
	java   string
	origin string
}

type dep struct {
	block  string
	props  []string
	source string // SourceXxx identifier
}

type prevTables struct {
	java   string // Java version of the tables
	source string // where they were read from
	blocks []prevBlock
	items  []prevItem
	biomes []prevBiome
	deps   []dep
}

func short(name string) string { return strings.TrimPrefix(name, "minecraft:") }

func long(name string) string {
	if strings.Contains(name, ":") {
		return name
	}
	return "minecraft:" + name
}

// ---------------------------------------------------------------------------------------------------------------
// Bootstrap: read the previous tables from the generated Go files (stateTable is keyed by Bedrock network hash and
// holds Java ids, so it needs the Dragonfly the tables were made for and the Mojang reports of their Java version).

func bootstrap(dir, reportsDir, javaVer string, df []dfState) *prevTables {
	if reportsDir == "" {
		log.Fatalf("%s has no %s: pass -prev-reports (the Mojang reports the old tables were generated for) to bootstrap", dir, decisionsFile)
	}
	jd := loadJava(filepath.Join(reportsDir, "blocks.json"))
	reg := loadRegistries(filepath.Join(reportsDir, "registries.json"))
	t := &prevTables{java: javaVer, source: dir + " (_gen.go, bootstrap)"}

	bf := parseGo(filepath.Join(dir, "blocks_gen.go"))
	if n := constInt(bf, "JavaStateCount"); n != len(jd.states) {
		log.Fatalf("blocks_gen.go was generated for %d Java states, %s has %d: wrong -prev-reports", n, reportsDir, len(jd.states))
	}
	byHash := map[uint32]string{}
	for _, s := range df {
		byHash[s.hash] = s.key
	}
	undecodable := 0
	for _, e := range compositeElts(bf, "stateTable") {
		v := must(strconv.ParseUint(e.(*ast.BasicLit).Value, 0, 64))
		key, ok := byHash[uint32(v>>32)]
		if !ok {
			undecodable++
			continue
		}
		t.blocks = append(t.blocks, prevBlock{key: key, java: jd.str(int(uint32(v))), origin: "geyser"})
	}
	if undecodable > 0 {
		log.Printf("bootstrap: %d stateTable hashes are not states of this Dragonfly; their decisions are lost", undecodable)
	}
	for _, e := range compositeElts(bf, "neighbourDependent") {
		var d dep
		for _, f := range e.(*ast.CompositeLit).Elts {
			kv := f.(*ast.KeyValueExpr)
			switch kv.Key.(*ast.Ident).Name {
			case "Block":
				d.block = unquote(kv.Value.(*ast.BasicLit).Value)
			case "Properties":
				for _, p := range kv.Value.(*ast.CompositeLit).Elts {
					d.props = append(d.props, unquote(p.(*ast.BasicLit).Value))
				}
			case "Source":
				d.source = kv.Value.(*ast.Ident).Name
			}
		}
		t.deps = append(t.deps, d)
	}

	names := itemNames(reg.items)
	itf := parseGo(filepath.Join(dir, "items_gen.go"))
	if n := constInt(itf, "JavaItemCount"); n != len(reg.items) {
		log.Fatalf("items_gen.go was generated for %d Java items, %s has %d: wrong -prev-reports", n, reportsDir, len(reg.items))
	}
	for _, e := range compositeElts(itf, "itemByName") {
		kv := e.(*ast.KeyValueExpr)
		t.items = append(t.items, prevItem{name: unquote(kv.Key.(*ast.BasicLit).Value), java: names[intLit(kv.Value)], origin: "geyser"})
	}
	for _, e := range compositeElts(itf, "itemByMeta") {
		kv := e.(*ast.KeyValueExpr)
		k := kv.Key.(*ast.CompositeLit).Elts
		t.items = append(t.items, prevItem{name: unquote(k[0].(*ast.BasicLit).Value), meta: intLit(k[1]), hasMeta: true,
			java: names[intLit(kv.Value)], origin: "geyser"})
	}

	bif := parseGo(filepath.Join(dir, "biomes_gen.go"))
	for _, e := range compositeElts(bif, "biomeNames") {
		kv := e.(*ast.KeyValueExpr)
		t.biomes = append(t.biomes, prevBiome{id: intLit(kv.Key), java: unquote(kv.Value.(*ast.BasicLit).Value), origin: "geyser"})
	}
	return t
}

func parseGo(path string) *ast.File {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	check(err)
	return f
}

func compositeElts(f *ast.File, name string) []ast.Expr {
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, s := range g.Specs {
			vs, ok := s.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || vs.Names[0].Name != name {
				continue
			}
			return vs.Values[0].(*ast.CompositeLit).Elts
		}
	}
	log.Fatalf("no var %s", name)
	return nil
}

func constInt(f *ast.File, name string) int {
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, s := range g.Specs {
			vs := s.(*ast.ValueSpec)
			if len(vs.Names) == 1 && vs.Names[0].Name == name {
				return intLit(vs.Values[0])
			}
		}
	}
	log.Fatalf("no const %s", name)
	return 0
}

func intLit(e ast.Expr) int {
	neg := false
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.SUB {
		neg, e = true, u.X
	}
	n := must(strconv.ParseInt(e.(*ast.BasicLit).Value, 0, 64))
	if neg {
		n = -n
	}
	return int(n)
}
