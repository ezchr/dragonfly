package entity

import (
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Behaviour implements the behaviour of an Ent.
type Behaviour interface {
	// Tick ticks the Ent using the Behaviour. A Movement is returned that
	// specifies the movement of the entity over the tick. Nil may be returned
	// if the entity did not move.
	Tick(e *Ent, tx *world.Tx) *Movement
}

// Ent is a world.Entity implementation that allows entity implementations to
// share a lot of code. It is currently under development and is prone to
// (breaking) changes.
type Ent struct {
	tx                *world.Tx
	handle            *world.EntityHandle
	data              *world.EntityData
	deferPortalTravel bool
	once              sync.Once
}

// Open converts a world.EntityHandle to an Ent in a world.Tx.
func Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) *Ent {
	return &Ent{tx: tx, handle: handle, data: data}
}

func (e *Ent) H() *world.EntityHandle {
	return e.handle
}

func (e *Ent) Behaviour() Behaviour {
	return e.data.Data.(Behaviour)
}

// ent allows entity types embedding an Ent to be handled like one.
func (e *Ent) ent() *Ent {
	return e
}

// ProjectileOwner returns the entity that owns this Ent, if its behaviour exposes an owner.
func (e *Ent) ProjectileOwner() *world.EntityHandle {
	if owned, ok := e.Behaviour().(interface{ Owner() *world.EntityHandle }); ok {
		return owned.Owner()
	}
	return nil
}

// Explode propagates the explosion behaviour of the underlying Behaviour.
func (e *Ent) Explode(src world.ExplosionSource, impact float64) {
	if expl, ok := e.Behaviour().(interface {
		Explode(e *Ent, src world.ExplosionSource, impact float64)
	}); ok {
		expl.Explode(e, src, impact)
	}
}

// Position returns the current position of the entity.
func (e *Ent) Position() mgl64.Vec3 {
	return e.data.Pos
}

// Velocity returns the current velocity of the entity. The values in the Vec3 returned represent the speed on
// that axis in blocks/tick.
func (e *Ent) Velocity() mgl64.Vec3 {
	return e.data.Vel
}

// SetVelocity sets the velocity of the entity. The values in the Vec3 passed represent the speed on
// that axis in blocks/tick.
func (e *Ent) SetVelocity(v mgl64.Vec3) {
	e.data.Vel = v
}

// heldItemsBehaviour may be implemented by a Behaviour to make its Ent satisfy item.Carrier - a
// generic hook so any custom entity (e.g. one displaying a held/equipped item, such as a real
// minecraft:sulfur_cube showing an absorbed block) can opt in without Ent needing to know about
// that entity's own type. Entities whose Behaviour does not implement this are simply not
// item.Carriers, matching the previous behaviour exactly (nothing implemented HeldItems on Ent
// before this).
type heldItemsBehaviour interface {
	HeldItems() (mainHand, offHand item.Stack)
}

// HeldItems returns the items currently held by the entity, if its Behaviour implements
// heldItemsBehaviour. Both stacks are empty otherwise.
func (e *Ent) HeldItems() (mainHand, offHand item.Stack) {
	if c, ok := e.Behaviour().(heldItemsBehaviour); ok {
		return c.HeldItems()
	}
	return item.Stack{}, item.Stack{}
}

// invisibleBehaviour may be implemented by a Behaviour to make its Ent report itself invisible to
// viewers, the same generic-hook pattern as heldItemsBehaviour just above (see its doc comment) -
// session's own invisible-entity check (server/session/entity_metadata.go) already accepts any
// world.Entity implementing Invisible() bool, previously only ever satisfied by *player.Player.
// This lets a non-player entity (e.g. a block-hunt disguise's own sulfur-cube body, which should
// never render - only the block it displays should) opt into the same mechanism.
type invisibleBehaviour interface {
	Invisible() bool
}

// Invisible reports whether the entity should be hidden from viewers, if its Behaviour implements
// invisibleBehaviour. False otherwise - i.e. entities whose Behaviour does not opt in render
// exactly as before this was added.
func (e *Ent) Invisible() bool {
	if i, ok := e.Behaviour().(invisibleBehaviour); ok {
		return i.Invisible()
	}
	return false
}

// Teleport teleports the entity to the position given.
func (e *Ent) Teleport(pos mgl64.Vec3) {
	viewers := e.tx.Viewers(e.data.Pos)
	e.data.Pos = pos
	for _, v := range viewers {
		v.ViewEntityTeleport(e, pos)
	}
}

// Rotation returns the rotation of the entity.
func (e *Ent) Rotation() cube.Rotation {
	return e.data.Rot
}

// Age returns the total time lived of this entity. It increases by
// time.Second/20 for every time Tick is called.
func (e *Ent) Age() time.Duration {
	return e.data.Age
}

// OnFireDuration ...
func (e *Ent) OnFireDuration() time.Duration {
	return e.data.FireDuration
}

// SetOnFire ...
func (e *Ent) SetOnFire(duration time.Duration) {
	duration = max(duration, 0)
	stateChanged := (e.data.FireDuration > 0) != (duration > 0)

	e.data.FireDuration = duration
	if stateChanged {
		e.updateState()
	}
}

// Extinguish ...
func (e *Ent) Extinguish() {
	e.SetOnFire(0)
}

// NameTag returns the name tag of the entity. An empty string is returned if
// no name tag was set.
func (e *Ent) NameTag() string {
	return e.data.Name
}

// SetNameTag changes the name tag of an entity. The name tag is removed if an
// empty string is passed.
func (e *Ent) SetNameTag(s string) {
	e.data.Name = s
	e.updateState()
}

// AlwaysShowNameTag returns whether the name tag of the entity is shown at all
// distances instead of only when the entity is looked at from up close.
func (e *Ent) AlwaysShowNameTag() bool {
	return e.data.AlwaysShowNameTag
}

// SetAlwaysShowNameTag changes whether the name tag of the entity is shown at
// all distances instead of only when the entity is looked at from up close.
func (e *Ent) SetAlwaysShowNameTag(alwaysShow bool) {
	e.data.AlwaysShowNameTag = alwaysShow
	e.updateState()
}

// updateState updates the state of the entity for all viewers of the entity.
func (e *Ent) updateState() {
	for _, v := range e.tx.Viewers(e.data.Pos) {
		v.ViewEntityState(e)
	}
}

// Tick ticks Ent, progressing its lifetime and closing the entity if it is
// in the void.
func (e *Ent) Tick(tx *world.Tx, current int64) {
	e.deferPortalTravel = true
	defer func() {
		e.deferPortalTravel = false
	}()

	y := e.data.Pos[1]
	if y < float64(tx.Range()[0]) && current%10 == 0 {
		_ = e.Close()
		return
	}
	e.SetOnFire(e.OnFireDuration() - time.Second/20)

	m := e.Behaviour().Tick(e, tx)
	if _, ok := e.handle.Entity(tx); !ok {
		// Removed while ticking, such as a picked up item, so viewers no longer know the entity.
		return
	}
	if e.finishPendingPortalTravel(tx) {
		return
	}
	if m != nil {
		m.Send()
	}
	if e.checkPortalInsiders() && e.finishPendingPortalTravel(tx) {
		return
	}
	e.stopPortalContact()
	e.data.Age += time.Second / 20
}

// Close closes the Ent and removes the associated entity from the world.
func (e *Ent) Close() error {
	e.once.Do(func() {
		e.tx.RemoveEntity(e)
		_ = e.handle.Close()
	})
	return nil
}

// TravelThroughPortal handles the entity touching a portal block.
func (e *Ent) TravelThroughPortal(tx *world.Tx, target world.Dimension) {
	if tc := e.portalTravelComputer(); tc != nil {
		if e.deferPortalTravel {
			tc.queuePortalTravel(tx, target)
			return
		}
		tc.EnterPortal(e, tx, target)
	}
}

// portalTravelComputer returns the behaviour's portal travel state, if any.
func (e *Ent) portalTravelComputer() *PortalTravelComputer {
	if b, ok := e.Behaviour().(portalTravelComputerProvider); ok {
		return b.PortalTravelComputer()
	}
	return nil
}

// stopPortalContact resets portal contact state when no portal was touched.
func (e *Ent) stopPortalContact() {
	if tc := e.portalTravelComputer(); tc != nil {
		tc.StopPortalContact()
	}
}

// pendingPortalTravel reports whether this tick queued terminal portal travel.
func (e *Ent) pendingPortalTravel() bool {
	if tc := e.portalTravelComputer(); tc != nil {
		return tc.hasPendingPortalTravel()
	}
	return false
}

// finishPendingPortalTravel starts queued terminal portal travel, if present.
func (e *Ent) finishPendingPortalTravel(tx *world.Tx) bool {
	if tc := e.portalTravelComputer(); tc != nil {
		return tc.finishPendingPortalTravel(e, tx)
	}
	return false
}

type portalBlock interface {
	Portal() world.Dimension
}

// checkPortalInsiders checks whether the entity is inside portal blocks.
// Other EntityInsider blocks are intentionally left to entity physics.
func (e *Ent) checkPortalInsiders() bool {
	box := e.H().Type().BBox(e).Translate(e.Position()).Grow(-0.0001)
	low, high := cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max())

	for blockPos := range cube.Range3D(low, high) {
		if p, ok := e.tx.Block(blockPos).(portalBlock); ok {
			e.TravelThroughPortal(e.tx, p.Portal())
			if e.pendingPortalTravel() {
				return true
			}
		}
	}
	return false
}
