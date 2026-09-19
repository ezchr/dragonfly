package session

import (
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/internal/sliceutil"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

var sessions = new(sessionList)

type sessionList struct {
	mu sync.Mutex
	s  []*Session
}

func (l *sessionList) Add(s *Session) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, other := range l.s {
		// Show all sessions to the new session and the new session to all
		// existing sessions.
		l.sendSessionTo(s, other)
		l.sendSessionTo(other, s)
	}
	// Show the new session to itself.
	l.sendSessionTo(s, s)
	l.s = append(l.s, s)
}

func (l *sessionList) Remove(s *Session, entity world.Entity) {
	l.mu.Lock()
	removedFrom := slices.Clone(l.s)
	for _, other := range l.s {
		l.unsendSessionFrom(s, other)
	}
	l.s = sliceutil.DeleteVal(l.s, s)
	l.mu.Unlock()

	if entity == nil {
		return
	}
	for _, other := range removedFrom {
		if other.viewLayer != nil {
			other.viewLayer.Remove(entity)
		}
	}
}

func (l *sessionList) Lookup(id uuid.UUID) (*Session, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if index := slices.IndexFunc(l.s, func(session *Session) bool {
		return session.ent.UUID() == id
	}); index != -1 {
		return l.s[index], true
	}
	return nil, false
}

func (l *sessionList) sendSessionTo(s, to *Session) {
	runtimeID := uint64(selfEntityRuntimeID)

	to.entityMutex.Lock()
	if s != to {
		to.currentEntityRuntimeID += 1
		runtimeID = to.currentEntityRuntimeID
	}
	to.entityRuntimeIDs[s.ent] = runtimeID
	to.entities[runtimeID] = s.ent
	to.entityMutex.Unlock()

	to.writePacket(&packet.PlayerList{
		Entries: []protocol.PlayerListEntry{{
			ActionType:     protocol.PlayerListActionAdd,
			UUID:           s.ent.UUID(),
			EntityUniqueID: int64(runtimeID),
			Username:       s.conn.IdentityData().DisplayName,
			XUID:           s.conn.IdentityData().XUID,
			BuildPlatform:  int32(protocol.DeviceUnknown),
			Skin:           skinToProtocol(s.joinSkin),
		}},
	})
}

func (l *sessionList) unsendSessionFrom(s, from *Session) {
	from.entityMutex.Lock()
	delete(from.entities, from.entityRuntimeIDs[s.ent])
	delete(from.entityRuntimeIDs, s.ent)
	from.entityMutex.Unlock()

	from.writePacket(&packet.PlayerList{
		Entries: []protocol.PlayerListEntry{{
			ActionType: protocol.PlayerListActionRemove,
			UUID:       s.ent.UUID(),
		}},
	})
}

// skinToProtocol converts a skin to its protocol representation.
func skinToProtocol(s skin.Skin) protocol.Skin {
	// DEBUGPATCH: both a body-animation-only filter and a strip-everything filter were tried and
	// reverted here 2026-09-18 while investigating a real floating-head/invisible-body report for
	// animated Persona skins. Neither improved things - stripping only body animations left the
	// same floating head, and stripping ALL animations (including the previously-untouched face
	// animation) made it worse, reducing the visible player down to just a floating hair piece.
	// That progression (removing more animation data => less of the model renders) is the
	// opposite of what "animation data causes the bug" would predict, so animation forwarding is
	// restored to real, complete, unfiltered data - the actual cause is elsewhere.
	var animations []protocol.SkinAnimation
	for _, animation := range s.Animations {
		protocolAnim := protocol.SkinAnimation{
			ImageWidth:  uint32(animation.Bounds().Max.X),
			ImageHeight: uint32(animation.Bounds().Max.Y),
			ImageData:   animation.Pix,
			FrameCount:  float32(animation.FrameCount),
		}
		switch animation.Type() {
		case skin.AnimationHead:
			protocolAnim.AnimationType = protocol.SkinAnimationHead
		case skin.AnimationBody32x32:
			protocolAnim.AnimationType = protocol.SkinAnimationBody32x32
		case skin.AnimationBody128x128:
			protocolAnim.AnimationType = protocol.SkinAnimationBody128x128
		}
		protocolAnim.ExpressionType = uint32(animation.AnimationExpression)
		animations = append(animations, protocolAnim)
	}
	// DEBUGPATCH: also tried sorting animations to put Head/Face before Body (the real client
	// always sends Body first, Head second) in case the receiving client needed a specific
	// canonical order to bind frames correctly - confirmed 2026-09-18 this did not fix the
	// floating-head/invisible-body symptom either, reverted back to forwarding s.Animations in
	// its original order.

	fullID := s.FullID
	if fullID == "" {
		fullID = uuid.New().String()
	}
	model := s.Model
	if len(model) == 0 {
		model = []byte("{}")
	}
	return protocol.Skin{
		PlayFabID:         s.PlayFabID,
		SkinID:            uuid.New().String(),
		SkinResourcePatch: s.ModelConfig.Encode(),
		SkinImageWidth:    uint32(s.Bounds().Max.X),
		SkinImageHeight:   uint32(s.Bounds().Max.Y),
		SkinData:          s.Pix,
		CapeImageWidth:    uint32(s.Cape.Bounds().Max.X),
		CapeImageHeight:   uint32(s.Cape.Bounds().Max.Y),
		CapeData:          s.Cape.Pix,
		SkinGeometry:      model,
		// ArmSize was never previously set here, so it always defaulted to the protocol zero value
		// (ArmSizeSlim = 0) regardless of the real player's actual arm size - meaning every player
		// relayed through this skin type was shown to others with slim (Alex-style) arm geometry no
		// matter what their real skin actually specified. skin.Skin.ArmSize now carries the real
		// value captured in parseSkin ("wide"/"slim", matching login.ClientData.ArmSize's own
		// string format exactly), so this maps it to the correct protocol constant instead of
		// silently defaulting.
		ArmSize: armSizeToProtocol(s.ArmSize),
		// PersonaSkin is intentionally always false here, regardless of the skin's original
		// PersonaSkin flag: skin.Skin has no fields for PersonaPieces/PieceTintColours (parseSkin
		// never reads them off the incoming login.ClientData either), so a Persona skin would be
		// re-broadcast as PersonaSkin: true with no piece data at all - a combination some clients
		// don't render, falling back to the default skin instead. The flat SkinData/SkinGeometry
		// captured above is already a complete, valid classic-style skin representation regardless
		// of whether the original skin was Persona-based, so forcing false here makes it render
		// through the normal flat-skin path, the same one non-Persona skins already use
		// successfully. Confirmed 2026-09-18 by reverting this to s.Persona as a test: the default
		// skin bug came straight back, and a separate invisible-body bug some players see was NOT
		// fixed by the revert either - that second bug is real but unrelated to this flag.
		PersonaSkin:               false,
		CapeID:                    uuid.New().String(),
		FullID:                    fullID,
		Animations:                animations,
		Trusted:                   true,
		OverrideAppearance:        true,
		GeometryDataEngineVersion: []byte(protocol.CurrentVersion),
	}
}

// armSizeToProtocol maps the real client's ArmSize string (login.ClientData.ArmSize, "wide" or
// "slim") to the protocol.ArmSize* constant a re-broadcast skin packet needs. Defaults to
// ArmSizeWide (the more common/vanilla-default value) for anything else, including an empty
// string from an older client that never sent one, rather than silently defaulting to slim.
func armSizeToProtocol(armSize string) uint8 {
	if armSize == "slim" {
		return protocol.ArmSizeSlim
	}
	return protocol.ArmSizeWide
}
