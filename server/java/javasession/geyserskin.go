package javasession

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	jserver "github.com/ezchr/go-mcjava/server"
)

// Bedrock players' skins for Java clients, from GeyserMC's global skin database
// (api.geysermc.org/v2/skin/<xuid>). Floodgate servers upload the skins of the Bedrock players
// who join them, and the API hands back the skin as a Mojang-signed Java texture: the property a
// Java client needs to show it. Players whose skin was never uploaded keep the default skin.

// geyserSkinURL is the lookup by decimal XUID.
const geyserSkinURL = "https://api.geysermc.org/v2/skin/"

const (
	skinRefresh    = time.Hour        // a found skin is looked up again after this
	skinRetry      = 10 * time.Minute // a failed or empty lookup is retried after this
	skinWaitForTab = 3 * time.Second  // how long the tab list holds a player back for their skin
)

type geyserSkin struct {
	done    chan struct{} // closed when the lookup finished
	started time.Time
	props   []jserver.Property // nil: no skin (not uploaded, or the lookup failed)
	at      time.Time          // when the lookup finished
}

var (
	bedrockSkinMu sync.Mutex
	bedrockSkins  = map[string]*geyserSkin{} // by XUID
	skinHTTP      = &http.Client{Timeout: 5 * time.Second}
	skinSlots     = make(chan struct{}, 4) // concurrent lookups
)

// PrefetchBedrockSkin starts looking up a Bedrock player's skin, so it is ready when Java
// clients first see them. Call it when a Bedrock player logs in; it returns at once.
func PrefetchBedrockSkin(xuid string) {
	if xuid == "" {
		return
	}
	bedrockSkinMu.Lock()
	defer bedrockSkinMu.Unlock()
	if sk, ok := bedrockSkins[xuid]; ok {
		select {
		case <-sk.done:
			stale := skinRetry
			if sk.props != nil {
				stale = skinRefresh
			}
			if time.Since(sk.at) < stale {
				return
			}
		default:
			return // in flight
		}
	}
	sk := &geyserSkin{done: make(chan struct{}), started: time.Now()}
	if old, ok := bedrockSkins[xuid]; ok {
		sk.props = old.props // keep showing the old skin while it refreshes
	}
	bedrockSkins[xuid] = sk
	go func() {
		props := fetchGeyserSkin(xuid)
		bedrockSkinMu.Lock()
		if props != nil || sk.props == nil {
			sk.props = props
		}
		sk.at = time.Now()
		bedrockSkinMu.Unlock()
		close(sk.done)
	}()
}

// bedrockSkinProps returns the skin property of a Bedrock player, and whether the lookup is
// finished (or has taken longer than skinWaitForTab: then the player is shown without).
func bedrockSkinProps(xuid string) (props []jserver.Property, settled bool) {
	if xuid == "" {
		return nil, true
	}
	bedrockSkinMu.Lock()
	sk, ok := bedrockSkins[xuid]
	bedrockSkinMu.Unlock()
	if !ok {
		PrefetchBedrockSkin(xuid)
		return nil, false
	}
	select {
	case <-sk.done:
		bedrockSkinMu.Lock()
		defer bedrockSkinMu.Unlock()
		return sk.props, true
	default:
		bedrockSkinMu.Lock()
		defer bedrockSkinMu.Unlock()
		return sk.props, time.Since(sk.started) > skinWaitForTab
	}
}

// fetchGeyserSkin looks a skin up; nil when there is none or the lookup failed.
func fetchGeyserSkin(xuid string) []jserver.Property {
	skinSlots <- struct{}{}
	defer func() { <-skinSlots }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, geyserSkinURL+xuid, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "dfjava (Dragonfly Java Edition support)")
	resp, err := skinHTTP.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var body struct {
		Value     string `json:"value"`
		Signature string `json:"signature"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body); err != nil {
		return nil
	}
	if body.Value == "" || body.Signature == "" {
		return nil
	}
	return []jserver.Property{{Name: "textures", Value: body.Value, Signature: body.Signature}}
}

// deferForSkin holds back showing a Bedrock player whose skin lookup has not finished, for at
// most skinWaitForTab: a Java client keeps the skin it first draws a player with, so a player
// shown before the lookup stays default-skinned until they come into view again. It reports
// whether the spawn was deferred; the player is then shown (with everything a spawn sends) once
// the lookup is done, unless they left view in the meantime (HideEntity calls it off).
func (s *Session) deferForSkin(p *player.Player) bool {
	if isJavaPlayer(p.UUID()) || p.XUID() == "" {
		return false
	}
	if _, settled := bedrockSkinProps(p.XUID()); settled {
		return false
	}
	h, xuid := p.H(), p.XUID()
	s.entMu.Lock()
	if s.deferred == nil {
		s.deferred = map[*world.EntityHandle]struct{}{}
	}
	if _, waiting := s.deferred[h]; waiting {
		s.entMu.Unlock()
		return true
	}
	s.deferred[h] = struct{}{}
	s.entMu.Unlock()
	go func() {
		deadline := time.Now().Add(skinWaitForTab)
		for time.Now().Before(deadline) {
			if _, settled := bedrockSkinProps(xuid); settled {
				break
			}
			select {
			case <-s.closed:
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
		_, _ = world.CallRef(context.Background(), world.NewEntityRef[world.Entity](h), func(tx *world.Tx, e world.Entity) (struct{}, error) {
			s.entMu.Lock()
			_, still := s.deferred[h]
			delete(s.deferred, h)
			s.entMu.Unlock()
			if !still {
				return struct{}{}, nil // left view meanwhile
			}
			// What the world sends a viewer for an entity coming into view (world.showEntity).
			s.ViewEntity(e)
			s.ViewEntityItems(e)
			s.ViewEntityArmour(e)
			s.ViewEntityState(e)
			return struct{}{}, nil
		})
	}()
	return true
}
