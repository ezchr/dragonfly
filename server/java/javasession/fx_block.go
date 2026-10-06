package javasession

import (
	"strconv"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/ezchr/go-mcjava/text"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/google/uuid"
)

// ViewBlockAction animates a block: chest/ender chest/shulker box lids (block_event), the crack
// overlay of a block someone else is breaking (block_destruction stages, which Java has the
// server send, where Bedrock animates from a speed) and decorated pot wobbles.
func (s *Session) ViewBlockAction(pos cube.Pos, a world.BlockAction) {
	switch t := a.(type) {
	case block.OpenAction:
		s.blockEvent(pos, 1, 1)
	case block.CloseAction:
		s.blockEvent(pos, 1, 0)
	case block.StartCrackAction:
		s.startCrack(pos, t.BreakTime)
	case block.ContinueCrackAction:
		s.continueCrack(pos, t.BreakTime)
	case block.StopCrackAction:
		s.stopCrack(pos)
	case block.DecoratedPotWobbleAction:
		style := byte(1) // DecoratedPotBlockEntity.WobbleStyle: 0 positive, 1 negative
		if t.Success {
			style = 0
		}
		s.blockEventFor(pos, decoratedPotJava, 1, style)
	}
}

// blockEvent sends a block_event for the block the client has at pos (the client ignores events
// whose block does not match its own).
func (s *Session) blockEvent(pos cube.Pos, b0, b1 byte) {
	st, ok := s.javaStateAt(pos)
	if !ok {
		return
	}
	s.blockEventFor(pos, javaBlockOf(st), b0, b1)
}

func (s *Session) blockEventFor(pos cube.Pos, javaBlock int32, b0, b1 byte) {
	if javaBlock = s.blockID(javaBlock); javaBlock < 0 {
		return
	}
	w := s.packet()
	w.Position(pos[0], pos[1], pos[2])
	w.Byte(b0)
	w.Byte(b1)
	w.VarInt(javaBlock)
	s.queue(v777.ClientboundPlayBlockEvent, w)
}

// javaStateAt is the Java state of the block at pos in the chunks this client has. Viewer methods
// run in the world's transaction, so reading the chunk is safe.
func (s *Session) javaStateAt(pos cube.Pos) (int32, bool) {
	if s.loader == nil {
		return 0, false
	}
	col, ok := s.loader.Chunk(world.ChunkPos{int32(pos[0] >> 4), int32(pos[2] >> 4)})
	if !ok || col.Chunk == nil {
		return 0, false
	}
	if r := col.Chunk.Range(); pos[1] < r[0] || pos[1] > r[1] {
		return 0, false
	}
	rid := col.Chunk.Block(uint8(pos[0]&15), int16(pos[1]), uint8(pos[2]&15), 0)
	bi := blocks()
	if int(rid) >= len(bi.java) {
		return 0, false
	}
	return int32(bi.java[rid]), true
}

// crack is a crack overlay the session animates for a block another player breaks.
type crack struct {
	id    int32 // block_destruction id: one per position, negative so it never is an entity's
	start time.Time
	total time.Duration
	stage int32
	timer *time.Timer // guarded by fxState.mu, like every field
	// gen counts the timer chains started for this crack. A timer callback only continues if its
	// chain is still the current one: one that was already waiting for the lock when the crack
	// was restarted must not start a second chain.
	gen uint64
}

func crackID(pos cube.Pos) int32 {
	h := uint32(pos[0])*73856093 ^ uint32(pos[1])*19349663 ^ uint32(pos[2])*83492791
	return -1 - int32(h&0x3fffffff)
}

func (s *Session) startCrack(pos cube.Pos, total time.Duration) {
	if total <= 0 || s.selfMining(pos) {
		return // instant, or the client draws its own
	}
	st := s.fxMake()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.cracks == nil {
		st.cracks = map[cube.Pos]*crack{}
	}
	c := st.cracks[pos]
	if c == nil {
		c = &crack{id: crackID(pos), stage: -1}
		st.cracks[pos] = c
	} else if c.timer != nil {
		c.timer.Stop()
	}
	c.start, c.total = time.Now(), total
	c.gen++
	s.crackTick(st, pos, c, c.gen)
}

// continueCrack changes the break time of a crack (another tool, an effect), keeping its progress.
func (s *Session) continueCrack(pos cube.Pos, total time.Duration) {
	st := s.fx()
	if st == nil || total <= 0 {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	c := st.cracks[pos]
	if c == nil {
		return
	}
	done := float64(time.Since(c.start)) / float64(c.total)
	c.total = total
	c.start = time.Now().Add(-time.Duration(done * float64(total)))
	if c.timer != nil {
		c.timer.Stop()
	}
	c.gen++
	s.crackTick(st, pos, c, c.gen)
}

func (s *Session) stopCrack(pos cube.Pos) {
	st := s.fx()
	if st == nil {
		return
	}
	st.mu.Lock()
	c := st.cracks[pos]
	delete(st.cracks, pos)
	if c != nil {
		if c.timer != nil {
			c.timer.Stop()
			c.timer = nil
		}
		c.gen++ // a callback already waiting for the lock ends its chain
	}
	st.mu.Unlock()
	if c == nil {
		return
	}
	s.blockDestruction(c.id, pos, 255) // any stage outside 0-9 removes the overlay
}

// crackTick sends the crack's current stage and schedules the next one, as timer chain gen.
// st.mu is held.
func (s *Session) crackTick(st *fxState, pos cube.Pos, c *crack, gen uint64) {
	elapsed := time.Since(c.start)
	stage := int32(elapsed * 10 / c.total)
	if stage > 9 {
		stage = 9
	}
	if stage != c.stage {
		c.stage = stage
		s.blockDestruction(c.id, pos, byte(stage))
	}
	if stage >= 9 {
		c.timer = nil
		return // Dragonfly stops it when the block breaks or the player gives up
	}
	next := c.total*time.Duration(stage+1)/10 - elapsed
	c.timer = time.AfterFunc(next, func() {
		st.mu.Lock()
		defer st.mu.Unlock()
		if st.cracks[pos] == c && c.gen == gen {
			s.crackTick(st, pos, c, gen)
		}
	})
}

func (s *Session) blockDestruction(id int32, pos cube.Pos, stage byte) {
	w := s.packet()
	w.VarInt(id)
	w.Position(pos[0], pos[1], pos[2])
	w.Byte(stage)
	s.queue(v777.ClientboundPlayBlockDestruction, w)
}

// stopCracks stops the crack timers of a closed session.
func (st *fxState) stopCracks() {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, c := range st.cracks {
		if c.timer != nil {
			c.timer.Stop()
			c.timer = nil
		}
		c.gen++
	}
	clear(st.cracks)
}

// ViewEntityWake shows a player leaving its bed: the wake-up animation (which also ends the sleep
// screen for the player itself), standing pose and no bed.
func (s *Session) ViewEntityWake(e world.Entity) {
	id, ok := s.entityID(e)
	if !ok {
		return
	}
	s.animate(id, 0) // WAKE_UP
	w := s.packet()
	w.VarInt(id)
	w.Byte(metaPose)
	w.VarInt(dataTypePose)
	w.VarInt(poseStandingID)
	w.Byte(metaSleepingPos)
	w.VarInt(dataTypeOptBlockPos)
	w.Bool(false)
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)
}

// ViewSleepingPlayers shows how many players sleep, like vanilla's action bar message.
func (s *Session) ViewSleepingPlayers(sleeping, max int) {
	if sleeping <= 0 {
		return
	}
	c := text.Translatable("sleep.players_sleeping", text.Plain(strconv.Itoa(sleeping)), text.Plain(strconv.Itoa(max)))
	w := s.packet()
	c.Write(w)
	s.queue(v777.ClientboundPlaySetActionBarText, w)
}

// ViewEntityAnimation plays a Bedrock resource pack animation (animate_entity). Java has no
// data-driven entity animations, so there is nothing to send.
func (*Session) ViewEntityAnimation(world.Entity, world.EntityAnimation) {}

// ViewEmote plays a Bedrock emote. Java has no emotes.
func (*Session) ViewEmote(world.Entity, uuid.UUID) {}
