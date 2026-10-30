package entity

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// NewArmourStand creates a new armour stand entity.
func NewArmourStand(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(ArmourStandType, newArmourStandBehaviour())
}

// ArmourStandType is a world.EntityType implementation for armour stands.
var ArmourStandType armourStandType

type armourStandType struct{}

func (armourStandType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return Open(tx, handle, data)
}

func (armourStandType) EncodeEntity() string { return "minecraft:armor_stand" }

func (armourStandType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.25, 0, -0.25, 0.25, 1.975, 0.25)
}

// DecodeNBT reads the vanilla layout: Armor (head, chest, legs, feet),
// Mainhand and Offhand as lists of item compounds, and Pose.
func (armourStandType) DecodeNBT(m map[string]any, data *world.EntityData) {
	b := newArmourStandBehaviour()
	stacks := func(key string) []item.Stack {
		list, _ := m[key].([]any)
		out := make([]item.Stack, len(list))
		for i, v := range list {
			if c, ok := v.(map[string]any); ok {
				if name, _ := c["Name"].(string); name != "" {
					out[i] = item.ReadNBT(c, nil)
				}
			}
		}
		return out
	}
	if a := stacks("Armor"); len(a) == 4 {
		b.armour.Set(a[0], a[1], a[2], a[3])
	}
	if h := stacks("Mainhand"); len(h) > 0 {
		b.mainHand = h[0]
	}
	if h := stacks("Offhand"); len(h) > 0 {
		b.offHand = h[0]
	}
	if pose, ok := m["Pose"].(map[string]any); ok {
		p, _ := pose["PoseIndex"].(int32)
		s, _ := pose["LastSignal"].(int32)
		b.pose, b.lastSignal = int(p)%armourStandPoses, int(s)
	}
	b.Apply(data)
}

func (armourStandType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*armourStandBehaviour)
	write := func(s item.Stack) any {
		if s.Empty() {
			return map[string]any{"Name": "", "Count": byte(0), "Damage": int16(0), "WasPickedUp": byte(0)}
		}
		return item.WriteNBT(s, true)
	}
	a := b.armour
	return map[string]any{
		"Armor":    []any{write(a.Helmet()), write(a.Chestplate()), write(a.Leggings()), write(a.Boots())},
		"Mainhand": []any{write(b.mainHand)},
		"Offhand":  []any{write(b.offHand)},
		"Pose":     map[string]any{"PoseIndex": int32(b.pose), "LastSignal": int32(b.lastSignal)},
	}
}

// armourStandPoses is the number of poses an armour stand cycles through.
const armourStandPoses = 13

// armourStandBehaviour is an armour stand as in vanilla Bedrock: it falls,
// shows the armour and items put on it, changes pose when sneak-clicked or
// powered by redstone, and breaks after two quick hits, dropping itself and
// everything it wore.
type armourStandBehaviour struct {
	armour            *inventory.Armour
	mainHand, offHand item.Stack

	mc           *MovementComputer
	fallDistance float64

	health     float64
	lastHit    time.Duration
	hurtTicks  int
	pose       int
	lastSignal int
}

func newArmourStandBehaviour() *armourStandBehaviour {
	return &armourStandBehaviour{
		armour:  inventory.NewArmour(nil),
		mc:      &MovementComputer{Gravity: 0.08, Drag: 0.02, DragBeforeGravity: true},
		health:  6,
		lastHit: -time.Hour,
	}
}

// Apply ...
func (b *armourStandBehaviour) Apply(data *world.EntityData) {
	data.Data = b
	// A name given with a name tag shows when looked at, as for mobs.
	data.AlwaysShowNameTag = false
}

func (b *armourStandBehaviour) Immobile() bool { return true }

func (b *armourStandBehaviour) Armour() *inventory.Armour { return b.armour }

func (b *armourStandBehaviour) HeldItems() (mainHand, offHand item.Stack) {
	return b.mainHand, b.offHand
}

// NameTaggable lets name tags rename the stand.
func (b *armourStandBehaviour) NameTaggable() bool { return true }

// PoseIndex returns the pose the stand is shown in, 0-12.
func (b *armourStandBehaviour) PoseIndex() int { return b.pose }

// HurtTicks returns how much the stand still wobbles from the last hit.
func (b *armourStandBehaviour) HurtTicks() int { return b.hurtTicks }

// Tick makes the stand fall, wobble out, burn and follow redstone power.
func (b *armourStandBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	m := b.mc.TickMovement(e, e.data.Pos, e.data.Vel, e.data.Rot, tx)
	e.data.Pos, e.data.Vel = m.pos, m.vel
	if m.dpos[1] < 0 {
		b.fallDistance -= m.dpos[1]
	} else if b.mc.OnGround() || m.dpos[1] > 0 {
		if b.mc.OnGround() && b.fallDistance >= 3 {
			tx.PlaySound(e.Position(), sound.ArmourStandLand{})
		}
		b.fallDistance = 0
	}

	changed := false
	if b.hurtTicks > 0 && e.Age()%(time.Second/10) == 0 {
		b.hurtTicks--
		changed = true
	}

	// Redstone at the stand's feet or in the block below picks the pose.
	pos := cube.PosFromVec3(e.Position())
	signal := tx.RedstonePower(pos)
	if signal == 0 {
		signal = tx.RedstonePower(pos.Side(cube.FaceDown))
	}
	if signal > 0 && signal != b.lastSignal {
		if p := min(signal, armourStandPoses) - 1; p != b.pose {
			b.pose = p
			changed = true
		}
	}
	b.lastSignal = signal

	if b.burn(e, tx) {
		return nil
	}
	if changed {
		e.updateState()
	}
	return m
}

// burn sets the stand alight in fire or lava and lets the fire hurt it. It
// returns true if the stand burned away.
func (b *armourStandBehaviour) burn(e *Ent, tx *world.Tx) bool {
	inFire := false
	box := ArmourStandType.BBox(e).Translate(e.Position())
	low, high := cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max())
	for x := low[0]; x <= high[0]; x++ {
		for y := low[1]; y <= high[1]; y++ {
			for z := low[2]; z <= high[2]; z++ {
				switch tx.Block(cube.Pos{x, y, z}).(type) {
				case block.Fire, block.Lava:
					inFire = true
				}
			}
		}
	}
	switch {
	case inFire:
		if e.OnFireDuration() <= 0 {
			e.SetOnFire(5 * time.Second)
		}
		b.health -= 0.05
	case e.OnFireDuration() > 0 && e.Age()%time.Second == 0:
		b.health--
	default:
		return false
	}
	if b.health > 0 {
		return false
	}
	b.dropHeld(e)
	_ = e.Close()
	return true
}

// Hurt handles everything that can damage the stand. Players break it with
// two hits within five ticks (creative players with one, without drops),
// projectiles break it at once, explosions destroy it with what it holds but
// not the stand itself, and fall damage only makes a sound.
func (b *armourStandBehaviour) Hurt(e *Ent, damage float64, src world.DamageSource) (float64, bool) {
	switch s := src.(type) {
	case VoidDamageSource:
		_ = e.Close()
		return damage, true
	case ExplosionDamageSource:
		b.dropHeld(e)
		_ = e.Close()
		return damage, true
	case FallDamageSource:
		e.tx.PlaySound(e.Position(), sound.ArmourStandLand{})
		return 0, false
	case ProjectileDamageSource:
		// arrows, tridents and wind charges break it; snowballs and eggs do not
		if s.Projectile == nil {
			return 0, false
		}
		switch s.Projectile.H().Type().EncodeEntity() {
		case "minecraft:arrow", "minecraft:thrown_trident", "minecraft:wind_charge_projectile", "minecraft:breeze_wind_charge_projectile":
			b.breakStand(e, true)
			return 0, true
		}
		return 0, false
	case AttackDamageSource:
		p, ok := s.Attacker.(interface{ GameMode() world.GameMode })
		if !ok {
			return 0, false
		}
		mode := p.GameMode()
		if mode.CreativeInventory() {
			b.breakStand(e, false)
			return 0, true
		}
		if !mode.AllowsEditing() {
			return 0, false
		}
		e.tx.PlaySound(e.Position(), sound.ArmourStandHit{})
		if e.Age()-b.lastHit > 5*time.Second/20 {
			b.lastHit = e.Age()
			b.hurtTicks = 9
			e.updateState()
			return 0, true
		}
		b.breakStand(e, true)
		return 0, true
	}
	return 0, false
}

// Explode destroys the stand and drops what it holds, but not the stand.
func (b *armourStandBehaviour) Explode(e *Ent, _ world.ExplosionSource, _ float64) {
	b.dropHeld(e)
	_ = e.Close()
}

// breakStand removes the stand with the break sound and particles, dropping
// the stand item and everything on it if drops is true.
func (b *armourStandBehaviour) breakStand(e *Ent, drops bool) {
	if _, ok := e.H().Entity(e.tx); !ok {
		return
	}
	if drops {
		b.drop(e, item.NewStack(item.ArmourStand{}, 1), mgl64.Vec3{0, 1, 0})
		b.dropHeld(e)
	}
	e.tx.PlaySound(e.Position(), sound.ArmourStandBreak{})
	_ = e.Close()
}

// dropHeld drops the armour and held items at random points over the stand.
func (b *armourStandBehaviour) dropHeld(e *Ent) {
	for _, s := range append(b.armour.Clear(), b.mainHand, b.offHand) {
		b.drop(e, s, mgl64.Vec3{rand.Float64()*0.5 - 0.25, rand.Float64()*1.5 + 0.25, rand.Float64()*0.5 - 0.25})
	}
	b.mainHand, b.offHand = item.Stack{}, item.Stack{}
}

func (b *armourStandBehaviour) drop(e *Ent, s item.Stack, offset mgl64.Vec3) {
	if s.Empty() {
		return
	}
	opts := world.EntitySpawnOpts{Position: e.Position().Add(offset)}
	e.tx.AddEntity(e.tx.World().EntityRegistry().Config().Item(opts, s))
}

// armourStandSlot is one place on the stand that holds an item.
type armourStandSlot struct {
	get func() item.Stack
	set func(item.Stack)
}

// slot returns the stand's slot for an item on the head (2), body (3), legs
// (4) or feet (5), main hand (0) or off hand (1), using vanilla's equipment
// slot numbers.
func (b *armourStandBehaviour) slot(n int) armourStandSlot {
	a := b.armour
	switch n {
	case 1:
		return armourStandSlot{func() item.Stack { return b.offHand }, func(s item.Stack) { b.offHand = s }}
	case 2:
		return armourStandSlot{a.Helmet, a.SetHelmet}
	case 3:
		return armourStandSlot{a.Chestplate, a.SetChestplate}
	case 4:
		return armourStandSlot{a.Leggings, a.SetLeggings}
	case 5:
		return armourStandSlot{a.Boots, a.SetBoots}
	}
	return armourStandSlot{func() item.Stack { return b.mainHand }, func(s item.Stack) { b.mainHand = s }}
}

// wornSlot returns the slot an item goes in when put on the stand.
func wornSlot(held item.Stack) int {
	switch it := held.Item().(type) {
	case item.HelmetType, block.Skull:
		return 2
	case block.Pumpkin:
		if it.Carved {
			return 2
		}
	case item.ChestplateType, item.Elytra:
		return 3
	case item.LeggingsType:
		return 4
	case item.BootsType:
		return 5
	case item.Shield:
		return 1
	}
	return 0
}

// clickedSlot returns the armour slot at the height the stand was clicked,
// as vanilla picks it, or -1 below the base plate.
func clickedSlot(dy float64) int {
	switch {
	case dy < 0.1:
		return -1
	case dy < 0.55:
		return 5
	case dy < 0.9:
		return 4
	case dy < 1.6:
		return 3
	}
	return 2
}

// Interact handles a player using the stand with held, clicking it at
// clickPos. Sneaking cycles the pose. Otherwise an item goes in its slot, and
// an empty hand takes the item from the part clicked, or from the hands.
// Interact returns what the user holds afterwards.
func (b *armourStandBehaviour) Interact(e *Ent, user item.User, held item.Stack, clickPos mgl64.Vec3) (item.Stack, bool) {
	if s, ok := user.(interface{ Sneaking() bool }); ok && s.Sneaking() {
		b.pose = (b.pose + 1) % armourStandPoses
		e.updateState()
		return held, true
	}
	switch held.Item().(type) {
	case item.ArmourStand:
		return held, false
	case item.NameTag:
		if held.CustomName() != "" {
			// a named tag renames the stand instead
			return held, false
		}
	}
	clicked := clickedSlot(clickPos.Y() - e.Position().Y())
	if clicked < 0 {
		return held, false
	}

	var slot armourStandSlot
	if held.Empty() {
		slot = b.slot(clicked)
		for _, n := range []int{0, 1} {
			if !slot.get().Empty() {
				break
			}
			slot = b.slot(n)
		}
	} else {
		slot = b.slot(wornSlot(held))
	}
	old := slot.get()
	// Comparable counts an empty stack as matching anything, so both are checked.
	if held.Empty() && old.Empty() || !held.Empty() && !old.Empty() && held.Comparable(old) {
		return held, false
	}

	creative := false
	if g, ok := user.(interface{ GameMode() world.GameMode }); ok {
		creative = g.GameMode().CreativeInventory()
	}
	after := old
	if !held.Empty() && (creative || held.Count() > 1) {
		// the user keeps the rest of the stack, so the old item goes elsewhere
		// in the inventory, and nothing happens if there is no room for it
		after = held
		if !creative {
			after = held.Grow(-1)
		}
		if !old.Empty() {
			inv, ok := user.(interface{ Inventory() *inventory.Inventory })
			if !ok {
				return held, false
			}
			if _, err := inv.Inventory().AddItem(old); err != nil && !creative {
				return held, false
			}
		}
	}
	if !held.Empty() {
		held = held.Grow(1 - held.Count())
	}
	slot.set(held)
	b.refresh(e)
	if !held.Empty() {
		e.tx.PlaySound(e.Position(), sound.EquipItem{Item: held.Item()})
	}
	return after, true
}

// refresh shows the stand's armour and held items to everyone near it.
func (b *armourStandBehaviour) refresh(e *Ent) {
	for _, v := range e.tx.Viewers(e.Position()) {
		v.ViewEntityArmour(e)
		v.ViewEntityItems(e)
	}
}
