package javasession

import (
	"math"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/java/javamap"
	jitem "github.com/df-mc/dragonfly/server/java/protocol/item"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// Entities drawn for Java players as a model of item displays, the way BetterModel draws a
// Blockbench model on Paper: the entity's Java stand-in (RegisterEntityAlias) is made invisible,
// and each part of the model is an item_display showing one item model of the server's Java
// resource pack. The displays move and turn with the entity (the same move packets, so the
// client interpolates them alike); a part's place and turn relative to the entity are set once.

// ModelPart is one part of an EntityModel.
type ModelPart struct {
	// ItemModel is the minecraft:item_model the part shows, e.g. "bettermodel:model/gubby_head":
	// a model whose (8, 8, 8) is the part's pivot.
	ItemModel string
	// Offset is where the part's pivot is relative to the entity's position, in blocks, for an
	// entity facing south (yaw 0, +z).
	Offset mgl64.Vec3
}

// EntityModel is the model a Java player sees instead of an entity's stand-in.
type EntityModel struct {
	Parts []ModelPart
	// Yaw turns every part about its pivot, in degrees (counter-clockwise seen from above, +x
	// towards -z): the model's root rotation.
	Yaw float64
}

var (
	entityModelsMu sync.RWMutex
	entityModels   = map[string]func(world.Entity) *EntityModel{}
)

// RegisterEntityModel draws the entities with the Bedrock identifier bedrock as the model pick
// returns for each (nil: the stand-in as it is). The entity needs a Java stand-in.
func RegisterEntityModel(bedrock string, pick func(world.Entity) *EntityModel) {
	entityModelsMu.Lock()
	defer entityModelsMu.Unlock()
	entityModels[bedrock] = pick
}

func entityModelFor(e world.Entity) *EntityModel {
	name := e.H().Type().EncodeEntity()
	entityModelsMu.RLock()
	pick := entityModels[name]
	entityModelsMu.RUnlock()
	if pick == nil {
		return nil
	}
	return pick(e)
}

// Entity data of the displays (26.2 and 26.3 ids): Entity.DATA_SHARED_FLAGS_ID, Display's
// interpolation durations, translation and left rotation, and ItemDisplay's item.
const (
	dataSharedFlags         = 0
	flagInvisible           = 0x20
	dataDisplayTransformDur = 9
	dataDisplayPosRotDur    = 10
	dataDisplayTranslation  = 11
	dataDisplayLeftRotation = 13
	dataItemDisplayItem     = 23
	dataTypeItemStack       = 7
	dataTypeVector3         = 39
	dataTypeQuaternion      = 40
)

// viewModel draws e (Java id id) as its model, if it has one.
func (s *Session) viewModel(e world.Entity, id int32, pos mgl64.Vec3, rot cube.Rotation) {
	m := entityModelFor(e)
	if m == nil || len(m.Parts) == 0 {
		return
	}
	typ := s.ver.BuiltinID("minecraft:entity_type", "minecraft:item_display")
	base, ok := javamap.ItemID("minecraft:paper", 0)
	if typ < 0 || !ok {
		return
	}
	w := s.packet()
	w.VarInt(id)
	w.Byte(dataSharedFlags)
	w.VarInt(dataTypeByte)
	w.Byte(flagInvisible)
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)

	// ItemDisplayRenderer turns the item half a turn about Y before drawing it (so items face
	// the viewer, as in item frames): undone here, so the model faces the way Yaw says.
	q := yawQuat(m.Yaw + 180)
	partRot := cube.Rotation{rot.Yaw(), 0}
	ids := make([]int32, 0, len(m.Parts))
	for _, p := range m.Parts {
		pid := s.clientEntityID()
		ids = append(ids, pid)
		w := s.packet()
		w.VarInt(pid)
		w.UUID(uuid.New())
		w.VarInt(typ)
		w.Float64(pos[0])
		w.Float64(pos[1])
		w.Float64(pos[2])
		w.LpVec3(0, 0, 0)
		w.Angle(0)
		w.Angle(float32(partRot.Yaw()))
		w.Angle(float32(partRot.Yaw()))
		w.VarInt(0)
		s.queue(v777.ClientboundPlayAddEntity, w)
		s.setTrack(pid, pos, partRot)

		js := jitem.Stack{Count: 1, ID: base, ItemModel: p.ItemModel}
		js.Add(jitem.CompItemModel)
		w = s.packet()
		w.VarInt(pid)
		w.Byte(dataDisplayPosRotDur)
		w.VarInt(dataTypeInt)
		w.VarInt(3) // moves and turns are interpolated over 3 ticks, like the stand-in's
		w.Byte(dataDisplayTranslation)
		w.VarInt(dataTypeVector3)
		w.Float32(float32(p.Offset[0]))
		w.Float32(float32(p.Offset[1]))
		w.Float32(float32(p.Offset[2]))
		w.Byte(dataDisplayLeftRotation)
		w.VarInt(dataTypeQuaternion)
		w.Float32(float32(q[0]))
		w.Float32(float32(q[1]))
		w.Float32(float32(q[2]))
		w.Float32(float32(q[3]))
		w.Byte(dataItemDisplayItem)
		w.VarInt(dataTypeItemStack)
		js.EncodeFor(w, s.items().proto)
		w.Byte(0xff)
		s.queue(v777.ClientboundPlaySetEntityData, w)
	}
	s.entMu.Lock()
	s.modelParts[id] = ids
	s.entMu.Unlock()
}

// yawQuat is the rotation by deg degrees about +Y as a quaternion (x, y, z, w).
func yawQuat(deg float64) [4]float64 {
	h := deg * math.Pi / 360
	return [4]float64{0, math.Sin(h), 0, math.Cos(h)}
}

// clientEntityID is a new Java entity id for an entity only the client has (a model part).
func (s *Session) clientEntityID() int32 {
	s.entMu.Lock()
	defer s.entMu.Unlock()
	s.nextEntityID++
	if s.nextEntityID == selfEntityID {
		s.nextEntityID++
	}
	return s.nextEntityID
}

// moveModel moves the parts of the model of the entity with Java id id along with it.
func (s *Session) moveModel(id int32, pos mgl64.Vec3, rot cube.Rotation, onGround bool) {
	s.entMu.Lock()
	ids := s.modelParts[id]
	s.entMu.Unlock()
	for _, pid := range ids {
		s.syncEntity(pid, pos, cube.Rotation{rot.Yaw(), 0}, onGround)
	}
}

// hideModel removes the parts of the model of the entity with Java id id.
func (s *Session) hideModel(id int32) {
	s.entMu.Lock()
	ids := s.modelParts[id]
	delete(s.modelParts, id)
	for _, pid := range ids {
		delete(s.tracks, pid)
	}
	s.entMu.Unlock()
	if len(ids) == 0 {
		return
	}
	w := s.packet()
	w.VarInt(int32(len(ids)))
	for _, pid := range ids {
		w.VarInt(pid)
	}
	s.queue(v777.ClientboundPlayRemoveEntities, w)
}
