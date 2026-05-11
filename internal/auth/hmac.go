// Package auth implements request signing for rcmd.
//
// Every authenticated request carries four headers:
//
//	X-Rcmd-Timestamp: <unix seconds>
//	X-Rcmd-Nonce:     <hex 16 random bytes>
//	X-Rcmd-Sig:       <hex HMAC-SHA256(key, METHOD\nPATH\nTS\nNONCE\nSHA256(body))>
//	X-Rcmd-Identity:  <self-declared identity, e.g. "alice" or "win-host-1">
//
// The relay verifies the signature against its single HMAC subkey
// (derived via HKDF from the master secret), checks the timestamp
// window, and rejects replays via an in-memory nonce cache. The
// identity is observational — the relay records who showed up but
// does not gate based on it (anyone with the master secret can claim
// any identity).
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/obay/rcmd/internal/api"
)

// Sign attaches the four auth headers to req. body is the exact bytes
// of the request body (or nil for GET).
func Sign(req *http.Request, identity string, hmacKey []byte, body []byte) error {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return fmt.Errorf("read nonce: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)

	bodyHash := sha256.Sum256(body)
	sig := computeSig(hmacKey, req.Method, req.URL.RequestURI(), ts, nonce, bodyHash[:])

	req.Header.Set(api.HeaderTimestamp, ts)
	req.Header.Set(api.HeaderNonce, nonce)
	req.Header.Set(api.HeaderSig, sig)
	req.Header.Set(api.HeaderIdentity, identity)
	return nil
}

// Verify checks the auth headers on req against the single shared HMAC
// key. body must be the bytes of the request body that the handler
// will read (caller is expected to read+buffer it before calling).
//
// Returns the self-declared identity from X-Rcmd-Identity on success.
// The identity is informational only: the relay accepts any non-empty
// value because every party in v2 shares the same HMAC subkey.
func Verify(req *http.Request, body []byte, hmacKey []byte, nonces *NonceCache) (string, error) {
	identity := req.Header.Get(api.HeaderIdentity)
	if identity == "" {
		return "", errors.New("missing identity")
	}
	ts := req.Header.Get(api.HeaderTimestamp)
	nonce := req.Header.Get(api.HeaderNonce)
	sig := req.Header.Get(api.HeaderSig)
	if ts == "" || nonce == "" || sig == "" {
		return "", errors.New("missing auth headers")
	}
	tsNum, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return "", errors.New("bad timestamp")
	}
	now := time.Now().Unix()
	if tsNum < now-api.ClockSkewSeconds || tsNum > now+api.ClockSkewSeconds {
		return "", errors.New("timestamp outside window")
	}
	if nonces.Seen(nonce, tsNum) {
		return "", errors.New("replayed nonce")
	}
	bodyHash := sha256.Sum256(body)
	want := computeSig(hmacKey, req.Method, req.URL.RequestURI(), ts, nonce, bodyHash[:])
	if !hmac.Equal([]byte(want), []byte(sig)) {
		return "", errors.New("bad signature")
	}
	return identity, nil
}

func computeSig(key []byte, method, path, ts, nonce string, bodyHash []byte) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(method))
	m.Write([]byte{'\n'})
	m.Write([]byte(path))
	m.Write([]byte{'\n'})
	m.Write([]byte(ts))
	m.Write([]byte{'\n'})
	m.Write([]byte(nonce))
	m.Write([]byte{'\n'})
	m.Write(bodyHash)
	return hex.EncodeToString(m.Sum(nil))
}

// NonceCache keeps recently-seen nonces to reject replays. Entries
// expire after 2 * ClockSkewSeconds since a request older than that
// fails timestamp validation anyway.
type NonceCache struct {
	mu      sync.Mutex
	entries map[string]int64 // nonce -> timestamp
}

func NewNonceCache() *NonceCache {
	return &NonceCache{entries: make(map[string]int64)}
}

// Seen returns true if the nonce has already been recorded; otherwise
// it records the nonce and returns false. Side-effect on first sight.
func (c *NonceCache) Seen(nonce string, ts int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gcLocked()
	if _, ok := c.entries[nonce]; ok {
		return true
	}
	c.entries[nonce] = ts
	return false
}

func (c *NonceCache) gcLocked() {
	cutoff := time.Now().Unix() - 2*api.ClockSkewSeconds
	for k, v := range c.entries {
		if v < cutoff {
			delete(c.entries, k)
		}
	}
}
