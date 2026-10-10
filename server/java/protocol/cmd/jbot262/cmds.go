package main

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/df-mc/dragonfly/server/java/protocol/wire"
)

// dumpCommands decodes a 26.x commands packet and writes every root command with its direct
// children ("literal" names, or <argument:type>) to /tmp/jbot_cmds.txt, for checking what a
// Java client is sent.
func dumpCommands(body []byte) {
	r := wire.NewReader(body)
	type node struct {
		kind     byte
		children []int32
		name     string
		parser   int32
	}
	n := int(r.VarInt())
	nodes := make([]node, n)
	for i := 0; i < n && r.Err == nil; i++ {
		flags := r.Byte()
		nd := node{kind: flags & 3, parser: -1}
		cc := int(r.VarInt())
		for j := 0; j < cc; j++ {
			nd.children = append(nd.children, r.VarInt())
		}
		if flags&0x08 != 0 {
			r.VarInt() // redirect
		}
		if nd.kind == 1 || nd.kind == 2 {
			nd.name = r.String(32767)
		}
		if nd.kind == 2 {
			nd.parser = r.VarInt()
			switch nd.parser {
			case 1, 3: // float, integer: flags, then optional min/max (4 bytes each)
				f := r.Byte()
				if f&1 != 0 {
					r.Int32()
				}
				if f&2 != 0 {
					r.Int32()
				}
			case 2, 4: // double, long: 8 bytes each
				f := r.Byte()
				if f&1 != 0 {
					r.Int64()
				}
				if f&2 != 0 {
					r.Int64()
				}
			case 5: // string: kind
				r.VarInt()
			case 6, 31: // entity, score_holder: flags
				r.Byte()
			case 43: // time: minimum
				r.Int32()
			case 44, 45, 46, 47, 48: // resource-ish: registry key
				r.String(32767)
			}
			if flags&0x10 != 0 {
				r.String(32767) // suggestions type
			}
		}
		nodes[i] = nd
	}
	root := int(r.VarInt())
	if r.Err != nil {
		log.Printf("commands: decode error %v", r.Err)
	}
	var lines []string
	if root >= 0 && root < len(nodes) {
		for _, ci := range nodes[root].children {
			c := nodes[ci]
			var kids []string
			for _, k := range c.children {
				kn := nodes[k]
				if kn.kind == 1 {
					kids = append(kids, kn.name)
				} else {
					kids = append(kids, fmt.Sprintf("<%s:%d>", kn.name, kn.parser))
				}
			}
			lines = append(lines, fmt.Sprintf("/%s  %s", c.name, strings.Join(kids, " | ")))
		}
	}
	sort.Strings(lines)
	_ = os.WriteFile("/tmp/jbot_cmds.txt", []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	log.Printf("commands: %d nodes, %d root commands (written to /tmp/jbot_cmds.txt)", n, len(lines))
}
