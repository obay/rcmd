package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

// TransferKeyBytes is the size of the per-transfer AEAD subkey.
const TransferKeyBytes = 32

// DeriveTransferKey derives a per-transfer AES-256-GCM key from the
// master secret and an opaque transfer identifier. Both the operator
// and the agent can derive the same key independently because they
// share the master secret. Same input → same key — deterministic.
//
// Mixing the transfer_id into the KDF means a leaked or guessed nonce
// for one transfer cannot break another transfer's confidentiality.
func DeriveTransferKey(master []byte, transferID string) []byte {
	r := hkdf.New(sha256.New, master, []byte(transferID), []byte("rcmd-transfer-v1"))
	out := make([]byte, TransferKeyBytes)
	_, _ = io.ReadFull(r, out)
	return out
}

// ChunkNonce returns the 12-byte AES-GCM nonce for chunk index i in a
// transfer. Format: 8 zero bytes || uint32 big-endian chunk index.
// The transfer_id is already mixed into the key via HKDF, so we don't
// need to repeat it in the nonce. Each (transfer, chunk_index) pair
// has a unique (key, nonce); GCM safety preserved.
func ChunkNonce(i uint32) []byte {
	n := make([]byte, 12)
	binary.BigEndian.PutUint32(n[8:], i)
	return n
}

// SealChunk encrypts plaintext for chunk index i and returns the
// ciphertext (including the 16-byte GCM auth tag). The chunk index is
// also bound as AAD so swapping chunks is detected.
func SealChunk(key []byte, i uint32, plaintext []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := ChunkNonce(i)
	aad := aadFor(i)
	return gcm.Seal(nil, nonce, plaintext, aad), nil
}

// OpenChunk decrypts the ciphertext for chunk index i. Returns an
// error if the auth tag fails verification.
func OpenChunk(key []byte, i uint32, ciphertext []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := ChunkNonce(i)
	aad := aadFor(i)
	pt, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("chunk %d: %w", i, err)
	}
	return pt, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != TransferKeyBytes {
		return nil, errors.New("transfer key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func aadFor(i uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, i)
	return b
}
