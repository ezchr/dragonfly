package javasession

import (
	"sync"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
)

// Bedrock clients tell the server which recipe they crafted; Java clients only fill the grid and
// take the result, so the server has to find the recipe itself. craftIndex does that over
// Dragonfly's recipe.Recipes(): shaped recipes (also mirrored, like vanilla), shapeless recipes and
// Dragonfly's dynamic recipes, with item tags and "any variant" inputs as Dragonfly defines them.

// ingredient is one recipe input, resolved once.
type ingredient struct {
	empty bool
	name  string
	meta  int16
	any   bool // any meta ("variants")
	tag   *recipe.ItemTag
}

func newIngredient(in recipe.Item) ingredient {
	if in == nil || in.Empty() {
		return ingredient{empty: true}
	}
	switch v := in.(type) {
	case item.Stack:
		name, meta := v.Item().EncodeItem()
		_, variants := v.Value("variants")
		return ingredient{name: name, meta: meta, any: variants}
	case recipe.ItemTag:
		return ingredient{tag: &v}
	}
	return ingredient{empty: true}
}

// matches reports whether a grid stack (name and meta already encoded) fits the ingredient. Stack
// data (names, enchantments) does not matter, as in vanilla.
func (in *ingredient) matches(name string, meta int16) bool {
	if in.tag != nil {
		return in.tag.Contains(name)
	}
	return in.name == name && (in.any || in.meta == meta)
}

// craftRecipe is a crafting table recipe ready for matching.
type craftRecipe struct {
	shaped   bool
	w, h     int
	in       []ingredient // shaped: w*h cells, row by row; shapeless: the non-empty inputs
	out      item.Stack
	userData bool // the output keeps the data of the input it was made from (dyed shulker boxes)
	prio     uint32
	order    int
}

// better reports whether r wins over o when both match (Dragonfly priority, then file order).
func (r *craftRecipe) better(o *craftRecipe) bool {
	if o == nil {
		return true
	}
	if r.prio != o.prio {
		return r.prio < o.prio
	}
	return r.order < o.order
}

type shapeKey struct {
	w, h  int8
	first string // name of the first non-empty cell, "#" if it is a tag
}

type craftIndex struct {
	shaped    map[shapeKey][]*craftRecipe
	shapeless [10][]*craftRecipe
	dynamic   []recipe.DynamicRecipe
	count     int
}

// recipes is built on first use (Dragonfly registers its recipes in init).
var recipes = onceRecipes(buildCraftIndex)

func buildCraftIndex() *craftIndex {
	ix := &craftIndex{shaped: map[shapeKey][]*craftRecipe{}}
	for i, r := range recipe.Recipes() {
		if r.Block() != "crafting_table" || len(r.Output()) == 0 {
			continue
		}
		cr := &craftRecipe{out: r.Output()[0], prio: r.Priority(), order: i}
		switch v := r.(type) {
		case recipe.Shaped:
			cr.shaped, cr.w, cr.h = true, v.Shape().Width(), v.Shape().Height()
			if cr.w < 1 || cr.h < 1 || cr.w > 3 || cr.h > 3 || len(v.Input()) != cr.w*cr.h {
				continue
			}
			first := ""
			for _, in := range v.Input() {
				ing := newIngredient(in)
				cr.in = append(cr.in, ing)
				if first == "" && !ing.empty {
					first = ing.name
					if ing.tag != nil {
						first = "#"
					}
				}
			}
			if first == "" {
				continue
			}
			k := shapeKey{int8(cr.w), int8(cr.h), first}
			ix.shaped[k] = append(ix.shaped[k], cr)
		case recipe.Shapeless, recipe.UserDataShapeless:
			_, cr.userData = v.(recipe.UserDataShapeless)
			for _, in := range r.Input() {
				ing := newIngredient(in)
				if ing.empty {
					continue
				}
				// An input of n items takes n grid slots.
				for range max(1, in.Count()) {
					cr.in = append(cr.in, ing)
				}
			}
			if len(cr.in) == 0 || len(cr.in) > 9 {
				continue
			}
			ix.shapeless[len(cr.in)] = append(ix.shapeless[len(cr.in)], cr)
		default:
			continue
		}
		ix.count++
	}
	for _, d := range recipe.DynamicRecipes() {
		if d.Block() == "crafting_table" {
			ix.dynamic = append(ix.dynamic, d)
		}
	}
	return ix
}

// grid is a crafting grid being matched: width x width cells, row by row.
type grid struct {
	width int
	cells []item.Stack
	names [9]string
	metas [9]int16
	n     int // non-empty cells
	// bounding box of the non-empty cells
	x0, y0, w, h int
}

func newGrid(width int, cells []item.Stack) *grid {
	g := &grid{width: width, cells: cells}
	x0, y0, x1, y1 := width, width, -1, -1
	for i, c := range cells {
		if c.Empty() {
			continue
		}
		g.names[i], g.metas[i] = c.Item().EncodeItem()
		g.n++
		x, y := i%width, i/width
		x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
	}
	if g.n > 0 {
		g.x0, g.y0, g.w, g.h = x0, y0, x1-x0+1, y1-y0+1
	}
	return g
}

// cell is the grid index of (x, y) in the bounding box, mirrored horizontally if asked.
func (g *grid) cell(x, y int, mirror bool) int {
	if mirror {
		x = g.w - 1 - x
	}
	return (g.y0+y)*g.width + g.x0 + x
}

// craftMatch is a matched recipe.
type craftMatch struct {
	r      *craftRecipe
	dyn    bool
	output item.Stack
}

// match finds the recipe the grid makes. It returns ok false if there is none.
func (ix *craftIndex) match(width int, cells []item.Stack) (craftMatch, bool) {
	g := newGrid(width, cells)
	if g.n == 0 {
		return craftMatch{}, false
	}
	var best *craftRecipe
	for _, mirror := range [2]bool{false, true} {
		first := g.names[g.firstCell(mirror)]
		for _, k := range [2]string{first, "#"} {
			for _, r := range ix.shaped[shapeKey{int8(g.w), int8(g.h), k}] {
				if r.better(best) && g.matchShaped(r, mirror) {
					best = r
				}
			}
		}
	}
	if g.n < len(ix.shapeless) {
		for _, r := range ix.shapeless[g.n] {
			if r.better(best) && g.matchShapeless(r) {
				best = r
			}
		}
	}
	if best != nil {
		return craftMatch{r: best, output: g.output(best)}, true
	}
	if len(ix.dynamic) > 0 {
		in := make([]recipe.Item, len(cells))
		for i, c := range cells {
			in[i] = c
		}
		for _, d := range ix.dynamic {
			if out, ok := d.Match(in); ok && len(out) > 0 && !out[0].Empty() {
				return craftMatch{dyn: true, output: out[0]}, true
			}
		}
	}
	return craftMatch{}, false
}

// firstCell is the first non-empty cell of the bounding box (row by row), mirrored if asked.
func (g *grid) firstCell(mirror bool) int {
	for y := range g.h {
		for x := range g.w {
			if i := g.cell(x, y, mirror); !g.cells[i].Empty() {
				return i
			}
		}
	}
	return 0
}

func (g *grid) matchShaped(r *craftRecipe, mirror bool) bool {
	for y := range g.h {
		for x := range g.w {
			ing := &r.in[y*r.w+x]
			i := g.cell(x, y, mirror)
			if ing.empty != g.cells[i].Empty() {
				return false
			}
			if !ing.empty && !ing.matches(g.names[i], g.metas[i]) {
				return false
			}
		}
	}
	return true
}

// matchShapeless matches every non-empty cell to a different ingredient (backtracking: with tags a
// greedy choice can fail where another assignment works).
func (g *grid) matchShapeless(r *craftRecipe) bool {
	var cells [9]int
	n := 0
	for i, c := range g.cells {
		if !c.Empty() {
			cells[n] = i
			n++
		}
	}
	var used [9]bool
	var try func(k int) bool
	try = func(k int) bool {
		if k == n {
			return true
		}
		i := cells[k]
		for j := range r.in {
			if used[j] || !r.in[j].matches(g.names[i], g.metas[i]) {
				continue
			}
			used[j] = true
			if try(k + 1) {
				return true
			}
			used[j] = false
		}
		return false
	}
	return try(0)
}

// output is the stack the recipe makes from this grid.
func (g *grid) output(r *craftRecipe) item.Stack {
	out := r.out
	if !r.userData {
		return out
	}
	// Dyeing a shulker box keeps its contents and name: copy the data of the input that is the same
	// kind of item as the output onto it.
	type decoder interface{ DecodeNBT(map[string]any) any }
	dec, ok := out.Item().(decoder)
	for _, c := range g.cells {
		if c.Empty() {
			continue
		}
		src, isNBT := c.Item().(world.NBTer)
		if !isNBT || !ok {
			continue
		}
		if _, sameKind := c.Item().(decoder); !sameKind {
			continue
		}
		it, ok := dec.DecodeNBT(src.EncodeNBT()).(world.Item)
		if !ok {
			continue
		}
		return c.Grow(out.Count() - c.Count()).WithItem(it)
	}
	return out
}

// craftRemainder is what is left in a grid slot after crafting with it (vanilla
// Item.craftRemainder): buckets and bottles come back empty.
func craftRemainder(it item.Stack) item.Stack {
	switch v := it.Item().(type) {
	case item.Bucket:
		if !v.Empty() {
			return item.NewStack(item.Bucket{}, 1)
		}
	case item.HoneyBottle, item.DragonBreath:
		return item.NewStack(item.GlassBottle{}, 1)
	}
	return item.Stack{}
}

// sameKindStacks reports whether two stacks are the same item (vanilla ItemStack.isSameItem).
func sameKindStacks(a, b item.Stack) bool {
	if a.Empty() || b.Empty() {
		return a.Empty() == b.Empty()
	}
	n1, m1 := a.Item().EncodeItem()
	n2, m2 := b.Item().EncodeItem()
	return n1 == n2 && m1 == m2
}

// onceRecipes is sync.OnceValue for tables built from Dragonfly's recipes, which are registered
// only when a server is made: a table built before that (empty) is built again on the next call.
func onceRecipes[T any](build func() T) func() T {
	var (
		mu   sync.Mutex
		v    T
		done atomic.Bool
	)
	return func() T {
		if done.Load() {
			return v
		}
		mu.Lock()
		defer mu.Unlock()
		if !done.Load() {
			v = build()
			if len(recipe.Recipes()) > 0 {
				done.Store(true)
			}
		}
		return v
	}
}
