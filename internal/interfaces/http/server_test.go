package httpserver_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	httpserver "github.com/market-raccoon/internal/interfaces/http"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newEngine(t *testing.T) *actor.Engine {
	t.Helper()
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	return e
}

func newGuardian(t *testing.T, e *actor.Engine) *actor.PID {
	t.Helper()
	// Use test name as suffix to avoid "actor name already claimed" warnings
	// when multiple tests share the same engine process namespace.
	id := "guardian-" + strings.ReplaceAll(t.Name(), "/", "-")
	pid := e.Spawn(
		actorruntime.NewGuardian(actorruntime.GuardianConfig{}),
		"guardian",
		actor.WithID(id),
	)
	// Give Guardian time to start its children.
	time.Sleep(50 * time.Millisecond)
	return pid
}

func newTestServer(e *actor.Engine, guardianPID *actor.PID) *httpserver.Server {
	srv := httpserver.NewServer(e, guardianPID, ":0", false, nil)
	// Use a tight timeout for tests.
	srv.SetSnapshotTimeout(2 * time.Second)
	return srv
}

// doRequest issues an HTTP request against the server's Handler (no real TCP).
func doRequest(t *testing.T, srv *httpserver.Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *strings.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	} else {
		reqBody = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reqBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// ---------------------------------------------------------------------------
// GET /healthz
// ---------------------------------------------------------------------------

func TestServer_Healthz_returns200(t *testing.T) {
	e := newEngine(t)
	guardianPID := newGuardian(t, e)
	defer e.Poison(guardianPID)

	srv := newTestServer(e, guardianPID)
	rec := doRequest(t, srv, http.MethodGet, "/healthz", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestServer_Healthz_returnsJSON(t *testing.T) {
	e := newEngine(t)
	guardianPID := newGuardian(t, e)
	defer e.Poison(guardianPID)

	srv := newTestServer(e, guardianPID)
	rec := doRequest(t, srv, http.MethodGet, "/healthz", "")

	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected application/json Content-Type, got %q", ct)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v\nbody: %s", err, rec.Body.String())
	}
	if body["status"] == "" {
		t.Fatalf("expected status field, got %#v", body)
	}
	if _, ok := body["ws_connected"]; !ok {
		t.Fatalf("expected ws_connected field, got %#v", body)
	}
	if _, ok := body["last_message_age_ms"]; !ok {
		t.Fatalf("expected last_message_age_ms field, got %#v", body)
	}
	if _, ok := body["last_publish_age_ms"]; !ok {
		t.Fatalf("expected last_publish_age_ms field, got %#v", body)
	}
}

// ---------------------------------------------------------------------------
// GET /runtime/snapshot
// ---------------------------------------------------------------------------

func TestServer_Snapshot_returns200(t *testing.T) {
	e := newEngine(t)
	guardianPID := newGuardian(t, e)
	defer e.Poison(guardianPID)

	srv := newTestServer(e, guardianPID)
	rec := doRequest(t, srv, http.MethodGet, "/runtime/snapshot", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", rec.Code, rec.Body.String())
	}
}

func TestServer_Snapshot_containsSubsystemsField(t *testing.T) {
	e := newEngine(t)
	guardianPID := newGuardian(t, e)
	defer e.Poison(guardianPID)

	srv := newTestServer(e, guardianPID)
	rec := doRequest(t, srv, http.MethodGet, "/runtime/snapshot", "")

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v\nbody: %s", err, rec.Body.String())
	}
	if _, ok := body["Subsystems"]; !ok {
		t.Fatalf("expected 'Subsystems' field in response, got keys: %v", keys(body))
	}
}

func TestServer_Snapshot_containsAllSubsystems(t *testing.T) {
	e := newEngine(t)
	guardianPID := newGuardian(t, e)
	defer e.Poison(guardianPID)

	srv := newTestServer(e, guardianPID)
	rec := doRequest(t, srv, http.MethodGet, "/runtime/snapshot", "")

	var body struct {
		Subsystems map[string]any `json:"Subsystems"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json unmarshal: %v\nbody: %s", err, rec.Body.String())
	}

	expected := []string{"marketdata", "aggregation", "delivery", "insights"}
	for _, sub := range expected {
		if _, ok := body.Subsystems[sub]; !ok {
			t.Errorf("missing subsystem %q in snapshot; got: %v", sub, keys(body.Subsystems))
		}
	}
}

// TestServer_Snapshot_timeout verifies that the handler returns 504 when the
// guardian does not respond within the configured timeout.
func TestServer_Snapshot_timeout(t *testing.T) {
	e := newEngine(t)
	// Spawn a guardian that never responds to Snapshot messages.
	silentPID := e.Spawn(func() actor.Receiver {
		return &silentActor{}
	}, "silent", actor.WithID("silent-guardian"))
	defer e.Poison(silentPID)

	srv := httpserver.NewServer(e, silentPID, ":0", false, nil)
	srv.SetSnapshotTimeout(100 * time.Millisecond) // very short for test

	rec := doRequest(t, srv, http.MethodGet, "/runtime/snapshot", "")

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d\nbody: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// POST /runtime/reload
// ---------------------------------------------------------------------------

func TestServer_Reload_returns202(t *testing.T) {
	e := newEngine(t)
	guardianPID := newGuardian(t, e)
	defer e.Poison(guardianPID)

	srv := newTestServer(e, guardianPID)
	rec := doRequest(t, srv, http.MethodPost, "/runtime/reload", "")

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}
}

func TestServer_Reload_returnsAcceptedJSON(t *testing.T) {
	e := newEngine(t)
	guardianPID := newGuardian(t, e)
	defer e.Poison(guardianPID)

	srv := newTestServer(e, guardianPID)
	rec := doRequest(t, srv, http.MethodPost, "/runtime/reload", "")

	var body map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v\nbody: %s", err, rec.Body.String())
	}
	if !body["accepted"] {
		t.Fatalf("expected accepted=true, got %v", body)
	}
}

// ---------------------------------------------------------------------------
// method routing (Go 1.22+ pattern routing)
// ---------------------------------------------------------------------------

func TestServer_WrongMethod_returns405(t *testing.T) {
	e := newEngine(t)
	guardianPID := newGuardian(t, e)
	defer e.Poison(guardianPID)

	srv := newTestServer(e, guardianPID)

	// POST to a GET-only endpoint.
	rec := doRequest(t, srv, http.MethodPost, "/healthz", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestServer_HandleFunc_doesNotOverrideCriticalRoutes(t *testing.T) {
	e := newEngine(t)
	guardianPID := newGuardian(t, e)
	defer e.Poison(guardianPID)

	srv := newTestServer(e, guardianPID)
	srv.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	rec := doRequest(t, srv, http.MethodGet, "/healthz", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from built-in healthz, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /readyz
// ---------------------------------------------------------------------------

func TestServer_Readyz_NoExpectedSubsystems_Returns200(t *testing.T) {
	e := newEngine(t)
	// Guardian with no factories and nil ExpectedSubsystems → always ready.
	pid := e.Spawn(
		actorruntime.NewGuardian(actorruntime.GuardianConfig{}),
		"guardian",
		actor.WithID("guardian-readyz-ready"),
	)
	time.Sleep(50 * time.Millisecond)
	defer e.Poison(pid)

	srv := newTestServer(e, pid)
	rec := doRequest(t, srv, http.MethodGet, "/readyz", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if ready, _ := body["ready"].(bool); !ready {
		t.Fatalf("expected ready=true, got body: %v", body)
	}
}

func TestServer_Readyz_PendingSubsystems_Returns503(t *testing.T) {
	e := newEngine(t)
	// Use a controlled actor that always answers ReadyQuery with Ready=false.
	// This tests the HTTP 503 path without relying on Guardian's internal
	// readySystems timing, which is non-deterministic in tests.
	notReadyPID := e.Spawn(func() actor.Receiver {
		return &notReadyActor{}
	}, "notready", actor.WithID("notready-guardian"))
	defer e.Poison(notReadyPID)

	srv := newTestServer(e, notReadyPID)
	rec := doRequest(t, srv, http.MethodGet, "/readyz", "")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d\nbody: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if ready, _ := body["ready"].(bool); ready {
		t.Fatalf("expected ready=false, got body: %v", body)
	}
}

func TestServer_Readyz_Timeout_Returns504(t *testing.T) {
	e := newEngine(t)
	silentPID := e.Spawn(func() actor.Receiver {
		return &silentActor{}
	}, "silent2", actor.WithID("silent-guardian-readyz"))
	defer e.Poison(silentPID)

	srv := httpserver.NewServer(e, silentPID, ":0", false, nil)
	srv.SetSnapshotTimeout(100 * time.Millisecond)

	rec := doRequest(t, srv, http.MethodGet, "/readyz", "")
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", rec.Code)
	}
}

func TestServer_Metrics_ExposesPrometheusFormat(t *testing.T) {
	e := newEngine(t)
	guardianPID := newGuardian(t, e)
	defer e.Poison(guardianPID)

	srv := newTestServer(e, guardianPID)
	rec := doRequest(t, srv, http.MethodGet, "/metrics", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Fatalf("expected text/plain content type, got %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "ingest_messages_total") {
		t.Fatalf("expected ingest_messages_total in metrics output")
	}
	if !strings.Contains(body, "process_goroutines") {
		t.Fatalf("expected process_goroutines in metrics output")
	}
}

func TestServer_Pprof_DisabledReturns404(t *testing.T) {
	e := newEngine(t)
	guardianPID := newGuardian(t, e)
	defer e.Poison(guardianPID)

	srv := httpserver.NewServer(e, guardianPID, ":0", false, nil)
	rec := doRequest(t, srv, http.MethodGet, "/debug/pprof/goroutine", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestServer_Pprof_EnabledLocalhostAllowed(t *testing.T) {
	e := newEngine(t)
	guardianPID := newGuardian(t, e)
	defer e.Poison(guardianPID)

	srv := httpserver.NewServer(e, guardianPID, ":0", true, nil)
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/goroutine?debug=1", strings.NewReader(""))
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "goroutine") {
		t.Fatalf("expected goroutine profile output")
	}
}

func TestServer_Pprof_EnabledRemoteForbidden(t *testing.T) {
	e := newEngine(t)
	guardianPID := newGuardian(t, e)
	defer e.Poison(guardianPID)

	srv := httpserver.NewServer(e, guardianPID, ":0", true, nil)
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/goroutine?debug=1", strings.NewReader(""))
	req.RemoteAddr = "203.0.113.10:12345"
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// test doubles
// ---------------------------------------------------------------------------

// silentActor never responds to any message — used to test the timeout path.
type silentActor struct{}

func (s *silentActor) Receive(c *actor.Context) {}

// notReadyActor replies to ReadyQuery with Ready=false.
type notReadyActor struct{}

func (n *notReadyActor) Receive(c *actor.Context) {
	if _, ok := c.Message().(actorruntime.ReadyQuery); ok {
		replyTo := c.Sender()
		if replyTo != nil {
			c.Send(replyTo, actorruntime.ReadyResponse{
				Ready:   false,
				Pending: []actorruntime.Subsystem{actorruntime.SubsystemMarketData},
			})
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func keys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
