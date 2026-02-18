package wsserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
)

func TestSessionWantsProto_QueryFormat(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws?format=proto", nil)
	if !sessionWantsProto(req) {
		t.Fatal("expected proto mode from query format=proto")
	}
}

func TestSessionWantsProto_Header(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("X-Delivery-Format", "proto")
	if !sessionWantsProto(req) {
		t.Fatal("expected proto mode from X-Delivery-Format header")
	}
}

func TestSessionWantsProto_DefaultJSON(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)
	if sessionWantsProto(req) {
		t.Fatal("did not expect proto mode by default")
	}
}

func TestHandleWS_AuthRejectsUnauthorized(t *testing.T) {
	srv := NewServer(
		nil,
		&actor.PID{},
		nil,
		nil,
		256,
		WithAuthConfig(AuthConfig{
			Enabled: true,
			APIKeys: map[string]string{"k1": "client-a"},
		}),
	)

	req := httptest.NewRequest("GET", "/ws", nil)
	rec := httptest.NewRecorder()
	srv.HandleUpgrade(rec, req)

	if rec.Code != 401 {
		t.Fatalf("status=%d want=401", rec.Code)
	}
}

func TestHandleWS_UpgradeSpawnsSessionWithValidAPIKey(t *testing.T) {
	spawned := make(chan struct{}, 1)
	srv := NewServer(
		nil,
		&actor.PID{},
		nil,
		nil,
		256,
		WithAuthConfig(AuthConfig{
			Enabled: true,
			APIKeys: map[string]string{"k1": "client-a"},
		}),
		WithSessionSpawner(func(cfg deliveryruntime.SessionConfig) *actor.PID {
			if cfg.ClientID != "client-a" {
				t.Fatalf("client_id=%q want=client-a", cfg.ClientID)
			}
			select {
			case spawned <- struct{}{}:
			default:
			}
			_ = cfg.Conn.Close()
			return &actor.PID{}
		}),
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", srv.HandleUpgrade)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	header := http.Header{}
	header.Set("X-API-Key", "k1")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURLFromHTTP(ts.URL)+"/ws", header)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if resp == nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status=%v want=%d", resp.StatusCode, http.StatusSwitchingProtocols)
	}
	select {
	case <-spawned:
	case <-time.After(time.Second):
		t.Fatal("expected session spawner to be called")
	}
}
