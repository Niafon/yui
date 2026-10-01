package crypto

import (
	"path/filepath"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	// Cheap parameters: the algorithm is what is under test, not the cost.
	return New(filepath.Join(t.TempDir(), "yui.key"), Params{Time: 1, MemoryKB: 32 * 1024, Threads: 1})
}

func TestInitialiseAndUnlock(t *testing.T) {
	s := newStore(t)
	recovery, err := s.Initialise("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if recovery == "" {
		t.Fatal("initialise must return a recovery key")
	}
	if !s.Unlocked() {
		t.Fatal("store should be unlocked after initialise")
	}
	if s.KeyID() == "" {
		t.Fatal("key id must be set")
	}
}

func TestWrongPasswordIsRejected(t *testing.T) {
	s := newStore(t)
	if _, err := s.Initialise("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if err := s.Unlock("wrong password"); err != ErrBadPassword {
		t.Fatalf("error = %v, want ErrBadPassword", err)
	}
}

func TestRecoveryKeyOpensTheStore(t *testing.T) {
	s := newStore(t)
	recovery, err := s.Initialise("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.EncryptString("любимый напиток — чай", "mem_1")
	if err != nil {
		t.Fatal(err)
	}

	// A fresh store over the same file: the password is gone, the memory is not.
	reopened := New(s.path, Params{Time: 1, MemoryKB: 32 * 1024, Threads: 1})
	if err := reopened.UnlockWithRecovery(recovery); err != nil {
		t.Fatalf("recovery unlock failed: %v (ADR-024 requires this path)", err)
	}
	got, err := reopened.DecryptString(sealed, "mem_1")
	if err != nil || got != "любимый напиток — чай" {
		t.Fatalf("decrypt = %q, %v", got, err)
	}
}

func TestChangePasswordKeepsData(t *testing.T) {
	s := newStore(t)
	if _, err := s.Initialise("first password"); err != nil {
		t.Fatal(err)
	}
	sealed, err := s.EncryptString("важный факт", "mem_2")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ChangePassword("first password", "second password"); err != nil {
		t.Fatal(err)
	}
	// The data key never changed, so nothing had to be re-encrypted.
	got, err := s.DecryptString(sealed, "mem_2")
	if err != nil || got != "важный факт" {
		t.Fatalf("decrypt after password change = %q, %v", got, err)
	}
	if err := s.Unlock("first password"); err != ErrBadPassword {
		t.Fatal("the old password must stop working")
	}
}

func TestCiphertextIsBoundToItsRecord(t *testing.T) {
	s := newStore(t)
	if _, err := s.Initialise("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	sealed, err := s.EncryptString("медицинская запись", "mem_3")
	if err != nil {
		t.Fatal(err)
	}
	// Moving a value to another row must fail, not silently decrypt.
	if _, err := s.DecryptString(sealed, "mem_4"); err == nil {
		t.Fatal("ciphertext must not open under a different record id")
	}
}

func TestLockedStoreRefusesToEncrypt(t *testing.T) {
	s := newStore(t)
	if _, err := s.EncryptString("x", "mem_5"); err != ErrLocked {
		t.Fatalf("error = %v, want ErrLocked", err)
	}
}

func TestRecoveryKeyRoundTripsThroughItsTextForm(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i * 7)
	}
	parsed, err := parseRecovery(formatRecovery(raw))
	if err != nil {
		t.Fatal(err)
	}
	for i := range raw {
		if parsed[i] != raw[i] {
			t.Fatalf("byte %d differs after formatting", i)
		}
	}
}

func TestKeyIDDoesNotLeakKeyMaterial(t *testing.T) {
	// The key id is stored in plaintext beside every encrypted row, so it must
	// be independent of the data key. Two stores created with the same password
	// must still get different ids, and no id may be a prefix of key material.
	a := newStore(t)
	b := newStore(t)
	if _, err := a.Initialise("same password twice"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Initialise("same password twice"); err != nil {
		t.Fatal(err)
	}
	if a.KeyID() == b.KeyID() {
		t.Fatal("key ids collided; they must be independent random values")
	}
	if len(a.KeyID()) < 8 {
		t.Fatalf("key id %q is too short to be unique", a.KeyID())
	}
}

func TestUnlockClearsTheCallerSecret(t *testing.T) {
	s := newStore(t)
	if _, err := s.Initialise("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	// unlockWith scrubs the secret it was handed; the password string itself
	// is immutable in Go, which is why the API takes bytes internally.
	secret := []byte("correct horse battery")
	if err := s.unlockWith(secret, false); err != nil {
		t.Fatal(err)
	}
	for i, b := range secret {
		if b != 0 {
			t.Fatalf("byte %d of the secret was not cleared after unlock", i)
		}
	}
}
