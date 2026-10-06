package javasession

import (
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// fxState is what the sound and particle code remembers per session: what the Java client
// predicted itself (so it is not played twice) and the crack animations it draws for other
// players' block breaking.
//
// The Java client plays these itself, and a vanilla server sends them to everyone but the player
// who caused them: the break sound and particles (level event 2001) of a block it destroyed, the
// hit sounds and crack particles while it mines, and the sounds of blocks it placed, doors,
// trapdoors and fence gates it toggled and buckets it used. Dragonfly sends them to every viewer,
// so the session drops them when they match what its client just did.
type fxState struct {
	mu sync.Mutex

	breaking bool     // the client is mining breakPos
	breakPos cube.Pos //
	broke    cube.Pos // the client destroyed this block itself (creative start, survival stop)
	brokeAt  time.Time
	usedOn   cube.Pos // the client used an item on this block (use_item_on)
	usedOnAt time.Time
	usedAt   time.Time // the client used an item (use_item or use_item_on)

	cracks map[cube.Pos]*crack
	probed bool // fxProbe ran
}

// predictWindow is how long after the client's action a matching sound counts as predicted.
const predictWindow = time.Second

var fxStates sync.Map // *Session -> *fxState

// fx returns the session's fx state, or nil if it has none yet.
func (s *Session) fx() *fxState {
	if v, ok := fxStates.Load(s); ok {
		return v.(*fxState)
	}
	return nil
}

// fxMake returns the session's fx state, creating it.
//
// TODO: once Session has a field `fx fxState`, fx and fxMake become `return &s.fx`.
func (s *Session) fxMake() *fxState {
	if st := s.fx(); st != nil {
		return st
	}
	v, loaded := fxStates.LoadOrStore(s, &fxState{})
	st := v.(*fxState)
	if !loaded {
		go func() {
			<-s.closed
			st.stopCracks()
			fxStates.Delete(s)
		}()
	}
	return st
}

// fxDestroyAction records the client's player_action. Hook: input.go, handleInput, case
// ServerboundPlayPlayerAction, right after the r.Err check: s.fxDestroyAction(action, pos)
func (s *Session) fxDestroyAction(action int32, pos cube.Pos) {
	switch action {
	case actionStartDestroy, actionAbortDestroy, actionStopDestroy:
	default:
		return
	}
	st := s.fxMake()
	now := time.Now()
	st.mu.Lock()
	switch action {
	case actionStartDestroy:
		// Creative breaks on start; in survival nothing breaks until stop, after the window.
		st.breaking, st.breakPos = true, pos
		st.broke, st.brokeAt = pos, now
	case actionAbortDestroy:
		st.breaking = false
	case actionStopDestroy:
		st.breaking = false
		st.broke, st.brokeAt = pos, now
	}
	st.mu.Unlock()
}

// fxUsedOn records the client's use_item_on. Hook: input.go, handleInput, case
// ServerboundPlayUseItemOn, before s.do: s.fxUsedOn(pos)
func (s *Session) fxUsedOn(pos cube.Pos) {
	st := s.fxMake()
	now := time.Now()
	st.mu.Lock()
	st.usedOn, st.usedOnAt, st.usedAt = pos, now, now
	st.mu.Unlock()
}

// fxUsedItem records the client's use_item (buckets use it). Hook: input.go, handleInput, case
// ServerboundPlayUseItem, before s.do: s.fxUsedItem()
func (s *Session) fxUsedItem() {
	st := s.fxMake()
	now := time.Now()
	st.mu.Lock()
	st.usedAt = now
	st.mu.Unlock()
}

// predictedBreak reports whether the client destroyed pos itself just now (and forgets it).
func (s *Session) predictedBreak(pos cube.Pos) bool {
	st := s.fx()
	if st == nil {
		return false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.broke == pos && !st.brokeAt.IsZero() && time.Since(st.brokeAt) < predictWindow {
		st.brokeAt = time.Time{}
		return true
	}
	return false
}

// selfMining reports whether the client is mining pos (or just broke it).
func (s *Session) selfMining(pos cube.Pos) bool {
	st := s.fx()
	if st == nil {
		return false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return (st.breaking && st.breakPos == pos) || (st.broke == pos && time.Since(st.brokeAt) < predictWindow)
}

// predictedUseOn reports whether a block sound at pos follows the client's own use_item_on: the
// client plays place, door, trapdoor, fence gate and tool sounds itself.
func (s *Session) predictedUseOn(pos cube.Pos) bool {
	st := s.fx()
	if st == nil {
		return false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.usedOnAt.IsZero() || time.Since(st.usedOnAt) >= predictWindow {
		return false
	}
	// The placed block is next to the clicked one; a door's other half is above or below it.
	for i := range 3 {
		if d := pos[i] - st.usedOn[i]; d < -1 || d > 1 {
			return false
		}
	}
	return true
}

// predictedUse reports whether the client used an item just now (bucket sounds).
func (s *Session) predictedUse() bool {
	st := s.fx()
	if st == nil {
		return false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return !st.usedAt.IsZero() && time.Since(st.usedAt) < predictWindow
}
