package javasession

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	jchunk "github.com/df-mc/dragonfly/server/java/protocol/chunk"
	"github.com/df-mc/dragonfly/server/world"
)

// A Java client draws some blocks only through their block entity: shulker boxes, chests, heads,
// banners, beds, bells... It makes that block entity itself when a block changes, but for the
// blocks of a chunk it has only the ones the chunk packet lists. Without the list those blocks
// were invisible until something changed them (a handful of the shulker boxes of a map showed).
// Each is listed with no data: what the client needs to draw it is in the block state.

// javaBlockEntityTypes maps a Bedrock block name to the Java block entity type that draws it.
// Suffixes (leading "*") cover the coloured and wooden families.
var javaBlockEntityTypes = []struct{ bedrock, java string }{
	{"*shulker_box", "minecraft:shulker_box"},
	{"minecraft:chest", "minecraft:chest"},
	{"*_chest", "minecraft:chest"}, // copper chests; trapped and ender are matched before
	{"minecraft:trapped_chest", "minecraft:trapped_chest"},
	{"minecraft:ender_chest", "minecraft:ender_chest"},
	{"*_hanging_sign", "minecraft:hanging_sign"},
	{"*_sign", "minecraft:sign"},
	{"*_skull", "minecraft:skull"},
	{"*_head", "minecraft:skull"},
	{"minecraft:skull", "minecraft:skull"},
	{"*_banner", "minecraft:banner"},
	{"minecraft:bed", "minecraft:bed"},
	{"minecraft:bell", "minecraft:bell"},
	{"minecraft:enchanting_table", "minecraft:enchanting_table"},
	{"minecraft:decorated_pot", "minecraft:decorated_pot"},
	{"minecraft:conduit", "minecraft:conduit"},
	{"minecraft:end_portal", "minecraft:end_portal"},
	{"minecraft:end_gateway", "minecraft:end_gateway"},
	{"minecraft:lectern", "minecraft:lectern"},
	{"minecraft:mob_spawner", "minecraft:mob_spawner"},
	{"minecraft:trial_spawner", "minecraft:trial_spawner"},
	{"minecraft:vault", "minecraft:vault"},
	{"*copper_golem_statue", "minecraft:copper_golem_statue"}, // every oxidation, waxed or not
}

// javaBlockEntityName returns the Java block entity type for a Bedrock block name, or "".
func javaBlockEntityName(bedrock string) string {
	switch bedrock {
	case "minecraft:trapped_chest":
		return "minecraft:trapped_chest"
	case "minecraft:ender_chest":
		return "minecraft:ender_chest"
	}
	for _, t := range javaBlockEntityTypes {
		if suffix, ok := strings.CutPrefix(t.bedrock, "*"); ok {
			if strings.HasSuffix(bedrock, suffix) {
				return t.java
			}
		} else if bedrock == t.bedrock {
			return t.java
		}
	}
	return ""
}

// addBlockEntities lists in col the blocks of the chunk a Java client draws through a block entity.
func (s *Session) addBlockEntities(col *jchunk.Column, blocks map[cube.Pos]world.Block) {
	for pos, b := range blocks {
		if b == nil {
			continue
		}
		name, _ := b.EncodeBlock()
		java := javaBlockEntityName(name)
		if java == "" {
			continue
		}
		typ := s.ver.BuiltinID("minecraft:block_entity_type", java)
		if typ < 0 {
			continue // this client's version has no such block entity
		}
		col.BlockEntities = append(col.BlockEntities, jchunk.BlockEntity{
			XZ: uint8(pos[0]&15)<<4 | uint8(pos[2]&15), Y: int16(pos[1]), Type: typ,
		})
	}
}
