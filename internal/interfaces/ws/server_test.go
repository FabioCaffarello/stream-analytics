package wsserver

import (
	"net/http/httptest"
	"testing"

	"github.com/anthdm/hollywood/actor"
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
	srv.HandleWS(rec, req)

	if rec.Code != 401 {
		t.Fatalf("status=%d want=401", rec.Code)
	}
}

func TestHandleWS_AuthAllowsKnownKey(t *testing.T) {
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

	req := httptest.NewRequest("GET", "/ws?api_key=k1", nil)
	rec := httptest.NewRecorder()
	srv.HandleWS(rec, req)

	if rec.Code == 401 {
		t.Fatal("expected request to pass auth check")
	}
}
