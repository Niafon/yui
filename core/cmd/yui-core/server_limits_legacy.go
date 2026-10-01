//go:build !go1.27

package main

import "net/http"

// MaxHeaderValueCount is not available before Go 1.27. MaxHeaderBytes and
// ReadHeaderTimeout remain active on legacy toolchains.
func configureServerLimits(_ *http.Server) {}
