// Package token mints and parses the opaque join token used by rcmd
// for first-time bootstrap of agents and operators.
//
// Wire format: base64(JSON({v, u, k})) where
//   v = schema version (currently 1)
//   u = relay URL (e.g. "https://relay.example.com")
//   k = base64-encoded master secret (32 bytes)
//
// Decoded, a token is on the order of ~100 chars. It is the single
// secret a new agent or operator pastes into `rcmd-agent join` or
// `rcmd join`. The token is **the** key; anyone who has it can join
// the relay and impersonate any identity. Treat accordingly.
package token

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// Version is the current token schema version.
const Version = 1

// Token is the decoded form of a join token.
type Token struct {
	Version      int    `json:"v"`
	RelayURL     string `json:"u"`
	MasterSecret string `json:"k"` // base64 32 bytes
}

// Mint encodes t as a base64-URL-safe opaque string.
func Mint(t Token) (string, error) {
	if t.Version == 0 {
		t.Version = Version
	}
	if t.RelayURL == "" {
		return "", errors.New("token: RelayURL is required")
	}
	if t.MasterSecret == "" {
		return "", errors.New("token: MasterSecret is required")
	}
	b, err := json.Marshal(t)
	if err != nil {
		return "", fmt.Errorf("marshal token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Parse decodes an opaque string back into a Token. It tolerates both
// base64 RawURL and standard padded forms so a token copy-pasted with
// trailing '=' still parses.
func Parse(s string) (Token, error) {
	if s == "" {
		return Token{}, errors.New("token: empty")
	}
	b, err := decodeFlexible(s)
	if err != nil {
		return Token{}, fmt.Errorf("token: %w", err)
	}
	var t Token
	if err := json.Unmarshal(b, &t); err != nil {
		return Token{}, fmt.Errorf("token: parse JSON: %w", err)
	}
	if t.Version == 0 {
		return Token{}, errors.New("token: missing version field")
	}
	if t.Version > Version {
		return Token{}, fmt.Errorf("token: version %d newer than this binary supports (%d)", t.Version, Version)
	}
	if t.RelayURL == "" {
		return Token{}, errors.New("token: missing relay URL")
	}
	if t.MasterSecret == "" {
		return Token{}, errors.New("token: missing master secret")
	}
	return t, nil
}

func decodeFlexible(s string) ([]byte, error) {
	// Try RawURL first (no padding), then padded forms.
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return nil, errors.New("not valid base64")
}
