package linktransfer

import "testing"

func TestTokenFromCode(t *testing.T) {
	tok := tokenFromCode("abc123")
	if len(tok) != 32 {
		t.Fatalf("expected 32 hex chars, got %d: %q", len(tok), tok)
	}
	if tokenFromCode("abc123") != tok {
		t.Fatal("deterministic failure")
	}
	if tokenFromCode("other") == tok {
		t.Fatal("different codes should produce different tokens")
	}
}
