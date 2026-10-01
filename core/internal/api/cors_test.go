package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yui-companion/core/internal/config"
)

func TestCORSAllowsOnlyStageOrigins(t *testing.T) {
	cfg := config.Default()
	cfg.Server.AllowedOrigins = []string{"https://stage.example/"}
	s := &Server{deps: Deps{Cfg: cfg}, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := s.cors(s.mux)

	for _, origin := range []string{"tauri://localhost", "http://tauri.localhost", "http://localhost:5273", "https://stage.example"} {
		pre := httptest.NewRequest(http.MethodOptions, "/v1/status", nil)
		pre.Header.Set("Origin", origin)
		pre.Header.Set("Access-Control-Request-Method", "GET")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, pre)
		if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != origin {
			t.Fatalf("%s preflight: %d %q", origin, rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
		}
		if rec.Header().Get("Access-Control-Allow-Headers") == "" {
			t.Fatalf("%s preflight lacks allowed headers", origin)
		}
	}
	evil := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	evil.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, evil)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("unknown origin was granted CORS access")
	}
	pre := httptest.NewRequest(http.MethodOptions, "/v1/status", nil)
	pre.Header.Set("Origin", "https://evil.example")
	pre.Header.Set("Access-Control-Request-Method", "GET")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, pre)
	if rec.Code == http.StatusNoContent {
		t.Fatal("preflight from an unknown origin was answered")
	}
}
