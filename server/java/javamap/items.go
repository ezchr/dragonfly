package javamap

import "github.com/df-mc/dragonfly/server/world"

type itemKey struct {
	name string
	meta int16
}

// ItemID returns the Java 26.3 item protocol id for a Bedrock item (name, meta) as Dragonfly encodes it. ok is
// false when the item is unknown; id is then the barrier item.
//
// Meta only selects the Java item for a few Bedrock items (banners, beds, ...). For the rest (potions, tools with
// damage, ...) the meta is carried by Java data components, not the item id.
func ItemID(name string, meta int16) (id int32, ok bool) {
	if id, ok := itemByMeta[itemKey{name, meta}]; ok {
		return id, true
	}
	if id, ok := itemByName[name]; ok {
		return id, true
	}
	return JavaItemBarrier, false
}

// Item returns the Java item protocol id of a Dragonfly item (barrier when unknown).
func Item(it world.Item) int32 {
	id, _ := ItemID(it.EncodeItem())
	return id
}
