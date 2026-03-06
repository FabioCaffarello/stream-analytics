package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	executionapp "github.com/market-raccoon/internal/core/execution/app"
	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
)

func newTestControlServer(cp *executionapp.InMemoryControlPlane) *Server {
	s := &Server{
		controlPlane: cp,
	}
	return s
}

func TestControlSnapshot_ReturnsActiveByDefault(t *testing.T) {
	cp := executionapp.NewInMemoryControlPlane()
	s := newTestControlServer(cp)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/control/snapshot", nil)
	s.handleControlSnapshot(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var dto controlSnapshotDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.State != "active" {
		t.Fatalf("state = %q, want active", dto.State)
	}
}

func TestControlApply_PauseAndResume(t *testing.T) {
	cp := executionapp.NewInMemoryControlPlane()
	s := newTestControlServer(cp)

	// Pause
	body := controlDirectiveRequest{
		Command: "pause",
		Issuer:  "test-operator",
		Reason:  "maintenance",
	}
	rec := postControl(t, s, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("pause: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var dto controlSnapshotDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.State != "paused" {
		t.Fatalf("state = %q, want paused", dto.State)
	}
	if dto.LastDirective == nil || dto.LastDirective.Issuer != "test-operator" {
		t.Fatal("last_directive not populated")
	}

	// Resume
	body.Command = "resume"
	rec = postControl(t, s, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("resume: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.State != "active" {
		t.Fatalf("state = %q, want active", dto.State)
	}
}

func TestControlApply_Halt(t *testing.T) {
	cp := executionapp.NewInMemoryControlPlane()
	s := newTestControlServer(cp)

	body := controlDirectiveRequest{
		Command: "halt",
		Issuer:  "emergency",
		Reason:  "incident",
	}
	rec := postControl(t, s, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("halt: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var dto controlSnapshotDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.State != "halted" {
		t.Fatalf("state = %q, want halted", dto.State)
	}

	// Resume from halted should fail
	body.Command = "resume"
	rec = postControl(t, s, body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("resume from halted: expected 409, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestControlApply_DisableStrategy(t *testing.T) {
	cp := executionapp.NewInMemoryControlPlane()
	s := newTestControlServer(cp)

	body := controlDirectiveRequest{
		Command:  "disable_strategy",
		TargetID: "momentum-v1",
		Issuer:   "operator",
	}
	rec := postControl(t, s, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var dto controlSnapshotDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, s := range dto.DisabledStrategies {
		if s == "momentum-v1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("disabled_strategies does not contain momentum-v1: %v", dto.DisabledStrategies)
	}

	// Verify control plane state directly
	snap := cp.Snapshot()
	allowed, reason := snap.IsExecutionAllowed("momentum-v1", "", "", "")
	if allowed {
		t.Fatal("execution should be denied for disabled strategy")
	}
	if reason != executiondomain.ReasonControlPlaneStrategyDisabled {
		t.Fatalf("reason = %q, want %q", reason, executiondomain.ReasonControlPlaneStrategyDisabled)
	}
}

func TestControlApply_InvalidCommand(t *testing.T) {
	cp := executionapp.NewInMemoryControlPlane()
	s := newTestControlServer(cp)

	body := controlDirectiveRequest{
		Command: "explode",
		Issuer:  "operator",
	}
	rec := postControl(t, s, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid command: expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestControlApply_MissingIssuer(t *testing.T) {
	cp := executionapp.NewInMemoryControlPlane()
	s := newTestControlServer(cp)

	body := controlDirectiveRequest{
		Command: "pause",
	}
	rec := postControl(t, s, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing issuer: expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestControlSnapshot_NilControlPlane(t *testing.T) {
	s := &Server{}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/control/snapshot", nil)
	s.handleControlSnapshot(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestControlApply_NilControlPlane(t *testing.T) {
	s := &Server{}

	rec := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"command":"pause","issuer":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/control", body)
	req.Header.Set("Content-Type", "application/json")
	s.handleControlApply(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestControlApply_InvalidJSON(t *testing.T) {
	cp := executionapp.NewInMemoryControlPlane()
	s := newTestControlServer(cp)

	rec := httptest.NewRecorder()
	body := bytes.NewBufferString(`{invalid`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/control", body)
	req.Header.Set("Content-Type", "application/json")
	s.handleControlApply(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestControlApply_DefaultsIssuedAtMs(t *testing.T) {
	cp := executionapp.NewInMemoryControlPlane()
	s := newTestControlServer(cp)

	body := controlDirectiveRequest{
		Command: "pause",
		Issuer:  "operator",
		// IssuedAtMs intentionally omitted
	}
	rec := postControl(t, s, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var dto controlSnapshotDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.UpdatedAtMs <= 0 {
		t.Fatal("updated_at_ms should be populated with server time")
	}
}

func postControl(t *testing.T, s *Server, body controlDirectiveRequest) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/control", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	s.handleControlApply(rec, req)
	return rec
}
