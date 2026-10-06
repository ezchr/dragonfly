package javasession

import (
	"slices"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	v777 "github.com/ezchr/go-mcjava/v777"
)

// Riding: Java clients learn who rides what from set_passengers (the vehicle's whole passenger
// list each time). The session keeps the lists itself, filled from mounts and dismounts and from
// the entities as they come into view, so it can send them without a world transaction.

// ViewEntityMount shows rider getting on rideable.
func (s *Session) ViewEntityMount(rider, rideable world.Entity, _ bool) {
	s.entMu.Lock()
	if s.riders == nil {
		s.riders = map[*world.EntityHandle][]*world.EntityHandle{}
	}
	if !slices.Contains(s.riders[rideable.H()], rider.H()) {
		s.riders[rideable.H()] = append(s.riders[rideable.H()], rider.H())
	}
	s.entMu.Unlock()
	s.sendPassengers(rideable.H())
}

// ViewEntityDismount shows rider getting off rideable.
func (s *Session) ViewEntityDismount(rider, rideable world.Entity) {
	s.entMu.Lock()
	list := slices.DeleteFunc(slices.Clone(s.riders[rideable.H()]), func(h *world.EntityHandle) bool { return h == rider.H() })
	if len(list) == 0 {
		delete(s.riders, rideable.H())
	} else {
		s.riders[rideable.H()] = list
	}
	s.entMu.Unlock()
	s.sendPassengers(rideable.H())
}

// noteRiding records what a newly shown entity rides or carries, then sends the passenger lists
// that involve it: a vehicle comes into view with its riders, or a rider after its vehicle.
func (s *Session) noteRiding(e world.Entity) {
	var vehicles []*world.EntityHandle
	if r, ok := e.(entity.Rideable); ok {
		var hs []*world.EntityHandle
		for _, seat := range r.Riders() {
			hs = append(hs, seat.Handle)
		}
		if len(hs) > 0 {
			s.entMu.Lock()
			if s.riders == nil {
				s.riders = map[*world.EntityHandle][]*world.EntityHandle{}
			}
			s.riders[e.H()] = hs
			s.entMu.Unlock()
			vehicles = append(vehicles, e.H())
		}
	}
	if p, ok := e.(*player.Player); ok {
		if v := p.RidingEntityHandle(); v != nil {
			s.entMu.Lock()
			if s.riders == nil {
				s.riders = map[*world.EntityHandle][]*world.EntityHandle{}
			}
			if !slices.Contains(s.riders[v], e.H()) {
				s.riders[v] = append(s.riders[v], e.H())
			}
			s.entMu.Unlock()
			vehicles = append(vehicles, v)
		}
	}
	for _, v := range vehicles {
		s.sendPassengers(v)
	}
}

// forgetRiding drops a hidden entity's passenger list.
func (s *Session) forgetRiding(h *world.EntityHandle) {
	s.entMu.Lock()
	delete(s.riders, h)
	s.entMu.Unlock()
}

// sendPassengers sends a vehicle's passengers, if the client has the vehicle. Riders the client
// does not have yet are left out; they are sent again when they come into view.
func (s *Session) sendPassengers(vehicle *world.EntityHandle) {
	s.entMu.Lock()
	vid, ok := s.entityIDs[vehicle]
	if !ok {
		s.entMu.Unlock()
		return
	}
	var ids []int32
	for _, h := range s.riders[vehicle] {
		if h == s.ent {
			ids = append(ids, selfEntityID)
		} else if id, ok := s.entityIDs[h]; ok {
			ids = append(ids, id)
		}
	}
	s.entMu.Unlock()
	w := s.packet()
	w.VarInt(vid)
	w.VarInt(int32(len(ids)))
	for _, id := range ids {
		w.VarInt(id)
	}
	s.queue(v777.ClientboundPlaySetPassengers, w)
}

// Entity data of a cushion: its colour (Cushion.DATA_COLOR, a DYE_COLOR).
const (
	cushionDataColour = 8
	dataTypeDyeColour = 43
)

// viewCushionColour sends the colour of a cushion that came into view.
func (s *Session) viewCushionColour(c *entity.Cushion, id int32) {
	colour := slices.Index(item.Colours(), c.Colour())
	if colour < 0 {
		return
	}
	w := s.packet()
	w.VarInt(id)
	w.Byte(cushionDataColour)
	w.VarInt(dataTypeDyeColour)
	w.VarInt(int32(colour)) // Java DyeColor ids run white..black like Dragonfly's
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)
}
