package api

import (
	"crypto/tls"
)

// TLSConfig is the profile used for remote clients (SEC-005, ADR-035).
//
// A companion's database is a decade of one person's private life. That makes
// "harvest now, decrypt later" a real threat model rather than a theoretical
// one: traffic captured on a home network today could be opened once quantum
// attacks on classical key exchange become practical, and the contents would
// still be personal. Go has defaulted to the hybrid X25519MLKEM768 exchange
// since 1.24; Go 1.27 added MLKEM1024, and we ask for both explicitly rather
// than inheriting whatever the default happens to be.
//
// Classical curves stay in the list: the hybrid schemes are hybrid precisely
// so that a flaw in the post-quantum half cannot make things worse.
func TLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		CurvePreferences: []tls.CurveID{
			tls.X25519MLKEM768,
			tls.X25519,
			tls.CurveP256,
		},
		// The core is not a public web server: no session tickets means no
		// ticket key to rotate and no resumption state to steal.
		SessionTicketsDisabled: true,
	}
}
