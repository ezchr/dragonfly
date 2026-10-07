package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/java/protocol/wire"
)

// Velocity modern forwarding: behind a Velocity proxy, the proxy authenticates players and tells the
// server who they are in a login custom query, signed with a secret both share (Velocity's
// forwarding.secret). The server then trusts the forwarded profile (UUID, name, skin) and address.
// Config.VelocitySecret turns it on; the listener must then only be reachable by the proxy.

const (
	velocityChannel = "velocity:player_info"
	// velocityForwardingVersion is MODERN_FORWARDING_DEFAULT: profile and address, no chat key.
	velocityForwardingVersion = 1
)

// velocityForward asks the proxy for the player's forwarded profile and checks its signature.
func (l *Listener) velocityForward(c *wire.Conn, deadline time.Time) (Profile, string, error) {
	var idBytes [4]byte
	_, _ = rand.Read(idBytes[:])
	queryID := int32(binary.BigEndian.Uint32(idBytes[:]) & 0x7fffffff)
	var w wire.Writer
	w.VarInt(queryID)
	w.String(velocityChannel)
	w.B = append(w.B, velocityForwardingVersion)
	if err := c.Send(v777.ClientboundLoginCustomQuery, w.B); err != nil {
		return Profile{}, "", err
	}
	id, body, err := c.ReadPacket()
	if err != nil {
		return Profile{}, "", err
	}
	if id != v777.ServerboundLoginCustomQueryAnswer {
		return Profile{}, "", fmt.Errorf("velocity: expected custom_query_answer, got %#x", id)
	}
	r := wire.NewReader(body)
	gotID := r.VarInt()
	has := r.Bool()
	if r.Err != nil {
		return Profile{}, "", r.Err
	}
	if gotID != queryID {
		return Profile{}, "", errors.New("velocity: answer to another query")
	}
	if !has {
		l.loginDisconnect(c, "This server requires you to connect through Velocity.")
		return Profile{}, "", errors.New("velocity: no forwarding data (not connected through Velocity, or modern forwarding is off)")
	}
	data := r.Rest()
	return parseVelocityForwarding(l.cfg.VelocitySecret, data)
}

// parseVelocityForwarding checks and reads a forwarding answer: an HMAC-SHA256 of the rest, then the
// version, the client's address, the profile's UUID, name and properties.
func parseVelocityForwarding(secret, data []byte) (Profile, string, error) {
	if len(data) < 32 {
		return Profile{}, "", errors.New("velocity: forwarding data too short")
	}
	sig, signed := data[:32], data[32:]
	mac := hmac.New(sha256.New, secret)
	mac.Write(signed)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return Profile{}, "", errors.New("velocity: forwarding signature does not match (forwarding secrets differ?)")
	}
	r := wire.NewReader(signed)
	ver := r.VarInt()
	addr := r.String(64)
	uid := r.UUID()
	name := r.String(16)
	n := r.VarInt()
	if r.Err != nil {
		return Profile{}, "", fmt.Errorf("velocity: %w", r.Err)
	}
	if ver < 1 {
		return Profile{}, "", fmt.Errorf("velocity: forwarding version %d", ver)
	}
	if n < 0 || n > 16 {
		return Profile{}, "", fmt.Errorf("velocity: %d profile properties", n)
	}
	p := Profile{UUID: uid, Name: name}
	for i := int32(0); i < n; i++ {
		var pr Property
		pr.Name = r.String(32767)
		pr.Value = r.String(32767)
		if r.Bool() {
			pr.Signature = r.String(32767)
		}
		p.Properties = append(p.Properties, pr)
	}
	if r.Err != nil {
		return Profile{}, "", fmt.Errorf("velocity: %w", r.Err)
	}
	if !validName(name) {
		return Profile{}, "", fmt.Errorf("velocity: invalid name %q", name)
	}
	return p, addr, nil
}
