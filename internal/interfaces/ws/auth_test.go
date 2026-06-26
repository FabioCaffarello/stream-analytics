package wsserver

import (
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestAuthConfig_Disabled_AllowsAnonymous(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)
	principal, p := (AuthConfig{}).Authenticate(req)
	if p != nil {
		t.Fatalf("expected nil problem, got %v", p)
	}
	if principal.ClientID != "anonymous" {
		t.Fatalf("clientID=%q want anonymous", principal.ClientID)
	}
	if !principal.HasScope("read") {
		t.Fatalf("expected read scope, scopes=%v", principal.Scopes)
	}
}

func TestAuthConfig_Authenticate_Header(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("X-API-Key", "k1")
	principal, p := (AuthConfig{
		Enabled: true,
		APIKeys: map[string]string{"k1": "client-a"},
	}).Authenticate(req)
	if p != nil {
		t.Fatalf("expected nil problem, got %v", p)
	}
	if principal.ClientID != "client-a" {
		t.Fatalf("clientID=%q want client-a", principal.ClientID)
	}
	if !principal.HasScope("read") {
		t.Fatalf("expected default read scope, scopes=%v", principal.Scopes)
	}
}

func TestAuthConfig_Authenticate_HeaderOnly(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("X-API-Key", "k1")
	principal, p := (AuthConfig{
		Enabled: true,
		APIKeys: map[string]string{"k1": "client-a"},
	}).Authenticate(req)
	if p != nil || principal.ClientID != "client-a" {
		t.Fatalf("header failed: client=%q problem=%v", principal.ClientID, p)
	}
}

func TestAuthConfig_Authenticate_QueryParam(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws?api_key=k1", nil)
	principal, p := (AuthConfig{
		Enabled: true,
		APIKeys: map[string]string{"k1": "client-a"},
	}).Authenticate(req)
	if p != nil {
		t.Fatalf("expected nil problem, got %v", p)
	}
	if principal.ClientID != "client-a" {
		t.Fatalf("clientID=%q want client-a", principal.ClientID)
	}
}

func TestAuthConfig_Authenticate_HeaderTakesPrecedence(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws?api_key=k2", nil)
	req.Header.Set("X-API-Key", "k1")
	principal, p := (AuthConfig{
		Enabled: true,
		APIKeys: map[string]string{"k1": "client-a", "k2": "client-b"},
	}).Authenticate(req)
	if p != nil {
		t.Fatalf("expected nil problem, got %v", p)
	}
	if principal.ClientID != "client-a" {
		t.Fatalf("clientID=%q want client-a (header takes precedence)", principal.ClientID)
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

func TestAuthConfig_Authenticate_APIKeyScopes(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("X-API-Key", "k1")
	principal, p := (AuthConfig{
		Enabled:      true,
		APIKeys:      map[string]string{"k1": "client-a"},
		APIKeyScopes: map[string][]string{"k1": {"read", "ops"}},
	}).Authenticate(req)
	if p != nil {
		t.Fatalf("expected nil problem, got %v", p)
	}
	if !principal.HasScope("read") || !principal.HasScope("ops") {
		t.Fatalf("missing expected scopes: %#v", principal.Scopes)
	}
}

func TestAuthConfig_Authenticate_JWT(t *testing.T) {
	claims := jwt.MapClaims{
		"sub":       "client-jwt",
		"tenant_id": "tenant-a",
		"scope":     "read",
		"iss":       "stream-analytics",
		"aud":       "odin",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	raw, err := token.SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	principal, p := (AuthConfig{
		Enabled: true,
		JWT: JWTAuthConfig{
			Enabled:     true,
			HS256Secret: "secret",
			Issuer:      "stream-analytics",
			Audience:    "odin",
		},
	}).Authenticate(req)
	if p != nil {
		t.Fatalf("expected nil problem, got %v", p)
	}
	if principal.ClientID != "client-jwt" {
		t.Fatalf("client_id=%q want=client-jwt", principal.ClientID)
	}
	if principal.TenantID != "tenant-a" {
		t.Fatalf("tenant_id=%q want=tenant-a", principal.TenantID)
	}
	if !principal.HasScope("read") {
		t.Fatalf("expected read scope in %v", principal.Scopes)
	}
}
