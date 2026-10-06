package javasession

import (
	"sync"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	jitem "github.com/ezchr/go-mcjava/item"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/go-gl/mathgl/mgl64"
)

// javaEntityName maps Bedrock entity ids whose Java name differs. Ids not listed use the same name.
var javaEntityName = map[string]string{
	"minecraft:xp_orb":                 "minecraft:experience_orb",
	"minecraft:xp_bottle":              "minecraft:experience_bottle",
	"minecraft:ender_crystal":          "minecraft:end_crystal",
	"minecraft:fireworks_rocket":       "minecraft:firework_rocket",
	"minecraft:thrown_trident":         "minecraft:trident",
	"minecraft:wind_charge_projectile": "minecraft:wind_charge",
	"minecraft:fishing_hook":           "minecraft:fishing_bobber",
	"minecraft:villager_v2":            "minecraft:villager",
	"minecraft:zombie_villager_v2":     "minecraft:zombie_villager",
	"minecraft:evocation_illager":      "minecraft:evoker",
	"minecraft:evocation_fang":         "minecraft:evoker_fangs",
	"minecraft:vindicator":             "minecraft:vindicator",
	"minecraft:zombie_pigman":          "minecraft:zombified_piglin",
	"minecraft:snow_golem":             "minecraft:snow_golem",
	"minecraft:leash_knot":             "minecraft:leash_knot",
	"minecraft:tripod_camera":          "",
	"minecraft:npc":                    "",
	"minecraft:agent":                  "",
	"minecraft:test_moving_ent":        "",
	"minecraft:cushion":                "minecraft:cushion", // 26.3; older versions have none
	"dragonfly:text":                   "minecraft:text_display",
}

// RegisterEntityAlias shows entities of a custom Bedrock type (a server's own entity, such as
// "zid:gubby") to Java players as a Java entity type ("minecraft:rabbit"). An empty java hides
// them. Call it before players join.
func RegisterEntityAlias(bedrock, java string) {
	javaEntityName[bedrock] = java
}

// networkEncoded is implemented by entities with a network id other than their save id.
type networkEncoded interface {
	NetworkEncodeEntity() string
}

// javaEntity is the Java entity type of e and the add_entity data field, or ok == false for
// entities Java has no equivalent of (they stay invisible to Java players).
func javaEntity(e world.Entity) (typ, data int32, ok bool) {
	name := e.H().Type().EncodeEntity()
	if n, ok := e.(networkEncoded); ok {
		name = n.NetworkEncodeEntity()
	}
	if j, listed := javaEntityName[name]; listed {
		if j == "" {
			return 0, 0, false
		}
		name = j
	}
	typ = v777.BuiltinID("minecraft:entity_type", name)
	if typ < 0 {
		return 0, 0, false
	}
	if ent, ok := e.(*entity.Ent); ok {
		if fb, ok := ent.Behaviour().(*entity.FallingBlockBehaviour); ok {
			// falling_block carries its block state in the data field.
			rid := world.BlockRuntimeID(fb.Block())
			if bi := blocks(); int(rid) < len(bi.java) {
				data = int32(bi.java[rid])
			}
		}
	}
	return typ, data, true
}

// skippedTypes are the entity types already logged as having no Java equivalent.
var skippedTypes sync.Map

// velocity of e, if it has one.
func velocity(e world.Entity) mgl64.Vec3 {
	if v, ok := e.(interface{ Velocity() mgl64.Vec3 }); ok {
		return v.Velocity()
	}
	return mgl64.Vec3{}
}

// viewOtherEntity spawns a non-player entity.
func (s *Session) viewOtherEntity(e world.Entity) {
	typ, data, ok := javaEntity(e)
	if ok {
		// The client's own ids: entity types and the falling block's state.
		if typ = s.ver.Builtin("minecraft:entity_type", typ); typ < 0 {
			ok = false
		} else if typ == s.ver.BuiltinID("minecraft:entity_type", "minecraft:falling_block") {
			data = s.ver.BlockState(data)
		}
	}
	if !ok {
		if _, seen := skippedTypes.LoadOrStore(e.H().Type().EncodeEntity(), true); !seen {
			s.log.Info("no Java entity for this type; Java players do not see it", "type", e.H().Type().EncodeEntity())
		}
		return
	}
	id := s.addEntityID(e)
	pos, rot, vel := e.Position(), e.Rotation(), velocity(e)
	u := e.H().UUID()
	w := s.packet()
	w.VarInt(id)
	w.UUID(u)
	w.VarInt(typ)
	w.Float64(pos[0])
	w.Float64(pos[1])
	w.Float64(pos[2])
	w.LpVec3(vel[0], vel[1], vel[2])
	w.Angle(float32(rot.Pitch()))
	w.Angle(float32(rot.Yaw()))
	w.Angle(float32(rot.Yaw()))
	w.VarInt(data)
	s.queue(v777.ClientboundPlayAddEntity, w)
	s.setTrack(id, pos, rot)
	s.viewEntityMeta(e, id)
	s.noteRiding(e)
}

// viewEntityMeta sends entity data a freshly spawned entity needs: an item entity's item.
func (s *Session) viewEntityMeta(e world.Entity, id int32) {
	if isTextEntity(e) {
		s.viewTextDisplay(e, id, true)
		return
	}
	if c, ok := e.(*entity.Cushion); ok {
		s.viewCushionColour(c, id)
		return
	}
	ent, ok := e.(*entity.Ent)
	if !ok {
		return
	}
	ib, ok := ent.Behaviour().(*entity.ItemBehaviour)
	if !ok {
		return
	}
	var js jitem.Stack
	javaStack(ib.Item(), &js)
	w := s.packet()
	w.VarInt(id)
	w.Byte(8)                        // ItemEntity.DATA_ITEM
	w.VarInt(7)                      // ITEM_STACK serializer
	js.EncodeFor(w, s.items().proto) // item and component ids of the client version
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)
}
