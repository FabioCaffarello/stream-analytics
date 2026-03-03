package wsserver

import (
	"context"
	"encoding/json"
	"net"
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

func TestWSClientModeFromRequestPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want wsClientMode
	}{
		{name: "v1 route", path: "/ws", want: wsClientModeV1},
		{name: "legacy route", path: "/ws/marketdata", want: wsClientModeLegacy},
		{name: "unknown defaults to v1", path: "/ws/custom", want: wsClientModeV1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			if got := wsClientModeFromRequestPath(req); got != tc.want {
				t.Fatalf("mode=%q want=%q", got, tc.want)
			}
		})
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
			if cfg.SlowClientDropThreshold != 7 {
				t.Fatalf("slow_client_drop_threshold=%d want=7", cfg.SlowClientDropThreshold)
			}
			select {
			case spawned <- struct{}{}:
			default:
			}
			_ = cfg.Conn.Close()
			return &actor.PID{}
		}),
		WithSlowClientDropThreshold(7),
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", srv.HandleUpgrade)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listener unavailable in this environment: %v", err)
		return
	}
	httpSrv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
	}
	go func() { _ = httpSrv.Serve(ln) }()
	defer func() { _ = httpSrv.Shutdown(context.Background()) }()

	header := http.Header{}
	header.Set("X-API-Key", "k1")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURLFromHTTP("http://"+ln.Addr().String())+"/ws", header)
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

func TestHandleIntrospection_ReturnsSnapshot(t *testing.T) {
	srv := NewServer(nil, &actor.PID{}, nil, nil, 256)
	req := httptest.NewRequest(http.MethodGet, "/introspection", nil)
	rec := httptest.NewRecorder()
	srv.HandleIntrospection(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d", rec.Code, http.StatusOK)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, ok := body["server_instance_id"]; !ok {
		t.Fatalf("missing server_instance_id in %v", body)
	}
	if _, ok := body["ws"]; !ok {
		t.Fatalf("missing ws introspection payload in %v", body)
	}
}

func TestHandleWS_ConnectionLimitPerKey(t *testing.T) {
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
		WithConnectionLimits(ConnectionLimits{
			MaxConnectionsPerIP:  100,
			MaxConnectionsPerKey: 1,
			MaxSubsPerConnection: 64,
			MaxSymbolsPerConn:    64,
		}),
		WithSessionSpawner(func(cfg deliveryruntime.SessionConfig) *actor.PID {
			return &actor.PID{}
		}),
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", srv.HandleUpgrade)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listener unavailable in this environment: %v", err)
		return
	}
	httpSrv := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second}
	go func() { _ = httpSrv.Serve(ln) }()
	defer func() { _ = httpSrv.Shutdown(context.Background()) }()

	header := http.Header{}
	header.Set("X-API-Key", "k1")
	conn1, resp1, err := websocket.DefaultDialer.Dial(wsURLFromHTTP("http://"+ln.Addr().String())+"/ws", header)
	if err != nil {
		t.Fatalf("first dial failed: %v", err)
	}
	defer func() { _ = conn1.Close() }()
	if resp1 == nil || resp1.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("first status=%v want=%d", resp1.StatusCode, http.StatusSwitchingProtocols)
	}

	conn2, resp2, err := websocket.DefaultDialer.Dial(wsURLFromHTTP("http://"+ln.Addr().String())+"/ws", header)
	if err == nil {
		_ = conn2.Close()
		t.Fatalf("expected second dial to fail due connection limit")
	}
	if resp2 == nil || resp2.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second status=%v want=%d", func() int {
			if resp2 == nil {
				return 0
			}
			return resp2.StatusCode
		}(), http.StatusTooManyRequests)
	}
}

// ── Legacy gate ──────────────────────────────────────────────────────────────

func TestHandleLegacyWS_Returns410WhenDisabled(t *testing.T) {
	srv := NewServer(
		nil,
		&actor.PID{},
		nil,
		nil,
		256,
		WithAllowLegacy(false),
	)
	req := httptest.NewRequest("GET", "/ws/marketdata", nil)
	rec := httptest.NewRecorder()
	srv.HandleLegacyWS(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("status=%d want=%d", rec.Code, http.StatusGone)
	}
}

func TestHandleLegacyWS_ProceedsWhenEnabled(t *testing.T) {
	srv := NewServer(
		nil,
		&actor.PID{},
		nil,
		nil,
		256,
		WithAllowLegacy(true),
		WithAuthConfig(AuthConfig{
			Enabled: true,
			APIKeys: map[string]string{"k1": "client-a"},
		}),
	)
	// Request without auth key should reach auth check (not 410).
	req := httptest.NewRequest("GET", "/ws/marketdata", nil)
	rec := httptest.NewRecorder()
	srv.HandleLegacyWS(rec, req)

	// Should get 401 (auth failure), not 410 (gone).
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want=%d (auth check should be reached)", rec.Code, http.StatusUnauthorized)
	}
}

func TestNewServer_DefaultAllowLegacyTrue(t *testing.T) {
	srv := NewServer(nil, &actor.PID{}, nil, nil, 256)
	if !srv.allowLegacy {
		t.Fatal("allowLegacy should default to true")
	}
}
