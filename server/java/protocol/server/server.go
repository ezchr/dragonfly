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
	"unicode/utf8"

	"github.com/df-mc/dragonfly/server/java/protocol/text"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/java/protocol/version"
	"github.com/df-mc/dragonfly/server/java/protocol/wire"
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
	// LoginTimeout bounds the whole of login and configuration, from accepting the connection to
	// handing the player to the game (30 s if zero). It includes the session server check.
	LoginTimeout time.Duration
	// HandshakeTimeout bounds the handshake and the whole status (server list) exchange (5 s if
	// zero). A login then gets the rest of LoginTimeout.
	HandshakeTimeout time.Duration
	Log              *slog.Logger

	// MaxPending caps the connections that have not finished login and configuration, server wide
	// (256 if zero; negative: no cap). Connections over the cap are closed at once.
	MaxPending int
	// MaxPendingPerIP caps them per client address (an IPv6 /64 counts as one address; 8 if zero,
	// negative: no cap).
	MaxPendingPerIP int
	// LoginsPerIPPerMinute caps the login attempts (handshakes with the login or transfer intent)
	// per address in any minute (20 if zero, negative: no cap). Status pings do not count.
	LoginsPerIPPerMinute int
	// AcceptTransfers accepts clients sent here by another server's transfer packet (handshake
	// intent 3), like vanilla's accepts-transfers. Off: they are refused, as vanilla does.
	AcceptTransfers bool

	// OnlineMode checks every login with the session server (Microsoft accounts only, encrypted
	// connection, real UUIDs and signed skins). Off: anyone can join under any name.
	OnlineMode bool
	// SessionServer is the session server's base URL (DefaultSessionServer if empty).
	SessionServer string
	// PreventProxyConnections also sends the client's IP to the session server, which then
	// refuses logins from a different address than the one the client authenticated from.
	PreventProxyConnections bool
	// MaxConcurrentAuth caps the session server requests in flight at once (8 if zero). Logins
	// over it wait for a free slot, within their LoginTimeout.
	MaxConcurrentAuth int
	// AuthPerMinute caps the session server requests per minute, server wide (300 if zero,
	// negative: no cap), so a flood of logins (no Mojang account is needed to reach this step)
	// cannot get the server's address rate limited by the session server, which would lock real
	// players out. Logins over it are refused with "try again".
	AuthPerMinute int
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
	// Version is the client's protocol version: the server writes v777 ids and remaps them with it.
	Version *version.Version
	Address string // what the client typed to connect (host)
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
	limits  *limiter
	authSem chan struct{}  // MaxConcurrentAuth slots
	authLim *windowCounter // AuthPerMinute
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
	if cfg.HandshakeTimeout == 0 {
		cfg.HandshakeTimeout = 5 * time.Second
	}
	cfg.HandshakeTimeout = min(cfg.HandshakeTimeout, cfg.LoginTimeout)
	cfg.MaxPending = orDefault(cfg.MaxPending, 256)
	cfg.MaxPendingPerIP = orDefault(cfg.MaxPendingPerIP, 8)
	cfg.LoginsPerIPPerMinute = orDefault(cfg.LoginsPerIPPerMinute, 20)
	cfg.MaxConcurrentAuth = orDefault(cfg.MaxConcurrentAuth, 8)
	cfg.AuthPerMinute = orDefault(cfg.AuthPerMinute, 300)
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
		http:    &http.Client{Timeout: 15 * time.Second},
		limits:  newLimiter(cfg.MaxPending, cfg.MaxPendingPerIP, cfg.LoginsPerIPPerMinute),
		authSem: make(chan struct{}, max(cfg.MaxConcurrentAuth, 1)),
		authLim: newWindowCounter(cfg.AuthPerMinute, time.Minute)}
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
		ip := ipKey(nc.RemoteAddr())
		if !l.limits.acquire(ip) {
			// Over a pending cap: drop it before spending a goroutine or buffers on it.
			nc.Close()
			continue
		}
		go l.handle(nc, ip)
	}
}

func (l *Listener) handle(nc net.Conn, ip string) {
	start := time.Now()
	nc.SetDeadline(start.Add(l.cfg.HandshakeTimeout))
	c := wire.NewConn(nc)
	// Login and configuration packets are small; the play limits apply from the hand-off.
	c.SetMaxPacket(maxPrePlayPacket)
	p, err := l.negotiate(c, ip, start.Add(l.cfg.LoginTimeout))
	l.limits.release(ip)
	if err != nil {
		if !errors.Is(err, errStatusDone) && !errors.Is(err, io.EOF) {
			l.cfg.Log.Debug("java login failed", "addr", nc.RemoteAddr(), "err", err)
		}
		c.Close()
		return
	}
	c.SetMaxPacket(0)
	nc.SetDeadline(time.Time{})
	select {
	case l.players <- p:
	case <-l.ctx.Done():
		c.Close()
	}
}

// maxPrePlayPacket is the largest packet accepted before play. The largest a vanilla client sends
// then is a custom_payload (32767 bytes of data).
const maxPrePlayPacket = 1 << 16

var errStatusDone = errors.New("status ping finished")

// negotiate runs handshake, then status or login + configuration. deadline is the login deadline.
func (l *Listener) negotiate(c *wire.Conn, ip string, deadline time.Time) (*Player, error) {
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
	case 2: // login
	case 3: // transfer
		if !l.cfg.AcceptTransfers {
			l.loginDisconnect(c, "Transfers are disabled on this server.")
			return nil, errors.New("transfer refused: AcceptTransfers is off")
		}
	default:
		return nil, fmt.Errorf("unknown intent %d", intent)
	}
	// Login and configuration share one deadline, counted from when the connection was accepted.
	c.NetConn().SetDeadline(deadline)
	if !l.limits.loginAttempt(ip) {
		l.loginDisconnect(c, "Too many login attempts from your address. Please wait a minute.")
		return nil, errors.New("too many login attempts from " + ip)
	}
	ver := version.ByProtocol(protocol)
	if ver == nil {
		msg := fmt.Sprintf("This server runs Minecraft %s. Please use %s.", versionNames(), versionNames())
		if protocol < version.All[len(version.All)-1].Protocol {
			msg = fmt.Sprintf("Outdated client! Please use %s.", versionNames())
		}
		l.loginDisconnect(c, msg)
		return nil, fmt.Errorf("protocol %d not supported", protocol)
	}
	prof, err := l.login(c, deadline)
	if err != nil {
		return nil, err
	}
	info, err := l.configure(c, ver)
	if err != nil {
		return nil, err
	}
	return &Player{Conn: c, Profile: prof, Info: info, Protocol: protocol, Version: ver, Address: host}, nil
}

// The newest version this package speaks; version.All lists every version clients may join with.
const (
	ProtocolVersion = 777
	GameVersion     = "26.3"
)

// versionNames is the supported versions for messages: "26.2 or 26.3".
func versionNames() string {
	s := ""
	for i := len(version.All) - 1; i >= 0; i-- {
		if s != "" {
			s += " or "
		}
		s += version.All[i].Name
	}
	return s
}

// status answers the server list: one status_request, then a ping (vanilla's
// ServerStatusPacketListenerImpl). A second status_request or a malformed ping ends the connection.
func (l *Listener) status(c *wire.Conn, protocol int32) error {
	answered := false
	for {
		id, body, err := c.ReadPacket()
		if err != nil {
			return err
		}
		switch id {
		case v777.ServerboundStatusStatusRequest:
			if answered || len(body) != 0 {
				return errors.New("status: repeated or malformed status_request")
			}
			answered = true
			st := l.cfg.Status()
			resp := map[string]any{
				"version":     statusVersion(protocol),
				"players":     map[string]any{"max": st.MaxPlayers, "online": st.Online},
				"description": map[string]any{"text": st.MOTD},
				// Chat is not signed (login says enforces_secure_chat false): say so up front.
				"enforcesSecureChat": false,
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
			if len(body) != 8 {
				return fmt.Errorf("status: ping of %d bytes, want a long", len(body))
			}
			c.Send(v777.ClientboundStatusPongResponse, body) // echo the long
			return errStatusDone
		default:
			return fmt.Errorf("status: unexpected packet %#x", id)
		}
	}
}

// loginDisconnect sends a login disconnect with a plain text reason (cut to a length the client
// accepts, on a character boundary).
func (l *Listener) loginDisconnect(c *wire.Conn, msg string) {
	js, _ := json.Marshal(map[string]string{"text": truncateUTF8(msg, maxReasonBytes)})
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

func (l *Listener) login(c *wire.Conn, deadline time.Time) (Profile, error) {
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
		if prof, err = l.authenticate(c, name, deadline); err != nil {
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
func (l *Listener) configure(c *wire.Conn, ver *version.Version) (ClientInfo, error) {
	var info ClientInfo
	var w wire.Writer
	w.String("minecraft:brand")
	w.String(l.cfg.Brand)
	c.WritePacket(ver.ClientboundConfig(v777.ClientboundConfigurationCustomPayload), w.B)
	// Feature flags and the known-packs offer go first; the client answers with the packs it has.
	// The vanilla packets already have the version's own ids.
	pk := ver.Configuration()
	registryData := ver.ClientboundConfig(v777.ClientboundConfigurationRegistryData)
	i := 0
	for ; i < len(pk) && pk[i].ID != registryData; i++ {
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
		switch ver.ServerboundConfig(id) {
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
			if sentRegistries {
				// Vanilla disconnects too: each answer would make us send (and compress) every
				// registry again.
				return info, errors.New("select_known_packs: sent twice")
			}
			n := int(r.VarInt())
			if n < 0 || n > 64 { // vanilla's limit
				return info, fmt.Errorf("select_known_packs: %d packs", n)
			}
			core := false
			for j := 0; j < n && r.Err == nil; j++ {
				ns, pid, pv := r.String(32767), r.String(32767), r.String(32767)
				if ns == "minecraft" && pid == "core" && pv == ver.Name {
					core = true
				}
			}
			if r.Err != nil {
				return info, fmt.Errorf("select_known_packs: %w", r.Err)
			}
			if !core {
				disconnect(c, ver.ClientboundConfig(v777.ClientboundConfigurationDisconnect), "Your client doesn't have the vanilla "+ver.Name+" data.")
				return info, errors.New("client lacks the vanilla core pack")
			}
			for ; i < len(pk); i++ {
				c.WritePacket(pk[i].ID, pk[i].Body)
			}
			c.WritePacket(ver.ClientboundConfig(v777.ClientboundConfigurationFinishConfiguration), nil)
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

// maxReasonBytes is how much of a login disconnect reason is sent: far more than a screen shows,
// and well inside the client's limit (a 262144-character JSON string).
const maxReasonBytes = 16384

// TextComponent writes a plain text component in network NBT (a nameless string tag), the form
// configuration and play packets use for chat components. A text too long for an NBT string
// (65535 bytes of modified UTF-8) is cut on a character boundary.
func TextComponent(w *wire.Writer, s string) {
	text.WriteString(w, s)
}

// truncateUTF8 cuts s to at most n bytes without splitting a UTF-8 sequence.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func orDefault(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

// statusVersion is the version a status reply announces: the client's own when it can join, so
// the server list shows it as compatible.
func statusVersion(protocol int32) map[string]any {
	if v := version.ByProtocol(protocol); v != nil {
		return map[string]any{"name": v.Name, "protocol": v.Protocol}
	}
	return map[string]any{"name": versionNames(), "protocol": ProtocolVersion}
}
