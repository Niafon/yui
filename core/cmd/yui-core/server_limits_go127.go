//go:build go1.27

package main

import "net/http"

func configureServerLimits(server *http.Server) {
	server.MaxHeaderValueCount = 64
}
