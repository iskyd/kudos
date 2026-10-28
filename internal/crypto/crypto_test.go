package crypto

import (
	"bytes"
	"errors"
	"testing"
)

func key(b byte) []byte { return bytes.Repeat([]byte{b}, KeySize) }

func mustNew(t *testing.T, k []byte) *Cipher {
	t.Helper()
	c, err := New(k)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNewKeySize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		key     []byte
		wantErr error
	}{
		{name: "32 bytes", key: key(1)},
		{name: "empty", key: nil, wantErr: ErrInvalidKey},
		{name: "16 bytes (AES-128)", key: key(1)[:16], wantErr: ErrInvalidKey},
		{name: "33 bytes", key: append(key(1), 0), wantErr: ErrInvalidKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(tt.key); !errors.Is(err, tt.wantErr) {
				t.Fatalf("New() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	c := mustNew(t, key(1))
	for _, plain := range [][]byte{{}, []byte("garmin-personal-api-key"), bytes.Repeat([]byte("x"), 4096)} {
		ct, err := c.Seal(plain)
		if err != nil {
			t.Fatalf("Seal: %v", err)
		}
		if ct[0] != currentVersion {
			t.Fatalf("version byte = %d, want %d", ct[0], currentVersion)
		}
		got, err := c.Open(ct)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if !bytes.Equal(got, plain) {
			t.Fatalf("round trip = %q, want %q", got, plain)
		}
	}
}

func TestSealUsesFreshNonce(t *testing.T) {
	t.Parallel()

	c := mustNew(t, key(1))
	a, _ := c.Seal([]byte("same"))
	b, _ := c.Seal([]byte("same"))
	if bytes.Equal(a, b) {
		t.Fatal("two seals of the same plaintext are identical")
	}
}

func TestOpenFailures(t *testing.T) {
	t.Parallel()

	c := mustNew(t, key(1))
	ct, err := c.Seal([]byte("secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	modified := func(f func(b []byte)) []byte {
		b := bytes.Clone(ct)
		f(b)
		return b
	}

	tests := []struct {
		name    string
		cipher  *Cipher
		input   []byte
		wantErr error
	}{
		{name: "tampered data", cipher: c, input: modified(func(b []byte) { b[len(b)-1] ^= 1 }), wantErr: ErrDecrypt},
		{name: "tampered nonce", cipher: c, input: modified(func(b []byte) { b[1] ^= 1 }), wantErr: ErrDecrypt},
		{name: "truncated", cipher: c, input: ct[:len(ct)-1], wantErr: ErrDecrypt},
		{name: "too short", cipher: c, input: ct[:5], wantErr: ErrDecrypt},
		{name: "empty", cipher: c, input: nil, wantErr: ErrDecrypt},
		{name: "wrong key", cipher: mustNew(t, key(2)), input: ct, wantErr: ErrDecrypt},
		{name: "unknown version", cipher: c, input: modified(func(b []byte) { b[0] = 2 }), wantErr: ErrKeyVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.cipher.Open(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Open() error = %v, want %v", err, tt.wantErr)
			}
			if got != nil {
				t.Fatalf("Open() returned plaintext %q on error", got)
			}
		})
	}
}
