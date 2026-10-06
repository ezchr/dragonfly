package javasession

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	jserver "github.com/ezchr/go-mcjava/server"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/wire"
	"github.com/google/uuid"
	"golang.org/x/text/language"
)

// Config configures Run.
type Config struct {
	// Server is the Dragonfly server Java players join.
	Server *server.Server
	// Listener accepts Java clients (handshake, login, configuration).
	Listener *jserver.Listener
	// ChunkRadius is the view distance in chunks (the client's own is used if smaller).
	ChunkRadius int
	Log         *slog.Logger
	// Identity gives a Java player their Dragonfly UUID and XUID (ViaBedrockIdentity if nil, which
	// keeps the data of players who joined through ViaProxy).
	Identity Identity
	// JoinMessage and QuitMessage are announced in chat when a Java player joins and leaves, as
	// Dragonfly does for Bedrock players (pass the server's Config.JoinMessage/QuitMessage).
	// Zero values announce nothing.
	JoinMessage, QuitMessage chat.Translation
	// Allow, if set, is asked before a Java player joins; a non-empty reason refuses them.
	Allow func(p jserver.Profile, id uuid.UUID, xuid string) (reason string)
}

// Run adds every Java client the listener accepts to the server. It returns when the listener is
// closed.
func Run(conf Config) {
	if conf.Log == nil {
		conf.Log = slog.Default()
	}
	if conf.ChunkRadius <= 0 {
		conf.ChunkRadius = 8
	}
	tabs := newTabList(conf.Server)
	for {
		jp, err := conf.Listener.Accept()
		if err != nil {
			return
		}
		go join(conf, tabs, jp)
	}
}

func join(conf Config, tabs *tabList, jp *jserver.Player) {
	identity := conf.Identity
	if identity == nil {
		identity = ViaBedrockIdentity
	}
	id, xuid := identity(jp.Profile)
	if conf.Allow != nil {
		if reason := conf.Allow(jp.Profile, id, xuid); reason != "" {
			conf.Log.Info("refused java login", "name", jp.Profile.Name, "xuid", xuid, "reason", reason)
			var w wire.Writer
			jserver.TextComponent(&w, reason)
			jp.Conn.Send(v777.ClientboundPlayDisconnect, w.B)
			jp.Conn.Close()
			return
		}
	}
	pc, w := conf.Server.LoadPlayer(id)
	pc.Name = jp.Profile.Name
	pc.UUID = id
	pc.XUID = xuid
	pc.Locale, _ = language.Parse(strings.ReplaceAll(jp.Info.Locale, "_", "-"))
	pc.Skin = javaSkin(jp.Profile.Properties)

	radius := int32(conf.ChunkRadius)
	if v := int32(jp.Info.ViewDistance); v > 1 && v < radius {
		radius = v
	}
	s := newSession(jp, radius, conf.Log)
	s.id = id
	s.tabs = tabs
	s.selfID = jp.Profile.UUID
	s.joinMessage, s.quitMessage = conf.JoinMessage, conf.QuitMessage
	s.xuid, s.skin = xuid, pc.Skin
	registerProfile(id, jp.Profile.Properties)
	registerProfile(jp.Profile.UUID, jp.Profile.Properties) // for the client's own tab entry
	s.sendLogin(pc, w)
	if err := conf.Server.AddPlayer(s, pc, w); err != nil {
		s.log.Info("join refused", "err", err)
		switch {
		case errors.Is(err, server.ErrAlreadyOnline):
			s.Disconnect("You are already logged in.")
		case errors.Is(err, server.ErrServerClosed):
			s.Disconnect("Server closed")
		default:
			s.Disconnect("Could not join: " + err.Error())
		}
		return
	}
	s.log.Info("joined", "addr", s.Addr(), "gamemode", gameModeID(pc.GameMode), "pos", pc.Position)
}

// SetCloseHandler ...
func (s *Session) SetCloseHandler(f func(*world.Tx, session.Controllable)) { s.onClose = f }

// gameModeID is the Java id of a Dragonfly game mode.
func gameModeID(m world.GameMode) int32 {
	switch m {
	case world.GameModeCreative:
		return 1
	case world.GameModeAdventure:
		return 2
	case world.GameModeSpectator:
		return 3
	}
	return 0
}

// dimensionKey is the Java dimension (and dimension type) of a Dragonfly dimension.
func dimensionKey(d world.Dimension) string {
	switch d {
	case world.Nether:
		return "minecraft:the_nether"
	case world.End:
		return "minecraft:the_end"
	}
	return "minecraft:overworld"
}

// sendLogin sends the play login, spawn point and position: what vanilla sends before chunks.
func (s *Session) sendLogin(pc player.Config, w *world.World) {
	dim := dimensionKey(w.Dimension())
	s.dim = dim
	p := s.packet()
	p.Int32(selfEntityID)
	p.Bool(false) // hardcore
	p.VarInt(3)
	p.String("minecraft:overworld")
	p.String("minecraft:the_nether")
	p.String("minecraft:the_end")
	p.VarInt(100) // max players (only for the tab list layout)
	p.VarInt(s.chunkRadius)
	p.VarInt(s.chunkRadius) // simulation distance
	p.Bool(false)           // reduced debug info
	p.Bool(true)            // death screen
	p.Bool(false)           // limited crafting
	p.VarInt(s.ver.RegistryID("minecraft:dimension_type", dim))
	p.String(dim)
	p.Int64(0)                        // hashed seed (biome noise; unknown to us)
	p.VarInt(gameModeID(pc.GameMode)) // game mode
	p.VarInt(0)                       // previous game mode: none
	p.Bool(false)                     // debug world
	p.Bool(false)                     // flat (only changes the void fog height)
	p.Bool(false)                     // no death location
	p.VarInt(0)                       // portal cooldown
	p.VarInt(63)                      // sea level
	p.Bool(false)                     // online mode
	p.Bool(false)                     // enforces secure chat
	s.queue(v777.ClientboundPlayLogin, p)

	sp := w.Spawn()
	p = s.packet()
	p.String(dim)
	p.Position(sp[0], sp[1], sp[2])
	p.Float32(0)
	p.Float32(0)
	s.queue(v777.ClientboundPlaySetDefaultSpawnPosition, p)

	s.teleport(pc.Position[0], pc.Position[1], pc.Position[2], float32(pc.Rotation.Yaw()), float32(pc.Rotation.Pitch()))

	p = s.packet()
	p.Byte(13) // start waiting for level chunks
	p.Float32(0)
	s.queue(v777.ClientboundPlayGameEvent, p)
}

// selfEntityID is the entity id a Java client knows itself by. Other entities get ids from the
// session's entity table, which never hands out this one.
const selfEntityID = 1

// teleport moves the client to an absolute position; its moves are ignored until it accepts.
func (s *Session) teleport(x, y, z float64, yaw, pitch float32) {
	id := s.teleportID.Add(1)
	s.pendingTeleport.Store(id)
	p := s.packet()
	p.VarInt(id)
	p.Float64(x)
	p.Float64(y)
	p.Float64(z)
	p.Float64(0)
	p.Float64(0)
	p.Float64(0)
	p.Float32(yaw)
	p.Float32(pitch)
	p.Int32(0) // nothing relative
	s.queue(v777.ClientboundPlayPlayerPosition, p)
}
