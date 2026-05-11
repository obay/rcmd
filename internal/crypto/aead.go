// Package crypto wraps AES-256-GCM for rcmd payloads.
//
// All command/result bodies are sealed with a pre-shared 32-byte key
// before being placed in the HTTPS body. The relay never has this key,
// so even though the corporate firewall does TLS inspection it cannot
// read what's being shipped.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/obay/rcmd/internal/api"
)

const KeyBytes = 32 // AES-256

// ParseKey decodes a base64-std key and verifies its length.
func ParseKey(b64 string) ([]byte, error) {
	k, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("decode key: %w", err)
	}
	if len(k) != KeyBytes {
		return nil, fmt.Errorf("key must be %d bytes, got %d", KeyBytes, len(k))
	}
	return k, nil
}

// Seal marshals v as JSON, encrypts with the given key, and returns an
// Envelope with base64 nonce and ciphertext.
func Seal(key []byte, v any) (api.Envelope, error) {
	plaintext, err := json.Marshal(v)
	if err != nil {
		return api.Envelope{}, fmt.Errorf("marshal payload: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return api.Envelope{}, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return api.Envelope{}, fmt.Errorf("new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return api.Envelope{}, fmt.Errorf("read nonce: %w", err)
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)
	return api.Envelope{
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ct),
	}, nil
}

// Open decrypts an Envelope and unmarshals it into v.
func Open(key []byte, env api.Envelope, v any) error {
	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil {
		return fmt.Errorf("decode nonce: %w", err)
	}
	ct, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return fmt.Errorf("decode ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("new gcm: %w", err)
	}
	if len(nonce) != gcm.NonceSize() {
		return errors.New("bad nonce length")
	}
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return fmt.Errorf("gcm open: %w", err)
	}
	if err := json.Unmarshal(pt, v); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}
	return nil
}

// NewKey returns a fresh 32-byte key, base64-encoded.
func NewKey() (string, error) {
	k := make([]byte, KeyBytes)
	if _, err := rand.Read(k); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(k), nil
}
