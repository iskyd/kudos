package api

import (
	"errors"
	"strings"
	"testing"
)

// cheapArgon keeps tests fast; production parameters are covered by TestDefaultArgon.
func cheapArgon() argonParams { return argonParams{memory: 64, time: 1, threads: 1} }

func TestHashAndVerifyPassword(t *testing.T) {
	t.Parallel()

	hash := hashPassword(cheapArgon(), "correct horse")
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Fatalf("hash = %q, want PHC argon2id", hash)
	}
	if hash == hashPassword(cheapArgon(), "correct horse") {
		t.Fatal("two hashes of one password are equal: salt is not random")
	}

	tests := []struct {
		name     string
		password string
		want     bool
	}{
		{name: "correct", password: "correct horse", want: true},
		{name: "wrong", password: "correct horsE"},
		{name: "empty", password: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := verifyPassword(hash, tt.password)
			if err != nil || got != tt.want {
				t.Fatalf("verifyPassword = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestVerifyPasswordMalformed(t *testing.T) {
	t.Parallel()

	tests := []string{
		"",
		"plain",
		"$argon2i$v=19$m=64,t=1,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=18$m=64,t=1,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=x,t=1,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=64,t=1,p=1$!!$aGFzaA",
		"$argon2id$v=19$m=64,t=1,p=1$c2FsdA$!!",
		"$argon2id$v=19$m=64,t=1,p=1$c2FsdA$",
	}
	for _, hash := range tests {
		t.Run(hash, func(t *testing.T) {
			t.Parallel()
			if _, err := verifyPassword(hash, "x"); !errors.Is(err, errMalformedHash) {
				t.Fatalf("verifyPassword(%q) error = %v, want errMalformedHash", hash, err)
			}
		})
	}
}

func TestDefaultArgon(t *testing.T) {
	t.Parallel()

	if p := defaultArgon(); p.memory != 64*1024 || p.time != 3 || p.threads != 4 {
		t.Fatalf("defaultArgon = %+v, want RFC 9106 (64 MiB, t=3, p=4)", p)
	}
	if s := NewServer(nil, nil, nil, Garmin{}, true); !strings.Contains(s.dummyHash, "$m=65536,t=3,p=4$") {
		t.Fatalf("NewServer dummy hash = %q, want production parameters", s.dummyHash)
	}
}
