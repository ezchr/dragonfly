package javasession

// Not implemented yet (self area): these do nothing. Generated with tools/stubgen from
// player.Session; delete a method here when it gets a real implementation.

import (
	"net"

	"github.com/df-mc/dragonfly/server/player/debug"
	"github.com/df-mc/dragonfly/server/player/hud"
	"github.com/df-mc/dragonfly/server/player/input"
	"github.com/df-mc/dragonfly/server/world"
)

// Methods the Java session does not implement yet: they do nothing. Generated with
// tools/stubgen from player.Session; delete a line when the method gets a real implementation.

func (*Session) AddDebugShape(_ debug.Shape)                            {}
func (*Session) ClearInputLocks()                                       {}
func (*Session) EnableCoordinates(_ bool)                               {}
func (*Session) EnableInstantRespawn(_ bool)                            {}
func (*Session) HideHudElement(_ hud.Element)                           {}
func (*Session) HudElementHidden(_ hud.Element) bool                    { return false }
func (*Session) InputLocked(_ input.Lock) bool                          { return false }
func (*Session) LockInput(_ input.Lock)                                 {}
func (*Session) RemoveAllDebugShapes()                                  {}
func (*Session) RemoveDebugShape(_ debug.Shape)                         {}
func (*Session) RemoveViewLayer(_ world.Entity)                         {}
func (*Session) SendDebugShapes(_ world.Dimension)                      {}
func (*Session) SendHudUpdates()                                        {}
func (*Session) SendInputLocks()                                        {}
func (*Session) ShowHudElement(_ hud.Element)                           {}
func (*Session) StartShowingEntity(_ world.Entity)                      {}
func (*Session) StopShowingEntity(_ world.Entity)                       {}
func (*Session) Transfer(_ net.IP, _ int)                               {}
func (*Session) UnlockInput(_ input.Lock)                               {}
func (*Session) ViewEntityGameMode(_ world.Entity)                      {}
func (*Session) ViewLayer() *world.ViewLayer                            { return nil }
func (*Session) ViewSkin(_ world.Entity)                                {}
func (*Session) ViewVisibility(_ world.Entity, _ world.VisibilityLevel) {}
func (*Session) VisibleDebugShapes() []debug.Shape                      { return nil }
