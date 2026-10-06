package javasession

import (
	"sync"
	"time"
)

// itemStates holds each session's inventory state until Session has a field for it.
var itemStates sync.Map // *Session -> *itemState (releasedItems once the session closed its windows)

// releasedItems marks a session whose inventory state is gone: Session.Close closed its windows and
// gave the items back.
var releasedItems = &itemState{}

// items returns the session's inventory state.
//
// The state lives until Session.Close has closed the windows (closeContainers calls releaseItems),
// not just until the connection closes: the read loop closes the connection before it closes the
// player, and the close needs the open window and the UI inventory to give the items back. A
// session that is gone gets a fresh state each call that is never stored, so nothing leaks.
//
// TODO: once Session has the field `items *itemState` (set in newSession with
// `s.items = newItemState(s)`), this becomes `return s.items` and itemStates goes.
func (s *Session) items() *itemState {
	if v, ok := itemStates.Load(s); ok {
		if st := v.(*itemState); st != releasedItems {
			return st
		}
		return newItemState(s)
	}
	if s.closed != nil {
		select {
		case <-s.closed:
			return newItemState(s) // closed before it ever had a state: nothing to keep
		default:
		}
	}
	st := newItemState(s)
	if v, loaded := itemStates.LoadOrStore(s, st); loaded {
		if st := v.(*itemState); st != releasedItems {
			return st
		}
		return newItemState(s)
	}
	if s.closed != nil {
		go func() {
			<-s.closed
			// Session.Close normally follows at once and releases the state; a player that never
			// gets closed (it never spawned) must not keep it forever.
			select {
			case <-st.released:
			case <-time.After(time.Minute):
			}
			itemStates.Delete(s)
		}()
	}
	return st
}

// releaseItems forgets the session's inventory state: the end of closeContainers, once the windows
// are closed and their items are back in the inventory.
func (s *Session) releaseItems() {
	v, ok := itemStates.Load(s)
	if !ok || v.(*itemState) == releasedItems {
		return
	}
	st := v.(*itemState)
	itemStates.Store(s, releasedItems)
	st.releaseOnce.Do(func() { close(st.released) })
	if s.closed == nil {
		itemStates.Delete(s)
		return
	}
	select {
	case <-s.closed:
		itemStates.Delete(s) // the watcher may have gone already
	default: // the watcher deletes the marker when the connection closes
	}
}
