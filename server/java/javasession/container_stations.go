package javasession

import (
	"math/rand/v2"
	"strings"
	"unicode"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/ezchr/dfjava/javamap"
	jitem "github.com/ezchr/go-mcjava/item"
	"github.com/ezchr/go-mcjava/text"
	v777 "github.com/ezchr/go-mcjava/v777"
)

// Results of the windows that make something: the crafting grids and the work stations. Java
// clients take a result from a slot the server fills; Bedrock clients send what they made, so the
// logic below is Dragonfly's Bedrock handler logic (session/handler_*.go) turned around: compute
// the result from the inputs, and apply it when the result is taken.

// updateResult recomputes the window's result slot from its inputs.
func (v *view) updateResult() {
	if v.res < 0 {
		return
	}
	v.slots[v.res] = v.computeResult()
}

func (v *view) computeResult() item.Stack {
	m := v.m
	switch m.kind {
	case menuPlayer, menuCrafting:
		first, width := m.grid()
		if cm, ok := recipes().match(width, v.slots[first:first+width*width]); ok {
			v.craft = cm
			return cm.output
		}
		v.craft = craftMatch{}
	case menuAnvil:
		res, cost, n := anvilResult(v.slots[0], v.slots[1], m.name, v.creative, v.st.proto)
		m.cost, m.repairN = cost, n
		return res
	case menuGrindstone:
		return grindResult(v.slots[0], v.slots[1])
	case menuStonecutter:
		in := v.slots[0]
		if name := itemName(in); name != m.selItem {
			m.selItem, m.sel = name, -1 // vanilla forgets the selection when the input changes
		}
		list := stonecutterFor(in, v.st.proto)
		if m.sel >= 0 && m.sel < len(list) {
			return list[m.sel].out
		}
		m.sel = -1
	case menuSmithing:
		return smithResult(v.slots[0], v.slots[1], v.slots[2])
	}
	return item.Stack{}
}

// mayTakeResult reports whether the result may be taken (an anvil wants its levels).
func (v *view) mayTakeResult() bool {
	if v.m.kind == menuAnvil {
		return v.m.cost > 0 && (v.creative || v.c.ExperienceLevel() >= v.m.cost)
	}
	return true
}

// takeResult uses up the inputs of the result that was just taken and computes the next one.
func (v *view) takeResult() {
	m := v.m
	switch m.kind {
	case menuPlayer, menuCrafting:
		first, width := m.grid()
		for js := first; js < first+width*width; js++ {
			it := v.slots[js]
			if it.Empty() {
				continue
			}
			rem := craftRemainder(it)
			it = it.Grow(-1)
			switch {
			case rem.Empty():
			case it.Empty():
				it = rem
			case it.Comparable(rem):
				it = it.Grow(rem.Count())
			default:
				if left := v.addToInventory(rem); !left.Empty() {
					v.drops = append(v.drops, drop{resultDrop, left})
				}
			}
			v.slots[js] = it
		}
	case menuAnvil:
		cost, creative, pos, tx, c := m.cost, v.creative, m.pos, v.tx, v.c
		v.slots[0] = item.Stack{}
		if m.repairN > 0 {
			v.slots[1] = v.slots[1].Grow(-m.repairN)
		} else {
			v.slots[1] = item.Stack{}
		}
		v.after = append(v.after, func() {
			if !creative {
				c.SetExperienceLevel(c.ExperienceLevel() - cost)
			}
			anvil, ok := tx.Block(pos).(block.Anvil)
			if !ok {
				return
			}
			if !creative && rand.Float64() < 0.12 {
				damaged := anvil.Break()
				if _, gone := damaged.(block.Air); gone {
					tx.PlaySound(pos.Vec3Centre(), sound.AnvilBreak{})
				} else {
					tx.PlaySound(pos.Vec3Centre(), sound.AnvilUse{})
				}
				tx.SetBlock(pos, damaged, nil)
				return
			}
			tx.PlaySound(pos.Vec3Centre(), sound.AnvilUse{})
		})
		m.name = nil
	case menuGrindstone:
		xp := experienceFromEnchantments(v.slots[0].WithEnchantments(v.slots[1].Enchantments()...))
		if v.slots[0].Empty() {
			xp = experienceFromEnchantments(v.slots[1])
		}
		v.slots[0], v.slots[1] = item.Stack{}, item.Stack{}
		tx, c := v.tx, v.c
		v.after = append(v.after, func() {
			for _, o := range entity.NewExperienceOrbs(entity.EyePosition(c), xp) {
				tx.AddEntity(o)
			}
		})
	case menuStonecutter:
		v.slots[0] = v.slots[0].Grow(-1)
	case menuSmithing:
		for i := range 3 {
			v.slots[i] = v.slots[i].Grow(-1)
		}
	}
	v.updateResult()
}

// furnaceExperience gives the experience a furnace collected when its output is taken.
func (v *view) furnaceExperience() {
	f, ok := v.tx.Block(v.m.pos).(interface{ ResetExperience() int })
	if !ok {
		return
	}
	for _, o := range entity.NewExperienceOrbs(entity.EyePosition(v.c), f.ResetExperience()) {
		v.tx.AddEntity(o)
	}
}

// canSmelt reports whether the furnace of the window smelts it (shift-click goes to the input).
func (m *menu) canSmelt(it item.Stack) bool {
	s, ok := it.Item().(item.Smeltable)
	if !ok {
		return false
	}
	info := s.SmeltInfo()
	if info.Product.Empty() {
		return false
	}
	switch m.cookTotal {
	case 200:
		return true
	}
	if m.smoker {
		return info.Food
	}
	return info.Ores
}

// playerCraftResult is the result of the 2x2 grid of the player's inventory.
func (st *itemState) playerCraftResult() item.Stack {
	var cells [4]item.Stack
	for i := range cells {
		cells[i], _ = st.ui.Item(uiCraftSmall + i)
	}
	if cm, ok := recipes().match(2, cells[:]); ok {
		return cm.output
	}
	return item.Stack{}
}

// menuButton handles container_button_click: an enchantment option or a stonecutter recipe.
func (s *Session) menuButton(tx *world.Tx, c session.Controllable, window int32, button int) {
	st := s.items()
	m := st.open.Load()
	if m == nil || m.id != window {
		return
	}
	if !s.menuStillValid(tx, c, m) {
		s.closeMenu(tx, c, true)
		return
	}
	switch m.kind {
	case menuEnchanting:
		s.enchant(tx, c, m, button)
	case menuStonecutter:
		v := st.newView(tx, c, m)
		if button >= 0 && button < len(stonecutterFor(v.slots[0], st.proto)) {
			m.sel = button
		}
	default:
		return
	}
	s.syncWindow(tx, c, m)
}

// renameItem handles rename_item: the name typed in the anvil.
func (s *Session) renameItem(tx *world.Tx, c session.Controllable, name string) {
	st := s.items()
	m := st.open.Load()
	if m == nil || m.kind != menuAnvil {
		return
	}
	if !s.menuStillValid(tx, c, m) {
		s.closeMenu(tx, c, true)
		return
	}
	// AnvilMenu.validateName: no control characters, at most 50 characters.
	name = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f || r == '§' {
			return -1
		}
		return r
	}, name)
	if len([]rune(name)) > 50 {
		return
	}
	m.name = &name
	s.syncWindow(tx, c, m)
}

// anvilResult is the anvil's result, its level cost and how many materials a repair uses
// (handler_anvil.go's handleCraftRecipeOptional, computed up front). p is the client's protocol (its
// item names).
func anvilResult(input, material item.Stack, name *string, creative bool, p *jitem.Proto) (item.Stack, int, int) {
	if input.Empty() {
		return item.Stack{}, 0, 0
	}
	result := input
	anvilCost := input.AnvilCost()
	if !material.Empty() {
		anvilCost += material.AnvilCost()
	}
	var actionCost, renameCost, repairCount int
	if !material.Empty() {
		if repairable, ok := input.Item().(item.Repairable); ok && repairable.RepairableBy(material) {
			var ok bool
			result, actionCost, repairCount, ok = repairItemWithMaterial(input, material, result)
			if !ok {
				return item.Stack{}, 0, 0
			}
		} else {
			_, book := material.Item().(item.EnchantedBook)
			_, durable := input.Item().(item.Durable)
			n1, m1 := input.Item().EncodeItem()
			n2, m2 := material.Item().EncodeItem()
			enchantedBook := book && len(material.Enchantments()) > 0
			if !enchantedBook && (n1 != n2 || m1 != m2 || !durable) {
				return item.Stack{}, 0, 0
			}
			if durable && !enchantedBook {
				result, actionCost = repairItemWithDurable(input, material, result)
			}
			var compatible, incompatible bool
			result, compatible, incompatible, actionCost = mergeEnchantments(input, material, result, actionCost, enchantedBook)
			if !durable && incompatible && !compatible {
				return item.Stack{}, 0, 0
			}
		}
	}
	if name != nil {
		// Vanilla: a blank name or the item's own name removes a custom name; anything else names it.
		current := text.Strip(input.CustomName())
		switch want := *name; {
		case strings.TrimSpace(want) == "" || (current == "" && want == defaultItemName(input, p)):
			if input.CustomName() != "" {
				renameCost = 1
				result = result.WithCustomName("")
			}
		case want != current:
			renameCost = 1
			result = result.WithCustomName(want)
		}
		actionCost += renameCost
	}
	if actionCost <= 0 {
		return item.Stack{}, 0, 0
	}
	cost := actionCost + anvilCost
	if renameCost == actionCost && cost >= 40 {
		cost = 39 // renaming alone never gets too expensive
	}
	if cost >= 40 && !creative {
		return item.Stack{}, cost, 0 // "Too Expensive!"
	}
	updated := result.AnvilCost()
	if !material.Empty() && updated < material.AnvilCost() {
		updated = material.AnvilCost()
	}
	if renameCost != actionCost {
		updated = updated*2 + 1
	}
	return result.WithAnvilCost(updated), cost, repairCount
}

// defaultItemName is the English name the client puts in the anvil's text field for an item
// without a custom name ("Diamond Sword" for minecraft:diamond_sword): the name of the item the client
// of protocol p sees.
func defaultItemName(it item.Stack, p *jitem.Proto) string {
	name := strings.TrimPrefix(p.ItemName(javamap.Item(it.Item())), "minecraft:")
	words := strings.Split(name, "_")
	for i, w := range words {
		if w == "" {
			continue
		}
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

// repairItemWithMaterial repairs a stack with its repair material (handler_anvil.go).
func repairItemWithMaterial(input, material, result item.Stack) (item.Stack, int, int, bool) {
	delta := min(input.MaxDurability()-input.Durability(), input.MaxDurability()/4)
	if delta <= 0 {
		return item.Stack{}, 0, 0, false
	}
	var cost, count int
	for ; delta > 0 && count < material.Count(); count, delta = count+1, min(result.MaxDurability()-result.Durability(), result.MaxDurability()/4) {
		result = result.WithDurability(result.Durability() + delta)
		cost++
	}
	return result, cost, count, true
}

// repairItemWithDurable repairs a stack with another of the same kind (handler_anvil.go).
func repairItemWithDurable(input, durable, result item.Stack) (item.Stack, int) {
	durability := min(input.Durability()+durable.Durability()+input.MaxDurability()*12/100, input.MaxDurability())
	var cost int
	if durability > input.Durability() {
		result = result.WithDurability(durability)
		cost += 2
	}
	return result, cost
}

// mergeEnchantments merges the material's enchantments onto the result (handler_anvil.go).
func mergeEnchantments(input, material, result item.Stack, cost int, enchantedBook bool) (item.Stack, bool, bool, int) {
	var hasCompatible, hasIncompatible bool
	for _, enchant := range material.Enchantments() {
		enchantType := enchant.Type()
		compatible := enchantType.CompatibleWithItem(input.Item())
		if _, ok := input.Item().(item.EnchantedBook); ok {
			compatible = true
		}
		for _, other := range input.Enchantments() {
			if otherType := other.Type(); enchantType != otherType && !enchantType.CompatibleWithEnchantment(otherType) {
				compatible = false
				cost++
			}
		}
		if !compatible {
			hasIncompatible = true
			continue
		}
		hasCompatible = true
		resultLevel := enchant.Level()
		levelCost := resultLevel
		if existing, ok := input.Enchantment(enchantType); ok {
			if existing.Level() > resultLevel || (existing.Level() == resultLevel && resultLevel == enchantType.MaxLevel()) {
				hasIncompatible = true
				continue
			} else if existing.Level() == resultLevel {
				resultLevel++
			}
			levelCost = resultLevel - existing.Level()
		}
		rarityCost := enchantType.Rarity().Cost()
		if enchantedBook {
			rarityCost = max(1, rarityCost/2)
		}
		result = result.WithEnchantments(item.NewEnchantment(enchantType, resultLevel))
		cost += rarityCost * levelCost
		if input.Count() > 1 {
			cost = 40
		}
	}
	return result, hasCompatible, hasIncompatible, cost
}

// grindResult is the grindstone's result (handler_grindstone.go).
func grindResult(first, second item.Stack) item.Stack {
	if first.Empty() && second.Empty() {
		return item.Stack{}
	}
	if first.Count() > 1 || second.Count() > 1 {
		return item.Stack{}
	}
	res := first
	if first.Empty() {
		res = second
	}
	if first.Empty() != second.Empty() && len(res.Enchantments()) == 0 {
		// Vanilla: a single item without enchantments gives nothing (else renaming then
		// grinding would wipe the prior-work penalty for free).
		return item.Stack{}
	}
	if !first.Empty() && !second.Empty() {
		n1, m1 := first.Item().EncodeItem()
		n2, m2 := second.Item().EncodeItem()
		if n1 != n2 || m1 != m2 {
			return item.Stack{}
		}
		if _, ok := first.Item().(item.Durable); !ok {
			return item.Stack{}
		}
		res = first.WithEnchantments(second.Enchantments()...)
		res = res.WithDurability(min(first.Durability()+second.Durability()+first.MaxDurability()*5/100, first.MaxDurability()))
	}
	for _, e := range res.Enchantments() {
		if c, ok := e.Type().(interface{ Curse() bool }); ok && c.Curse() {
			continue
		}
		res = res.WithoutEnchantments(e.Type())
	}
	return res.WithAnvilCost(0)
}

// experienceFromEnchantments is the experience a grindstone gives for the enchantments it removes.
func experienceFromEnchantments(stack item.Stack) int {
	var total int
	for _, e := range stack.Enchantments() {
		if c, ok := e.Type().(interface{ Curse() bool }); ok && c.Curse() {
			continue
		}
		cost, _ := e.Type().Cost(e.Level())
		total += cost
	}
	if total == 0 {
		return 0
	}
	minXP := (total + 1) / 2
	return minXP + rand.IntN(minXP)
}

// smithingIndex holds the smithing table recipes and what each input slot accepts.
type smithingIndex struct {
	recipes                  []smithRecipe
	template, base, addition map[string]bool
}

type smithRecipe struct {
	base, addition, template ingredient
	out                      item.Stack
	trim                     bool
}

var smithing = onceRecipes(func() *smithingIndex {
	ix := &smithingIndex{template: map[string]bool{}, base: map[string]bool{}, addition: map[string]bool{}}
	add := func(set map[string]bool, in ingredient) {
		if in.tag != nil {
			for _, it := range world.Items() {
				if name, _ := it.EncodeItem(); in.tag.Contains(name) {
					set[name] = true
				}
			}
			return
		}
		set[in.name] = true
	}
	for _, r := range recipe.Recipes() {
		if r.Block() != "smithing_table" || len(r.Input()) != 3 {
			continue
		}
		sr := smithRecipe{base: newIngredient(r.Input()[0]), addition: newIngredient(r.Input()[1]), template: newIngredient(r.Input()[2])}
		switch r.(type) {
		case recipe.SmithingTransform:
			if len(r.Output()) == 0 {
				continue
			}
			sr.out = r.Output()[0]
		case recipe.SmithingTrim:
			sr.trim = true
		default:
			continue
		}
		ix.recipes = append(ix.recipes, sr)
		add(ix.base, sr.base)
		add(ix.addition, sr.addition)
		add(ix.template, sr.template)
	}
	return ix
})

// smithResult is the smithing table's result (handler_smithing.go).
func smithResult(template, base, addition item.Stack) item.Stack {
	if template.Empty() || base.Empty() || addition.Empty() {
		return item.Stack{}
	}
	tn, tm := template.Item().EncodeItem()
	bn, bm := base.Item().EncodeItem()
	an, am := addition.Item().EncodeItem()
	for _, r := range smithing().recipes {
		if !r.template.matches(tn, tm) || !r.base.matches(bn, bm) || !r.addition.matches(an, am) {
			continue
		}
		res := base.Grow(1 - base.Count())
		if !r.trim {
			return res.WithItem(r.out.Item())
		}
		t, ok := template.Item().(item.SmithingTemplate)
		mat, ok2 := addition.Item().(item.ArmourTrimMaterial)
		trimmable, ok3 := base.Item().(item.Trimmable)
		if !ok || !ok2 || !ok3 {
			return item.Stack{}
		}
		return res.WithItem(trimmable.WithTrim(item.ArmourTrim{Template: t.Template, Material: mat}))
	}
	return item.Stack{}
}

// stoneRecipe is a stonecutter recipe, in the order the client got them (update_recipes).
type stoneRecipe struct {
	in  []int32 // Java item ids the input accepts
	out item.Stack
	jo  int32 // Java id of the output
}

var stonecutterRecipes = onceRecipes(func() []stoneRecipe {
	var list []stoneRecipe
	for _, r := range recipe.Recipes() {
		if r.Block() != "stonecutter" || len(r.Input()) != 1 || len(r.Output()) == 0 {
			continue
		}
		jo := javamap.Item(r.Output()[0].Item())
		if jo <= 0 {
			continue
		}
		ids := javaIDsOf(newIngredient(r.Input()[0]))
		if len(ids) == 0 {
			continue
		}
		list = append(list, stoneRecipe{in: ids, out: r.Output()[0], jo: jo})
	}
	return list
})

// javaIDsOf lists the Java items an ingredient accepts.
func javaIDsOf(in ingredient) []int32 {
	var ids []int32
	addIt := func(it world.Item) {
		if id := javamap.Item(it); id > 0 && !containsID(ids, id) {
			ids = append(ids, id)
		}
	}
	switch {
	case in.empty:
	case in.tag != nil:
		for _, it := range world.Items() {
			if name, _ := it.EncodeItem(); in.tag.Contains(name) {
				addIt(it)
			}
		}
	default:
		if it, ok := world.ItemByName(in.name, in.meta); ok {
			addIt(it)
		}
	}
	return ids
}

func containsID(ids []int32, id int32) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// stonecutterFor lists the stonecutter recipes for an input, in the order of the list the client of
// protocol p has (the index is the button it sends): a client of an older protocol matches the
// recipes by its own item ids, which several newest items may share.
func stonecutterFor(in item.Stack, p *jitem.Proto) []stoneRecipe {
	if in.Empty() {
		return nil
	}
	id := javamap.Item(in.Item())
	var out []stoneRecipe
	for _, r := range stonecutterRecipes() {
		if p == nil && containsID(r.in, id) || p != nil && containsMapped(r.in, id, p) {
			out = append(out, r)
		}
	}
	return out
}

// containsMapped reports whether ids has an item that p's version sees as the item it sees for id.
func containsMapped(ids []int32, id int32, p *jitem.Proto) bool {
	o := p.Item(id)
	for _, x := range ids {
		if p.Item(x) == o {
			return true
		}
	}
	return false
}

// Beacon effects: Bedrock effect id (Dragonfly) <-> Java mob_effect, and the pyramid level each needs.
var beaconEffects = []struct {
	bedrock int
	java    string
	level   int
}{
	{1, "minecraft:speed", 1}, {3, "minecraft:haste", 1},
	{11, "minecraft:resistance", 2}, {8, "minecraft:jump_boost", 2},
	{5, "minecraft:strength", 3}, {10, "minecraft:regeneration", 4},
}

// beaconEffectData is the beacon window's data value of an effect: its id in the client's protocol p
// + 1, 0 for none.
func beaconEffectData(t effect.LastingType, p *jitem.Proto) int32 {
	if t == nil {
		return 0
	}
	id, ok := effect.ID(t)
	if !ok {
		return 0
	}
	for _, e := range beaconEffects {
		if e.bedrock == id {
			return p.Effect(v777.BuiltinID("minecraft:mob_effect", e.java)) + 1
		}
	}
	return 0
}

// setBeacon handles set_beacon: the effects chosen in the beacon window, paid with the item in its
// payment slot.
func (s *Session) setBeacon(tx *world.Tx, c session.Controllable, primary, secondary int32) {
	st := s.items()
	m := st.open.Load()
	if m == nil || m.kind != menuBeacon {
		return
	}
	if !s.menuStillValid(tx, c, m) {
		s.closeMenu(tx, c, true)
		return
	}
	beacon, ok := tx.Block(m.pos).(block.Beacon)
	if !ok {
		return
	}
	payment, _ := st.ui.Item(uiBeacon)
	if p, ok := payment.Item().(item.BeaconPayment); !ok || !p.PayableForBeacon() {
		return
	}
	pick := func(java int32, secondary bool) (effect.LastingType, bool) {
		if java < 0 {
			return nil, true
		}
		for _, e := range beaconEffects {
			if v777.BuiltinID("minecraft:mob_effect", e.java) != java {
				continue
			}
			level := e.level
			if secondary && e.bedrock != 10 {
				// The secondary power is regeneration or a second level of the primary.
				level = 4
			}
			if beacon.Level() < level {
				return nil, false
			}
			t, ok := effect.ByID(e.bedrock)
			if !ok {
				return nil, false
			}
			lt, ok := t.(effect.LastingType)
			return lt, ok
		}
		return nil, false
	}
	p, ok1 := pick(primary, false)
	sec, ok2 := pick(secondary, true)
	if !ok1 || !ok2 || p == nil {
		return
	}
	beacon.Primary, beacon.Secondary = p, sec
	tx.SetBlock(m.pos, beacon, nil)
	st.applying.Store(true)
	_ = st.ui.SetItem(uiBeacon, payment.Grow(-1))
	st.applying.Store(false)
	s.syncWindow(tx, c, m)
}
