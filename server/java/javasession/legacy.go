package javasession

import (
	v776 "github.com/df-mc/dragonfly/server/java/protocol/v776"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
)

// handleLegacy handles a packet of an older client version that the newest version (whose ids
// the session works in) does not have. id is the client's own packet id.
func (s *Session) handleLegacy(id int32, body []byte) error {
	if s.ver.Protocol == 776 && id == v776.ServerboundPlaySwing {
		// 26.2 sends swing for every arm swing (attacks and block hits too), where 26.3 sends
		// punch only for a swing at the air: show the swing, without the punch-air event.
		s.do(func(tx *world.Tx, c session.Controllable) { c.SwingArm() })
	}
	return nil
}
