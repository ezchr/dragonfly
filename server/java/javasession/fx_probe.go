package javasession

import (
	"image/color"
	"os"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/go-gl/mathgl/mgl64"
)

// fxProbe (DFJAVA_FX_PROBE=1) sends every mapped sound, particle, effect and block animation to
// each Java player once, 3 seconds after its first sound or particle, so a real client can be
// checked for decode errors (tools/client.sh). Off by default: one bool check per call.
var fxProbe = os.Getenv("DFJAVA_FX_PROBE") == "1"

func (s *Session) maybeProbe() {
	if !fxProbe {
		return
	}
	st := s.fxMake()
	st.mu.Lock()
	started := st.probed
	st.probed = true
	st.mu.Unlock()
	if started {
		return
	}
	time.AfterFunc(3*time.Second, func() { s.do(s.probe) })
}

func (s *Session) probe(tx *world.Tx, c session.Controllable) {
	pos := c.Position()
	s.log.Info("fx probe", "sounds", len(allSounds), "particles", len(allParticles))
	for _, snd := range allSounds {
		s.PlaySound(snd, pos)
	}
	for i, pa := range allParticles {
		s.ViewParticle(pos.Add(mgl64.Vec3{float64(i%5) - 2, 1.5, float64(i/5) - 2}), pa)
	}
	for id := 1; id <= 30; id++ {
		if t, ok := effect.ByID(id); ok {
			if lt, ok := t.(effect.LastingType); ok {
				s.SendEffect(effect.New(lt, 1, 10*time.Second))
			}
			s.SendEffectRemoval(t)
		}
	}
	s.SendEffect(effect.NewInfinite(effect.NightVision, 1).WithoutParticles())
	s.SendSpeed(0.13)
	s.SendSpeed(0.1)

	// Entity data with effect particles and the bed fields, on the player itself.
	w := s.packet()
	w.VarInt(selfEntityID)
	s.writeLivingFxData(w, c)
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)

	b := cube.PosFromVec3(pos).Side(cube.FaceDown)
	s.ViewBlockAction(b, block.OpenAction{})
	s.ViewBlockAction(b, block.CloseAction{})
	s.ViewBlockAction(b, block.DecoratedPotWobbleAction{Success: true})
	s.ViewBlockAction(b, block.StartCrackAction{BreakTime: time.Second})
	time.AfterFunc(1500*time.Millisecond, func() {
		s.do(func(*world.Tx, session.Controllable) { s.ViewBlockAction(b, block.StopCrackAction{}) })
	})
	s.ViewSleepingPlayers(1, 2)
	s.ViewEntityWake(c)
}

// allSounds is one value of every Dragonfly sound type (92).
var allSounds = []world.Sound{
	sound.BlockPlace{Block: block.Stone{}}, sound.BlockBreaking{Block: block.Stone{}}, sound.GlassBreak{},
	sound.Fizz{}, sound.AnvilLand{}, sound.AnvilUse{}, sound.AnvilBreak{}, sound.ChestOpen{}, sound.ChestClose{},
	sound.EnderChestOpen{}, sound.EnderChestClose{}, sound.BarrelOpen{}, sound.BarrelClose{}, sound.Deny{},
	sound.ShulkerBoxOpen{}, sound.ShulkerBoxClose{}, sound.DoorOpen{Block: block.WoodDoor{}},
	sound.DoorClose{Block: block.WoodDoor{}}, sound.TrapdoorOpen{Block: block.WoodTrapdoor{}},
	sound.TrapdoorClose{Block: block.WoodTrapdoor{}}, sound.FenceGateOpen{Block: block.WoodFenceGate{}},
	sound.FenceGateClose{Block: block.WoodFenceGate{}}, sound.DoorCrash{}, sound.Click{}, sound.Ignite{},
	sound.TNT{}, sound.FireExtinguish{}, sound.Note{Instrument: sound.Banjo(), Pitch: 24},
	sound.MusicDiscPlay{DiscType: sound.DiscLavaChicken()}, sound.MusicDiscEnd{}, sound.ItemAdd{},
	sound.ItemFrameRemove{}, sound.ItemFrameRotate{}, sound.FurnaceCrackle{}, sound.CampfireCrackle{},
	sound.BlastFurnaceCrackle{}, sound.SmokerCrackle{}, sound.ComposterEmpty{}, sound.ComposterFill{},
	sound.ComposterFillLayer{}, sound.ComposterReady{}, sound.PotionBrewed{}, sound.PowerOn{}, sound.PowerOff{},
	sound.LecternBookPlace{}, sound.SignWaxed{}, sound.WaxedSignFailedInteraction{}, sound.WaxRemoved{},
	sound.CopperScraped{}, sound.DecoratedPotInserted{Progress: 0.5}, sound.DecoratedPotInsertFailed{},
	sound.EnderEyePlaced{}, sound.EndPortalCreated{},
	sound.Attack{Damage: true}, sound.Drowning{}, sound.Burning{}, sound.Fall{Distance: 5}, sound.Burp{},
	sound.Pop{}, sound.Explosion{}, sound.Thunder{}, sound.LevelUp{}, sound.Experience{}, sound.GhastWarning{},
	sound.GhastShoot{}, sound.FireworkLaunch{}, sound.FireworkHugeBlast{}, sound.FireworkBlast{},
	sound.FireworkTwinkle{}, sound.CushionPlace{}, sound.CushionSit{}, sound.CushionBreak{},
	sound.ItemBreak{}, sound.ShieldBlock{}, sound.ItemThrow{}, sound.ItemUseOn{Block: block.Farmland{}},
	sound.EquipItem{Item: item.Helmet{Tier: item.ArmourTierDiamond{}}}, sound.BucketFill{Liquid: block.Water{}},
	sound.BucketEmpty{Liquid: block.Lava{}}, sound.BowShoot{}, sound.CrossbowLoad{Stage: sound.CrossbowLoadingEnd},
	sound.CrossbowShoot{}, sound.ArrowHit{}, sound.Teleport{}, sound.UseSpyglass{}, sound.StopUsingSpyglass{},
	sound.GoatHorn{Horn: sound.Dream()}, sound.FireCharge{}, sound.Totem{},
	sound.LightningExplode{}, sound.LightningThunder{},
	sound.Custom{Name: "random.orb", Volume: 1, Pitch: 1},
}

// allParticles is one value of every Dragonfly particle type (21).
var allParticles = []world.Particle{
	particle.Flame{}, particle.Dust{Colour: color.RGBA{R: 255, A: 255}}, particle.BlockBreak{Block: block.Stone{}},
	particle.BlockBreakNoSound{Block: block.Stone{}}, particle.PunchBlock{Block: block.Stone{}, Face: cube.FaceUp},
	particle.BlockForceField{}, particle.BoneMeal{}, particle.Note{Instrument: sound.Piano(), Pitch: 12},
	particle.DragonEggTeleport{Diff: cube.Pos{3, -2, 5}}, particle.Evaporate{}, particle.WaterDrip{},
	particle.LavaDrip{}, particle.Lava{}, particle.DustPlume{},
	particle.HugeExplosion{}, particle.EndermanTeleport{}, particle.SnowballPoof{}, particle.EggSmash{},
	particle.Splash{}, particle.Effect{Colour: color.RGBA{G: 255, A: 255}}, particle.EntityFlame{},
}
