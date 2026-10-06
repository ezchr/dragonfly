package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	v777 "github.com/ezchr/go-mc/java/v777"
	"github.com/ezchr/go-mc/java/wire"
)

// Known values from the Minecraft Wiki (protocol encryption page).
func TestSignedHex(t *testing.T) {
	for name, want := range map[string]string{
		"Notch": "4ed1f46bbe04bc756bcb17c0c7ce3e4632f06a48",
		"jeb_":  "-7c9d5b0044c130109a5d7b5fb5c317c02b4e28c1",
		"simon": "88e16a1019277b15d58faf0541e11910eb756f6",
	} {
		d := sha1.Sum([]byte(name))
		if got := signedHex(d[:]); got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
}

// TestOnlineLogin runs an encrypted online-mode login against a fake session server, doing the
// client's side of the handshake by hand.
func TestOnlineLogin(t *testing.T) {
	var gotHash string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHash = r.URL.Query().Get("serverId")
		if r.URL.Query().Get("username") != "Steve" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id": "069a79f444e94726a5befca90e38aaf5", "name": "Steve",
			"properties": []map[string]string{{"name": "textures", "value": "dGV4", "signature": "c2ln"}},
		})
	}))
	defer fake.Close()

	l, err := Listen("127.0.0.1:0", Config{OnlineMode: true, SessionServer: fake.URL, CompressionThreshold: 256})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	nc, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	c := wire.NewConn(nc)
	defer c.Close()
	var w wire.Writer
	w.VarInt(ProtocolVersion)
	w.String("localhost")
	w.Uint16(25565)
	w.VarInt(2)
	c.WritePacket(v777.ServerboundHandshakeIntention, w.B)
	w.Reset()
	w.String("Steve")
	w.UUID([16]byte{})
	c.Send(v777.ServerboundLoginHello, w.B)

	id, body, err := c.ReadPacket()
	if err != nil || id != v777.ClientboundLoginHello {
		t.Fatalf("encryption request: id %#x err %v", id, err)
	}
	r := wire.NewReader(body)
	serverID := r.String(20)
	pubDER := append([]byte(nil), r.ByteArray(1024)...)
	token := append([]byte(nil), r.ByteArray(16)...)
	if !r.Bool() || r.Err != nil {
		t.Fatalf("bad encryption request: %v", r.Err)
	}
	pubAny, err := x509.ParsePKIXPublicKey(pubDER)
	if err != nil {
		t.Fatal(err)
	}
	pub := pubAny.(*rsa.PublicKey)
	secret := make([]byte, 16)
	rand.Read(secret)
	encSecret, _ := rsa.EncryptPKCS1v15(rand.Reader, pub, secret)
	encToken, _ := rsa.EncryptPKCS1v15(rand.Reader, pub, token)
	w.Reset()
	w.ByteArray(encSecret)
	w.ByteArray(encToken)
	c.Send(v777.ServerboundLoginKey, w.B)
	c.EnableEncryption(secret)

	// After encryption: set compression, then login finished with the session server's profile.
	nc.SetDeadline(time.Now().Add(5 * time.Second))
	id, body, err = c.ReadPacket()
	if err != nil || id != v777.ClientboundLoginLoginCompression {
		t.Fatalf("expected compression, got %#x err %v", id, err)
	}
	c.SetThreshold(int(wire.NewReader(body).VarInt()))
	id, body, err = c.ReadPacket()
	if err != nil || id != v777.ClientboundLoginLoginFinished {
		t.Fatalf("expected login finished, got %#x err %v", id, err)
	}
	r = wire.NewReader(body)
	u := r.UUID()
	name := r.String(16)
	if fmt.Sprintf("%x", u) != "069a79f444e94726a5befca90e38aaf5" || name != "Steve" {
		t.Fatalf("profile %x %q", u, name)
	}
	if n := r.VarInt(); n != 1 || r.String(64) != "textures" || r.String(32767) != "dGV4" || !r.Bool() || r.String(1024) != "c2ln" {
		t.Fatalf("textures property not passed on (err %v)", r.Err)
	}
	if want := ServerHash(serverID, secret, pubDER); gotHash != want {
		t.Fatalf("session server got hash %q, want %q", gotHash, want)
	}
}
