package javasession

import (
	"github.com/df-mc/dragonfly/server/world"
	v777 "github.com/ezchr/go-mcjava/v777"
)

// soundSource is Java's SoundSource (the sound packet's category), by ordinal.
type soundSource int32

const (
	srcMaster soundSource = iota
	srcMusic
	srcRecords
	srcWeather
	srcBlocks
	srcHostile
	srcNeutral
	srcPlayers
	srcAmbient
	srcVoice
	srcUI
)

// blockSoundGroupDef is a vanilla SoundType by name (fx_blocksound_gen.go).
type blockSoundGroupDef struct {
	volume, pitch float32
	names         [5]string // break, step, place, hit, fall
}

// blockSoundGroup is a vanilla SoundType with sound event registry ids (-1: unknown).
type blockSoundGroup struct {
	volume, pitch               float32
	brk, step, place, hit, fall int32
}

// The lookup tables the fx code uses, resolved once from names to 26.3 registry ids, so a lookup
// on the hot path is an array index.
var (
	blockSoundGroups [len(blockSoundGroupDefs)]blockSoundGroup
	openCloseIDs     [len(openCloseDefs)][2]int32
	soundIDs         [sndCount]int32
	particleIDs      [ptCount]int32
	// soundByName maps Java sound event paths ("entity.player.levelup") and Bedrock playsound
	// names ("random.levelup") to Java sound event ids, for sound.Custom.
	soundByName map[string]int32
	// mobEffects maps Dragonfly (Bedrock) effect ids to Java mob_effect ids (-1: none).
	mobEffects [64]int32
	// jukeboxSongs maps sound.DiscType to minecraft:jukebox_song registry ids (-1: none).
	jukeboxSongs [32]int32

	javaEggItem      int32
	barrierState     int32
	decoratedPotJava int32
)

func soundEventID(name string) int32 {
	return v777.BuiltinID("minecraft:sound_event", "minecraft:"+name)
}

func init() {
	for i, d := range blockSoundGroupDefs {
		g := &blockSoundGroups[i]
		g.volume, g.pitch = d.volume, d.pitch
		g.brk, g.step, g.place, g.hit, g.fall = soundEventID(d.names[0]), soundEventID(d.names[1]),
			soundEventID(d.names[2]), soundEventID(d.names[3]), soundEventID(d.names[4])
	}
	for i, d := range openCloseDefs {
		if d[0] == "" {
			openCloseIDs[i] = [2]int32{-1, -1}
			continue
		}
		openCloseIDs[i] = [2]int32{soundEventID(d[0]), soundEventID(d[1])}
	}
	for i, n := range soundNames {
		soundIDs[i] = soundEventID(n)
	}
	for i, n := range particleNames {
		particleIDs[i] = v777.BuiltinID("minecraft:particle_type", "minecraft:"+n)
	}

	reg := v777.Builtin["minecraft:sound_event"]
	soundByName = make(map[string]int32, len(reg)+len(bedrockSoundNames))
	for name, id := range reg {
		soundByName[name[len("minecraft:"):]] = id
		soundByName[name] = id
	}
	for _, m := range bedrockSoundNames {
		if _, ok := soundByName[m[0]]; !ok {
			if id, ok := reg["minecraft:"+m[1]]; ok {
				soundByName[m[0]] = id
			}
		}
	}

	for i := range mobEffects {
		mobEffects[i] = -1
	}
	for id, name := range bedrockEffectNames {
		mobEffects[id] = v777.BuiltinID("minecraft:mob_effect", "minecraft:"+name)
	}
	for i := range jukeboxSongs {
		jukeboxSongs[i] = -1
	}
	for i, name := range discSongs {
		jukeboxSongs[i] = v777.RegistryID("minecraft:jukebox_song", "minecraft:"+name)
	}

	javaEggItem = v777.BuiltinID("minecraft:item", "minecraft:egg")
	barrierState = javaBlockDefaultState("minecraft:barrier")
	decoratedPotJava = v777.BuiltinID("minecraft:block", "minecraft:decorated_pot")
}

// javaBlockDefaultState returns the first state of a Java block, or 0.
func javaBlockDefaultState(name string) int32 {
	id := v777.BuiltinID("minecraft:block", name)
	if id < 0 || int(id) >= len(blockFirstState) {
		return 0
	}
	return int32(blockFirstState[id])
}

// javaState is the Java block state id of a Dragonfly block.
func javaState(b world.Block) (int32, bool) {
	if b == nil {
		return 0, false
	}
	bi := blocks() // first: it finalises the default registry
	rid := world.BlockRuntimeID(b)
	if int(rid) >= len(bi.java) {
		return 0, false
	}
	return int32(bi.java[rid]), true
}

// soundGroupOf is the vanilla SoundType of a Java block state.
func soundGroupOf(state int32) *blockSoundGroup {
	if state < 0 || int(state) >= len(stateSoundGroup) {
		return &blockSoundGroups[0]
	}
	return &blockSoundGroups[stateSoundGroup[state]]
}

// blockSound is the SoundType of a Dragonfly block.
func blockSound(b world.Block) (*blockSoundGroup, bool) {
	st, ok := javaState(b)
	if !ok {
		return nil, false
	}
	return soundGroupOf(st), true
}

// openCloseOf is the open and close sounds of a door, trapdoor or fence gate state (-1 if none).
func openCloseOf(state int32) [2]int32 {
	lo, hi := 0, len(openCloseRuns)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if int32(openCloseRuns[m][1]) < state {
			lo = m + 1
		} else {
			hi = m
		}
	}
	if lo < len(openCloseRuns) && int32(openCloseRuns[lo][0]) <= state {
		return openCloseIDs[openCloseRuns[lo][2]]
	}
	return [2]int32{-1, -1}
}

// javaBlockOf is the Java block (minecraft:block registry id) of a block state.
func javaBlockOf(state int32) int32 {
	lo, hi := 0, len(blockFirstState)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if int32(blockFirstState[m]) <= state {
			lo = m + 1
		} else {
			hi = m
		}
	}
	return int32(lo - 1)
}

// bedrockEffectNames are the Java mob effects of Dragonfly's (Bedrock) effect ids
// (entity/effect/register.go). Fatal poison is poison that can kill: Java shows it as poison.
var bedrockEffectNames = map[int]string{
	1: "speed", 2: "slowness", 3: "haste", 4: "mining_fatigue", 5: "strength", 6: "instant_health",
	7: "instant_damage", 8: "jump_boost", 9: "nausea", 10: "regeneration", 11: "resistance",
	12: "fire_resistance", 13: "water_breathing", 14: "invisibility", 15: "blindness",
	16: "night_vision", 17: "hunger", 18: "weakness", 19: "poison", 20: "wither", 21: "health_boost",
	22: "absorption", 23: "saturation", 24: "levitation", 25: "poison", 26: "conduit_power",
	27: "slow_falling", 28: "bad_omen", 29: "hero_of_the_village", 30: "darkness",
}

// discSongs are the jukebox songs of sound.DiscType values, in DiscType order.
var discSongs = [...]string{
	"13", "cat", "blocks", "chirp", "far", "mall", "mellohi", "stal", "strad", "ward", "11", "wait",
	"otherside", "pigstep", "5", "relic", "creator", "creator_music_box", "precipice", "tears",
	"lava_chicken",
}
