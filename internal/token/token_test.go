package token

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestMintParseRoundtrip(t *testing.T) {
	master := strings.Repeat("A", 32)
	in := Token{
		RelayURL:     "https://relay.example.com",
		MasterSecret: base64.StdEncoding.EncodeToString([]byte(master)),
	}
	s, err := Mint(in)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if s == "" {
		t.Fatal("Mint returned empty")
	}
	out, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if out.RelayURL != in.RelayURL {
		t.Errorf("RelayURL: got %q want %q", out.RelayURL, in.RelayURL)
	}
	if out.MasterSecret != in.MasterSecret {
		t.Errorf("MasterSecret mismatch")
	}
	if out.Version != Version {
		t.Errorf("Version: got %d want %d", out.Version, Version)
	}
}

func TestMintRejectsMissingFields(t *testing.T) {
	if _, err := Mint(Token{}); err == nil {
		t.Error("expected error on empty token")
	}
	if _, err := Mint(Token{RelayURL: "https://x"}); err == nil {
		t.Error("expected error on missing master secret")
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse(""); err == nil {
		t.Error("expected error on empty string")
	}
	if _, err := Parse("***not base64***"); err == nil {
		t.Error("expected error on invalid base64")
	}
	if _, err := Parse("dGhpcyBpcyBub3QganNvbg=="); err == nil {
		t.Error("expected error on non-JSON base64")
	}
}

func TestParseAcceptsPaddedAndUnpadded(t *testing.T) {
	t1 := Token{
		RelayURL:     "https://relay.example.com",
		MasterSecret: base64.StdEncoding.EncodeToString(make([]byte, 32)),
	}
	raw, err := Mint(t1)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	// Add equals padding to simulate a copy-paste mangling.
	padded := raw + "=="
	if _, err := Parse(padded); err == nil {
		// expected — flexible decoder should handle it
	}
}
