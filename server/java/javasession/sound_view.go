package javasession

import (
	"math"
	"math/rand/v2"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/version"
	"github.com/ezchr/go-mcjava/wire"
	"github.com/go-gl/mathgl/mgl64"
)

// Java level_event types (net.minecraft.world.level.block.LevelEvent).
const (
	levelEventDispenserDispense  = 1000
	levelEventFireExtinguish     = 1009
	levelEventPlayJukeboxSong    = 1010
	levelEventStopJukeboxSong    = 1011
	levelEventGhastWarning       = 1015
	levelEventGhastFireball      = 1016
	levelEventZombieDoorCrash    = 1021
	levelEventAnvilBroken        = 1029
	levelEventAnvilUsed          = 1030
	levelEventAnvilLand          = 1031
	levelEventEndPortalSpawn     = 1038
	levelEventEndPortalFrameFill = 1503
	levelEventBoneMeal           = 1505
	levelEventDestroyBlock       = 2001 // break sound + particles of a state
	levelEventPotionSplash       = 2002
	levelEventEvaporate          = 2009
	levelEventDestroyNoSound     = 2014
	levelEventDragonEggTeleport  = 2015
	levelEventEndermanTeleport   = 2018
	levelEventDestroyProgress    = 2019 // crack particles on a face
	levelEventWaxOn              = 3003
	levelEventWaxOff             = 3004
	levelEventScrape             = 3005
)

// PlaySound plays a sound to this player only.
func (s *Session) PlaySound(t world.Sound, pos mgl64.Vec3) { s.playSound(pos, t, false) }

// ViewSound plays a sound that happened in the world. Sounds the Java client already played
// itself (see fxState) are dropped.
func (s *Session) ViewSound(pos mgl64.Vec3, t world.Sound) { s.playSound(pos, t, true) }

func rnd() float32 { return rand.Float32() }

// centre is the centre of the block a position is in: vanilla plays block sounds there.
func centre(pos mgl64.Vec3) mgl64.Vec3 {
	return mgl64.Vec3{math.Floor(pos[0]) + 0.5, math.Floor(pos[1]) + 0.5, math.Floor(pos[2]) + 0.5}
}

// playSound maps a Dragonfly sound to Java. broadcast says the sound was broadcast to the world (so
// the client may have predicted it); PlaySound's sounds are always played.
func (s *Session) playSound(pos mgl64.Vec3, t world.Sound, broadcast bool) {
	s.maybeProbe()
	switch so := t.(type) {
	// Entities.
	case sound.Attack:
		if so.Damage {
			s.sound(pos, sndAttackStrong, srcPlayers, 1, 1)
		} else {
			s.sound(pos, sndAttackNoDamage, srcPlayers, 1, 1)
		}
	case sound.Drowning:
		s.sound(pos, sndHurtDrown, srcPlayers, 1, (rnd()-rnd())*0.2+1)
	case sound.Burning:
		s.sound(pos, sndHurtOnFire, srcPlayers, 1, (rnd()-rnd())*0.2+1)
	case sound.Fall:
		if so.Distance > 4 {
			s.sound(pos, sndBigFall, srcPlayers, 1, 1)
		} else {
			s.sound(pos, sndSmallFall, srcPlayers, 1, 1)
		}
	case sound.Burp:
		s.sound(pos, sndBurp, srcPlayers, 0.5, rnd()*0.1+0.9)
	case sound.Pop:
		s.sound(pos, sndItemPickup, srcPlayers, 0.2, ((rnd()-rnd())*0.7+1)*2)
	case sound.Explosion:
		s.sound(pos, sndExplode, srcBlocks, 4, (1+(rnd()-rnd())*0.2)*0.7)
	case sound.Thunder:
		s.sound(pos, sndThunder, srcWeather, 10000, 0.8+rnd()*0.2)
	case sound.LightningThunder:
		s.sound(pos, sndThunder, srcWeather, 10000, 0.8+rnd()*0.2)
	case sound.LightningExplode:
		s.sound(pos, sndLightningImpact, srcWeather, 2, 0.5+rnd()*0.2)
	case sound.LevelUp:
		s.sound(pos, sndLevelUp, srcPlayers, 0.75, 1)
	case sound.Experience:
		s.sound(pos, sndExperienceOrb, srcPlayers, 0.1, (rnd()-rnd())*0.35+0.9)
	case sound.GhastWarning:
		s.levelEvent(levelEventGhastWarning, cube.PosFromVec3(pos), 0, false)
	case sound.GhastShoot:
		s.levelEvent(levelEventGhastFireball, cube.PosFromVec3(pos), 0, false)
	case sound.FireworkLaunch:
		s.sound(pos, sndFireworkLaunch, srcAmbient, 3, 1)
	case sound.FireworkHugeBlast:
		s.sound(pos, sndFireworkLargeBlast, srcAmbient, 20, 0.95+rnd()*0.1)
	case sound.FireworkBlast:
		s.sound(pos, sndFireworkBlast, srcAmbient, 20, 0.95+rnd()*0.1)
	case sound.FireworkTwinkle:
		s.sound(pos, sndFireworkTwinkle, srcAmbient, 20, 0.9+rnd()*0.15)
	case sound.CushionPlace: // Bedrock-only block: wool sounds
		s.sound(pos, sndWoolPlace, srcBlocks, 1, 0.8)
	case sound.CushionSit:
		s.sound(pos, sndWoolStep, srcBlocks, 1, 1)
	case sound.CushionBreak:
		s.sound(pos, sndWoolBreak, srcBlocks, 1, 0.8)

	// Blocks.
	case sound.BlockPlace:
		if broadcast && s.predictedUseOn(cube.PosFromVec3(pos)) {
			return
		}
		if g, ok := blockSound(so.Block); ok {
			s.soundID(centre(pos), g.place, srcBlocks, (g.volume+1)/2, g.pitch*0.8)
		}
	case sound.BlockBreaking:
		if broadcast && s.selfMining(cube.PosFromVec3(pos)) {
			return // the client plays its own hit sounds while it mines
		}
		if g, ok := blockSound(so.Block); ok {
			s.soundID(centre(pos), g.hit, srcBlocks, (g.volume+1)/8, g.pitch*0.5)
		}
	case sound.DoorOpen:
		s.openClose(pos, so.Block, 0, sndWoodenDoorOpen, broadcast)
	case sound.DoorClose:
		s.openClose(pos, so.Block, 1, sndWoodenDoorClose, broadcast)
	case sound.TrapdoorOpen:
		s.openClose(pos, so.Block, 0, sndWoodenTrapdoorOpen, broadcast)
	case sound.TrapdoorClose:
		s.openClose(pos, so.Block, 1, sndWoodenTrapdoorClose, broadcast)
	case sound.FenceGateOpen:
		s.openClose(pos, so.Block, 0, sndFenceGateOpen, broadcast)
	case sound.FenceGateClose:
		s.openClose(pos, so.Block, 1, sndFenceGateClose, broadcast)
	case sound.GlassBreak: // splash potions and bottles o' enchanting
		s.sound(pos, sndSplashPotionBreak, srcNeutral, 1, rnd()*0.1+0.9)
	case sound.Fizz:
		s.sound(centre(pos), sndLavaExtinguish, srcBlocks, 0.5, 2.6+(rnd()-rnd())*0.8)
	case sound.AnvilLand:
		s.levelEvent(levelEventAnvilLand, cube.PosFromVec3(pos), 0, false)
	case sound.AnvilUse:
		s.levelEvent(levelEventAnvilUsed, cube.PosFromVec3(pos), 0, false)
	case sound.AnvilBreak:
		s.levelEvent(levelEventAnvilBroken, cube.PosFromVec3(pos), 0, false)
	case sound.ChestOpen:
		s.sound(centre(pos), sndChestOpen, srcBlocks, 0.5, rnd()*0.1+0.9)
	case sound.ChestClose:
		s.sound(centre(pos), sndChestClose, srcBlocks, 0.5, rnd()*0.1+0.9)
	case sound.EnderChestOpen:
		s.sound(centre(pos), sndEnderChestOpen, srcBlocks, 0.5, rnd()*0.1+0.9)
	case sound.EnderChestClose:
		s.sound(centre(pos), sndEnderChestClose, srcBlocks, 0.5, rnd()*0.1+0.9)
	case sound.BarrelOpen:
		s.sound(centre(pos), sndBarrelOpen, srcBlocks, 0.5, rnd()*0.1+0.9)
	case sound.BarrelClose:
		s.sound(centre(pos), sndBarrelClose, srcBlocks, 0.5, rnd()*0.1+0.9)
	case sound.ShulkerBoxOpen:
		s.sound(centre(pos), sndShulkerBoxOpen, srcBlocks, 0.5, rnd()*0.1+0.9)
	case sound.ShulkerBoxClose:
		s.sound(centre(pos), sndShulkerBoxClose, srcBlocks, 0.5, rnd()*0.1+0.9)
	case sound.Deny: // Bedrock's deny block has no Java sound: a low drum
		s.sound(pos, sndDeny, srcBlocks, 1, 0.5)
	case sound.DoorCrash:
		s.levelEvent(levelEventZombieDoorCrash, cube.PosFromVec3(pos), 0, false)
	case sound.Click:
		s.levelEvent(levelEventDispenserDispense, cube.PosFromVec3(pos), 0, false)
	case sound.Ignite:
		s.sound(pos, sndFlintAndSteel, srcBlocks, 1, rnd()*0.4+0.8)
	case sound.TNT:
		s.sound(pos, sndTNTPrimed, srcBlocks, 1, 1)
	case sound.FireExtinguish:
		s.levelEvent(levelEventFireExtinguish, cube.PosFromVec3(pos), 0, false)
	case sound.Note:
		i := so.Instrument.Int32()
		if i < 0 || i > int32(sndNotePling-sndNoteHarp) {
			i = 0
		}
		s.sound(centre(pos), sndNoteHarp+jsound(i), srcRecords, 3, notePitch(so.Pitch))
	case sound.MusicDiscPlay:
		if d := so.DiscType.Uint8(); int(d) < len(jukeboxSongs) && jukeboxSongs[d] >= 0 {
			s.levelEvent(levelEventPlayJukeboxSong, cube.PosFromVec3(pos), jukeboxSongs[d], false)
		}
	case sound.MusicDiscEnd:
		s.levelEvent(levelEventStopJukeboxSong, cube.PosFromVec3(pos), 0, false)
	case sound.ItemAdd:
		s.sound(pos, sndItemFrameAdd, srcBlocks, 1, 1)
	case sound.ItemFrameRemove:
		s.sound(pos, sndItemFrameRemove, srcBlocks, 1, 1)
	case sound.ItemFrameRotate:
		s.sound(pos, sndItemFrameRotate, srcBlocks, 1, 1)
	case sound.FurnaceCrackle:
		s.sound(pos, sndFurnaceCrackle, srcBlocks, 1, 1)
	case sound.CampfireCrackle:
		s.sound(pos, sndCampfireCrackle, srcBlocks, 0.5+rnd(), rnd()*0.7+0.6)
	case sound.BlastFurnaceCrackle:
		s.sound(pos, sndBlastFurnaceCrackle, srcBlocks, 1, 1)
	case sound.SmokerCrackle:
		s.sound(pos, sndSmokerSmoke, srcBlocks, 1, 1)
	case sound.ComposterEmpty:
		s.sound(pos, sndComposterEmpty, srcBlocks, 1, 1)
	case sound.ComposterFill:
		s.sound(pos, sndComposterFill, srcBlocks, 1, 1)
	case sound.ComposterFillLayer:
		s.sound(pos, sndComposterFillSuccess, srcBlocks, 1, 1)
	case sound.ComposterReady:
		s.sound(pos, sndComposterReady, srcBlocks, 1, 1)
	case sound.PotionBrewed:
		s.sound(pos, sndBrewingStandBrew, srcBlocks, 1, 1)
	case sound.PowerOn: // levers
		s.sound(pos, sndLeverClick, srcBlocks, 0.3, 0.6)
	case sound.PowerOff:
		s.sound(pos, sndLeverClick, srcBlocks, 0.3, 0.5)
	case sound.LecternBookPlace:
		s.sound(pos, sndBookPut, srcBlocks, 1, 1)
	case sound.SignWaxed: // the wax_on level event only spawns particles in 26.3 (26.2: both)
		if s.ver.Native() {
			s.sound(centre(pos), sndWaxOn, srcBlocks, 1, 1)
		}
		s.levelEvent(levelEventWaxOn, cube.PosFromVec3(pos), 0, false)
	case sound.WaxedSignFailedInteraction:
		s.sound(centre(pos), sndSignWaxedFail, srcBlocks, 1, 1)
	case sound.WaxRemoved:
		s.sound(centre(pos), sndWaxOff, srcBlocks, 1, 1)
		s.levelEvent(levelEventWaxOff, cube.PosFromVec3(pos), 0, false)
	case sound.CopperScraped:
		s.sound(centre(pos), sndScrape, srcBlocks, 1, 1)
		s.levelEvent(levelEventScrape, cube.PosFromVec3(pos), 0, false)
	case sound.DecoratedPotInserted:
		s.sound(centre(pos), sndDecoratedPotInsert, srcBlocks, 1, 0.7+0.5*float32(so.Progress))
	case sound.DecoratedPotInsertFailed:
		s.sound(centre(pos), sndDecoratedPotInsertFail, srcBlocks, 1, 1)
	case sound.EnderEyePlaced:
		s.levelEvent(levelEventEndPortalFrameFill, cube.PosFromVec3(pos), 0, false)
	case sound.EndPortalCreated:
		s.levelEvent(levelEventEndPortalSpawn, cube.PosFromVec3(pos), 0, true)

	// Items.
	case sound.ItemBreak:
		s.sound(pos, sndItemBreak, srcPlayers, 0.8, 0.8+rnd()*0.4)
	case sound.ShieldBlock:
		s.sound(pos, sndShieldBlock, srcPlayers, 1, 0.8+rnd()*0.4)
	case sound.ItemThrow:
		s.sound(pos, sndSnowballThrow, srcNeutral, 0.5, 0.4/(rnd()*0.4+0.8))
	case sound.ItemUseOn:
		if broadcast && s.predictedUseOn(cube.PosFromVec3(pos)) {
			return
		}
		s.itemUseOn(centre(pos), so.Block)
	case sound.EquipItem:
		s.sound(pos, equipSound(so.Item), srcPlayers, 1, 1)
	case sound.BucketFill:
		if broadcast && s.predictedUse() {
			return
		}
		if _, water := so.Liquid.(block.Water); water {
			s.sound(centre(pos), sndBucketFill, srcBlocks, 1, 1)
		} else {
			s.sound(centre(pos), sndBucketFillLava, srcBlocks, 1, 1)
		}
	case sound.BucketEmpty:
		if broadcast && s.predictedUse() {
			return
		}
		if _, water := so.Liquid.(block.Water); water {
			s.sound(centre(pos), sndBucketEmpty, srcBlocks, 1, 1)
		} else {
			s.sound(centre(pos), sndBucketEmptyLava, srcBlocks, 1, 1)
		}
	case sound.BowShoot:
		s.sound(pos, sndArrowShoot, srcPlayers, 1, 1/(rnd()*0.4+1.2)+0.5)
	case sound.CrossbowLoad:
		snd := sndCrossbowLoadingStart
		switch so.Stage {
		case sound.CrossbowLoadingMiddle:
			snd = sndCrossbowLoadingMiddle
		case sound.CrossbowLoadingEnd:
			snd = sndCrossbowLoadingEnd
		default:
			if so.QuickCharge {
				snd = sndCrossbowQuickCharge1
			}
		}
		s.sound(pos, snd, srcPlayers, 0.5, 1)
	case sound.CrossbowShoot:
		s.sound(pos, sndCrossbowShoot, srcPlayers, 1, 1/(rnd()*0.5+1.8)+0.53)
	case sound.ArrowHit:
		s.sound(pos, sndArrowHit, srcNeutral, 1, 1.2/(rnd()*0.2+0.9))
	case sound.Teleport:
		s.sound(pos, sndPlayerTeleport, srcPlayers, 1, 1)
	case sound.UseSpyglass:
		s.sound(pos, sndSpyglassUse, srcPlayers, 1, 1)
	case sound.StopUsingSpyglass:
		s.sound(pos, sndSpyglassStop, srcPlayers, 1, 1)
	case sound.GoatHorn:
		h := so.Horn.Uint8()
		if h > 7 {
			h = 0
		}
		s.sound(pos, sndGoatHorn0+jsound(h), srcRecords, 16, 1)
	case sound.FireCharge:
		s.sound(pos, sndFireChargeUse, srcBlocks, 1, (rnd()-rnd())*0.2+1)
	case sound.Totem:
		s.sound(pos, sndTotemUse, srcPlayers, 1, 1)

	case sound.Custom:
		s.customSound(pos, so.Name, float32(so.Volume), float32(so.Pitch))
	}
}

// notePitch is vanilla NoteBlock.getPitchFromNote: 2^((note-12)/12).
func notePitch(note int) float32 {
	return float32(math.Exp2(float64(note-12) / 12))
}

// openClose plays a door, trapdoor or fence gate sound of the block's set type; which is 0 for
// open and 1 for close, fallback the wooden sound for blocks without one.
func (s *Session) openClose(pos mgl64.Vec3, b world.Block, which int, fallback jsound, broadcast bool) {
	if broadcast && s.predictedUseOn(cube.PosFromVec3(pos)) {
		return
	}
	id := soundIDs[fallback]
	if st, ok := javaState(b); ok {
		if p := openCloseOf(st); p[which] >= 0 {
			id = p[which]
		}
	}
	s.soundID(centre(pos), id, srcBlocks, 1, rnd()*0.1+0.9)
}

// itemUseOn is the sound of a tool used on a block, by the block it turned into.
func (s *Session) itemUseOn(pos mgl64.Vec3, b world.Block) {
	switch bl := b.(type) {
	case block.Farmland:
		s.sound(pos, sndHoeTill, srcBlocks, 1, 1)
	case block.DirtPath:
		s.sound(pos, sndShovelFlatten, srcBlocks, 1, 1)
	case block.Log, block.Wood:
		s.sound(pos, sndAxeStrip, srcBlocks, 1, 1)
	case block.Cake:
		s.sound(pos, sndEat, srcPlayers, 1, 1)
	default:
		if g, ok := blockSound(bl); ok {
			s.soundID(pos, g.place, srcBlocks, (g.volume+1)/2, g.pitch*0.8)
		}
	}
}

// equipSound is the armour equip sound of an item.
func equipSound(it world.Item) jsound {
	var tier item.ArmourTier
	switch i := it.(type) {
	case item.Helmet:
		tier = i.Tier
	case item.Chestplate:
		tier = i.Tier
	case item.Leggings:
		tier = i.Tier
	case item.Boots:
		tier = i.Tier
	case item.Elytra:
		return sndEquipElytra
	}
	switch tier.(type) {
	case item.ArmourTierLeather:
		return sndEquipLeather
	case item.ArmourTierCopper:
		return sndEquipCopper
	case item.ArmourTierGold:
		return sndEquipGold
	case item.ArmourTierChain:
		return sndEquipChain
	case item.ArmourTierIron:
		return sndEquipIron
	case item.ArmourTierDiamond:
		return sndEquipDiamond
	case item.ArmourTierNetherite:
		return sndEquipNetherite
	}
	return sndEquipGeneric
}

// customSound plays a sound by name: a Java sound event, a Bedrock playsound name Geyser maps to
// one, or else the name as an inline sound event (resource pack sounds).
func (s *Session) customSound(pos mgl64.Vec3, name string, vol, pitch float32) {
	if id, ok := soundByName[name]; ok {
		s.soundID(pos, id, srcMaster, vol, pitch)
		return
	}
	if !validSoundName(name) {
		return
	}
	w := s.packet()
	w.VarInt(0) // inline sound event
	w.String(name)
	w.Bool(false) // no fixed range
	s.finishSound(w, pos, srcMaster, vol, pitch)
}

// validSoundName reports whether a name parses as a Java identifier ([namespace:]path); anything
// else makes the client drop the connection.
func validSoundName(n string) bool {
	if n == "" || len(n) > 32767 {
		return false
	}
	path := n
	if i := strings.IndexByte(n, ':'); i >= 0 {
		path = n[i+1:]
		for j := 0; j < i; j++ {
			if !identChar(n[j]) {
				return false
			}
		}
	}
	if path == "" {
		return false
	}
	for j := 0; j < len(path); j++ {
		if !identChar(path[j]) && path[j] != '/' {
			return false
		}
	}
	return true
}

func identChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.'
}

// sound plays a Java sound event of the mapping.
func (s *Session) sound(pos mgl64.Vec3, snd jsound, src soundSource, vol, pitch float32) {
	s.soundID(pos, soundIDs[snd], src, vol, pitch)
}

// soundID writes a sound packet for a sound event registry id.
func (s *Session) soundID(pos mgl64.Vec3, id int32, src soundSource, vol, pitch float32) {
	if id < 0 {
		return
	}
	if l := s.legacy(); l != nil {
		if id = version.Map(l.sound, id); id < 0 {
			return
		}
	}
	w := s.packet()
	w.VarInt(id + 1) // Holder<SoundEvent>: registry id + 1 (0 is an inline event)
	s.finishSound(w, pos, src, vol, pitch)
}

// finishSound writes the rest of a sound packet after its sound event and queues it.
func (s *Session) finishSound(w *wire.Writer, pos mgl64.Vec3, src soundSource, vol, pitch float32) {
	w.VarInt(int32(src))
	// Positions are fixed point, 1/8 block, truncated like vanilla's (int)(x * 8.0).
	w.Int32(int32(pos[0] * 8))
	w.Int32(int32(pos[1] * 8))
	w.Int32(int32(pos[2] * 8))
	w.Float32(vol)
	w.Float32(pitch)
	w.Int64(int64(rand.Uint64())) // seed of the sound's variant pick
	s.queue(v777.ClientboundPlaySound, w)
}

// levelEvent writes a level_event; global events play wherever the player is (end portal spawn).
func (s *Session) levelEvent(typ int32, pos cube.Pos, data int32, global bool) {
	if l := s.legacy(); l != nil {
		var ok bool
		if typ, data, ok = s.levelEvent262(l, typ, pos, data); !ok {
			return
		}
	}
	w := s.packet()
	w.Int32(typ)
	w.Position(pos[0], pos[1], pos[2])
	w.Int32(data)
	w.Bool(global)
	s.queue(v777.ClientboundPlayLevelEvent, w)
}

// levelEvent262 turns a 26.3 level event into a 26.2 one: data that is a block state or a jukebox
// song gets the client's id, and the particle-only events 26.3 added (2014-2019) are sent as
// level_particles instead, or dropped where 26.2 vanilla shows nothing (crack particles of
// another player's mining). ok is false when no level event is to be sent.
func (s *Session) levelEvent262(l *legacyIDs, typ int32, pos cube.Pos, data int32) (int32, int32, bool) {
	switch typ {
	case levelEventDestroyBlock:
		return typ, s.ver.BlockState(data), true
	case levelEventPlayJukeboxSong:
		data = version.Map(l.jukeboxSong, data)
		return typ, data, data >= 0
	case levelEventDestroyNoSound:
		// Break particles without the sound: block particles over the block.
		if l.particleBlock >= 0 {
			w := s.packet()
			w.VarInt(l.particleBlock)
			w.VarInt(s.ver.BlockState(data))
			s.particleAt262(w, false, pos.Vec3Centre(), 0.25, 0.25, 0.25, 0.05, 48)
		}
		return 0, 0, false
	case levelEventDragonEggTeleport, levelEventEndermanTeleport:
		if l.particlePortal >= 0 {
			at, spread := pos.Vec3Centre(), float32(0.5)
			if typ == levelEventEndermanTeleport {
				at, spread = pos.Vec3Middle().Add(mgl64.Vec3{0, 1, 0}), 0.6
			}
			w := s.packet()
			w.VarInt(l.particlePortal)
			s.particleAt262(w, false, at, spread, spread, spread, 0.1, 128)
		}
		return 0, 0, false
	case levelEventDestroyProgress:
		return 0, 0, false
	}
	return typ, data, true
}
