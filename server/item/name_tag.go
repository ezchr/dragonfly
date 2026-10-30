package item

import (
	"github.com/df-mc/dragonfly/server/world"
)

// NameTag is an item that names a mob or an armour stand after the name the
// tag itself was given.
type NameTag struct{}

// UseOnEntity gives the entity the tag's name, if the tag has one and the
// entity is a mob or an armour stand. Players cannot be named.
func (NameTag) UseOnEntity(e world.Entity, _ *world.Tx, user User, ctx *UseContext) bool {
	held, _ := user.HeldItems()
	name := held.CustomName()
	n, ok := e.(nameTagTarget)
	if name == "" || !ok || !nameTaggable(e) || n.NameTag() == name {
		return false
	}
	n.SetNameTag(name)
	if a, ok := e.(interface{ SetAlwaysShowNameTag(bool) }); ok {
		// a named mob shows its name when looked at
		a.SetAlwaysShowNameTag(false)
	}
	ctx.SubtractFromCount(1)
	return true
}

// nameTagTarget is an entity with a name tag that can be changed.
type nameTagTarget interface {
	NameTag() string
	SetNameTag(s string)
}

// nameTaggable reports whether a name tag can rename e: entities that say so,
// such as armour stands, and living entities other than players.
func nameTaggable(e world.Entity) bool {
	if t, ok := e.(interface{ NameTaggable() bool }); ok && t.NameTaggable() {
		return true
	}
	_, living := e.(interface{ Health() float64 })
	return living && e.H().Type().EncodeEntity() != "minecraft:player"
}

// EncodeItem ...
func (NameTag) EncodeItem() (name string, meta int16) {
	return "minecraft:name_tag", 0
}
