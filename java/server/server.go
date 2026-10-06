// Package server accepts Java Edition clients and takes them through handshake, status (server
// list ping), login and configuration, handing the game a connection that is ready for play.
//
// Configuration sends vanilla's own registry, tag and feature packets (generated per version in
// java/v777 from a vanilla join), so the client sees exactly the registries a vanilla server
// would send. The client must already have vanilla's core data pack (every vanilla client does);
// registry entries are then sent by name only.
package server

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	v777 "github.com/ezchr/go-mc/java/v777"
	"github.com/ezchr/go-mc/java/wire"
)

// Status is what the server list shows.
type Status struct {
	MOTD       string
	MaxPlayers int
	Online     int
	Favicon    string // optional data:image/png;base64,...
}

// Config configures a Listener.
type Config struct {
	// Status is asked for the server list entry on every ping.
	Status func() Status
	// CompressionThreshold: packets of at least this many bytes are compressed. 256 like vanilla;
	// negative turns compression off.
	CompressionThreshold int
	// Brand is the server brand the F3 screen shows.
	Brand string
	// LoginTimeout bounds handshake to end of configuration.
	LoginTimeout time.Duration
	Log          *slog.Logger

	// OnlineMode checks every login with the session server (Microsoft accounts only, encrypted
	// connection, real UUIDs and signed skins). Off: anyone can join under any name.
	OnlineMode bool
	// SessionServer is the session server's base URL (DefaultSessionServer if empty).
	SessionServer string
	// PreventProxyConnections also sends the client's IP to the session server, which then
	// refuses logins from a different address than the one the client authenticated from.
	PreventProxyConnections bool
}

// ClientInfo is what the client reports about itself in configuration.
type ClientInfo struct {
	Locale         string
	ViewDistance   int8
	ChatMode       int32
	ChatColours    bool
	SkinParts      uint8
	MainHand       int32
	TextFiltering  bool
	AllowListing   bool
	ParticleStatus int32
}

// Profile is a logged-in player's identity.
type Profile struct {
	UUID       [16]byte
	Name       string
	Properties []Property
}

// Property is a profile property (textures carries the skin).
type Property struct {
	Name, Value, Signature string
}

// Player is a client that finished configuration. Its Conn is in the play state: the game's next
// packet must be the play login packet (ClientboundPlayLogin).
type Player struct {
	Conn     *wire.Conn
	Profile  Profile
	Info     ClientInfo
	Protocol int32
	Address  string // what the client typed to connect (host)
}

// Listener accepts Java clients.
type Listener struct {
	cfg     Config
	ln      net.Listener
	players chan *Player
	ctx     context.Context
	cancel  context.CancelFunc
	key     *authKey // online mode only
	http    *http.Client
}

// Listen starts accepting on addr.
func Listen(addr string, cfg Config) (*Listener, error) {
	if cfg.Status == nil {
		cfg.Status = func() Status { return Status{MOTD: "A Minecraft Server", MaxPlayers: 20} }
	}
	if cfg.Brand == "" {
		cfg.Brand = "dragonfly"
	}
	if cfg.LoginTimeout == 0 {
		cfg.LoginTimeout = 30 * time.Second
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.SessionServer == "" {
		cfg.SessionServer = DefaultSessionServer
	}
	var key *authKey
	if cfg.OnlineMode {
		var err error
		if key, err = newAuthKey(); err != nil {
			return nil, err
		}
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	l := &Listener{cfg: cfg, ln: ln, players: make(chan *Player), ctx: ctx, cancel: cancel, key: key,
		http: &http.Client{Timeout: 15 * time.Second}}
	go l.acceptLoop()
	return l, nil
}

// Addr is the listening address.
func (l *Listener) Addr() net.Addr { return l.ln.Addr() }

// Close stops accepting.
func (l *Listener) Close() error {
	l.cancel()
	return l.ln.Close()
}

// Accept returns the next player that finished configuration.
func (l *Listener) Accept() (*Player, error) {
	select {
	case p := <-l.players:
		return p, nil
	case <-l.ctx.Done():
		return nil, net.ErrClosed
	}
}

func (l *Listener) acceptLoop() {
	for {
		nc, err := l.ln.Accept()
		if err != nil {
			if l.ctx.Err() == nil {
				l.cfg.Log.Error("java listener", "err", err)
			}
			return
		}
		go l.handle(nc)
	}
}

func (l *Listener) handle(nc net.Conn) {
	nc.SetDeadline(time.Now().Add(l.cfg.LoginTimeout))
	c := wire.NewConn(nc)
	p, err := l.negotiate(c)
	if err != nil {
		if !errors.Is(err, errStatusDone) && !errors.Is(err, io.EOF) {
			l.cfg.Log.Debug("java login failed", "addr", nc.RemoteAddr(), "err", err)
		}
		c.Close()
		return
	}
	nc.SetDeadline(time.Time{})
	select {
	case l.players <- p:
	case <-l.ctx.Done():
		c.Close()
	}
}

var errStatusDone = errors.New("status ping finished")

// negotiate runs handshake, then status or login + configuration.
func (l *Listener) negotiate(c *wire.Conn) (*Player, error) {
	id, body, err := c.ReadPacket()
	if err != nil {
		return nil, err
	}
	if id != v777.ServerboundHandshakeIntention {
		return nil, fmt.Errorf("expected handshake, got packet %#x", id)
	}
	r := wire.NewReader(body)
	protocol := r.VarInt()
	host := r.String(255)
	r.Uint16()
	intent := r.VarInt()
	if r.Err != nil {
		return nil, r.Err
	}
	switch intent {
	case 1:
		return nil, l.status(c, protocol)
	case 2, 3: // login, transfer
	default:
		return nil, fmt.Errorf("unknown intent %d", intent)
	}
	if protocol != ProtocolVersion {
		msg := fmt.Sprintf("This server runs Minecraft %s. Please use %s.", GameVersion, GameVersion)
		if protocol < ProtocolVersion {
			msg = fmt.Sprintf("Outdated client! Please use %s.", GameVersion)
		}
		l.loginDisconnect(c, msg)
		return nil, fmt.Errorf("protocol %d, want %d", protocol, ProtocolVersion)
	}
	prof, err := l.login(c)
	if err != nil {
		return nil, err
	}
	info, err := l.configure(c)
	if err != nil {
		return nil, err
	}
	return &Player{Conn: c, Profile: prof, Info: info, Protocol: protocol, Address: host}, nil
}

// Versions this package speaks.
const (
	ProtocolVersion = 777
	GameVersion     = "26.3"
)

func (l *Listener) status(c *wire.Conn, protocol int32) error {
	for {
		id, body, err := c.ReadPacket()
		if err != nil {
			return err
		}
		switch id {
		case v777.ServerboundStatusStatusRequest:
			st := l.cfg.Status()
			resp := map[string]any{
				"version":     map[string]any{"name": GameVersion, "protocol": ProtocolVersion},
				"players":     map[string]any{"max": st.MaxPlayers, "online": st.Online},
				"description": map[string]any{"text": st.MOTD},
			}
			if st.Favicon != "" {
				resp["favicon"] = st.Favicon
			}
			js, _ := json.Marshal(resp)
			var w wire.Writer
			w.String(string(js))
			if err := c.Send(v777.ClientboundStatusStatusResponse, w.B); err != nil {
				return err
			}
		case v777.ServerboundStatusPingRequest:
			c.Send(v777.ClientboundStatusPongResponse, body) // echo the long
			return errStatusDone
		default:
			return fmt.Errorf("status: unexpected packet %#x", id)
		}
	}
}

func (l *Listener) loginDisconnect(c *wire.Conn, msg string) {
	js, _ := json.Marshal(map[string]string{"text": msg})
	var w wire.Writer
	w.String(string(js))
	c.Send(v777.ClientboundLoginLoginDisconnect, w.B)
}

// OfflineUUID is the UUID an offline-mode server gives a name: UUID v3 of "OfflinePlayer:<name>".
func OfflineUUID(name string) [16]byte {
	u := md5.Sum([]byte("OfflinePlayer:" + name))
	u[6] = u[6]&0x0f | 0x30
	u[8] = u[8]&0x3f | 0x80
	return u
}

func (l *Listener) login(c *wire.Conn) (Profile, error) {
	id, body, err := c.ReadPacket()
	if err != nil {
		return Profile{}, err
	}
	if id != v777.ServerboundLoginHello {
		return Profile{}, fmt.Errorf("login: expected hello, got %#x", id)
	}
	r := wire.NewReader(body)
	name := r.String(16)
	r.UUID()
	if r.Err != nil {
		return Profile{}, r.Err
	}
	if !validName(name) {
		l.loginDisconnect(c, "Invalid player name")
		return Profile{}, fmt.Errorf("login: invalid name %q", name)
	}
	prof := Profile{UUID: OfflineUUID(name), Name: name}
	if l.cfg.OnlineMode {
		var err error
		if prof, err = l.authenticate(c, name); err != nil {
			return Profile{}, err
		}
	}

	if t := l.cfg.CompressionThreshold; t >= 0 {
		var w wire.Writer
		w.VarInt(int32(t))
		if err := c.Send(v777.ClientboundLoginLoginCompression, w.B); err != nil {
			return Profile{}, err
		}
		c.SetThreshold(t)
	}
	var w wire.Writer
	writeProfile(&w, prof)
	var session [16]byte
	rand.Read(session[:])
	session[6] = session[6]&0x0f | 0x40
	session[8] = session[8]&0x3f | 0x80
	w.UUID(session)
	if err := c.Send(v777.ClientboundLoginLoginFinished, w.B); err != nil {
		return Profile{}, err
	}
	id, _, err = c.ReadPacket()
	if err != nil {
		return Profile{}, err
	}
	if id != v777.ServerboundLoginLoginAcknowledged {
		return Profile{}, fmt.Errorf("login: expected login_acknowledged, got %#x", id)
	}
	return prof, nil
}

func validName(n string) bool {
	if len(n) < 1 || len(n) > 16 {
		return false
	}
	for _, ch := range n {
		if ch <= ' ' || ch >= 0x7f {
			return false
		}
	}
	return true
}

func writeProfile(w *wire.Writer, p Profile) {
	w.UUID(p.UUID)
	w.String(p.Name)
	w.VarInt(int32(len(p.Properties)))
	for _, pr := range p.Properties {
		w.String(pr.Name)
		w.String(pr.Value)
		w.Bool(pr.Signature != "")
		if pr.Signature != "" {
			w.String(pr.Signature)
		}
	}
}

// configure runs the configuration phase and returns what the client said about itself.
func (l *Listener) configure(c *wire.Conn) (ClientInfo, error) {
	var info ClientInfo
	var w wire.Writer
	w.String("minecraft:brand")
	w.String(l.cfg.Brand)
	c.WritePacket(v777.ClientboundConfigurationCustomPayload, w.B)
	// Feature flags and the known-packs offer go first; the client answers with the packs it has.
	pk := v777.VanillaConfiguration()
	i := 0
	for ; i < len(pk) && pk[i].ID != v777.ClientboundConfigurationRegistryData; i++ {
		c.WritePacket(pk[i].ID, pk[i].Body)
	}
	if err := c.Flush(); err != nil {
		return info, err
	}
	sentRegistries := false
	for {
		id, body, err := c.ReadPacket()
		if err != nil {
			return info, err
		}
		r := wire.NewReader(body)
		switch id {
		case v777.ServerboundConfigurationClientInformation:
			info = ClientInfo{
				Locale: r.String(16), ViewDistance: r.Int8(), ChatMode: r.VarInt(), ChatColours: r.Bool(),
				SkinParts: r.Byte(), MainHand: r.VarInt(), TextFiltering: r.Bool(), AllowListing: r.Bool(),
				ParticleStatus: r.VarInt(),
			}
			if r.Err != nil {
				return info, fmt.Errorf("client_information: %w", r.Err)
			}
		case v777.ServerboundConfigurationSelectKnownPacks:
			n := int(r.VarInt())
			core := false
			for j := 0; j < n && r.Err == nil; j++ {
				ns, pid, ver := r.String(32767), r.String(32767), r.String(32767)
				if ns == "minecraft" && pid == "core" && ver == GameVersion {
					core = true
				}
			}
			if r.Err != nil {
				return info, fmt.Errorf("select_known_packs: %w", r.Err)
			}
			if !core {
				disconnect(c, v777.ClientboundConfigurationDisconnect, "Your client doesn't have the vanilla "+GameVersion+" data.")
				return info, errors.New("client lacks the vanilla core pack")
			}
			for ; i < len(pk); i++ {
				c.WritePacket(pk[i].ID, pk[i].Body)
			}
			c.WritePacket(v777.ClientboundConfigurationFinishConfiguration, nil)
			if err := c.Flush(); err != nil {
				return info, err
			}
			sentRegistries = true
		case v777.ServerboundConfigurationFinishConfiguration:
			if !sentRegistries {
				return info, errors.New("client finished configuration early")
			}
			return info, nil
		case v777.ServerboundConfigurationCustomPayload, v777.ServerboundConfigurationKeepAlive,
			v777.ServerboundConfigurationPong, v777.ServerboundConfigurationResourcePack:
			// Brand and plugin channels: nothing to do yet.
		default:
			return info, fmt.Errorf("configuration: unexpected packet %#x", id)
		}
	}
}

// disconnect sends a configuration or play disconnect with a plain text reason.
func disconnect(c *wire.Conn, id int32, msg string) {
	var w wire.Writer
	TextComponent(&w, msg)
	c.Send(id, w.B)
}

// TextComponent writes a plain text component in network NBT (a nameless string tag), the form
// configuration and play packets use for chat components.
func TextComponent(w *wire.Writer, text string) {
	w.Byte(8) // TAG_String
	writeModifiedUTF8(w, text)
}

// writeModifiedUTF8 writes Java's DataOutput.writeUTF format: u16 length, then UTF-8 with NUL as
// 0xC0 0x80 and supplementary characters as surrogate pairs.
func writeModifiedUTF8(w *wire.Writer, s string) {
	start := len(w.B)
	w.Uint16(0)
	for _, r := range s {
		switch {
		case r == 0:
			w.B = append(w.B, 0xc0, 0x80)
		case r < 0x80:
			w.B = append(w.B, byte(r))
		case r < 0x800:
			w.B = append(w.B, 0xc0|byte(r>>6), 0x80|byte(r&0x3f))
		case r < 0x10000:
			w.B = append(w.B, 0xe0|byte(r>>12), 0x80|byte(r>>6&0x3f), 0x80|byte(r&0x3f))
		default:
			r -= 0x10000
			for _, s := range []rune{0xd800 + r>>10, 0xdc00 + r&0x3ff} {
				w.B = append(w.B, 0xe0|byte(s>>12), 0x80|byte(s>>6&0x3f), 0x80|byte(s&0x3f))
			}
		}
	}
	n := len(w.B) - start - 2
	w.B[start] = byte(n >> 8)
	w.B[start+1] = byte(n)
}
