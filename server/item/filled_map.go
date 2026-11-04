package item

// FilledMap is a map that shows a picture the server draws: the client asks for the pixels of
// map ID when it shows the map (session.SetMapPixels sets them). It shows nothing of the world.
type FilledMap struct {
	// ID is the map's id; maps with the same ID show the same picture.
	ID int64
}

// EncodeItem ...
func (FilledMap) EncodeItem() (name string, meta int16) {
	return "minecraft:filled_map", 0
}

// EncodeNBT ...
func (m FilledMap) EncodeNBT() map[string]any {
	return map[string]any{"map_uuid": m.ID, "map_is_init": uint8(1), "map_scale": int32(0)}
}

// DecodeNBT ...
func (m FilledMap) DecodeNBT(data map[string]any) any {
	m.ID, _ = data["map_uuid"].(int64)
	return m
}
