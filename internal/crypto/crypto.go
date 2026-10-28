// Package crypto encrypts secrets at rest with AES-256-GCM.
//
// Ciphertext layout: key version (1 byte) || nonce (12 bytes) || sealed data.
// The version prefix lets a future key rotation tell old ciphertexts apart.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// KeySize is the required key length in bytes (AES-256).
const KeySize = 32

// currentVersion tags every ciphertext sealed with the configured key.
const currentVersion byte = 1

var (
	// ErrInvalidKey means the key is not KeySize bytes long.
	ErrInvalidKey = errors.New("crypto: key must be 32 bytes")
	// ErrKeyVersion means the ciphertext was sealed with an unknown key version.
	ErrKeyVersion = errors.New("crypto: unknown key version")
	// ErrDecrypt means the ciphertext is malformed, tampered or sealed with another key.
	ErrDecrypt = errors.New("crypto: decryption failed")
)

// Cipher seals and opens secrets with one AES-256-GCM key.
type Cipher struct {
	aead cipher.AEAD
}

// New returns a Cipher for a 32-byte key.
func New(key []byte) (*Cipher, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Seal encrypts plaintext with a random nonce.
func (c *Cipher) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("reading nonce: %w", err)
	}
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+c.aead.Overhead())
	out = append(append(out, currentVersion), nonce...)
	return c.aead.Seal(out, nonce, plaintext, nil), nil
}

// Open decrypts a ciphertext produced by Seal.
func (c *Cipher) Open(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < 1+c.aead.NonceSize()+c.aead.Overhead() {
		return nil, ErrDecrypt
	}
	if ciphertext[0] != currentVersion {
		return nil, ErrKeyVersion
	}
	nonce := ciphertext[1 : 1+c.aead.NonceSize()]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext[1+c.aead.NonceSize():], nil)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}
