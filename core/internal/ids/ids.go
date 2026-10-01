// Package ids generates short, prefixed, collision-resistant identifiers.
package ids

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
)

var enc = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// New returns an id such as "mem_k3f9x2ab7qz1".
func New(prefix string) string {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is fatal for security-relevant ids; the caller
		// cannot do anything useful with a weak id, so panic loudly.
		panic("ids: entropy source unavailable: " + err.Error())
	}
	s := enc.EncodeToString(b)
	if prefix == "" {
		return s
	}
	return prefix + "_" + s
}

// Trace returns a correlation id used across components (NFR-005).
func Trace() string { return New("tr") }

// Token returns a 32 byte secret encoded for transport (device pairing).
func Token() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("ids: entropy source unavailable: " + err.Error())
	}
	return enc.EncodeToString(b)
}

// ShortCode returns a human readable pairing code, e.g. "4TQ9-B2KM".
func ShortCode() string {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		panic("ids: entropy source unavailable: " + err.Error())
	}
	s := strings.ToUpper(enc.EncodeToString(b))
	return s[0:4] + "-" + s[4:8]
}
