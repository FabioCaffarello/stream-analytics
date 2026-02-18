// Package httpserver exposes runtime observability and control endpoints.
//
// v1 routes:
//
//	GET  /healthz            → 200 {"status":"ok"}          (liveness)
//	GET  /readyz             → 200/503 {"ready":bool,...}   (readiness)
//	GET  /runtime/snapshot   → 200 JSON of runtime.SnapshotState
//	GET  /shardz             → 200/404 JSON shard status    (shard diagnostics)
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
	"net"
	"net/http"
	"net/http/pprof"
	"strings"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/market-raccoon/internal/actors/runtime"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/observability"
)

const defaultSnapshotTimeout = 5 * time.Second
const httpContentTypeProto = "application/x-protobuf"

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

	// readyGate is an optional pre-check for /readyz.  When set and
	// returning false, the endpoint returns 503 without querying the
	// Guardian.  Used by cmd/store to gate readiness on ClickHouse +
	// consumer startup.
	readyGate func() bool

	tlsCertFile string
	tlsKeyFile  string
	wsHandler   http.HandlerFunc
}

type Option func(*Server)

func WithTLS(certFile, keyFile string) Option {
	return func(s *Server) {
		s.tlsCertFile = strings.TrimSpace(certFile)
		s.tlsKeyFile = strings.TrimSpace(keyFile)
	}
}

func WithWSHandler(handler http.HandlerFunc) Option {
	return func(s *Server) {
		s.wsHandler = handler
	}
}

// NewServer creates a Server that listens on addr and talks to guardianPID.
// If logger is nil, slog.Default() is used.
func NewServer(
	engine *actor.Engine,
	guardianPID *actor.PID,
	addr string,
	enablePprof bool,
	logger *slog.Logger,
	opts ...Option,
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
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("GET /runtime/snapshot", s.handleSnapshot)
	mux.HandleFunc("GET /runtime/overload", s.handleRuntimeOverload)
	mux.HandleFunc("GET /runtime/storage", s.handleRuntimeStorage)
	mux.HandleFunc("GET /runtime/ws", s.handleRuntimeWS)
	mux.HandleFunc("GET /shardz", s.handleShardz)
	mux.HandleFunc("POST /runtime/reload", s.handleReload)
	if s.wsHandler != nil {
		mux.HandleFunc("GET /ws", s.wsHandler)
	}
	mux.Handle("GET /metrics", withProcessMetrics(metrics.Handler()))
	if enablePprof {
		s.registerPprofRoutes(mux)
	}
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

// SetReadyGate installs an optional pre-check for the /readyz endpoint.
// When fn returns false, /readyz returns 503 without querying the Guardian.
func (s *Server) SetReadyGate(fn func() bool) { s.readyGate = fn }

// ListenAndServe starts the HTTP server.  It blocks until the server stops.
func (s *Server) ListenAndServe() error {
	s.logger.Info("http server listening", "addr", s.httpServer.Addr)
	if s.tlsCertFile != "" && s.tlsKeyFile != "" {
		return s.httpServer.ListenAndServeTLS(s.tlsCertFile, s.tlsKeyFile)
	}
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
	case "GET /healthz", "GET /readyz", "GET /runtime/snapshot", "GET /runtime/overload", "GET /runtime/storage", "GET /runtime/ws", "GET /shardz", "POST /runtime/reload", "GET /metrics":
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
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	resp := s.engine.Request(s.guardianPID, runtime.Snapshot{}, s.snapshotTimeout)
	result, err := resp.Result()
	if err != nil {
		s.logger.Warn("healthz snapshot timeout", "err", err)
		writeResponse(w, r, http.StatusGatewayTimeout, "runtime.healthz", map[string]string{"error": "healthz timeout"})
		return
	}
	state, ok := result.(runtime.SnapshotState)
	if !ok {
		writeResponse(w, r, http.StatusInternalServerError, "runtime.healthz", map[string]string{"error": "unexpected response"})
		return
	}

	sub, hasMD := state.Subsystems[runtime.SubsystemMarketData]
	lastMsgAge := int64(-1)
	lastPubAge := int64(-1)
	now := time.Now()
	if hasMD && !sub.LastMessageAt.IsZero() {
		lastMsgAge = now.Sub(sub.LastMessageAt).Milliseconds()
	}
	if hasMD && !sub.LastPublishAt.IsZero() {
		lastPubAge = now.Sub(sub.LastPublishAt).Milliseconds()
	}
	status := "ok"
	if hasMD && (!sub.Running || !sub.Connected) {
		status = "degraded"
	}

	writeResponse(w, r, http.StatusOK, "runtime.healthz", map[string]any{
		"status":              status,
		"ws_connected":        sub.Connected,
		"last_message_age_ms": lastMsgAge,
		"last_publish_age_ms": lastPubAge,
	})
}

// handleReadyz queries the Guardian for readiness state.
// Returns 200 when all expected subsystems are running; 503 otherwise.
// Returns 504 if the Guardian does not respond in time.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if s.readyGate != nil && !s.readyGate() {
		writeResponse(w, r, http.StatusServiceUnavailable, "runtime.readyz", map[string]any{
			"ready": false,
			"gate":  "startup",
		})
		return
	}
	resp := s.engine.Request(s.guardianPID, runtime.ReadyQuery{}, s.snapshotTimeout)
	result, err := resp.Result()
	if err != nil {
		s.logger.Warn("readyz request timed out", "err", err)
		writeResponse(w, r, http.StatusGatewayTimeout, "runtime.readyz", map[string]string{"error": "readyz timeout"})
		return
	}
	rr, ok := result.(runtime.ReadyResponse)
	if !ok {
		s.logger.Error("unexpected readyz response type")
		writeResponse(w, r, http.StatusInternalServerError, "runtime.readyz", map[string]string{"error": "unexpected response"})
		return
	}
	if !rr.Ready {
		writeResponse(w, r, http.StatusServiceUnavailable, "runtime.readyz", map[string]any{
			"ready":   false,
			"pending": rr.Pending,
		})
		return
	}
	writeResponse(w, r, http.StatusOK, "runtime.readyz", map[string]bool{"ready": true})
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
		writeResponse(w, r, http.StatusGatewayTimeout, "runtime.snapshot", map[string]string{"error": "snapshot timeout"})
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
		writeResponse(w, r, http.StatusInternalServerError, "runtime.snapshot", map[string]string{"error": "unexpected response"})
		return
	}
	writeResponse(w, r, http.StatusOK, "runtime.snapshot", state)
}

// handleReload sends a ReloadConfig message to the Guardian and returns 202.
// The reload is asynchronous — the HTTP response does not wait for completion.
func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if !supportedRequestContentType(r.Header.Get("Content-Type")) {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}
	s.engine.Send(s.guardianPID, runtime.ReloadConfig{})
	writeResponse(w, r, http.StatusAccepted, "runtime.reload", map[string]bool{"accepted": true})
}

func (s *Server) handleRuntimeOverload(w http.ResponseWriter, r *http.Request) {
	snapshot := buildPolicyKitOverloadSnapshot()
	writeResponse(w, r, http.StatusOK, "runtime.overload", snapshot)
}

func (s *Server) handleRuntimeStorage(w http.ResponseWriter, r *http.Request) {
	snapshot := buildStorageStateSnapshot()
	writeResponse(w, r, http.StatusOK, "runtime.storage", snapshot)
}

func (s *Server) handleRuntimeWS(w http.ResponseWriter, r *http.Request) {
	snapshot := buildWSStateSnapshot()
	writeResponse(w, r, http.StatusOK, "runtime.ws", snapshot)
}

// handleShardz returns live shard topology, lag, and budget status as JSON.
// Returns 404 when this process is not running in shard mode.
func (s *Server) handleShardz(w http.ResponseWriter, r *http.Request) {
	if !observability.ShardConfigured() {
		writeResponse(w, r, http.StatusNotFound, "runtime.shardz", map[string]string{
			"error": "sharding not configured",
		})
		return
	}
	snapshot := observability.SnapshotShardState()
	writeResponse(w, r, http.StatusOK, "runtime.shardz", snapshot)
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

func writeResponse(w http.ResponseWriter, r *http.Request, code int, envelopeType string, body any) {
	if acceptsProto(r) {
		writeProtoEnvelope(w, code, envelopeType, body)
		return
	}
	writeJSON(w, code, body)
}

func writeProtoEnvelope(w http.ResponseWriter, code int, envelopeType string, body any) {
	jsonPayload, err := json.Marshal(body)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encode failed"})
		return
	}
	raw, p := contracts.MarshalEnvelopeV1FromPayload(envelopeType, jsonPayload, "application/json")
	if p != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encode failed"})
		return
	}
	w.Header().Set("Content-Type", httpContentTypeProto)
	w.WriteHeader(code)
	if _, err := w.Write(raw); err != nil {
		slog.Error("httpserver: proto write failed", "err", err)
	}
}

func acceptsProto(r *http.Request) bool {
	if r == nil {
		return false
	}
	accept := strings.TrimSpace(r.Header.Get("Accept"))
	if accept == "" {
		return false
	}
	for _, token := range strings.Split(accept, ",") {
		mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(token, ";", 2)[0]))
		if mediaType == httpContentTypeProto {
			return true
		}
	}
	return false
}

func supportedRequestContentType(raw string) bool {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(raw, ";", 2)[0]))
	switch ct {
	case "", "application/json", httpContentTypeProto:
		return true
	default:
		return false
	}
}

func withProcessMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metrics.UpdateProcessMetrics()
		next.ServeHTTP(w, r)
	})
}

type overloadThresholdJSON struct {
	QueueRatio   float64 `json:"queue_ratio"`
	BacklogRatio float64 `json:"backlog_ratio"`
	MapRatio     float64 `json:"map_ratio"`
	LatencyMs    float64 `json:"latency_ms"`
}

type overloadThresholdPairJSON struct {
	Enter   overloadThresholdJSON `json:"enter"`
	Recover overloadThresholdJSON `json:"recover"`
}

type overloadPartitionJSON struct {
	Stream           string                    `json:"stream"`
	Venue            string                    `json:"venue"`
	OverloadLevel    int                       `json:"overload_level"`
	Thresholds       overloadThresholdPairJSON `json:"thresholds"`
	Stride           int                       `json:"stride"`
	ActivePartitions int                       `json:"active_partitions"`
}

type overloadResponseJSON struct {
	Partitions             []overloadPartitionJSON `json:"partitions"`
	ActivePartitions       int                     `json:"active_partitions"`
	ActivePartitionsCapped bool                    `json:"active_partitions_capped"`
}

type storagePathJSON struct {
	LastOK     any    `json:"last_ok"`
	LastError  string `json:"last_error"`
	FailsTotal any    `json:"fails_total"`
}

type storageCommitterJSON struct {
	LastOK    any    `json:"last_ok"`
	LastError string `json:"last_error"`
}

type storageResponseJSON struct {
	Hot       storagePathJSON      `json:"hot"`
	Cold      storagePathJSON      `json:"cold"`
	Committer storageCommitterJSON `json:"committer"`
}

type wsResponseJSON struct {
	SessionsActive       any `json:"sessions_active"`
	PreferProtoSessions  any `json:"prefer_proto_sessions"`
	DeliveriesProtoTotal any `json:"deliveries_proto_total"`
	DeliveriesJSONTotal  any `json:"deliveries_json_total"`
	ReconnectsTotal      any `json:"reconnects_total"`
}

func buildPolicyKitOverloadSnapshot() overloadResponseJSON {
	entries := observability.SnapshotPolicyKitOverload()
	out := overloadResponseJSON{
		Partitions:       make([]overloadPartitionJSON, 0, len(entries)),
		ActivePartitions: len(entries),
	}
	for _, entry := range entries {
		out.Partitions = append(out.Partitions, overloadPartitionJSON{
			Stream:        entry.Stream,
			Venue:         entry.Venue,
			OverloadLevel: entry.OverloadLevel,
			Thresholds: overloadThresholdPairJSON{
				Enter: overloadThresholdJSON{
					QueueRatio:   entry.Thresholds.Enter.QueueRatio,
					BacklogRatio: entry.Thresholds.Enter.BacklogRatio,
					MapRatio:     entry.Thresholds.Enter.MapRatio,
					LatencyMs:    entry.Thresholds.Enter.LatencyMs,
				},
				Recover: overloadThresholdJSON{
					QueueRatio:   entry.Thresholds.Recover.QueueRatio,
					BacklogRatio: entry.Thresholds.Recover.BacklogRatio,
					MapRatio:     entry.Thresholds.Recover.MapRatio,
					LatencyMs:    entry.Thresholds.Recover.LatencyMs,
				},
			},
			Stride:           entry.Stride,
			ActivePartitions: out.ActivePartitions,
		})
	}
	return out
}

func buildStorageStateSnapshot() storageResponseJSON {
	snapshot := observability.SnapshotStorageState()
	return storageResponseJSON{
		Hot: storagePathJSON{
			LastOK:     boolOrUnknown(snapshot.Hot.LastOKKnown, snapshot.Hot.LastOK),
			LastError:  snapshot.Hot.LastError,
			FailsTotal: numberOrUnknown(snapshot.Hot.FailsTotalKnown, snapshot.Hot.FailsTotal),
		},
		Cold: storagePathJSON{
			LastOK:     boolOrUnknown(snapshot.Cold.LastOKKnown, snapshot.Cold.LastOK),
			LastError:  snapshot.Cold.LastError,
			FailsTotal: numberOrUnknown(snapshot.Cold.FailsTotalKnown, snapshot.Cold.FailsTotal),
		},
		Committer: storageCommitterJSON{
			LastOK:    boolOrUnknown(snapshot.Committer.LastOKKnown, snapshot.Committer.LastOK),
			LastError: snapshot.Committer.LastError,
		},
	}
}

func buildWSStateSnapshot() wsResponseJSON {
	snapshot := observability.SnapshotWSState()
	return wsResponseJSON{
		SessionsActive:       signedNumberOrUnknown(snapshot.SessionsActiveKnown, snapshot.SessionsActive),
		PreferProtoSessions:  signedNumberOrUnknown(snapshot.PreferProtoSessionsKnown, snapshot.PreferProtoSessions),
		DeliveriesProtoTotal: numberOrUnknown(snapshot.DeliveriesProtoTotalKnown, snapshot.DeliveriesProtoTotal),
		DeliveriesJSONTotal:  numberOrUnknown(snapshot.DeliveriesJSONTotalKnown, snapshot.DeliveriesJSONTotal),
		ReconnectsTotal:      numberOrUnknown(snapshot.ReconnectsTotalKnown, snapshot.ReconnectsTotal),
	}
}

func boolOrUnknown(known bool, value bool) any {
	if !known {
		return "unknown"
	}
	return value
}

func numberOrUnknown(known bool, value uint64) any {
	if !known {
		return "unknown"
	}
	return value
}

func signedNumberOrUnknown(known bool, value int64) any {
	if !known {
		return "unknown"
	}
	return value
}

func (s *Server) registerPprofRoutes(mux *http.ServeMux) {
	mux.Handle("GET /debug/pprof/", localhostOnly(http.HandlerFunc(pprof.Index)))
	mux.Handle("GET /debug/pprof/cmdline", localhostOnly(http.HandlerFunc(pprof.Cmdline)))
	mux.Handle("GET /debug/pprof/profile", localhostOnly(http.HandlerFunc(pprof.Profile)))
	mux.Handle("GET /debug/pprof/symbol", localhostOnly(http.HandlerFunc(pprof.Symbol)))
	mux.Handle("GET /debug/pprof/trace", localhostOnly(http.HandlerFunc(pprof.Trace)))
	mux.Handle("GET /debug/pprof/allocs", localhostOnly(pprof.Handler("allocs")))
	mux.Handle("GET /debug/pprof/block", localhostOnly(pprof.Handler("block")))
	mux.Handle("GET /debug/pprof/goroutine", localhostOnly(pprof.Handler("goroutine")))
	mux.Handle("GET /debug/pprof/heap", localhostOnly(pprof.Handler("heap")))
	mux.Handle("GET /debug/pprof/mutex", localhostOnly(pprof.Handler("mutex")))
	mux.Handle("GET /debug/pprof/threadcreate", localhostOnly(pprof.Handler("threadcreate")))
}

func localhostOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
