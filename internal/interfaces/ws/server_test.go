package wsserver

import (
	"net/http/httptest"
	"testing"
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
