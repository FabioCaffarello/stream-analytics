package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	workspaceapp "github.com/FabioCaffarello/stream-analytics/internal/core/workspace/app"
	"github.com/FabioCaffarello/stream-analytics/internal/core/workspace/domain"
)

// ---------------------------------------------------------------------------
// In-memory test repository
// ---------------------------------------------------------------------------

type memWorkspaceRepo struct {
	ws *domain.Workspace
}

func (m *memWorkspaceRepo) Load() (*domain.Workspace, error) { return m.ws, nil }
func (m *memWorkspaceRepo) Save(ws *domain.Workspace) error  { m.ws = ws; return nil }
func (m *memWorkspaceRepo) Delete() error                    { m.ws = nil; return nil }

// newTestServerWithWorkspace creates a minimal Server with workspace routes
// wired for testing. When svc is nil the handler treats it as "not implemented".
func newTestServerWithWorkspace(svc *workspaceapp.WorkspaceService) *Server {
	mux := http.NewServeMux()
	s := &Server{mux: mux, workspaceSvc: svc}
	mux.HandleFunc("GET /api/v1/workspace", s.handleGetWorkspace)
	mux.HandleFunc("PUT /api/v1/workspace", s.handlePutWorkspace)
	mux.HandleFunc("DELETE /api/v1/workspace", s.handleDeleteWorkspace)
	return s
}

func newTestSvc() (*workspaceapp.WorkspaceService, *memWorkspaceRepo) {
	repo := &memWorkspaceRepo{}
	return workspaceapp.NewWorkspaceService(repo), repo
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func workspacePUT(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/workspace", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	srv.mux.ServeHTTP(rr, req)
	return rr
}

func workspaceGET(t *testing.T, srv *Server) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspace", nil)
	srv.mux.ServeHTTP(rr, req)
	return rr
}

func workspaceDELETE(t *testing.T, srv *Server) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/workspace", nil)
	srv.mux.ServeHTTP(rr, req)
	return rr
}

// ---------------------------------------------------------------------------
// GET /api/v1/workspace
// ---------------------------------------------------------------------------

func TestGetWorkspace_NoSavedState_204(t *testing.T) {
	svc, _ := newTestSvc()
	srv := newTestServerWithWorkspace(svc)
	rr := workspaceGET(t, srv)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Body.Len() != 0 {
		t.Errorf("expected empty body for 204, got %d bytes", rr.Body.Len())
	}
}

func TestGetWorkspace_SavedState_200(t *testing.T) {
	svc, repo := newTestSvc()
	repo.ws = domain.Reconstitute(10, "V6|cell1|cell2", "abc123", map[string]string{"theme": "dark"}, 1709971200000)
	srv := newTestServerWithWorkspace(svc)
	rr := workspaceGET(t, srv)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp workspaceGetResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SchemaVersion != 10 {
		t.Errorf("schema_version: expected 10, got %d", resp.SchemaVersion)
	}
	if resp.LayoutV6 != "V6|cell1|cell2" {
		t.Errorf("layout_v6: expected V6|cell1|cell2, got %s", resp.LayoutV6)
	}
	if resp.Fingerprint != "abc123" {
		t.Errorf("fingerprint: expected abc123, got %s", resp.Fingerprint)
	}
	if resp.Settings["theme"] != "dark" {
		t.Errorf("settings[theme]: expected dark, got %s", resp.Settings["theme"])
	}
	if resp.SavedAtMs != 1709971200000 {
		t.Errorf("saved_at_ms: expected 1709971200000, got %d", resp.SavedAtMs)
	}
}

func TestGetWorkspace_JSONShape(t *testing.T) {
	svc, repo := newTestSvc()
	repo.ws = domain.Reconstitute(10, "V6|x", "f", map[string]string{}, 1)
	srv := newTestServerWithWorkspace(svc)
	rr := workspaceGET(t, srv)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(rr.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	expectedKeys := []string{"schema_version", "layout_v6", "fingerprint", "settings", "saved_at_ms"}
	for _, key := range expectedKeys {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing top-level key: %s", key)
		}
	}
}

func TestGetWorkspace_NilStore_NotImplemented(t *testing.T) {
	srv := newTestServerWithWorkspace(nil)
	rr := workspaceGET(t, srv)

	// Accept either 501 or 404 — both are valid for "store not configured".
	if rr.Code != http.StatusNotImplemented && rr.Code != http.StatusNotFound {
		t.Fatalf("expected 501 or 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// PUT /api/v1/workspace
// ---------------------------------------------------------------------------

func TestPutWorkspace_ValidPayload_200(t *testing.T) {
	svc, _ := newTestSvc()
	srv := newTestServerWithWorkspace(svc)
	body := `{"schema_version":10,"layout_v6":"V6|a|b","settings":{"theme":"dark"}}`
	rr := workspacePUT(t, srv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if saved, ok := resp["saved"].(bool); !ok || !saved {
		t.Errorf("expected saved=true, got %v", resp["saved"])
	}
	if _, ok := resp["fingerprint"]; !ok {
		t.Error("expected fingerprint in response")
	}
	fp, _ := resp["fingerprint"].(string)
	if fp == "" {
		t.Error("fingerprint should be non-empty")
	}
	if _, ok := resp["saved_at_ms"]; !ok {
		t.Error("expected saved_at_ms in response")
	}
	savedAtMs, _ := resp["saved_at_ms"].(float64)
	if savedAtMs <= 0 {
		t.Errorf("saved_at_ms should be positive, got %v", savedAtMs)
	}
}

func TestPutWorkspace_EmptyBody_400(t *testing.T) {
	svc, _ := newTestSvc()
	srv := newTestServerWithWorkspace(svc)
	rr := workspacePUT(t, srv, "")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPutWorkspace_MissingLayoutV6_400(t *testing.T) {
	svc, _ := newTestSvc()
	srv := newTestServerWithWorkspace(svc)
	body := `{"schema_version":10,"settings":{}}`
	rr := workspacePUT(t, srv, body)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPutWorkspace_MissingSchemaVersion_400(t *testing.T) {
	svc, _ := newTestSvc()
	srv := newTestServerWithWorkspace(svc)
	body := `{"layout_v6":"V6|a","settings":{}}`
	rr := workspacePUT(t, srv, body)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPutWorkspace_LayoutV6_InvalidPrefix_400(t *testing.T) {
	svc, _ := newTestSvc()
	srv := newTestServerWithWorkspace(svc)
	body := `{"schema_version":10,"layout_v6":"INVALID|a|b","settings":{}}`
	rr := workspacePUT(t, srv, body)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPutWorkspace_FutureSchemaVersion_409(t *testing.T) {
	svc, _ := newTestSvc()
	srv := newTestServerWithWorkspace(svc)
	body := `{"schema_version":999,"layout_v6":"V6|a","settings":{}}`
	rr := workspacePUT(t, srv, body)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPutWorkspace_IdempotentSave_SameFingerprint(t *testing.T) {
	svc, _ := newTestSvc()
	srv := newTestServerWithWorkspace(svc)

	body := `{"schema_version":10,"layout_v6":"V6|a|b","settings":{"theme":"dark"}}`

	// First save
	rr1 := workspacePUT(t, srv, body)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first PUT: expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}
	var resp1 map[string]any
	if err := json.NewDecoder(rr1.Body).Decode(&resp1); err != nil {
		t.Fatalf("decode first: %v", err)
	}
	fp1, _ := resp1["fingerprint"].(string)

	// Second save with identical payload
	rr2 := workspacePUT(t, srv, body)
	if rr2.Code != http.StatusOK {
		t.Fatalf("second PUT: expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}
	var resp2 map[string]any
	if err := json.NewDecoder(rr2.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	fp2, _ := resp2["fingerprint"].(string)

	if fp1 != fp2 {
		t.Errorf("identical payloads should produce same fingerprint: %s vs %s", fp1, fp2)
	}
}

func TestPutWorkspace_OverwriteNewFingerprint(t *testing.T) {
	svc, _ := newTestSvc()
	srv := newTestServerWithWorkspace(svc)

	body1 := `{"schema_version":10,"layout_v6":"V6|a","settings":{}}`
	body2 := `{"schema_version":10,"layout_v6":"V6|b","settings":{}}`

	rr1 := workspacePUT(t, srv, body1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first PUT: expected 200, got %d", rr1.Code)
	}
	var resp1 map[string]any
	if err := json.NewDecoder(rr1.Body).Decode(&resp1); err != nil {
		t.Fatalf("decode first: %v", err)
	}
	fp1, _ := resp1["fingerprint"].(string)

	rr2 := workspacePUT(t, srv, body2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("second PUT: expected 200, got %d", rr2.Code)
	}
	var resp2 map[string]any
	if err := json.NewDecoder(rr2.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	fp2, _ := resp2["fingerprint"].(string)

	if fp1 == fp2 {
		t.Error("different layouts should produce different fingerprints")
	}
}

func TestPutWorkspace_NilStore_NotImplemented(t *testing.T) {
	srv := newTestServerWithWorkspace(nil)
	body := `{"schema_version":10,"layout_v6":"V6|a","settings":{}}`
	rr := workspacePUT(t, srv, body)

	if rr.Code != http.StatusNotImplemented && rr.Code != http.StatusNotFound {
		t.Fatalf("expected 501 or 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// DELETE /api/v1/workspace
// ---------------------------------------------------------------------------

func TestDeleteWorkspace_ReturnsFirstRun(t *testing.T) {
	svc, repo := newTestSvc()
	repo.ws = domain.Reconstitute(10, "V6|a", "fp", nil, 1000)
	srv := newTestServerWithWorkspace(svc)

	rr := workspaceDELETE(t, srv)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}

	// GET should return 204 (first-run) after DELETE.
	rrGet := workspaceGET(t, srv)
	if rrGet.Code != http.StatusNoContent {
		t.Fatalf("expected 204 after delete, got %d", rrGet.Code)
	}
}

func TestDeleteWorkspace_AlreadyEmpty_204(t *testing.T) {
	svc, _ := newTestSvc()
	srv := newTestServerWithWorkspace(svc)

	rr := workspaceDELETE(t, srv)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestDeleteWorkspace_NilStore_NotImplemented(t *testing.T) {
	srv := newTestServerWithWorkspace(nil)
	rr := workspaceDELETE(t, srv)

	if rr.Code != http.StatusNotImplemented && rr.Code != http.StatusNotFound {
		t.Fatalf("expected 501 or 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestDeleteWorkspace_ThenPut_ThenGet(t *testing.T) {
	svc, repo := newTestSvc()
	repo.ws = domain.Reconstitute(10, "V6|old", "fp", nil, 1000)
	srv := newTestServerWithWorkspace(svc)

	// Delete existing
	workspaceDELETE(t, srv)

	// PUT new
	body := `{"schema_version":10,"layout_v6":"V6|new","settings":{}}`
	rrPut := workspacePUT(t, srv, body)
	if rrPut.Code != http.StatusOK {
		t.Fatalf("PUT after DELETE: expected 200, got %d", rrPut.Code)
	}

	// GET should return new
	rrGet := workspaceGET(t, srv)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("GET after PUT: expected 200, got %d", rrGet.Code)
	}
	var resp workspaceGetResponse
	if err := json.NewDecoder(rrGet.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.LayoutV6 != "V6|new" {
		t.Errorf("expected V6|new, got %s", resp.LayoutV6)
	}
}

// ---------------------------------------------------------------------------
// Integration: PUT then GET round-trip
// ---------------------------------------------------------------------------

func TestWorkspace_PutThenGet_RoundTrip(t *testing.T) {
	svc, _ := newTestSvc()
	srv := newTestServerWithWorkspace(svc)

	payload := `{"schema_version":10,"layout_v6":"V6|cell1|cell2|cell3","settings":{"theme":"light","zoom":"1.5"}}`
	rrPut := workspacePUT(t, srv, payload)
	if rrPut.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d: %s", rrPut.Code, rrPut.Body.String())
	}
	var putResp map[string]any
	if err := json.NewDecoder(rrPut.Body).Decode(&putResp); err != nil {
		t.Fatalf("decode PUT: %v", err)
	}
	putFP, _ := putResp["fingerprint"].(string)

	rrGet := workspaceGET(t, srv)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d: %s", rrGet.Code, rrGet.Body.String())
	}

	var state workspaceGetResponse
	if err := json.NewDecoder(rrGet.Body).Decode(&state); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if state.SchemaVersion != 10 {
		t.Errorf("schema_version: expected 10, got %d", state.SchemaVersion)
	}
	if state.LayoutV6 != "V6|cell1|cell2|cell3" {
		t.Errorf("layout_v6: expected V6|cell1|cell2|cell3, got %s", state.LayoutV6)
	}
	if state.Fingerprint != putFP {
		t.Errorf("fingerprint mismatch: PUT returned %s, GET returned %s", putFP, state.Fingerprint)
	}
	if state.Settings["theme"] != "light" {
		t.Errorf("settings[theme]: expected light, got %s", state.Settings["theme"])
	}
	if state.Settings["zoom"] != "1.5" {
		t.Errorf("settings[zoom]: expected 1.5, got %s", state.Settings["zoom"])
	}
	if state.SavedAtMs <= 0 {
		t.Error("saved_at_ms should be positive after save")
	}
}

func TestWorkspace_FirstGet204_ThenPut_ThenGet200(t *testing.T) {
	svc, _ := newTestSvc()
	srv := newTestServerWithWorkspace(svc)

	// First GET should be 204 (no saved state)
	rr1 := workspaceGET(t, srv)
	if rr1.Code != http.StatusNoContent {
		t.Fatalf("first GET: expected 204, got %d", rr1.Code)
	}

	// PUT a workspace
	payload := `{"schema_version":10,"layout_v6":"V6|main","settings":{}}`
	rrPut := workspacePUT(t, srv, payload)
	if rrPut.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d: %s", rrPut.Code, rrPut.Body.String())
	}

	// Second GET should be 200 with data
	rr2 := workspaceGET(t, srv)
	if rr2.Code != http.StatusOK {
		t.Fatalf("second GET: expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}

	var state workspaceGetResponse
	if err := json.NewDecoder(rr2.Body).Decode(&state); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if state.LayoutV6 != "V6|main" {
		t.Errorf("layout_v6: expected V6|main, got %s", state.LayoutV6)
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestPutWorkspace_MalformedJSON_400(t *testing.T) {
	svc, _ := newTestSvc()
	srv := newTestServerWithWorkspace(svc)
	rr := workspacePUT(t, srv, `{not json}`)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPutWorkspace_LayoutV6_ExactPrefix(t *testing.T) {
	svc, _ := newTestSvc()
	srv := newTestServerWithWorkspace(svc)

	// "V6" alone (no pipe) should still be accepted as a valid prefix.
	body := `{"schema_version":10,"layout_v6":"V6","settings":{}}`
	rr := workspacePUT(t, srv, body)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for bare V6 prefix, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPutWorkspace_LargePayload(t *testing.T) {
	svc, _ := newTestSvc()
	srv := newTestServerWithWorkspace(svc)

	// Build a large but valid layout string.
	var buf bytes.Buffer
	buf.WriteString("V6")
	for i := 0; i < 500; i++ {
		buf.WriteString("|cell")
	}
	payload := map[string]any{
		"schema_version": 10,
		"layout_v6":      buf.String(),
		"settings":       map[string]string{},
	}
	body, _ := json.Marshal(payload)
	rr := workspacePUT(t, srv, string(body))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for large payload, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPutWorkspace_NullSettings_Accepted(t *testing.T) {
	svc, _ := newTestSvc()
	srv := newTestServerWithWorkspace(svc)
	// Settings is null/missing in JSON — handler should accept and default
	// to empty map or nil.
	body := `{"schema_version":10,"layout_v6":"V6|a"}`
	rr := workspacePUT(t, srv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for null settings, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPutWorkspace_SchemaVersionZero_400(t *testing.T) {
	svc, _ := newTestSvc()
	srv := newTestServerWithWorkspace(svc)
	body := `{"schema_version":0,"layout_v6":"V6|a","settings":{}}`
	rr := workspacePUT(t, srv, body)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for schema_version=0, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPutWorkspace_NegativeSchemaVersion_400(t *testing.T) {
	svc, _ := newTestSvc()
	srv := newTestServerWithWorkspace(svc)
	body := `{"schema_version":-1,"layout_v6":"V6|a","settings":{}}`
	rr := workspacePUT(t, srv, body)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative schema_version, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPutWorkspace_ResponseIncludesSavedAtMs(t *testing.T) {
	svc, _ := newTestSvc()
	srv := newTestServerWithWorkspace(svc)
	body := `{"schema_version":10,"layout_v6":"V6|ts","settings":{}}`
	rr := workspacePUT(t, srv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["saved_at_ms"]; !ok {
		t.Error("response should include saved_at_ms")
	}
	ts, _ := resp["saved_at_ms"].(float64)
	if ts <= 0 {
		t.Errorf("saved_at_ms should be positive, got %v", ts)
	}
}
