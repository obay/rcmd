package crypto

import (
	"bytes"
	"testing"
)

func TestSealOpenChunkRoundtrip(t *testing.T) {
	master, _ := NewMasterSecret()
	key := DeriveTransferKey(master, "transfer-abc")

	for _, i := range []uint32{0, 1, 7, 1000, 65535} {
		pt := []byte("chunk payload " + string(rune('A'+(i%26))))
		ct, err := SealChunk(key, i, pt)
		if err != nil {
			t.Fatalf("seal chunk %d: %v", i, err)
		}
		got, err := OpenChunk(key, i, ct)
		if err != nil {
			t.Fatalf("open chunk %d: %v", i, err)
		}
		if !bytes.Equal(got, pt) {
			t.Errorf("chunk %d: got %q want %q", i, got, pt)
		}
	}
}

func TestOpenChunkRejectsWrongIndex(t *testing.T) {
	master, _ := NewMasterSecret()
	key := DeriveTransferKey(master, "x")
	ct, _ := SealChunk(key, 0, []byte("hi"))
	if _, err := OpenChunk(key, 1, ct); err == nil {
		t.Error("expected open with wrong index to fail (AAD mismatch)")
	}
}

func TestDifferentTransfersUseDifferentKeys(t *testing.T) {
	master, _ := NewMasterSecret()
	k1 := DeriveTransferKey(master, "t1")
	k2 := DeriveTransferKey(master, "t2")
	if bytes.Equal(k1, k2) {
		t.Error("different transfer_ids should produce different keys")
	}
	// Ciphertext from t1 must not open under t2's key.
	ct, _ := SealChunk(k1, 0, []byte("secret"))
	if _, err := OpenChunk(k2, 0, ct); err == nil {
		t.Error("expected cross-transfer open to fail")
	}
}
