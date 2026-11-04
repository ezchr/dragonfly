package javasession

import (
	"image/color"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/java/javamap"
	jitem "github.com/df-mc/dragonfly/server/java/protocol/item"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/java/protocol/wire"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// Item frames are blocks in Dragonfly and on Bedrock but entities on Java: a Java player gets an
// item_frame entity for every item frame block it is sent, showing the frame's item. A map in one
// (item.FilledMap) shows the picture session.SetMapPixels gave it, in Java's map colours.

// Entity data of an item frame (ItemFrame.DATA_ITEM and DATA_ROTATION): 8 is the frame's
// direction (HangingEntity.DATA_DIRECTION), which the spawn packet sets.
const (
	dataItemFrameItem     = 9
	dataItemFrameRotation = 10
)

// frameState is a session's item frame entities and the maps it was sent.
type frameState struct {
	mu       sync.Mutex
	entities map[cube.Pos]int32 // frame block -> its entity
	maps     map[int64]bool     // map ids whose picture the client has
}

// showFrames shows the item frames among a chunk's block entities.
func (s *Session) showFrames(blocks map[cube.Pos]world.Block) {
	for pos, b := range blocks {
		if f, ok := b.(block.ItemFrame); ok {
			s.showFrame(pos, f)
		}
	}
}

// updateFrame follows a block change at pos: a frame there gets an entity, a frame that went
// loses its own.
func (s *Session) updateFrame(pos cube.Pos, b world.Block) {
	if f, ok := b.(block.ItemFrame); ok {
		s.showFrame(pos, f)
		return
	}
	s.hideFrames(func(p cube.Pos) bool { return p == pos })
}

// hideChunkFrames removes the frame entities of a chunk the client forgets.
func (s *Session) hideChunkFrames(c world.ChunkPos) {
	s.hideFrames(func(p cube.Pos) bool { return p[0]>>4 == int(c[0]) && p[2]>>4 == int(c[1]) })
}

func (s *Session) hideFrames(match func(cube.Pos) bool) {
	s.frames.mu.Lock()
	var ids []int32
	for p, id := range s.frames.entities {
		if match(p) {
			ids = append(ids, id)
			delete(s.frames.entities, p)
		}
	}
	s.frames.mu.Unlock()
	if len(ids) == 0 {
		return
	}
	w := s.packet()
	w.VarInt(int32(len(ids)))
	for _, id := range ids {
		w.VarInt(id)
	}
	s.queue(v777.ClientboundPlayRemoveEntities, w)
}

func (s *Session) showFrame(pos cube.Pos, f block.ItemFrame) {
	s.hideFrames(func(p cube.Pos) bool { return p == pos })
	name := "minecraft:item_frame"
	if f.Glowing {
		name = "minecraft:glow_item_frame"
	}
	typ := s.ver.BuiltinID("minecraft:entity_type", name)
	if typ < 0 {
		return
	}
	id := s.clientEntityID()
	s.frames.mu.Lock()
	if s.frames.entities == nil {
		s.frames.entities, s.frames.maps = map[cube.Pos]int32{}, map[int64]bool{}
	}
	s.frames.entities[pos] = id
	s.frames.mu.Unlock()

	c := pos.Vec3Centre()
	w := s.packet()
	w.VarInt(id)
	w.UUID(uuid.New())
	w.VarInt(typ)
	w.Float64(c[0])
	w.Float64(c[1])
	w.Float64(c[2])
	w.LpVec3(0, 0, 0)
	w.Angle(0)
	w.Angle(0)
	w.Angle(0)
	// The frame's direction (Direction.get3DDataValue) is the way it faces out of its block.
	w.VarInt(int32(javaDirection(f.Facing.Opposite())))
	s.queue(v777.ClientboundPlayAddEntity, w)

	if f.Item.Empty() {
		return
	}
	w = s.packet()
	w.VarInt(id)
	w.Byte(dataItemFrameItem)
	w.VarInt(dataTypeItemStack)
	s.writeFrameItem(w, f.Item)
	w.Byte(dataItemFrameRotation)
	w.VarInt(dataTypeInt)
	w.VarInt(int32(f.Rotations))
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)
}

// writeFrameItem writes the Java item a frame shows for it: a map is sent its picture first.
func (s *Session) writeFrameItem(w *wire.Writer, it item.Stack) {
	m, ok := it.Item().(item.FilledMap)
	if !ok {
		s.writeStack(w, it)
		return
	}
	base, ok := javamap.ItemID("minecraft:filled_map", 0)
	if !ok {
		jitem.WriteEmpty(w)
		return
	}
	s.sendMap(m.ID, false)
	js := jitem.Stack{Count: 1, ID: base, Raw: []jitem.RawComponent{{Type: jitem.CompMapId, Data: wire.AppendVarInt(nil, javaMapID(m.ID))}}}
	js.EncodeFor(w, s.items().proto)
}

// sendMap sends the picture of map id, if the client does not have it yet or again is set.
func (s *Session) sendMap(id int64, again bool) {
	s.frames.mu.Lock()
	if s.frames.maps == nil {
		s.frames.maps = map[int64]bool{}
	}
	had := s.frames.maps[id]
	s.frames.maps[id] = true
	s.frames.mu.Unlock()
	if had && !again {
		return
	}
	pixels, ok := session.MapPixels(id)
	if !ok {
		return
	}
	w := s.packet()
	w.VarInt(javaMapID(id))
	w.Byte(0)     // scale
	w.Bool(false) // locked
	w.Bool(false) // no decorations
	w.Byte(128)   // columns
	w.Byte(128)   // rows
	w.Byte(0)     // x
	w.Byte(0)     // z
	w.VarInt(128 * 128)
	for _, p := range pixels {
		w.Byte(javaMapColour(p))
	}
	s.queue(v777.ClientboundPlayMapItemData, w)
}

func init() {
	// A changed picture goes to every Java player who was sent the map.
	session.OnMapChange(func(id int64) {
		javaSessions.Range(func(_, v any) bool {
			s := v.(*Session)
			s.frames.mu.Lock()
			had := s.frames.maps[id]
			s.frames.mu.Unlock()
			if had {
				s.sendMap(id, true)
			}
			return true
		})
	})
}

// javaDirection is d's Direction.get3DDataValue: down 0, up 1, north 2, south 3, west 4, east 5.
func javaDirection(f cube.Face) int {
	switch f {
	case cube.FaceDown:
		return 0
	case cube.FaceUp:
		return 1
	case cube.FaceNorth:
		return 2
	case cube.FaceSouth:
		return 3
	case cube.FaceWest:
		return 4
	}
	return 5
}

var (
	javaMapsMu sync.Mutex
	javaMaps   = map[int64]int32{}
)

// javaMapID numbers the maps for Java, whose map ids are a VarInt.
func javaMapID(id int64) int32 {
	javaMapsMu.Lock()
	defer javaMapsMu.Unlock()
	jid, ok := javaMaps[id]
	if !ok {
		jid = int32(len(javaMaps) + 1)
		javaMaps[id] = jid
	}
	return jid
}

var (
	mapColourMu    sync.Mutex
	mapColourCache = map[color.RGBA]byte{}
)

// javaMapColour is the Java map colour closest to c; a see-through pixel is transparent.
func javaMapColour(c color.RGBA) byte {
	if c.A < 0x80 {
		return 0
	}
	mapColourMu.Lock()
	defer mapColourMu.Unlock()
	if v, ok := mapColourCache[c]; ok {
		return v
	}
	best, bestD := byte(0), -1
	for id := 1; id < len(mapBaseColours); id++ {
		base := mapBaseColours[id]
		if base == [3]uint8{} {
			continue
		}
		for shade, mul := range [4]int{180, 220, 255, 135} {
			r, g, b := int(base[0])*mul/255, int(base[1])*mul/255, int(base[2])*mul/255
			dr, dg, db := r-int(c.R), g-int(c.G), b-int(c.B)
			d := 2*dr*dr + 4*dg*dg + 3*db*db
			if bestD < 0 || d < bestD {
				best, bestD = byte(id*4+shade), d
			}
		}
	}
	mapColourCache[c] = best
	return best
}
