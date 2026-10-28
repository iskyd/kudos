package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argonParams are argon2id cost parameters. Production uses defaultArgon;
// every hash records its own parameters, so they can change without a migration.
type argonParams struct {
	memory  uint32 // KiB
	time    uint32
	threads uint8
}

// defaultArgon is RFC 9106's second recommended option (64 MiB, 3 passes).
func defaultArgon() argonParams { return argonParams{memory: 64 * 1024, time: 3, threads: 4} }

const (
	argonSaltLen = 16
	argonKeyLen  = 32
)

var errMalformedHash = errors.New("malformed password hash")

// hashPassword returns password hashed with argon2id in PHC string format.
func hashPassword(p argonParams, password string) string {
	salt := make([]byte, argonSaltLen)
	_, _ = rand.Read(salt) // never fails: crypto/rand.Read panics instead of returning an error
	key := argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.memory, p.time, p.threads, b64.EncodeToString(salt), b64.EncodeToString(key))
}

// verifyPassword reports whether password matches the PHC-encoded hash.
func verifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != fmt.Sprintf("v=%d", argon2.Version) {
		return false, errMalformedHash
	}
	var p argonParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return false, errMalformedHash
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, errMalformedHash
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, errMalformedHash
	}
	got := argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, uint32(len(want))) //nolint:gosec // len(want) is a decoded hash length, far below 2^32
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
