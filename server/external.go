package server

import (
	"errors"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// ExternalSession connects a player that did not come through a Listener, such
// as a Java Edition client. It does its own login, then joins through
// LoadPlayer and AddPlayer, and from then on is the player's Session.
type ExternalSession interface {
	player.Session
	// Spawn is called in the player's world transaction right after the
	// player entity is added. The session starts sending the world and
	// reading the client's input from here.
	Spawn(c session.Controllable, tx *world.Tx)
}

// incomingSession is what Accept needs from a joining player's session.
type incomingSession interface {
	Spawn(c session.Controllable, tx *world.Tx)
	Disconnect(message string)
	CloseConnection()
}

// LoadPlayer returns the saved data of the player with this UUID and the
// world they should spawn in. A player without saved data gets the default
// world's spawn and game mode. External sessions call it before sending their
// own login packets (which need the position, dimension and game mode).
func (srv *Server) LoadPlayer(id uuid.UUID) (player.Config, *world.World) {
	d, w, err := srv.conf.PlayerProvider.Load(id, srv.dimension)
	if err != nil {
		w = srv.world
		d.Position = w.Spawn().Vec3Centre()
		d.GameMode = w.DefaultGameMode()
	}
	return d, w
}

// ErrAlreadyOnline is returned by AddPlayer when a player with the same UUID
// is already online.
var ErrAlreadyOnline = errors.New("already logged in")

// AddPlayer adds a player controlled by an external session. conf comes from
// LoadPlayer with Name, XUID, UUID, Locale and Skin filled in. The player is
// then handed out by Accept like players from Listeners, and Spawn is called
// on the session.
//
// The session must call the returned function exactly once when the player
// leaves, in the player's world transaction (nil tx if the world is already
// closed): it saves the player's data and removes them from the server.
func (srv *Server) AddPlayer(s ExternalSession, conf player.Config, w *world.World) (onClose func(tx *world.Tx, c session.Controllable), err error) {
	if _, ok := srv.Player(conf.UUID); ok {
		return nil, ErrAlreadyOnline
	}
	srv.pwg.Add(1)
	conf.Session = s
	handle := world.EntitySpawnOpts{Position: conf.Position, ID: conf.UUID}.New(player.Type, conf)
	srv.incoming <- incoming{s: s, w: w, conf: conf, p: &onlinePlayer{name: conf.Name, xuid: conf.XUID, handle: handle}}
	return srv.handleSessionClose, nil
}
