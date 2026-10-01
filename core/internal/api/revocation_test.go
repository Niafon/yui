package api

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yui-companion/core/internal/audit"
	"github.com/yui-companion/core/internal/config"
	"github.com/yui-companion/core/internal/eventbus"
	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/session"
	"github.com/yui-companion/core/internal/store/memstore"
	"github.com/yui-companion/core/internal/ws"
)

func TestRevocationClosesBothLivePlanes(t *testing.T) {
	ctx := context.Background()
	st, err := memstore.Open("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := config.Default()
	cfg.Server.LoopbackToken = "desktop-test-token"
	bus := eventbus.New()
	sessions := session.NewManager(st.Sessions(), bus)
	device := &model.Device{ID: "phone", Kind: "android", TokenHash: HashToken("phone-test-token")}
	if err := st.Devices().Upsert(ctx, device); err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.Start(ctx, "identity", "owner", device.ID, model.ModeNormal, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := New(Deps{Cfg: cfg, Store: st, Sessions: sessions, Audit: audit.New(st.Audit(), bus)})
	httpServer := httptest.NewServer(s.Handler())
	t.Cleanup(httpServer.Close)
	var sockets []*ws.Conn
	for _, plane := range []string{"control", "data"} {
		raw, err := net.DialTimeout("tcp", strings.TrimPrefix(httpServer.URL, "http://"), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = raw.Close() })
		_ = raw.SetDeadline(time.Now().Add(3 * time.Second))
		_, err = fmt.Fprintf(raw, "GET /v1/%s?session=%s&token=phone-test-token HTTP/1.1\r\nHost: localhost\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n", plane, sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		reader := bufio.NewReader(raw)
		response, err := http.ReadResponse(reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusSwitchingProtocols {
			t.Fatal(response.Status)
		}
		conn := ws.NewConn(raw, 0)
		// A pong proves registration and the reader are ready before revoking.
		if err := conn.WriteText([]byte(`{"type":"ping"}`)); err != nil {
			t.Fatal(err)
		}
		_, payload, err := conn.ReadMessage()
		if err != nil || !strings.Contains(string(payload), "pong") {
			t.Fatalf("ping: %s, %v", payload, err)
		}
		sockets = append(sockets, conn)
	}
	request, _ := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/devices/phone/revoke", nil)
	request.Header.Set("Authorization", "Bearer desktop-test-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatal(response.Status)
	}
	for _, conn := range sockets {
		_, _, err := conn.ReadMessage()
		if err == nil {
			t.Fatal("revoked socket still open")
		}
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			t.Fatal("revocation did not close socket")
		}
	}
	// Also cover a handshake that authenticated immediately before revocation.
	serverSide, clientSide := net.Pipe()
	defer clientSide.Close()
	late := ws.NewConn(serverSide, 0)
	defer late.Close()
	if err := s.trackSocket(ctx, late, caller{DeviceID: device.ID}, func() {}); err == nil {
		t.Fatal("late registration resurrected revoked access")
	}
	request, _ = http.NewRequest(http.MethodGet, httpServer.URL+"/v1/devices", nil)
	request.Header.Set("Authorization", "Bearer phone-test-token")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatal("revoked token authenticated")
	}
}
