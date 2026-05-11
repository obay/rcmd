package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"io"

	"golang.org/x/crypto/hkdf"
)

// MasterSecretBytes is the size in bytes of the rcmd master secret.
const MasterSecretBytes = 32

// NewMasterSecret returns 32 cryptographically random bytes for use as
// a rcmd master secret. The same value populates the relay state,
// agent state, and operator state; the operator and agent each derive
// HMAC and AEAD subkeys from it via HKDF.
func NewMasterSecret() ([]byte, error) {
	b := make([]byte, MasterSecretBytes)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// DeriveHMACSubkey returns the 32-byte subkey used for HMAC-SHA256
// request signing. HKDF-SHA256 with info="rcmd-hmac-v1".
func DeriveHMACSubkey(master []byte) []byte {
	return hkdfExtract32(master, "rcmd-hmac-v1")
}

// DeriveAEADSubkey returns the 32-byte subkey used for AES-256-GCM
// payload encryption. HKDF-SHA256 with info="rcmd-aes-v1".
func DeriveAEADSubkey(master []byte) []byte {
	return hkdfExtract32(master, "rcmd-aes-v1")
}

func hkdfExtract32(master []byte, info string) []byte {
	r := hkdf.New(sha256.New, master, nil, []byte(info))
	out := make([]byte, KeyBytes)
	_, _ = io.ReadFull(r, out)
	return out
}
