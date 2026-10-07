package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"testing"

	"github.com/df-mc/dragonfly/server/java/protocol/wire"
)

func velocityData(secret []byte, name string) []byte {
	var w wire.Writer
	w.VarInt(1)
	w.String("203.0.113.7")
	w.UUID([16]byte{1, 2, 3})
	w.String(name)
	w.VarInt(1)
	w.String("textures")
	w.String("dGV4dHVyZXM=")
	w.Bool(true)
	w.String("c2ln")
	mac := hmac.New(sha256.New, secret)
	mac.Write(w.B)
	return append(mac.Sum(nil), w.B...)
}

func TestVelocityForwarding(t *testing.T) {
	secret := []byte("s3cret")
	p, addr, err := parseVelocityForwarding(secret, velocityData(secret, "Steve"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Steve" || p.UUID != [16]byte{1, 2, 3} || addr != "203.0.113.7" {
		t.Fatalf("got %+v %s", p, addr)
	}
	if len(p.Properties) != 1 || p.Properties[0].Name != "textures" || p.Properties[0].Signature != "c2ln" {
		t.Fatalf("properties %+v", p.Properties)
	}
	if _, _, err := parseVelocityForwarding([]byte("other"), velocityData(secret, "Steve")); err == nil {
		t.Fatal("a wrong secret was accepted")
	}
	d := velocityData(secret, "Steve")
	d[len(d)-1] ^= 1
	if _, _, err := parseVelocityForwarding(secret, d); err == nil {
		t.Fatal("tampered data was accepted")
	}
}
