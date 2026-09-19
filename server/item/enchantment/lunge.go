package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Lunge is a spear enchantment that propels the wielder forward when landing a jab attack, at the cost of
// hunger and exhaustion scaling with its level. It does not trigger on a charge attack.
var Lunge lunge

type lunge struct{}

// Name ...
func (lunge) Name() string {
	return "Lunge"
}

// MaxLevel ...
func (lunge) MaxLevel() int {
	return 3
}

// Cost ...
func (lunge) Cost(level int) (int, int) {
	minCost := 5 + (level-1)*8
	return minCost, minCost + 20
}

// Rarity ...
func (lunge) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityUncommon
}

// Speed returns the forward speed, in blocks/tick, that a jab attack with this level of Lunge propels the
// wielder at.
func (lunge) Speed(level int) float64 {
	return 0.458 * float64(level)
}

// FoodCost returns the hunger points consumed by triggering Lunge at this level.
func (lunge) FoodCost(level int) int {
	return level
}

// ExhaustionCost returns the exhaustion applied by triggering Lunge at this level.
func (lunge) ExhaustionCost(level int) float64 {
	return 4 * float64(level)
}

// MinimumFood returns the hunger points the wielder must have for Lunge to trigger. Vanilla disables Lunge
// at 5 or fewer hunger points, so it only ever fires with 6 or more - one whole shank of headroom above the
// hunger it will actually spend, even at its highest level.
func (lunge) MinimumFood() int {
	return 6
}

// CompatibleWithEnchantment ...
func (lunge) CompatibleWithEnchantment(item.EnchantmentType) bool {
	return true
}

// CompatibleWithItem ...
func (lunge) CompatibleWithItem(i world.Item) bool {
	_, ok := i.(item.Spear)
	return ok
}
