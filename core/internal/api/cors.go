package api

import (
	"net/http"
	"strings"
)

// DefaultAllowedOrigins are the pages that legitimately live on another origin
// than the core: the packaged Tauri shell (tauri:// on macOS/Linux,
// http(s)://tauri.localhost on Windows) and the Vite development server. A
// browser tab on any other site still cannot read core responses; every
// non-pairing call needs the bearer token anyway.
var DefaultAllowedOrigins = []string{
	"tauri://localhost",
	"http://tauri.localhost",
	"https://tauri.localhost",
	"http://localhost:5273",
	"http://127.0.0.1:5273",
}

// cors answers preflights and tags responses for the allow-listed origins.
func (s *Server) cors(next http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, origin := range DefaultAllowedOrigins {
		allowed[origin] = true
	}
	if s.deps.Cfg != nil {
		for _, origin := range s.deps.Cfg.Server.AllowedOrigins {
			allowed[strings.TrimRight(origin, "/")] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" || !allowed[origin] {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Add("Vary", "Origin")
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Trace-Id")
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.Set("Access-Control-Expose-Headers", "X-Trace-Id")
		next.ServeHTTP(w, r)
	})
}
