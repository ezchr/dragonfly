package block

import (
	"image/color"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Cauldron is a block that holds water, lava or powder snow. Water in it can be
// taken out with bottles, coloured with dyes and used to dye or wash leather
// armour.
type Cauldron struct {
	transparent

	// Level is how full the cauldron is, from 0 (empty) to 6 (full). A bucket
	// fills or empties all of it, a bottle a third.
	Level int
	// Liquid is what the cauldron holds when Level is above 0.
	Liquid CauldronLiquid
	// Colour is the colour that dyes gave the water, or the zero value for
	// clear water.
	Colour color.RGBA
}

// Model ...
func (Cauldron) Model() world.BlockModel {
	return model.Cauldron{}
}

// BreakInfo ...
func (c Cauldron) BreakInfo() BreakInfo {
	return newBreakInfo(2, pickaxeHarvestable, pickaxeEffective, oneOf(Cauldron{})).withBlastResistance(10)
}

// LightEmissionLevel ...
func (c Cauldron) LightEmissionLevel() uint8 {
	if c.Level > 0 && c.Liquid == CauldronLava() {
		return 15
	}
	return 0
}

// UseOnBlock places an empty cauldron.
func (c Cauldron) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, c)
	if !used {
		return false
	}
	place(tx, pos, Cauldron{}, user, ctx)
	return placed(ctx)
}

// Activate uses the item held on the cauldron as vanilla Bedrock does:
// buckets and bottles fill and empty it, dyes colour its water, and leather
// armour dipped in water takes the water's colour or, in clear water, is
// washed.
func (c Cauldron) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	held, _ := u.HeldItems()
	if held.Empty() {
		return false
	}
	water := c.Level == 0 || c.Liquid == CauldronWater()
	switch it := held.Item().(type) {
	case item.Bucket:
		if liquid, ok := it.Content.Liquid(); ok {
			fill := CauldronWater()
			if liquid.LiquidType() == "lava" {
				fill = CauldronLava()
			}
			if c.Level > 0 && c.Liquid != fill {
				// another liquid: the two do not mix and the cauldron empties
				c.set(pos, tx, 0, c.Liquid, color.RGBA{})
				tx.PlaySound(pos.Vec3Centre(), sound.CauldronEvent{Event: sound.CauldronExplode})
			} else {
				c.set(pos, tx, 6, fill, color.RGBA{})
				tx.PlaySound(pos.Vec3Centre(), sound.CauldronEvent{Event: fillEvent(fill)})
			}
			c.useUp(ctx, item.NewStack(item.Bucket{}, 1))
			return true
		}
		if it.Empty() && c.Level == 6 {
			var bucket item.Stack
			switch c.Liquid {
			case CauldronWater():
				bucket = item.NewStack(item.Bucket{Content: item.LiquidBucketContent(Water{Still: true, Depth: 8})}, 1)
			case CauldronLava():
				bucket = item.NewStack(item.Bucket{Content: item.LiquidBucketContent(Lava{Still: true, Depth: 8})}, 1)
			default:
				return false // no powder snow bucket yet
			}
			c.set(pos, tx, 0, c.Liquid, color.RGBA{})
			tx.PlaySound(pos.Vec3Centre(), sound.CauldronEvent{Event: takeEvent(c.Liquid)})
			c.useUp(ctx, bucket)
			return true
		}
	case item.GlassBottle:
		if c.Level < 2 || c.Liquid != CauldronWater() {
			return false
		}
		level, colour := c.Level-2, c.Colour
		if level == 0 {
			colour = color.RGBA{}
		}
		c.set(pos, tx, level, CauldronWater(), colour)
		tx.PlaySound(pos.Vec3Centre(), sound.CauldronEvent{Event: sound.CauldronTakeWater})
		c.useUp(ctx, item.NewStack(item.Potion{Type: potion.Water()}, 1))
		return true
	case item.Potion:
		if it.Type != potion.Water() || !water || c.Level >= 6 {
			return false
		}
		c.set(pos, tx, min(c.Level+2, 6), CauldronWater(), c.Colour)
		tx.PlaySound(pos.Vec3Centre(), sound.CauldronEvent{Event: sound.CauldronFillWater})
		c.useUp(ctx, item.NewStack(item.GlassBottle{}, 1))
		return true
	case item.Dye:
		if c.Level == 0 || c.Liquid != CauldronWater() {
			return false
		}
		// The client redraws the water colour only when the block changes, so,
		// as vanilla does, the level goes one down and back up.
		c.set(pos, tx, c.Level-1, CauldronWater(), c.Colour)
		c.set(pos, tx, c.Level, CauldronWater(), mixDye(c.Colour, it.Colour))
		tx.PlaySound(pos.Vec3Centre(), sound.CauldronEvent{Event: sound.CauldronAddDye})
		ctx.SubtractFromCount(1)
		return true
	}
	if c.Level == 0 || c.Liquid != CauldronWater() {
		return false
	}
	dyed, ok := dyeLeather(held, c.Colour)
	if !ok {
		return false
	}
	event := sound.CauldronDyeArmour
	if c.Colour == (color.RGBA{}) {
		event = sound.CauldronCleanArmour
	}
	level, colour := c.Level-1, c.Colour
	if level == 0 {
		colour = color.RGBA{}
	}
	c.set(pos, tx, level, CauldronWater(), colour)
	tx.PlaySound(pos.Vec3Centre(), sound.CauldronEvent{Event: event})
	ctx.SubtractFromCount(1)
	ctx.NewItem = dyed
	return true
}

// set changes the cauldron's level, liquid and colour.
func (c Cauldron) set(pos cube.Pos, tx *world.Tx, level int, liquid CauldronLiquid, colour color.RGBA) {
	c.Level, c.Liquid, c.Colour = level, liquid, colour
	if level == 0 {
		c.Liquid, c.Colour = CauldronWater(), color.RGBA{}
	}
	tx.SetBlock(pos, c, nil)
}

// useUp takes the held item and gives back what it turned into.
func (c Cauldron) useUp(ctx *item.UseContext, result item.Stack) {
	ctx.SubtractFromCount(1)
	ctx.NewItem = result
	ctx.NewItemSurvivalOnly = true
}

// dyeColours are the colours of the dyes as vanilla mixes them, in the order
// of item.Colour.
var dyeColours = [...]color.RGBA{
	{0xf9, 0xff, 0xfe, 0xff}, {0xf9, 0x80, 0x1d, 0xff}, {0xc7, 0x4e, 0xbd, 0xff}, {0x3a, 0xb3, 0xda, 0xff},
	{0xfe, 0xd8, 0x3d, 0xff}, {0x80, 0xc7, 0x1f, 0xff}, {0xf3, 0x8b, 0xaa, 0xff}, {0x47, 0x4f, 0x52, 0xff},
	{0x9d, 0x9d, 0x97, 0xff}, {0x16, 0x9c, 0x9c, 0xff}, {0x89, 0x32, 0xb8, 0xff}, {0x3c, 0x44, 0xaa, 0xff},
	{0x83, 0x54, 0x32, 0xff}, {0x5e, 0x7c, 0x16, 0xff}, {0xb0, 0x2e, 0x26, 0xff}, {0x1d, 0x1d, 0x21, 0xff},
}

// mixDye adds a dye to the water's colour as vanilla does: the average of
// the colour the water has and the dye's.
func mixDye(water color.RGBA, dye item.Colour) color.RGBA {
	d := dyeColours[dye.Uint8()%uint8(len(dyeColours))]
	if water == (color.RGBA{}) {
		return d
	}
	return color.RGBA{
		R: uint8((int(water.R) + int(d.R)) / 2),
		G: uint8((int(water.G) + int(d.G)) / 2),
		B: uint8((int(water.B) + int(d.B)) / 2),
		A: 0xff,
	}
}

// dyeLeather returns held, leather armour, dyed the colour passed, or washed
// clean if the colour is the zero value. It returns false for anything else,
// and for clean armour put in clear water.
func dyeLeather(held item.Stack, colour color.RGBA) (item.Stack, bool) {
	tier := func(t item.ArmourTier) (item.ArmourTierLeather, bool) {
		l, ok := t.(item.ArmourTierLeather)
		if !ok || colour == (color.RGBA{}) && l.Colour == (color.RGBA{}) {
			return l, false
		}
		l.Colour = colour
		return l, true
	}
	var it world.Item
	switch a := held.Item().(type) {
	case item.Helmet:
		t, ok := tier(a.Tier)
		if !ok {
			return held, false
		}
		a.Tier = t
		it = a
	case item.Chestplate:
		t, ok := tier(a.Tier)
		if !ok {
			return held, false
		}
		a.Tier = t
		it = a
	case item.Leggings:
		t, ok := tier(a.Tier)
		if !ok {
			return held, false
		}
		a.Tier = t
		it = a
	case item.Boots:
		t, ok := tier(a.Tier)
		if !ok {
			return held, false
		}
		a.Tier = t
		it = a
	default:
		return held, false
	}
	return held.Grow(1 - held.Count()).WithItem(it), true
}

// fillEvent and takeEvent return the event of filling or emptying a cauldron
// with a bucket of liquid.
func fillEvent(l CauldronLiquid) sound.CauldronEventType {
	if l == CauldronLava() {
		return sound.CauldronFillLava
	}
	return sound.CauldronFillWater
}

func takeEvent(l CauldronLiquid) sound.CauldronEventType {
	if l == CauldronLava() {
		return sound.CauldronTakeLava
	}
	return sound.CauldronTakeWater
}

// EncodeItem ...
func (Cauldron) EncodeItem() (name string, meta int16) {
	return "minecraft:cauldron", 0
}

// EncodeBlock ...
func (c Cauldron) EncodeBlock() (string, map[string]any) {
	return "minecraft:cauldron", map[string]any{"fill_level": int32(c.Level), "cauldron_liquid": c.Liquid.String()}
}

// EncodeNBT ...
func (c Cauldron) EncodeNBT() map[string]any {
	m := map[string]any{"id": "Cauldron", "PotionId": int16(-1), "PotionType": int16(-1), "Items": []any{}}
	if c.Colour != (color.RGBA{}) {
		m["CustomColor"] = int32(uint32(0xff)<<24 | uint32(c.Colour.R)<<16 | uint32(c.Colour.G)<<8 | uint32(c.Colour.B))
	}
	return m
}

// DecodeNBT ...
func (c Cauldron) DecodeNBT(data map[string]any) any {
	c.Colour = color.RGBA{}
	if v, ok := data["CustomColor"].(int32); ok {
		u := uint32(v)
		c.Colour = color.RGBA{R: uint8(u >> 16), G: uint8(u >> 8), B: uint8(u), A: 0xff}
	}
	return c
}

// allCauldrons ...
func allCauldrons() (b []world.Block) {
	for _, l := range CauldronLiquids() {
		for level := 0; level <= 6; level++ {
			b = append(b, Cauldron{Level: level, Liquid: l})
		}
	}
	return
}

// CauldronLiquid is what a cauldron holds.
type CauldronLiquid struct {
	cauldronLiquid
}

type cauldronLiquid uint8

// CauldronWater is water in a cauldron.
func CauldronWater() CauldronLiquid { return CauldronLiquid{0} }

// CauldronLava is lava in a cauldron.
func CauldronLava() CauldronLiquid { return CauldronLiquid{1} }

// CauldronPowderSnow is powder snow in a cauldron.
func CauldronPowderSnow() CauldronLiquid { return CauldronLiquid{2} }

// CauldronLiquids returns every liquid a cauldron can hold.
func CauldronLiquids() []CauldronLiquid {
	return []CauldronLiquid{CauldronWater(), CauldronLava(), CauldronPowderSnow()}
}

// Uint8 ...
func (l cauldronLiquid) Uint8() uint8 { return uint8(l) }

// String ...
func (l cauldronLiquid) String() string {
	switch l {
	case 1:
		return "lava"
	case 2:
		return "powder_snow"
	}
	return "water"
}
