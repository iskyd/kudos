package api

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/iskyd/kudos/internal/crypto"
)

// ErrInvalidCursor is returned for a cursor that is malformed, tampered with or
// was issued under another key. It renders as an invalid-request problem.
var ErrInvalidCursor = &Error{Kind: KindInvalidRequest, Detail: "invalid cursor"}

// Cursors encodes pagination cursors. A cursor is the JSON of a position,
// sealed with AES-GCM: clients can neither read nor forge it. Cursors are not
// bound to a user; list queries filter by user_id in SQL anyway.
type Cursors struct {
	cipher *crypto.Cipher
}

// NewCursors derives a cursor-only subkey from the master key, so a cursor can
// never be swapped with an encrypted secret.
func NewCursors(masterKey []byte) (*Cursors, error) {
	key, err := hkdf.Key(sha256.New, masterKey, nil, "kudos api cursor v1", crypto.KeySize)
	if err != nil {
		return nil, fmt.Errorf("deriving cursor key: %w", err)
	}
	c, err := crypto.New(key)
	if err != nil {
		return nil, fmt.Errorf("creating cursor cipher: %w", err)
	}
	return &Cursors{cipher: c}, nil
}

// Encode returns the cursor for position v.
func (cs *Cursors) Encode(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encoding cursor: %w", err)
	}
	sealed, err := cs.cipher.Seal(b)
	if err != nil {
		return "", fmt.Errorf("sealing cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Decode stores the position in cursor s into v. Any failure is ErrInvalidCursor.
func (cs *Cursors) Decode(s string, v any) error {
	sealed, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return ErrInvalidCursor
	}
	b, err := cs.cipher.Open(sealed)
	if err != nil {
		return ErrInvalidCursor
	}
	if err := json.Unmarshal(b, v); err != nil {
		return ErrInvalidCursor
	}
	return nil
}
