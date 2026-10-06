package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	v777 "github.com/ezchr/go-mc/java/v777"
	"github.com/ezchr/go-mc/java/wire"
)

// DefaultSessionServer is Mojang's session server.
const DefaultSessionServer = "https://sessionserver.mojang.com"

// authKey is the RSA key pair used for the encryption handshake (vanilla uses 1024 bits).
type authKey struct {
	priv *rsa.PrivateKey
	pub  []byte // DER SubjectPublicKeyInfo, as sent to the client
}

func newAuthKey() (*authKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		return nil, err
	}
	pub, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return nil, err
	}
	return &authKey{priv: priv, pub: pub}, nil
}

// ServerHash is the hash both the client and the session server use to identify a login:
// SHA-1 of serverID, the shared secret and the public key, printed as a signed (two's
// complement) hex number without leading zeros, the way Java's BigInteger.toString(16) prints it.
func ServerHash(serverID string, secret, publicKey []byte) string {
	h := sha1.New()
	h.Write([]byte(serverID))
	h.Write(secret)
	h.Write(publicKey)
	return signedHex(h.Sum(nil))
}

func signedHex(digest []byte) string {
	n := new(big.Int).SetBytes(digest)
	if digest[0]&0x80 != 0 {
		// Negative in two's complement: subtract 2^(8*len).
		n.Sub(n, new(big.Int).Lsh(big.NewInt(1), uint(len(digest)*8)))
	}
	return n.Text(16)
}

// authenticate runs the encryption handshake and checks the login with the session server.
func (l *Listener) authenticate(c *wire.Conn, name string) (Profile, error) {
	var token [4]byte
	rand.Read(token[:])
	var w wire.Writer
	w.String("") // server id: empty since 1.7
	w.ByteArray(l.key.pub)
	w.ByteArray(token[:])
	w.Bool(true) // should authenticate
	if err := c.Send(v777.ClientboundLoginHello, w.B); err != nil {
		return Profile{}, err
	}
	id, body, err := c.ReadPacket()
	if err != nil {
		return Profile{}, err
	}
	if id != v777.ServerboundLoginKey {
		return Profile{}, fmt.Errorf("login: expected encryption response, got %#x", id)
	}
	r := wire.NewReader(body)
	encSecret := r.ByteArray(256)
	encToken := r.ByteArray(256)
	if r.Err != nil {
		return Profile{}, r.Err
	}
	gotToken, err := rsa.DecryptPKCS1v15(rand.Reader, l.key.priv, encToken)
	if err != nil || !bytes.Equal(gotToken, token[:]) {
		return Profile{}, errors.New("login: verify token mismatch")
	}
	secret, err := rsa.DecryptPKCS1v15(rand.Reader, l.key.priv, encSecret)
	if err != nil || len(secret) != 16 {
		return Profile{}, errors.New("login: bad shared secret")
	}
	if err := c.EnableEncryption(secret); err != nil {
		return Profile{}, err
	}
	var ip string
	if l.cfg.PreventProxyConnections {
		ip, _, _ = net.SplitHostPort(c.NetConn().RemoteAddr().String())
	}
	prof, err := l.hasJoined(name, ServerHash("", secret, l.key.pub), ip)
	if err != nil {
		l.loginDisconnect(c, "Failed to verify username!")
		return Profile{}, err
	}
	return prof, nil
}

// hasJoined asks the session server whether name logged in to this server (hash).
func (l *Listener) hasJoined(name, hash, ip string) (Profile, error) {
	q := url.Values{"username": {name}, "serverId": {hash}}
	if ip != "" {
		q.Set("ip", ip)
	}
	base := strings.TrimRight(l.cfg.SessionServer, "/")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/session/minecraft/hasJoined?"+q.Encode(), nil)
	if err != nil {
		return Profile{}, err
	}
	resp, err := l.http.Do(req)
	if err != nil {
		return Profile{}, fmt.Errorf("session server: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Profile{}, fmt.Errorf("session server: %s did not log in to this server (HTTP %d)", name, resp.StatusCode)
	}
	var p struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Properties []struct {
			Name      string `json:"name"`
			Value     string `json:"value"`
			Signature string `json:"signature"`
		} `json:"properties"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<16)).Decode(&p); err != nil {
		return Profile{}, fmt.Errorf("session server: %w", err)
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(p.ID, "-", ""))
	if err != nil || len(raw) != 16 {
		return Profile{}, fmt.Errorf("session server: bad id %q", p.ID)
	}
	prof := Profile{Name: p.Name}
	copy(prof.UUID[:], raw)
	for _, pr := range p.Properties {
		prof.Properties = append(prof.Properties, Property{Name: pr.Name, Value: pr.Value, Signature: pr.Signature})
	}
	return prof, nil
}
