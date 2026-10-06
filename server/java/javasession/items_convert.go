package javasession

import (
	"image/color"
	"sync"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/ezchr/dfjava/javamap"
	jitem "github.com/ezchr/go-mcjava/item"
	v777 "github.com/ezchr/go-mcjava/v777"
)

// Java item ids the conversion needs.
var (
	javaTippedArrow   = mustJavaItem("minecraft:tipped_arrow")
	javaEnchantedBook = mustJavaItem("minecraft:enchanted_book")
	javaPotion        = mustJavaItem("minecraft:potion")
	javaSplashPotion  = mustJavaItem("minecraft:splash_potion")
	javaLingering     = mustJavaItem("minecraft:lingering_potion")
)

func mustJavaItem(name string) int32 {
	id, ok := jitem.ByName(name)
	if !ok {
		panic("javasession: no Java item " + name)
	}
	return id
}

// bedrockEnchantments are the Java keys of Bedrock enchantment ids (Dragonfly registers its
// enchantments under the Bedrock ids).
var bedrockEnchantments = [...]string{
	0: "protection", 1: "fire_protection", 2: "feather_falling", 3: "blast_protection",
	4: "projectile_protection", 5: "thorns", 6: "respiration", 7: "depth_strider", 8: "aqua_affinity",
	9: "sharpness", 10: "smite", 11: "bane_of_arthropods", 12: "knockback", 13: "fire_aspect",
	14: "looting", 15: "efficiency", 16: "silk_touch", 17: "unbreaking", 18: "fortune", 19: "power",
	20: "punch", 21: "flame", 22: "infinity", 23: "luck_of_the_sea", 24: "lure", 25: "frost_walker",
	26: "mending", 27: "binding_curse", 28: "vanishing_curse", 29: "impaling", 30: "riptide",
	31: "loyalty", 32: "channeling", 33: "multishot", 34: "piercing", 35: "quick_charge",
	36: "soul_speed", 37: "swift_sneak", 38: "wind_burst", 39: "density", 40: "breach", 41: "lunge",
}

type enchantTable struct {
	toJava   [len(bedrockEnchantments)]int32 // -1: no Java enchantment
	fromJava map[int32]item.EnchantmentType
}

var enchantments = sync.OnceValue(func() *enchantTable {
	t := &enchantTable{fromJava: map[int32]item.EnchantmentType{}}
	for id, name := range bedrockEnchantments {
		t.toJava[id] = v777.RegistryID("minecraft:enchantment", "minecraft:"+name)
		if et, ok := item.EnchantmentByID(id); ok && t.toJava[id] >= 0 {
			t.fromJava[t.toJava[id]] = et
		}
	}
	return t
})

// bedrockPotions are the Java potions of Bedrock potion ids. Bedrock's long mundane and decay
// (wither) potions have no Java potion: they are -1 and handled apart.
var bedrockPotions = [...]int32{
	jitem.PotionWater, jitem.PotionMundane, -1, jitem.PotionThick, jitem.PotionAwkward,
	jitem.PotionNightVision, jitem.PotionLongNightVision, jitem.PotionInvisibility, jitem.PotionLongInvisibility,
	jitem.PotionLeaping, jitem.PotionLongLeaping, jitem.PotionStrongLeaping,
	jitem.PotionFireResistance, jitem.PotionLongFireResistance,
	jitem.PotionSwiftness, jitem.PotionLongSwiftness, jitem.PotionStrongSwiftness,
	jitem.PotionSlowness, jitem.PotionLongSlowness,
	jitem.PotionWaterBreathing, jitem.PotionLongWaterBreathing,
	jitem.PotionHealing, jitem.PotionStrongHealing, jitem.PotionHarming, jitem.PotionStrongHarming,
	jitem.PotionPoison, jitem.PotionLongPoison, jitem.PotionStrongPoison,
	jitem.PotionRegeneration, jitem.PotionLongRegeneration, jitem.PotionStrongRegeneration,
	jitem.PotionStrength, jitem.PotionLongStrength, jitem.PotionStrongStrength,
	jitem.PotionWeakness, jitem.PotionLongWeakness,
	-1, // 36 decay
	jitem.PotionTurtleMaster, jitem.PotionLongTurtleMaster, jitem.PotionStrongTurtleMaster,
	jitem.PotionSlowFalling, jitem.PotionLongSlowFalling, jitem.PotionStrongSlowness,
	jitem.PotionWindCharged, jitem.PotionWeaving, jitem.PotionOozing, jitem.PotionInfested,
}

const (
	bedrockLongMundane = 2
	bedrockDecay       = 36
	decayColour        = 0x736156
)

// javaPotionToBedrock is the reverse of bedrockPotions.
var javaPotionToBedrock = func() map[int32]int32 {
	m := make(map[int32]int32, len(bedrockPotions))
	for b, j := range bedrockPotions {
		if j >= 0 {
			if _, ok := m[j]; !ok {
				m[j] = int32(b)
			}
		}
	}
	return m
}()

func setPotion(js *jitem.Stack, p potion.Potion) {
	id := int(p.Uint8())
	pc := &js.PotionContents
	switch {
	case id < len(bedrockPotions) && bedrockPotions[id] >= 0:
		pc.HasPotion, pc.Potion = true, bedrockPotions[id]
	case id == bedrockLongMundane:
		pc.HasPotion, pc.Potion = true, jitem.PotionMundane
	case id == bedrockDecay:
		// Wither II for 40 seconds, the way Bedrock's decay potion works.
		pc.HasCustomColor, pc.CustomColor = true, decayColour
		pc.Effects = append(pc.Effects, jitem.Effect{ID: jitem.EffectWither, Amplifier: 1, Duration: 800, ShowParticles: true, ShowIcon: true})
	default:
		pc.HasPotion, pc.Potion = true, jitem.PotionWater
	}
	js.Add(jitem.CompPotionContents)
}

func dyedColour(t item.ArmourTier, js *jitem.Stack) {
	if l, ok := t.(item.ArmourTierLeather); ok && l.Colour != (color.RGBA{}) {
		js.DyedColor = int32(l.Colour.R)<<16 | int32(l.Colour.G)<<8 | int32(l.Colour.B)
		js.Add(jitem.CompDyedColor)
	}
}

// javaStack converts a Dragonfly stack to a Java one, reusing js's slices.
func javaStack(ds item.Stack, js *jitem.Stack) {
	// Most item.Stack getters call Stack.Empty, which calls EncodeItem, which allocates for tools and
	// armour (names built per call): each getter is called at most once here.
	js.Reset()
	it := ds.Item()
	if it == nil {
		return
	}
	js.Count = int32(ds.Count())
	js.ID = javamap.Item(it)
	switch v := it.(type) {
	case item.Potion:
		setPotion(js, v.Type)
	case item.SplashPotion:
		setPotion(js, v.Type)
	case item.LingeringPotion:
		setPotion(js, v.Type)
	case item.Arrow:
		if v.Tip.Uint8() > 4 { // what Bedrock counts as a tipped arrow
			js.ID = javaTippedArrow
			setPotion(js, v.Tip)
		}
	case item.Helmet:
		dyedColour(v.Tier, js)
	case item.Chestplate:
		dyedColour(v.Tier, js)
	case item.Leggings:
		dyedColour(v.Tier, js)
	case item.Boots:
		dyedColour(v.Tier, js)
	}

	mc := int32(64)
	if c, ok := it.(item.MaxCounter); ok {
		mc = int32(c.MaxCount())
	}
	if mc != jitem.DefaultMaxStackSize(js.ID) && mc >= 1 && mc <= 99 {
		js.MaxStackSize = mc
		js.Add(jitem.CompMaxStackSize)
	}
	if d, ok := it.(item.Durable); ok {
		md := d.DurabilityInfo().MaxDurability
		if dmg := md - ds.Durability(); md > 0 && dmg > 0 {
			// Bedrock durabilities are often one more than Java's: send ours so the bar is right.
			if int32(md) != jitem.DefaultMaxDamage(js.ID) {
				js.MaxDamage = int32(md)
				js.Add(jitem.CompMaxDamage)
			}
			js.Damage = int32(dmg)
			js.Add(jitem.CompDamage)
		}
	}
	if ds.Unbreakable() {
		js.Add(jitem.CompUnbreakable)
	}
	if n := ds.CustomName(); n != "" {
		js.CustomName.Text = n
		js.Add(jitem.CompCustomName)
	}
	if lore := ds.Lore(); len(lore) > 0 {
		for _, l := range lore {
			js.Lore = append(js.Lore, jitem.Text{Text: l})
		}
		js.Add(jitem.CompLore)
	}
	if es := ds.Enchantments(); len(es) > 0 {
		t := enchantments()
		dst, comp := &js.Enchantments, jitem.CompEnchantments
		if js.ID == javaEnchantedBook {
			dst, comp = &js.StoredEnchantments, jitem.CompStoredEnchantments
		}
		for _, e := range es {
			id, ok := item.EnchantmentID(e.Type())
			if !ok || id < 0 || id >= len(t.toJava) || t.toJava[id] < 0 {
				continue
			}
			*dst = append(*dst, jitem.Enchantment{ID: t.toJava[id], Level: int32(e.Level())})
		}
		if len(*dst) > 0 {
			js.Add(comp)
		}
	}
}

// javaItems maps Java item ids to the Dragonfly items they come from (for stacks a creative client
// sends). Items with meta 0 win when several Bedrock items map to one Java item.
var javaItems = sync.OnceValue(func() map[int32]world.Item {
	type key struct {
		meta int16
		name string
	}
	m := map[int32]world.Item{}
	keys := map[int32]key{}
	for _, it := range world.Items() {
		name, meta := it.EncodeItem()
		id, ok := javamap.ItemID(name, meta)
		if !ok {
			continue
		}
		if prev, ok := keys[id]; ok && (prev.meta < meta || prev.meta == meta && prev.name <= name) {
			continue
		}
		m[id], keys[id] = it, key{meta, name}
	}
	return m
})

func javaPotionType(js *jitem.Stack) potion.Potion {
	if js.Has(jitem.CompPotionContents) && js.PotionContents.HasPotion {
		if b, ok := javaPotionToBedrock[js.PotionContents.Potion]; ok {
			return potion.From(b)
		}
	}
	return potion.Water()
}

func leatherTier(t item.ArmourTier, js *jitem.Stack) item.ArmourTier {
	if _, ok := t.(item.ArmourTierLeather); ok && js.Has(jitem.CompDyedColor) {
		c := js.DyedColor
		return item.ArmourTierLeather{Colour: color.RGBA{R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c), A: 0xff}}
	}
	return t
}

// Limits on what a creative client makes: Bedrock creative players in Dragonfly only get items from
// the creative inventory, so a Java creative client gets those plus a bounded name and lore
// (vanilla: lore up to 256 lines; the anvil names up to 50 characters).
const (
	maxCreativeName      = 64
	maxCreativeLore      = 16
	maxCreativeLoreChars = 128
)

// dragonflyStack converts a Java stack (sent by a creative client) to a Dragonfly stack. ok is false
// for items Dragonfly does not have. Names and lore are cut to the creative limits, enchantment
// levels to the enchantment's maximum, and unbreakable is not taken.
func dragonflyStack(js *jitem.Stack) (item.Stack, bool) {
	if js.Empty() {
		return item.Stack{}, true
	}
	var it world.Item
	switch js.ID {
	case javaPotion:
		it = item.Potion{Type: javaPotionType(js)}
	case javaSplashPotion:
		it = item.SplashPotion{Type: javaPotionType(js)}
	case javaLingering:
		it = item.LingeringPotion{Type: javaPotionType(js)}
	case javaTippedArrow:
		it = item.Arrow{Tip: javaPotionType(js)}
	default:
		var ok bool
		if it, ok = javaItems()[js.ID]; !ok {
			return item.Stack{}, false
		}
	}
	switch v := it.(type) {
	case item.Helmet:
		v.Tier = leatherTier(v.Tier, js)
		it = v
	case item.Chestplate:
		v.Tier = leatherTier(v.Tier, js)
		it = v
	case item.Leggings:
		v.Tier = leatherTier(v.Tier, js)
		it = v
	case item.Boots:
		v.Tier = leatherTier(v.Tier, js)
		it = v
	}
	ds := item.NewStack(it, 1)
	ds = ds.Grow(max(1, min(int(js.Count), ds.MaxCount())) - 1)
	if js.Has(jitem.CompDamage) && js.Damage > 0 {
		if md := ds.MaxDurability(); md > 0 {
			ds = ds.WithDurability(max(1, md-int(js.Damage)))
		}
	}
	if js.Has(jitem.CompCustomName) && js.CustomName.Text != "" {
		ds = ds.WithCustomName(cutRunes(js.CustomName.Text, maxCreativeName))
	}
	if js.Has(jitem.CompLore) && len(js.Lore) > 0 {
		lines := make([]string, min(len(js.Lore), maxCreativeLore))
		for i := range lines {
			lines[i] = cutRunes(js.Lore[i].Text, maxCreativeLoreChars)
		}
		ds = ds.WithLore(lines...)
	}
	ench := js.Enchantments
	if js.ID == javaEnchantedBook {
		ench = js.StoredEnchantments
	}
	if len(ench) > 0 {
		t := enchantments()
		es := make([]item.Enchantment, 0, len(ench))
		for _, e := range ench {
			if et, ok := t.fromJava[e.ID]; ok && e.Level > 0 {
				es = append(es, item.NewEnchantment(et, min(int(e.Level), max(1, et.MaxLevel()))))
			}
		}
		ds = ds.WithEnchantments(es...)
	}
	return ds, true
}

// cutRunes cuts s to at most n characters.
func cutRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := 0
	for j := range s {
		if i == n {
			return s[:j]
		}
		i++
	}
	return s
}
