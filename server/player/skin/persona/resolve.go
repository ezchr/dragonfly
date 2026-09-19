// Package persona rebuilds a persona skin into an ordinary flat skin.
//
// A skin built in the in-game character creator is not self contained: the
// client sends a texture and a geometry that only describe the parts of the
// persona the receiving client can already resolve locally, and leaves the rest
// to the persona piece list, which is a list of marketplace content IDs rather
// than any pixel or mesh data. A server that re-broadcasts such a skin to other
// players therefore has nothing to send for the pieces themselves, and the
// players on the other end see an incomplete model.
//
// Minecraft publishes the resolved appearance of a persona separately, keyed by
// XUID and needing no authentication, as a binary glTF export. That export is a
// plain box model using the vanilla player bone names, so it can be converted
// back into the flat texture and geometry pair that an ordinary skin is made
// of. This package fetches it, caches it per XUID, and converts it.
package persona

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/player/skin"
)

// Enabled controls whether persona skins are resolved at all. Resolving reaches
// out to a Minecraft service, so it is kept behind a single switch that a
// server not wanting that traffic can turn off.
// Resolution is off: rebuilding a persona into a flat skin was tried against a
// real animated persona and did not fix the floating head it was meant to fix,
// so the server is back to forwarding whatever the client sends. The package is
// left in place because the conversion itself is correct and verified; only the
// assumption that a rebuilt skin renders the body turned out to be wrong.
var Enabled = false

// Endpoint is the URL template the resolved persona model is fetched from. The
// single verb is the XUID of the player.
const Endpoint = "https://persona-secondary.franchise.minecraft-services.net/api/v1.0/profile/xuid/%s/image/modelbinary"

const (
	// successTTL and failureTTL are how long a resolved model and a failed
	// lookup are kept. A persona only changes when the player edits it, so a
	// generous success lifetime keeps repeat joins free, while failures are
	// retried soon enough that an outage does not leave a player looking wrong
	// for the rest of the session.
	successTTL = 30 * time.Minute
	failureTTL = 5 * time.Minute

	// fetchTimeout bounds the whole request. Resolution happens off the world
	// goroutine, so a slow response costs nothing but a slightly later skin.
	fetchTimeout = 15 * time.Second

	// maxBodySize caps the download. Observed exports are around 30KB, so this
	// leaves generous headroom while refusing to buffer something unbounded
	// from a remote service.
	maxBodySize = 8 << 20
)

// result is a cache entry. It is published to the cache before the fetch it
// describes completes, so that several players joining at once with the same
// persona wait on one request rather than starting one each.
type result struct {
	done chan struct{}
	at   time.Time
	m    *Model
	err  error
}

func (r *result) expired(now time.Time) bool {
	ttl := successTTL
	if r.err != nil {
		ttl = failureTTL
	}
	return now.Sub(r.at) > ttl
}

var (
	cacheMu sync.Mutex
	cache   = map[string]*result{}

	client = &http.Client{Timeout: fetchTimeout}
)

// Resolve fetches and converts the persona model of the XUID passed. Results
// are cached per XUID, and concurrent calls for the same XUID share a single
// request. It blocks, so it must not be called from a world transaction.
func Resolve(ctx context.Context, xuid string) (*Model, error) {
	if !Enabled {
		return nil, fmt.Errorf("persona resolution is disabled")
	}
	if xuid == "" {
		return nil, fmt.Errorf("no xuid to resolve a persona for")
	}

	now := time.Now()
	cacheMu.Lock()
	r, ok := cache[xuid]
	if ok {
		select {
		case <-r.done:
			if r.expired(now) {
				ok = false
			}
		default:
			// Still in flight. Wait for it rather than starting a second one.
		}
	}
	if !ok {
		r = &result{done: make(chan struct{}), at: now}
		cache[xuid] = r
		cacheMu.Unlock()

		r.m, r.err = fetch(ctx, xuid)
		r.at = time.Now()
		close(r.done)
		return r.m, r.err
	}
	cacheMu.Unlock()

	select {
	case <-r.done:
		return r.m, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// fetch downloads and converts a single persona export.
func fetch(ctx context.Context, xuid string) (*Model, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(Endpoint, xuid), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch persona model: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch persona model: unexpected status %v", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, fmt.Errorf("read persona model: %w", err)
	}
	return Build(b)
}

// Apply returns s rebuilt around the resolved persona model: the rebuilt
// classic texture replaces the skin, the stock player geometry the rebuild
// targets replaces the model, and the persona flag is cleared, because the
// result is now an ordinary flat skin that needs nothing from the receiving
// client to render.
//
// The face animation of an animated persona is deliberately dropped. The
// resolved export is a still model, and an animation entry pointing at a face
// geometry that is no longer part of the model is worse than none.
func Apply(s skin.Skin, m *Model) skin.Skin {
	out := skin.New(m.Width, m.Height)
	copy(out.Pix, m.Pix)

	out.Persona = false
	out.PlayFabID = s.PlayFabID
	out.Cape = s.Cape
	out.SkinColour = s.SkinColour
	out.ArmSize = m.ArmSize
	// No custom geometry: the rebuild targets one of the stock player models, so
	// the skin carries only a resource patch naming it, exactly as an ordinary
	// skin does.
	out.Model = nil
	out.ModelConfig = skin.ModelConfig{Default: m.Geometry}
	// Clients cache a skin against its full ID. Reusing the ID the persona
	// arrived under would let a client serve the persona it already has cached,
	// which is the broken appearance this rebuild exists to replace, so the
	// rebuilt skin is published under its own derived ID. It stays stable across
	// rejoins so the cache still does its job.
	if s.FullID != "" {
		out.FullID = s.FullID + "-resolved"
	}
	return out
}
