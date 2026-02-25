package wsserver

import (
	"net/http/httptest"
	"testing"
)

func TestAuthConfig_Disabled_AllowsAnonymous(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)
	clientID, p := (AuthConfig{}).Authenticate(req)
	if p != nil {
		t.Fatalf("expected nil problem, got %v", p)
	}
	if clientID != "anonymous" {
		t.Fatalf("clientID=%q want anonymous", clientID)
	}
}

func TestAuthConfig_Authenticate_Header(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("X-API-Key", "k1")
	clientID, p := (AuthConfig{
		Enabled: true,
		APIKeys: map[string]string{"k1": "client-a"},
	}).Authenticate(req)
	if p != nil {
		t.Fatalf("expected nil problem, got %v", p)
	}
	if clientID != "client-a" {
		t.Fatalf("clientID=%q want client-a", clientID)
	}
}

func TestAuthConfig_Authenticate_HeaderOnly(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("X-API-Key", "k1")
	clientID, p := (AuthConfig{
		Enabled: true,
		APIKeys: map[string]string{"k1": "client-a"},
	}).Authenticate(req)
	if p != nil || clientID != "client-a" {
		t.Fatalf("header failed: client=%q problem=%v", clientID, p)
	}
}

func TestAuthConfig_Authenticate_QueryParam(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws?api_key=k1", nil)
	clientID, p := (AuthConfig{
		Enabled: true,
		APIKeys: map[string]string{"k1": "client-a"},
	}).Authenticate(req)
	if p != nil {
		t.Fatalf("expected nil problem, got %v", p)
	}
	if clientID != "client-a" {
		t.Fatalf("clientID=%q want client-a", clientID)
	}
}

func TestAuthConfig_Authenticate_HeaderTakesPrecedence(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws?api_key=k2", nil)
	req.Header.Set("X-API-Key", "k1")
	clientID, p := (AuthConfig{
		Enabled: true,
		APIKeys: map[string]string{"k1": "client-a", "k2": "client-b"},
	}).Authenticate(req)
	if p != nil {
		t.Fatalf("expected nil problem, got %v", p)
	}
	if clientID != "client-a" {
		t.Fatalf("clientID=%q want client-a (header takes precedence)", clientID)
	}
}

func TestAuthConfig_Authenticate_MissingOrInvalid(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)
	_, p := (AuthConfig{Enabled: true, APIKeys: map[string]string{"k1": "client-a"}}).Authenticate(req)
	if p == nil {
		t.Fatal("expected missing api key problem")
	}

	req2 := httptest.NewRequest("GET", "/ws", nil)
	req2.Header.Set("X-API-Key", "unknown")
	_, p = (AuthConfig{Enabled: true, APIKeys: map[string]string{"k1": "client-a"}}).Authenticate(req2)
	if p == nil {
		t.Fatal("expected invalid api key problem")
	}
}
