package httpserver_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	deliveryruntime "github.com/FabioCaffarello/stream-analytics/internal/actors/delivery/runtime"
	httpserver "github.com/FabioCaffarello/stream-analytics/internal/interfaces/http"
	wsserver "github.com/FabioCaffarello/stream-analytics/internal/interfaces/ws"
	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
)

func wsURLFromHTTP(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

func TestWSAuth_ValidKey(t *testing.T) {
	spawned := make(chan struct{}, 1)
	ws := wsserver.NewServer(
		nil,
		&actor.PID{},
		nil,
		nil,
		256,
		wsserver.WithAuthConfig(wsserver.AuthConfig{
			Enabled: true,
			APIKeys: map[string]string{"k1": "client-a"},
		}),
		wsserver.WithSessionSpawner(func(cfg deliveryruntime.SessionConfig) *actor.PID {
			if cfg.ClientID != "client-a" {
				t.Fatalf("client_id=%q want=client-a", cfg.ClientID)
			}
			select {
			case spawned <- struct{}{}:
			default:
			}
			if cfg.Conn != nil {
				_ = cfg.Conn.Close()
			}
			return &actor.PID{}
		}),
	)

	srv := httpserver.NewServer(nil, nil, ":0", false, nil, httpserver.WithWSHandler(ws.HandleWS))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	header := http.Header{}
	header.Set("X-API-Key", "k1")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURLFromHTTP(ts.URL)+"/ws", header)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	if resp == nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status=%v want=%d", resp.StatusCode, http.StatusSwitchingProtocols)
	}
	_ = conn.Close()

	select {
	case <-spawned:
	case <-time.After(time.Second):
		t.Fatal("expected session spawner to be called")
	}
}

func TestWSAuth_InvalidKey(t *testing.T) {
	ws := wsserver.NewServer(
		nil,
		&actor.PID{},
		nil,
		nil,
		256,
		wsserver.WithAuthConfig(wsserver.AuthConfig{
			Enabled: true,
			APIKeys: map[string]string{"k1": "client-a"},
		}),
	)

	srv := httpserver.NewServer(nil, nil, ":0", false, nil, httpserver.WithWSHandler(ws.HandleWS))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	header := http.Header{}
	header.Set("X-API-Key", "invalid")
	_, resp, err := websocket.DefaultDialer.Dial(wsURLFromHTTP(ts.URL)+"/ws", header)
	if err == nil {
		t.Fatal("expected unauthorized websocket handshake failure")
	}
	if resp == nil {
		t.Fatal("expected HTTP response on failed websocket handshake")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusUnauthorized)
	}
}
