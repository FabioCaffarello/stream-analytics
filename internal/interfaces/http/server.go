// Package httpserver exposes runtime observability and control endpoints.
//
// v1 routes:
//
//	GET  /healthz            → 200 {"status":"ok"}          (liveness)
//	GET  /readyz             → 200/503 {"ready":bool,...}   (readiness)
//	GET  /runtime/snapshot   → 200 JSON of runtime.SnapshotState
//	POST /runtime/reload     → 202 {"accepted":true}
//
// The snapshot and readyz endpoints use engine.Request() (Hollywood built-in)
// so that no extra actor needs to be spawned per request.  The Guardian's
// Snapshot and ReadyQuery handlers must support falling back to c.Sender()
// when ReplyTo is nil.
package httpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/market-raccoon/internal/actors/runtime"
)

const defaultSnapshotTimeout = 5 * time.Second

// Server wraps net/http and provides runtime HTTP endpoints.
type Server struct {
	engine      *actor.Engine
	guardianPID *actor.PID
	logger      *slog.Logger
	httpServer  *http.Server
	mux         *http.ServeMux

	// snapshotTimeout controls how long the snapshot handler waits for a
	// Guardian response before returning 504.  Settable for testing.
	snapshotTimeout time.Duration
}

// NewServer creates a Server that listens on addr and talks to guardianPID.
// If logger is nil, slog.Default() is used.
func NewServer(
	engine *actor.Engine,
	guardianPID *actor.PID,
	addr string,
	logger *slog.Logger,
) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		engine:          engine,
		guardianPID:     guardianPID,
		logger:          logger,
		snapshotTimeout: defaultSnapshotTimeout,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("GET /runtime/snapshot", s.handleSnapshot)
	mux.HandleFunc("POST /runtime/reload", s.handleReload)
	s.mux = mux

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return s
}

// ListenAndServe starts the HTTP server.  It blocks until the server stops.
func (s *Server) ListenAndServe() error {
	s.logger.Info("http server listening", "addr", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server using the provided context.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// Handler returns the http.Handler used by this server (useful in tests).
func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

// HandleFunc registers an additional method-aware route on the underlying mux.
// It must be called before ListenAndServe.
func (s *Server) HandleFunc(pattern string, handler http.HandlerFunc) {
	switch pattern {
	case "GET /healthz", "GET /readyz", "GET /runtime/snapshot", "POST /runtime/reload":
		s.logger.Warn("httpserver: refusing to override critical route", "pattern", pattern)
		return
	}
	s.mux.HandleFunc(pattern, handler)
}

// SetSnapshotTimeout overrides the default snapshot request timeout.
// Primarily useful in tests to avoid long waits.
func (s *Server) SetSnapshotTimeout(d time.Duration) {
	s.snapshotTimeout = d
}

// ---------------------------------------------------------------------------
// handlers
// ---------------------------------------------------------------------------

// handleHealthz returns 200 OK unconditionally.
// It is a liveness probe: it only checks that the HTTP layer is alive.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReadyz queries the Guardian for readiness state.
// Returns 200 when all expected subsystems are running; 503 otherwise.
// Returns 504 if the Guardian does not respond in time.
func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	resp := s.engine.Request(s.guardianPID, runtime.ReadyQuery{}, s.snapshotTimeout)
	result, err := resp.Result()
	if err != nil {
		s.logger.Warn("readyz request timed out", "err", err)
		writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "readyz timeout"})
		return
	}
	rr, ok := result.(runtime.ReadyResponse)
	if !ok {
		s.logger.Error("unexpected readyz response type")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unexpected response"})
		return
	}
	if !rr.Ready {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ready":   false,
			"pending": rr.Pending,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ready": true})
}

// handleSnapshot queries the Guardian actor and returns its SnapshotState as JSON.
//
// It uses Hollywood's engine.Request() which sends Snapshot{} with the
// internal Response actor as the sender.  The Guardian falls back to
// c.Sender() when Snapshot.ReplyTo is nil.
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	resp := s.engine.Request(s.guardianPID, runtime.Snapshot{}, s.snapshotTimeout)
	result, err := resp.Result()
	if err != nil {
		s.logger.Warn("snapshot request failed", "err", err)
		writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "snapshot timeout"})
		return
	}
	state, ok := result.(runtime.SnapshotState)
	if !ok {
		s.logger.Error("unexpected snapshot response type",
			"type", func() string {
				if result == nil {
					return "<nil>"
				}
				return "unknown"
			}())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unexpected response"})
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// handleReload sends a ReloadConfig message to the Guardian and returns 202.
// The reload is asynchronous — the HTTP response does not wait for completion.
func (s *Server) handleReload(w http.ResponseWriter, _ *http.Request) {
	s.engine.Send(s.guardianPID, runtime.ReloadConfig{})
	writeJSON(w, http.StatusAccepted, map[string]bool{"accepted": true})
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// WriteHeader already sent; nothing we can do except log.
		slog.Error("httpserver: json encode failed", "err", err)
	}
}
