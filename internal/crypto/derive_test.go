package crypto

import (
	"bytes"
	"testing"
)

func TestDerivationDeterministicAndDistinct(t *testing.T) {
	master, err := NewMasterSecret()
	if err != nil {
		t.Fatalf("NewMasterSecret: %v", err)
	}
	if len(master) != MasterSecretBytes {
		t.Fatalf("master len = %d, want %d", len(master), MasterSecretBytes)
	}

	h1 := DeriveHMACSubkey(master)
	h2 := DeriveHMACSubkey(master)
	if !bytes.Equal(h1, h2) {
		t.Error("DeriveHMACSubkey not deterministic")
	}

	a1 := DeriveAEADSubkey(master)
	a2 := DeriveAEADSubkey(master)
	if !bytes.Equal(a1, a2) {
		t.Error("DeriveAEADSubkey not deterministic")
	}

	if bytes.Equal(h1, a1) {
		t.Error("HMAC and AEAD subkeys must differ — info-string separation broken")
	}

	if len(h1) != KeyBytes || len(a1) != KeyBytes {
		t.Errorf("subkey size: hmac=%d aead=%d, want %d", len(h1), len(a1), KeyBytes)
	}
}

func TestSubkeyDiffersAcrossMasters(t *testing.T) {
	m1, _ := NewMasterSecret()
	m2, _ := NewMasterSecret()
	if bytes.Equal(DeriveHMACSubkey(m1), DeriveHMACSubkey(m2)) {
		t.Error("different masters produced same HMAC subkey")
	}
	if bytes.Equal(DeriveAEADSubkey(m1), DeriveAEADSubkey(m2)) {
		t.Error("different masters produced same AEAD subkey")
	}
}
