package session

import (
	"context"
	"log/slog"
	"time"

	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/player/skin/persona"
	"github.com/df-mc/dragonfly/server/world"
)

const (
	// personaApplyAttempts and personaApplyRetry bound the wait for the player
	// to actually be present in the world. Resolution starts as soon as the
	// skin is known, which at login is before the player has been added, and a
	// cached model comes back fast enough to lose that race.
	personaApplyAttempts = 20
	personaApplyRetry    = 500 * time.Millisecond

	// personaApplyTimeout stops a closed or wedged world from holding the
	// resolving goroutine open forever.
	personaApplyTimeout = 10 * time.Second
)

// ResolvePersona rebuilds a persona skin into an ordinary flat skin in the
// background and applies it to the entity behind the handle passed once it is
// ready.
//
// A persona skin is not self contained: the pieces it is assembled from are
// sent as marketplace content IDs, not as pixels, so a server re-broadcasting
// one has nothing to send for the pieces themselves and the players receiving
// it see an incomplete model. Resolving fetches the finished appearance from
// Minecraft and converts it into the flat texture and geometry pair an ordinary
// skin is made of, which every client can render without owning anything.
//
// Nothing about this is on the hot path: the fetch happens on its own
// goroutine, the result is cached per XUID, and the skin is only swapped once
// it has arrived. If resolution fails the player keeps the skin they joined
// with, so the worst case is the behaviour this server had before.
func ResolvePersona(w *world.World, h *world.EntityHandle, xuid string, s skin.Skin, log *slog.Logger) {
	if !persona.Enabled || !s.Persona || xuid == "" || w == nil || h == nil {
		return
	}
	if log == nil {
		log = slog.Default()
	}
	go func() {
		m, err := persona.Resolve(context.Background(), xuid)
		if err != nil {
			log.Debug("resolve persona skin: "+err.Error(), "xuid", xuid)
			return
		}
		if len(m.Skipped) > 0 {
			log.Debug("persona skin has parts that could not be converted",
				"xuid", xuid, "parts", m.Skipped)
		}
		for attempt := 0; attempt < personaApplyAttempts; attempt++ {
			if applyPersona(w, h, m) {
				return
			}
			time.Sleep(personaApplyRetry)
		}
		log.Debug("resolved persona skin was never applied: player never entered the world", "xuid", xuid)
	}()
}

// applyPersona swaps the skin of the entity behind the handle for the resolved
// model, reporting whether the attempt settled the matter. A false return means
// only that the entity was not in the world yet and the caller should try
// again.
func applyPersona(w *world.World, h *world.EntityHandle, m *persona.Model) bool {
	done := make(chan bool, 1)
	w.Do(func(tx *world.Tx) {
		e, ok := h.Entity(tx)
		if !ok {
			done <- false
			return
		}
		c, ok := e.(Controllable)
		if !ok {
			done <- true
			return
		}
		cur := c.Skin()
		if !cur.Persona {
			// Either this already ran, or the player switched to a skin that
			// needs no resolving while the model was being fetched. Either way
			// there is nothing left to do.
			done <- true
			return
		}
		c.SetSkin(persona.Apply(cur, m))
		done <- true
	})
	select {
	case ok := <-done:
		return ok
	case <-time.After(personaApplyTimeout):
		return false
	}
}
