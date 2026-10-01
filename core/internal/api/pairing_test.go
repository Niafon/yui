package api

import (
	"strings"
	"testing"
	"time"
)

func TestPairingMobileSeparators(t *testing.T) {
	for _, separator := range []string{"-", "–", "—", "−", " ", ""} {
		store := newPairingStore()
		item := store.create()
		input := "\u00a0" + strings.ToLower(strings.ReplaceAll(item.Code, "-", separator)) + "\n"
		if !store.claim(input) {
			t.Fatalf("mobile code rejected for separator %q", separator)
		}
		if store.claim(item.Code) {
			t.Fatal("normalization must not allow replay")
		}
	}
	for _, code := range []string{"ABCD", "ABCD-EFGH-extra", ""} {
		if normalizePairingCode(code) != "" {
			t.Fatalf("invalid length accepted: %q", code)
		}
	}
}

func TestPairingStoreLifetimeAndSingleUse(t *testing.T) {
	store := newPairingStore()
	createdAt := time.Now()
	item := store.create()

	if item.ExpiresAt.Before(createdAt.Add(23*time.Hour + 59*time.Minute)) {
		t.Fatalf("pairing code expires too soon: created at %v, expires at %v", createdAt, item.ExpiresAt)
	}
	if item.ExpiresAt.After(time.Now().Add(24*time.Hour + time.Second)) {
		t.Fatalf("pairing code expires too late: expires at %v", item.ExpiresAt)
	}

	if !store.claim(item.Code) {
		t.Fatal("fresh pairing code was rejected")
	}
	if store.claim(item.Code) {
		t.Fatal("pairing code was accepted more than once")
	}

	expired := store.create()
	expired.ExpiresAt = time.Now().Add(-time.Second)
	if store.claim(expired.Code) {
		t.Fatal("expired pairing code was accepted")
	}
}
