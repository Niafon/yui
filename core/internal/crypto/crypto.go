// Package crypto implements envelope encryption for sensitive fields, media
// and provider secrets (ADR-024, ADR-025).
//
// The storage engine is not trusted with plaintext sensitive fields, so the core encrypts
// before writing. The layout is deliberate:
//
//	password ──Argon2id──▶ KEK ──AES-256-GCM──▶ wrapped DEK  (key file)
//	                                    │
//	                    DEK ──AES-256-GCM──▶ field and media ciphertext
//
// The data key is generated once and never derived from the password. That is
// what makes a password change cost microseconds instead of a full re-encrypt,
// and what allows a second, independent way in: a recovery key that wraps the
// same DEK. Losing the password does not lose the memory (the owner's
// requirement); losing both does, by design.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

// zero overwrites key material as soon as it is no longer needed.
//
// Go does not promise that no copy survives in a register or on the stack, so
// this is mitigation, not a guarantee. Go 1.26 introduced runtime/secret for
// exactly this problem; secret_on.go upgrades this to real scrubbing when
// built with -tags yuisecret (ADR-032).
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
	runtime.KeepAlive(b)
}

var (
	ErrLocked        = errors.New("crypto: key store is locked")
	ErrBadPassword   = errors.New("crypto: password or recovery key is wrong")
	ErrNoKeyFile     = errors.New("crypto: key file not found")
	ErrKeyFileExists = errors.New("crypto: key file already exists")
)

const (
	keyFileVersion = 1
	dekSize        = 32 // AES-256
	saltSize       = 16
	// rotateAfter keeps random 96 bit nonces far from the birthday bound.
	rotateAfter = uint64(1) << 32
)

// Params are the Argon2id cost parameters. They are stored in the key file so
// they can be raised later without invalidating existing files.
type Params struct {
	Time     uint32 `json:"time"`
	MemoryKB uint32 `json:"memory_kb"`
	Threads  uint8  `json:"threads"`
}

// DefaultParams follows the current OWASP guidance: 64 MiB, 3 passes.
func DefaultParams() Params {
	return Params{Time: 3, MemoryKB: 64 * 1024, Threads: 4}
}

// wrappedKey is one way to unwrap the data key.
type wrappedKey struct {
	Salt   []byte `json:"salt"`
	Nonce  []byte `json:"nonce"`
	Cipher []byte `json:"cipher"`
	Params Params `json:"params"`
}

// keyFile is the on-disk format. It contains no plaintext key material: with
// neither the password nor the recovery key it is inert.
type keyFile struct {
	Version    int         `json:"version"`
	KeyID      string      `json:"key_id"`
	CreatedAt  time.Time   `json:"created_at"`
	ByPassword *wrappedKey `json:"by_password"`
	ByRecovery *wrappedKey `json:"by_recovery"`
	Operations uint64      `json:"operations"`
}

// Store holds the unwrapped data key in memory only while unlocked.
type Store struct {
	mu     sync.RWMutex
	path   string
	file   keyFile
	aead   cipher.AEAD
	params Params
	ops    uint64
}

// New prepares a store for the given key file. It does not read the file:
// call Initialise or Unlock.
func New(path string, params Params) *Store {
	if params.Time == 0 {
		params = DefaultParams()
	}
	return &Store{path: path, params: params}
}

// Initialise creates a new key file and returns the recovery key, which is
// shown to the owner exactly once. Refuses to overwrite an existing file: that
// would destroy every memory encrypted under the old key.
func (s *Store) Initialise(password string) (recoveryKey string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := os.Stat(s.path); err == nil {
		return "", ErrKeyFileExists
	}
	if len(password) < 8 {
		return "", errors.New("crypto: password must be at least 8 characters")
	}

	dek := make([]byte, dekSize)
	if _, err := rand.Read(dek); err != nil {
		return "", err
	}
	recovery := make([]byte, 32)
	if _, err := rand.Read(recovery); err != nil {
		return "", err
	}
	// The recovery bytes are rendered to text below and then scrubbed; the DEK
	// survives only inside the AEAD, never as a loose slice.
	defer zero(recovery)

	// The key id is independent random data. Deriving it from the DEK would
	// publish part of the key: the id is stored in plaintext in the key file
	// and next to every encrypted row.
	idBytes := make([]byte, 6)
	if _, err := rand.Read(idBytes); err != nil {
		return "", err
	}

	// Wrapping touches raw key bytes; secret mode, when built in, makes the
	// runtime erase the stack temporaries afterwards (ADR-032).
	var byPassword, byRecovery *wrappedKey
	var wrapErr error
	Scrubbed(func() {
		byPassword, wrapErr = wrap(dek, []byte(password), s.params)
		if wrapErr != nil {
			return
		}
		byRecovery, wrapErr = wrap(dek, recovery, s.params)
	})
	if wrapErr != nil {
		return "", wrapErr
	}

	s.file = keyFile{
		Version:    keyFileVersion,
		KeyID:      "key_" + encodeKey(idBytes),
		CreatedAt:  time.Now().UTC(),
		ByPassword: byPassword,
		ByRecovery: byRecovery,
	}
	if err := s.save(); err != nil {
		return "", err
	}
	if err := s.useKey(dek); err != nil {
		return "", err
	}
	return formatRecovery(recovery), nil
}

// Unlock opens the store with the password.
func (s *Store) Unlock(password string) error {
	return s.unlockWith([]byte(password), false)
}

// UnlockWithRecovery opens the store with the recovery key printed at setup.
// This is the path that makes password loss survivable.
func (s *Store) UnlockWithRecovery(recoveryKey string) error {
	raw, err := parseRecovery(recoveryKey)
	if err != nil {
		return err
	}
	return s.unlockWith(raw, true)
}

func (s *Store) unlockWith(secret []byte, useRecovery bool) error {
	defer zero(secret)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return err
	}
	slot := s.file.ByPassword
	if useRecovery {
		slot = s.file.ByRecovery
	}
	if slot == nil {
		return ErrNoKeyFile
	}
	var dek []byte
	var err error
	Scrubbed(func() { dek, err = unwrap(slot, secret) })
	if err != nil {
		return err
	}
	return s.useKey(dek)
}

// ChangePassword rewraps the same data key. Nothing already encrypted has to
// be touched, which is the whole point of the envelope layout.
func (s *Store) ChangePassword(oldPassword, newPassword string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return err
	}
	if len(newPassword) < 8 {
		return errors.New("crypto: password must be at least 8 characters")
	}
	dek, err := unwrap(s.file.ByPassword, []byte(oldPassword))
	if err != nil {
		return err
	}
	wrapped, err := wrap(dek, []byte(newPassword), s.params)
	if err != nil {
		zero(dek)
		return err
	}
	s.file.ByPassword = wrapped
	if err := s.save(); err != nil {
		return err
	}
	return s.useKey(dek)
}

func (s *Store) useKey(dek []byte) error {
	// aes.NewCipher copies the key into its expanded schedule, so the caller's
	// slice is dead weight from here on.
	defer zero(dek)
	block, err := aes.NewCipher(dek)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	s.aead = aead
	s.ops = s.file.Operations
	return nil
}

func (s *Store) Unlocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.aead != nil
}

func (s *Store) KeyID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.file.KeyID
}

// NeedsRotation reports that the key has been used enough times that a fresh
// one should be issued before random nonces become a real collision risk.
func (s *Store) NeedsRotation() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ops >= rotateAfter
}

// Encrypt seals plaintext. The associated data binds the ciphertext to its
// record, so a value cannot be moved from one row to another.
func (s *Store) Encrypt(plaintext, associated []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.aead == nil {
		return nil, ErrLocked
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := s.aead.Seal(nil, nonce, plaintext, associated)
	s.ops++
	// The counter is persisted lazily: losing a few counts on a crash is
	// harmless, losing the ability to write is not.
	if s.ops%1000 == 0 {
		s.file.Operations = s.ops
		_ = s.save()
	}
	return append(nonce, sealed...), nil
}

// Decrypt opens a value produced by Encrypt.
func (s *Store) Decrypt(sealed, associated []byte) ([]byte, error) {
	s.mu.RLock()
	aead := s.aead
	s.mu.RUnlock()
	if aead == nil {
		return nil, ErrLocked
	}
	if len(sealed) < aead.NonceSize() {
		return nil, errors.New("crypto: ciphertext too short")
	}
	nonce := sealed[:aead.NonceSize()]
	return aead.Open(nil, nonce, sealed[aead.NonceSize():], associated)
}

// EncryptString is the convenience path used for memory content.
func (s *Store) EncryptString(text, recordID string) ([]byte, error) {
	return s.Encrypt([]byte(text), []byte(recordID))
}

func (s *Store) DecryptString(sealed []byte, recordID string) (string, error) {
	out, err := s.Decrypt(sealed, []byte(recordID))
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNoKeyFile
		}
		return err
	}
	var file keyFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return err
	}
	if file.Version != keyFileVersion {
		return errors.New("crypto: unsupported key file version")
	}
	s.file = file
	return nil
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.file, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func wrap(dek, secret []byte, p Params) (*wrappedKey, error) {
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	kek := argon2.IDKey(secret, salt, p.Time, p.MemoryKB, p.Threads, dekSize)
	defer zero(kek)
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return &wrappedKey{
		Salt:   salt,
		Nonce:  nonce,
		Cipher: aead.Seal(nil, nonce, dek, []byte("yui-dek")),
		Params: p,
	}, nil
}

func unwrap(w *wrappedKey, secret []byte) ([]byte, error) {
	if w == nil {
		return nil, ErrNoKeyFile
	}
	p := w.Params
	if p.Time == 0 {
		p = DefaultParams()
	}
	kek := argon2.IDKey(secret, w.Salt, p.Time, p.MemoryKB, p.Threads, dekSize)
	defer zero(kek)
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	dek, err := aead.Open(nil, w.Nonce, w.Cipher, []byte("yui-dek"))
	if err != nil {
		return nil, ErrBadPassword
	}
	if subtle.ConstantTimeEq(int32(len(dek)), int32(dekSize)) != 1 {
		return nil, ErrBadPassword
	}
	return dek, nil
}

var recoveryEncoding = base32.NewEncoding("ABCDEFGHJKLMNPQRSTUVWXYZ23456789").WithPadding(base32.NoPadding)

// formatRecovery renders the recovery key in readable groups. It is shown once
// and never stored anywhere by the core.
func formatRecovery(raw []byte) string {
	s := recoveryEncoding.EncodeToString(raw)
	var groups []string
	for i := 0; i < len(s); i += 5 {
		end := i + 5
		if end > len(s) {
			end = len(s)
		}
		groups = append(groups, s[i:end])
	}
	return strings.Join(groups, "-")
}

func parseRecovery(text string) ([]byte, error) {
	cleaned := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(text), "-", ""))
	raw, err := recoveryEncoding.DecodeString(cleaned)
	if err != nil {
		return nil, ErrBadPassword
	}
	if len(raw) != 32 {
		return nil, ErrBadPassword
	}
	return raw, nil
}

func encodeKey(b []byte) string {
	return strings.ToLower(recoveryEncoding.EncodeToString(b))
}
