package javasession

import (
	"math"
	"math/rand/v2"
	"slices"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	jitem "github.com/ezchr/go-mcjava/item"
)

// The enchanting table: Dragonfly's option logic (session/handler_enchanting.go, deterministic for
// the player's enchantment seed) shown through the Java window's data slots: 0-2 level
// requirements, 3 the seed, 4-6 an enchantment of each option (Java id), 7-9 its level.

// enchantData is the enchanting window's data for the item in it, with the enchantment ids of the
// client's protocol p.
func enchantData(tx *world.Tx, c session.Controllable, pos cube.Pos, input item.Stack, p *jitem.Proto) [10]int32 {
	vals := [10]int32{3: int32(c.EnchantmentSeed()) & -16, 4: -1, 5: -1, 6: -1, 7: -1, 8: -1, 9: -1}
	if input.Empty() || input.Count() != 1 {
		return vals
	}
	costs, enchants := availableEnchantments(tx, c, pos, input)
	for i := range min(3, len(enchants)) {
		if len(enchants[i]) == 0 {
			continue
		}
		vals[i] = int32(costs[i])
		e := enchants[i][0]
		if id, ok := item.EnchantmentID(e.Type()); ok && id < len(enchantments().toJava) && enchantments().toJava[id] >= 0 {
			vals[4+i], vals[7+i] = p.Enchantment(enchantments().toJava[id]), int32(e.Level())
		}
	}
	return vals
}

// enchant applies enchantment option i (container_button_click 0-2).
func (s *Session) enchant(tx *world.Tx, c session.Controllable, m *menu, i int) {
	st := s.items()
	input, _ := st.ui.Item(uiEnchantInput)
	if i < 0 || i > 2 || input.Empty() || input.Count() != 1 {
		return
	}
	costs, enchants := availableEnchantments(tx, c, m.pos, input)
	if len(enchants) <= i || len(enchants[i]) == 0 {
		return
	}
	cost := i + 1
	st.applying.Store(true)
	defer st.applying.Store(false)
	if !c.GameMode().CreativeInventory() {
		if c.ExperienceLevel() < costs[i] || c.ExperienceLevel() < cost {
			return
		}
		lapis, _ := st.ui.Item(uiEnchantLapis)
		if _, ok := lapis.Item().(item.LapisLazuli); !ok || lapis.Count() < cost {
			return
		}
		c.SetExperienceLevel(c.ExperienceLevel() - cost)
		_ = st.ui.SetItem(uiEnchantLapis, lapis.Grow(-cost))
	}
	c.ResetEnchantmentSeed()
	if _, book := input.Item().(item.Book); book {
		input = input.WithItem(item.EnchantedBook{})
	}
	_ = st.ui.SetItem(uiEnchantInput, input.WithEnchantments(enchants[i]...))
}

// availableEnchantments is determineAvailableEnchantments of handler_enchanting.go: the level
// requirement and enchantments of the three options.
func availableEnchantments(tx *world.Tx, c session.Controllable, pos cube.Pos, stack item.Stack) ([]int, [][]item.Enchantment) {
	enchantable, ok := stack.Item().(item.Enchantable)
	if !ok || len(stack.Enchantments()) > 0 {
		return nil, nil
	}
	seed := uint64(c.EnchantmentSeed())
	random := rand.New(rand.NewPCG(seed, seed))
	bookshelves := searchBookshelves(tx, pos)
	value := enchantable.EnchantmentValue()

	baseCost := random.IntN(8) + 1 + (bookshelves >> 1) + random.IntN(bookshelves+1)
	upper := max(baseCost/3, 1)
	middle := baseCost*2/3 + 1
	lower := max(baseCost, bookshelves*2)
	return []int{upper, middle, lower}, [][]item.Enchantment{
		createEnchantments(random, stack, value, upper),
		createEnchantments(random, stack, value, middle),
		createEnchantments(random, stack, value, lower),
	}
}

func createEnchantments(random *rand.Rand, stack item.Stack, value, level int) []item.Enchantment {
	randomBonus := (random.Float64() + random.Float64() - 1.0) * 0.15
	cost := level + 1 + random.IntN(value/4+1) + random.IntN(value/4+1)
	cost = min(max(int(math.Round(float64(cost)+float64(cost)*randomBonus)), 1), math.MaxInt32)

	it := stack.Item()
	_, book := it.(item.Book)
	available := make([]item.Enchantment, 0, len(item.Enchantments()))
	for _, enchant := range item.Enchantments() {
		if t, ok := enchant.(interface{ Treasure() bool }); ok && t.Treasure() {
			continue
		}
		if !book && !enchant.CompatibleWithItem(it) {
			continue
		}
		for i := enchant.MaxLevel(); i > 0; i-- {
			if minCost, maxCost := enchant.Cost(i); cost >= minCost && cost <= maxCost {
				available = append(available, item.NewEnchantment(enchant, i))
				break
			}
		}
	}
	if len(available) == 0 {
		return nil
	}
	selected := make([]item.Enchantment, 0, len(available))
	enchant := weightedRandomEnchantment(random, available)
	selected = append(selected, enchant)
	ind := slices.Index(available, enchant)
	available = slices.Delete(available, ind, ind+1)

	for random.IntN(50) <= cost {
		last := selected[len(selected)-1]
		available = slices.DeleteFunc(available, func(e item.Enchantment) bool {
			return !last.Type().CompatibleWithEnchantment(e.Type())
		})
		if len(available) == 0 {
			break
		}
		enchant = weightedRandomEnchantment(random, available)
		selected = append(selected, enchant)
		ind = slices.Index(available, enchant)
		available = slices.Delete(available, ind, ind+1)
		cost /= 2
	}
	return selected
}

func searchBookshelves(tx *world.Tx, pos cube.Pos) (shelves int) {
	for x := -1; x <= 1; x++ {
		for z := -1; z <= 1; z++ {
			for y := 0; y <= 1; y++ {
				if x == 0 && z == 0 {
					continue
				}
				if _, ok := tx.Block(pos.Add(cube.Pos{x, y, z})).(block.Air); !ok {
					continue
				}
				if _, ok := tx.Block(pos.Add(cube.Pos{x * 2, y, z * 2})).(block.Bookshelf); ok {
					shelves++
				}
				if x != 0 && z != 0 {
					if _, ok := tx.Block(pos.Add(cube.Pos{x * 2, y, z})).(block.Bookshelf); ok {
						shelves++
					}
					if _, ok := tx.Block(pos.Add(cube.Pos{x, y, z * 2})).(block.Bookshelf); ok {
						shelves++
					}
				}
				if shelves >= 15 {
					return 15
				}
			}
		}
	}
	return shelves
}

func weightedRandomEnchantment(rs *rand.Rand, enchants []item.Enchantment) item.Enchantment {
	var total int
	for _, e := range enchants {
		total += e.Type().Rarity().Weight()
	}
	r := rs.IntN(total)
	for _, e := range enchants {
		r -= e.Type().Rarity().Weight()
		if r < 0 {
			return e
		}
	}
	return enchants[len(enchants)-1]
}
