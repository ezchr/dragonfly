package javasession

import (
	"github.com/df-mc/dragonfly/server/entity"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
)

// Armour stands: Bedrock draws one of 13 poses picked by an index, Java takes a rotation for the
// head, body, arms and legs. The angles are those of Bedrock's pose animations, which Java reads
// the same way (Bedrock's default pose is Java's default pose).
const (
	dataArmourStandFlags = 15 // ArmorStand.DATA_CLIENT_FLAGS
	dataArmourStandHead  = 16 // then body, left arm, right arm, left leg, right leg
	dataTypeRotations    = 9

	armourStandShowArms       = 0x04 // Bedrock armour stands always have arms
	entityEventArmourStandHit = 32
)

// armourStandPoses holds the head, body, left arm, right arm, left leg and right leg rotations of
// each Bedrock pose: default, none, solemn, athena, brandish, honor, entertain, salute, riposte,
// zombie, cancan A, cancan B and hero.
var armourStandPoses = [13][6][3]float32{
	{{0, 0, 0}, {0, 0, 0}, {-10, 0, -10}, {-15, 0, 10}, {-1, 0, -1}, {1, 0, 1}},
	{{0, 0, 0}, {0, 0, 0}, {0, 0, 0}, {0, 0, 0}, {0, 0, 0}, {0, 0, 0}},
	{{15, 0, 0}, {0, 0, 2}, {-30, 15, 15}, {-60, -20, -10}, {-1, 0, -1}, {1, 0, 1}},
	{{-5, 0, 0}, {0, 0, 2}, {10, 0, -5}, {-60, 20, -10}, {-3, -3, -3}, {3, 3, 3}},
	{{-15, 0, 0}, {0, 0, -2}, {20, 0, -10}, {-110, 50, 0}, {5, -3, -3}, {-5, 3, 3}},
	{{-15, 0, 0}, {0, 0, 0}, {-110, 35, 0}, {-110, -35, 0}, {5, -3, -3}, {-5, 3, 3}},
	{{-15, 0, 0}, {0, 0, 0}, {-110, -35, 0}, {-110, 35, 0}, {5, -3, -3}, {-5, 3, 3}},
	{{0, 0, 0}, {0, 0, 0}, {10, 0, -5}, {-70, -40, 0}, {-1, 0, -1}, {1, 0, 1}},
	{{16, 20, 0}, {0, 0, 0}, {4, 8, 237}, {246, 0, 89}, {-14, -18, -16}, {8, 20, 4}},
	{{-10, 0, -5}, {0, 0, 0}, {-105, 0, 0}, {-100, 0, 0}, {7, 0, 0}, {-46, 0, 0}},
	{{-5, 18, 0}, {0, 22, 0}, {8, 0, -114}, {0, 84, 111}, {-111, 55, 0}, {0, 23, -13}},
	{{-10, -20, 0}, {0, -18, 0}, {0, 0, -112}, {8, 90, 111}, {0, 0, 13}, {-119, -42, 0}},
	{{-4, 67, 0}, {0, 8, 0}, {16, 32, -8}, {-99, 63, 0}, {0, -75, -8}, {4, 63, 8}},
}

// armourStand is what the armour stand's behaviour tells viewers.
type armourStand interface {
	PoseIndex() int
	HurtTicks() int
}

// viewArmourStand sends an armour stand's arms and pose, and its wobble right after a hit.
func (s *Session) viewArmourStand(e world.Entity, id int32) {
	if e.H().Type() != entity.ArmourStandType {
		return
	}
	ent, ok := e.(interface{ Behaviour() entity.Behaviour })
	if !ok {
		return
	}
	a, ok := ent.Behaviour().(armourStand)
	if !ok {
		return
	}
	pose := armourStandPoses[0]
	if i := a.PoseIndex(); i >= 0 && i < len(armourStandPoses) {
		pose = armourStandPoses[i]
	}
	w := s.packet()
	w.VarInt(id)
	w.Byte(dataArmourStandFlags)
	w.VarInt(dataTypeByte)
	w.Byte(armourStandShowArms)
	for i, r := range pose {
		w.Byte(byte(dataArmourStandHead + i))
		w.VarInt(dataTypeRotations)
		w.Float32(r[0])
		w.Float32(r[1])
		w.Float32(r[2])
	}
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)

	if a.HurtTicks() == 9 {
		// a fresh hit: the client wobbles the stand and plays the hit sound
		w := s.packet()
		w.Int32(id)
		w.Byte(entityEventArmourStandHit)
		s.queue(v777.ClientboundPlayEntityEvent, w)
	}
}
