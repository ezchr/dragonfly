package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ezchr/go-mc/java/text"
	v777 "github.com/ezchr/go-mc/java/v777"
	"github.com/ezchr/go-mc/java/wire"
)

func listen(t *testing.T, cfg Config) *Listener {
	t.Helper()
	l, err := Listen("127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() { // a game that takes every player and drops it
		for {
			p, err := l.Accept()
			if err != nil {
				return
			}
			p.Conn.Close()
		}
	}()
	return l
}

func dial(t *testing.T, l *Listener) *wire.Conn {
	t.Helper()
	nc, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	nc.SetDeadline(time.Now().Add(10 * time.Second))
	c := wire.NewConn(nc)
	t.Cleanup(func() { c.Close() })
	return c
}

func handshake(c *wire.Conn, intent int32) {
	var w wire.Writer
	w.VarInt(ProtocolVersion)
	w.String("localhost")
	w.Uint16(25565)
	w.VarInt(intent)
	c.WritePacket(v777.ServerboundHandshakeIntention, w.B)
}

// closedSoon reports whether the server closes c (a read ends without a packet) within d.
func closedSoon(c *wire.Conn, d time.Duration) bool {
	c.NetConn().SetReadDeadline(time.Now().Add(d))
	for {
		_, _, err := c.ReadPacket()
		if err != nil {
			ne, ok := err.(net.Error)
			return !(ok && ne.Timeout())
		}
	}
}

func TestStatusOnce(t *testing.T) {
	l := listen(t, Config{CompressionThreshold: -1})
	// One status, then a second request: answered once, then closed.
	c := dial(t, l)
	handshake(c, 1)
	c.WritePacket(v777.ServerboundStatusStatusRequest, nil)
	c.Send(v777.ServerboundStatusStatusRequest, nil)
	if id, _, err := c.ReadPacket(); err != nil || id != v777.ClientboundStatusStatusResponse {
		t.Fatalf("first status: %#x %v", id, err)
	}
	if _, _, err := c.ReadPacket(); err == nil {
		t.Fatal("second status_request answered")
	}
	// A ping that is not a long gets no pong.
	c = dial(t, l)
	handshake(c, 1)
	c.Send(v777.ServerboundStatusPingRequest, make([]byte, 1000))
	if id, _, err := c.ReadPacket(); err == nil {
		t.Fatalf("1000-byte ping answered with %#x", id)
	}
	// The normal exchange still works.
	c = dial(t, l)
	handshake(c, 1)
	c.Send(v777.ServerboundStatusStatusRequest, nil)
	if id, _, err := c.ReadPacket(); err != nil || id != v777.ClientboundStatusStatusResponse {
		t.Fatalf("status: %#x %v", id, err)
	}
	c.Send(v777.ServerboundStatusPingRequest, []byte{1, 2, 3, 4, 5, 6, 7, 8})
	if id, body, err := c.ReadPacket(); err != nil || id != v777.ClientboundStatusPongResponse || len(body) != 8 {
		t.Fatalf("pong: %#x %d %v", id, len(body), err)
	}
}

// loginOffline runs an offline login up to configuration.
func loginOffline(t *testing.T, c *wire.Conn, name string) {
	t.Helper()
	var w wire.Writer
	w.String(name)
	w.UUID([16]byte{})
	c.Send(v777.ServerboundLoginHello, w.B)
	id, _, err := c.ReadPacket()
	if err != nil || id != v777.ClientboundLoginLoginFinished {
		t.Fatalf("login finished: %#x %v", id, err)
	}
	c.Send(v777.ServerboundLoginLoginAcknowledged, nil)
}

func sendKnownPacks(c *wire.Conn) {
	var w wire.Writer
	w.VarInt(1)
	w.String("minecraft")
	w.String("core")
	w.String(GameVersion)
	c.Send(v777.ServerboundConfigurationSelectKnownPacks, w.B)
}

func TestKnownPacksOnce(t *testing.T) {
	l := listen(t, Config{CompressionThreshold: -1})
	c := dial(t, l)
	handshake(c, 2)
	loginOffline(t, c, "Steve")
	sendKnownPacks(c)
	for {
		id, _, err := c.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if id == v777.ClientboundConfigurationFinishConfiguration {
			break
		}
	}
	sendKnownPacks(c)
	n := 0
	for {
		_, _, err := c.ReadPacket()
		if err != nil {
			break
		}
		n++
	}
	if n != 0 {
		t.Fatalf("second select_known_packs answered with %d packets", n)
	}
}

func TestTransferIntent(t *testing.T) {
	l := listen(t, Config{CompressionThreshold: -1})
	c := dial(t, l)
	handshake(c, 3)
	loginHello(c, "Steve")
	if id, _, err := c.ReadPacket(); err != nil || id != v777.ClientboundLoginLoginDisconnect {
		t.Fatalf("transfer: expected a disconnect, got %#x %v", id, err)
	}
	l2 := listen(t, Config{CompressionThreshold: -1, AcceptTransfers: true})
	c = dial(t, l2)
	handshake(c, 3)
	loginOffline(t, c, "Steve")
}

func loginHello(c *wire.Conn, name string) {
	var w wire.Writer
	w.String(name)
	w.UUID([16]byte{})
	c.Send(v777.ServerboundLoginHello, w.B)
}

func TestPendingCaps(t *testing.T) {
	l := listen(t, Config{CompressionThreshold: -1, MaxPending: 2})
	a, b := dial(t, l), dial(t, l)
	time.Sleep(100 * time.Millisecond) // let the accept loop count them
	c := dial(t, l)
	if !closedSoon(c, 2*time.Second) {
		t.Fatal("third pending connection kept with MaxPending 2")
	}
	if closedSoon(a, 200*time.Millisecond) || closedSoon(b, 200*time.Millisecond) {
		t.Fatal("pending connections under the cap closed")
	}
	// Finishing one frees its slot.
	a.Close()
	time.Sleep(100 * time.Millisecond)
	d := dial(t, l)
	handshake(d, 1)
	d.Send(v777.ServerboundStatusStatusRequest, nil)
	if id, _, err := d.ReadPacket(); err != nil || id != v777.ClientboundStatusStatusResponse {
		t.Fatalf("slot not freed: %#x %v", id, err)
	}
}

func TestLimiterPerIP(t *testing.T) {
	now := time.Unix(1000, 0)
	lim := newLimiter(100, 2, 3)
	lim.now = func() time.Time { return now }
	if !lim.acquire("1.2.3.4") || !lim.acquire("1.2.3.4") {
		t.Fatal("first two refused")
	}
	if lim.acquire("1.2.3.4") {
		t.Fatal("third pending connection from one address accepted")
	}
	if !lim.acquire("5.6.7.8") {
		t.Fatal("other address refused")
	}
	lim.release("1.2.3.4")
	if !lim.acquire("1.2.3.4") {
		t.Fatal("released slot not reusable")
	}
	for i := 0; i < 3; i++ {
		if !lim.loginAttempt("1.2.3.4") {
			t.Fatalf("login %d refused", i)
		}
	}
	if lim.loginAttempt("1.2.3.4") {
		t.Fatal("fourth login in a minute accepted")
	}
	now = now.Add(61 * time.Second)
	if !lim.loginAttempt("1.2.3.4") {
		t.Fatal("login refused after the minute")
	}
	// Loopback (a local proxy) is only under the global cap.
	for i := 0; i < 10; i++ {
		if !lim.acquire("127.0.0.1") || !lim.loginAttempt("127.0.0.1") {
			t.Fatal("loopback limited per address")
		}
	}
	// Idle addresses are forgotten.
	for _, ip := range []string{"1.2.3.4", "1.2.3.4", "5.6.7.8"} {
		lim.release(ip)
	}
	now = now.Add(2 * time.Minute)
	lim.acquire("9.9.9.9")
	if _, ok := lim.ips["5.6.7.8"]; ok {
		t.Fatal("idle address kept")
	}
	if ipKey(&net.TCPAddr{IP: net.ParseIP("2001:db8:1:2:aaaa::1")}) != ipKey(&net.TCPAddr{IP: net.ParseIP("2001:db8:1:2:bbbb::2")}) {
		t.Fatal("one IPv6 /64 counted as two addresses")
	}
	if k := ipKey(&net.TCPAddr{IP: net.ParseIP("::1")}); !isLoopback(k) {
		t.Fatalf("::1 key %q not loopback", k)
	}
}

func TestHandshakeTimeout(t *testing.T) {
	l := listen(t, Config{CompressionThreshold: -1, HandshakeTimeout: 300 * time.Millisecond})
	c := dial(t, l)
	start := time.Now()
	if !closedSoon(c, 3*time.Second) {
		t.Fatal("silent connection not closed")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("closed after %v", d)
	}
}

// onlineClient does the client side of an online login up to the session server check and
// returns the first packet after it.
func onlineClient(t *testing.T, l *Listener, name string) int32 {
	c := dial(t, l)
	handshake(c, 2)
	loginHello(c, name)
	id, body, err := c.ReadPacket()
	if err != nil || id != v777.ClientboundLoginHello {
		t.Errorf("encryption request: %#x %v", id, err)
		return -1
	}
	r := wire.NewReader(body)
	r.String(20)
	pubDER := append([]byte(nil), r.ByteArray(1024)...)
	token := append([]byte(nil), r.ByteArray(16)...)
	pubAny, err := x509.ParsePKIXPublicKey(pubDER)
	if err != nil {
		t.Error(err)
		return -1
	}
	secret := make([]byte, 16)
	rand.Read(secret)
	encSecret, _ := rsa.EncryptPKCS1v15(rand.Reader, pubAny.(*rsa.PublicKey), secret)
	encToken, _ := rsa.EncryptPKCS1v15(rand.Reader, pubAny.(*rsa.PublicKey), token)
	var w wire.Writer
	w.ByteArray(encSecret)
	w.ByteArray(encToken)
	c.Send(v777.ServerboundLoginKey, w.B)
	c.EnableEncryption(secret)
	id, _, err = c.ReadPacket()
	if err != nil {
		return -1
	}
	return id
}

// TestAuthLimits: the session server sees at most MaxConcurrentAuth requests at once and at most
// AuthPerMinute a minute; logins over the rate are told to try again.
func TestAuthLimits(t *testing.T) {
	var inFlight, peak, total atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		total.Add(1)
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
		inFlight.Add(-1)
		json.NewEncoder(w).Encode(map[string]any{"id": "069a79f444e94726a5befca90e38aaf5", "name": r.URL.Query().Get("username")})
	}))
	defer fake.Close()
	l := listen(t, Config{OnlineMode: true, SessionServer: fake.URL, CompressionThreshold: -1,
		MaxConcurrentAuth: 2, AuthPerMinute: 5})
	var wg sync.WaitGroup
	var finished, refused atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch onlineClient(t, l, "Steve") {
			case v777.ClientboundLoginLoginFinished:
				finished.Add(1)
			case v777.ClientboundLoginLoginDisconnect:
				refused.Add(1)
			}
		}()
	}
	wg.Wait()
	if peak.Load() > 2 {
		t.Errorf("%d session server requests at once, cap 2", peak.Load())
	}
	if total.Load() != 5 || finished.Load() != 5 || refused.Load() != 3 {
		t.Errorf("session server got %d requests; %d logins finished, %d refused (want 5, 5, 3)",
			total.Load(), finished.Load(), refused.Load())
	}
}

func TestDisconnectTruncation(t *testing.T) {
	long := strings.Repeat("é", 40000) + strings.Repeat("\U0001F600", 10000) // > 65535 bytes
	var w wire.Writer
	TextComponent(&w, long)
	r := wire.NewReader(w.B)
	if r.Byte() != text.TagString {
		t.Fatal("not a string tag")
	}
	n := int(r.Uint16())
	if n != r.Len() || n > 0xffff {
		t.Fatalf("length field %d, %d bytes follow", n, r.Len())
	}
	// Modified UTF-8 of 'é' is 2 bytes: a cut never leaves half of one.
	if n%2 != 0 {
		t.Fatalf("cut inside a character: %d bytes", n)
	}
	for _, s := range []string{strings.Repeat("a", 20000), strings.Repeat("é", 9000), strings.Repeat("\U0001F600", 5000)} {
		got := truncateUTF8(s, maxReasonBytes)
		if len(got) > maxReasonBytes || !utf8.ValidString(got) || !strings.HasPrefix(s, got) || len(got) < maxReasonBytes-3 {
			t.Fatalf("truncateUTF8: %d bytes, valid %v", len(got), utf8.ValidString(got))
		}
	}
	// The login disconnect of an over-long reason still decodes as a JSON string.
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	l := &Listener{}
	go l.loginDisconnect(wire.NewConn(a), strings.Repeat("é", 100000))
	id, body, err := wire.NewConn(b).ReadPacket()
	if err != nil || id != v777.ClientboundLoginLoginDisconnect {
		t.Fatalf("%#x %v", id, err)
	}
	var m map[string]string
	js := wire.NewReader(body).String(262144)
	if err := json.Unmarshal([]byte(js), &m); err != nil || !utf8.ValidString(m["text"]) || len(m["text"]) > maxReasonBytes {
		t.Fatalf("reason: %v, %d bytes", err, len(m["text"]))
	}
}
