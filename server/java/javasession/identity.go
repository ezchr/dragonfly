package javasession

import (
	"crypto/md5"
	"hash/fnv"
	"math"
	"strconv"

	jserver "github.com/df-mc/dragonfly/server/java/protocol/server"
	"github.com/google/uuid"
)

// Identity gives a Java player the UUID and XUID Dragonfly knows them by.
type Identity func(p jserver.Profile) (id uuid.UUID, xuid string)

// ViaBedrockIdentity is the identity ViaBedrock gave Java players: an XUID made from the username
// (abs of FNV-1 64, as Java's Math.abs on a long) and the UUID Bedrock derives from an XUID. Using
// it keeps the saved data, teams and scores of players who joined through ViaProxy before.
//
// A Floodgate player (a Bedrock player a Velocity proxy forwards through Geyser) has a UUID of zeros
// and their XUID: they get the identity a Bedrock player with that XUID has on this server, so their
// data is the same whichever way they come in.
func ViaBedrockIdentity(p jserver.Profile) (uuid.UUID, string) {
	if xuid, ok := floodgateXUID(p.UUID); ok {
		return xuidUUID(xuid), xuid
	}
	h := fnv.New64() // FNV-1, like ViaBedrock's Fnv1.fnv1_64
	h.Write([]byte(p.Name))
	v := int64(h.Sum64())
	if v < 0 && v != math.MinInt64 { // Math.abs(Long.MIN_VALUE) stays negative in Java
		v = -v
	}
	xuid := strconv.FormatInt(v, 10)
	return xuidUUID(xuid), xuid
}

// floodgateXUID reads the XUID out of a Floodgate UUID (the top half zero, the XUID below).
func floodgateXUID(u [16]byte) (string, bool) {
	for _, b := range u[:8] {
		if b != 0 {
			return "", false
		}
	}
	var x uint64
	for _, b := range u[8:] {
		x = x<<8 | uint64(b)
	}
	if x == 0 {
		return "", false
	}
	return strconv.FormatUint(x, 10), true
}

// xuidUUID is UUID.nameUUIDFromBytes("pocket-auth-1-xuid:" + xuid).
func xuidUUID(xuid string) uuid.UUID {
	u := md5.Sum([]byte("pocket-auth-1-xuid:" + xuid))
	u[6] = u[6]&0x0f | 0x30
	u[8] = u[8]&0x3f | 0x80
	return uuid.UUID(u)
}

// JavaIdentity uses the Java profile's own UUID and no XUID.
func JavaIdentity(p jserver.Profile) (uuid.UUID, string) { return uuid.UUID(p.UUID), "" }
