package version

import (
	"testing"

	v776 "github.com/df-mc/dragonfly/server/java/protocol/v776"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
)

func TestV776Packets(t *testing.T) {
	cb := map[int32]int32{
		v777.ClientboundPlayLogin:               v776.ClientboundPlayLogin,
		v777.ClientboundPlayAddEntity:           v776.ClientboundPlayAddEntity,
		v777.ClientboundPlayLevelChunkWithLight: v776.ClientboundPlayLevelChunkWithLight,
		v777.ClientboundPlaySetTime:             v776.ClientboundPlaySetTime,
		v777.ClientboundPlayKeepAlive:           v776.ClientboundPlayKeepAlive,
		v777.ClientboundPlayDisconnect:          v776.ClientboundPlayDisconnect,
		v777.ClientboundPlayContainerSetContent: v776.ClientboundPlayContainerSetContent,
		v777.ClientboundPlaySwingAnimation:      -1,
		v777.ClientboundPlayPlayerInfoUpdate:    v776.ClientboundPlayPlayerInfoUpdate,
		v777.ClientboundPlayForgetLevelChunk:    v776.ClientboundPlayForgetLevelChunk,
		v777.ClientboundPlaySystemChat:          v776.ClientboundPlaySystemChat,
	}
	for n, o := range cb {
		if got := V776.ClientboundPlay(n); got != o {
			t.Errorf("clientbound %#x: got %#x, want %#x", n, got, o)
		}
	}
	sb := map[int32]int32{
		v776.ServerboundPlayMovePlayerPos:  v777.ServerboundPlayMovePlayerPos,
		v776.ServerboundPlayUseItemOn:      v777.ServerboundPlayUseItemOn,
		v776.ServerboundPlayKeepAlive:      v777.ServerboundPlayKeepAlive,
		v776.ServerboundPlayContainerClick: v777.ServerboundPlayContainerClick,
		v776.ServerboundPlayChat:           v777.ServerboundPlayChat,
		v776.ServerboundPlaySwing:          -1,
		v776.ServerboundPlayInteract:       v777.ServerboundPlayInteract,
	}
	for o, n := range sb {
		if got := V776.ServerboundPlay(o); got != n {
			t.Errorf("serverbound %#x: got %#x, want %#x", o, got, n)
		}
	}
	if V776.ClientboundConfig(v777.ClientboundConfigurationFinishConfiguration) != v776.ClientboundConfigurationFinishConfiguration {
		t.Error("finish_configuration")
	}
	if Newest.ClientboundPlay(v777.ClientboundPlaySwingAnimation) != v777.ClientboundPlaySwingAnimation {
		t.Error("newest must not remap")
	}
}

func TestV776Registries(t *testing.T) {
	for _, c := range []struct{ reg, name string }{
		{"minecraft:item", "minecraft:diamond_sword"},
		{"minecraft:entity_type", "minecraft:zombie"},
		{"minecraft:sound_event", "minecraft:entity.player.hurt"},
		{"minecraft:particle_type", "minecraft:crit"},
		{"minecraft:menu", "minecraft:generic_9x3"},
	} {
		n, o := Newest.BuiltinID(c.reg, c.name), V776.BuiltinID(c.reg, c.name)
		if n < 0 || o < 0 || V776.Builtin(c.reg, n) != o {
			t.Errorf("%s %s: newest %d, 26.2 %d, mapped %d", c.reg, c.name, n, o, V776.Builtin(c.reg, n))
		}
	}
	// New content gets a stand-in.
	if V776.Builtin("minecraft:item", Newest.BuiltinID("minecraft:item", "minecraft:poplar_planks")) != V776.BuiltinID("minecraft:item", "minecraft:birch_planks") {
		t.Error("poplar planks should be birch planks")
	}
	if V776.Builtin("minecraft:item", Newest.BuiltinID("minecraft:item", "minecraft:red_wool_slab")) != V776.BuiltinID("minecraft:item", "minecraft:red_wool") {
		t.Error("red wool slab item should be red wool")
	}
	for _, c := range []struct{ reg, name string }{
		{"minecraft:worldgen/biome", "minecraft:plains"},
		{"minecraft:dimension_type", "minecraft:the_nether"},
		{"minecraft:damage_type", "minecraft:generic"},
	} {
		n, o := Newest.RegistryID(c.reg, c.name), V776.RegistryID(c.reg, c.name)
		if n < 0 || o < 0 || V776.Synced(c.reg, n) != o {
			t.Errorf("%s %s: newest %d, 26.2 %d, mapped %d", c.reg, c.name, n, o, V776.Synced(c.reg, n))
		}
	}
	if V776.BlockState(0) != 0 || V776.BlockState(1) != 1 {
		t.Error("air and stone keep their ids")
	}
	if len(V776.Configuration()) == 0 {
		t.Error("no configuration")
	}
	t.Logf("26.2: %d block states, %d biomes; 26.3: %d biomes", V776.BlockStates, V776.Biomes, Newest.Biomes)
}
