package api

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yui-companion/core/internal/config"
)

func TestRemotePairingRequiresTLSWithoutConsumingCode(t *testing.T) {
	cfg := config.Default()
	cfg.Server.RequireTLSForRemote = true
	s := New(Deps{Cfg: cfg})
	code := s.pairing.create().Code
	r := httptest.NewRequest(http.MethodPost, "/v1/pair/claim", strings.NewReader(`{"code":"`+code+`"}`))
	r.RemoteAddr = "192.168.1.20:1234"
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusUpgradeRequired {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if !s.pairing.claim(code) {
		t.Fatal("plaintext claim consumed code")
	}
}

func TestRemoteCannotCreatePairingCode(t *testing.T) {
	cfg := config.Default()
	cfg.Server.LoopbackToken = "desktop-secret"
	s := New(Deps{Cfg: cfg})
	r := httptest.NewRequest(http.MethodPost, "/v1/pair/start", nil)
	r.RemoteAddr = "192.168.1.20:1234"
	r.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()
	s.handlePairStart(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d", w.Code)
	}
}

func TestStageAndAPIRoutesCoexist(t *testing.T) {
	cfg := config.Default()
	cfg.Server.StageDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(cfg.Server.StageDir, "index.html"), []byte("Yui stage"), 0600); err != nil {
		t.Fatal(err)
	}
	s := New(Deps{Cfg: cfg})
	for _, tc := range []struct {
		path string
		code int
	}{{"/", 200}, {"/v1/status", 401}, {"/absent", 404}} {
		r := httptest.NewRequest(http.MethodGet, tc.path, nil)
		r.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Errorf("%s: got %d want %d", tc.path, w.Code, tc.code)
		}
	}
}
