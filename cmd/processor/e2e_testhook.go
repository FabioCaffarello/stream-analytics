package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	envE2ETestMode            = "E2E_TEST_MODE"
	envE2EHTTPAddr            = "E2E_HTTP_ADDR"
	envE2ETransientInstrument = "E2E_TRANSIENT_INSTRUMENT"
	envE2ETransientFails      = "E2E_TRANSIENT_FAILS"
	envE2EInjectJoinFixture   = "E2E_INJECT_JOIN_FIXTURE"
	envE2EJoinInstrument      = "E2E_JOIN_INSTRUMENT"
	envProcessorRunMode       = "RUN_MODE"
	envProcessorMode          = "MARKET_RACCOON_MODE"
	defaultProcessorProbePort = "18082"
)

// e2eRuntime adds process-level hooks used only in integration tests.
// It is impossible to enable unless E2E_TEST_MODE=1.
type e2eRuntime struct {
	enabled bool
	logger  *slog.Logger

	ready atomic.Bool
	srv   *http.Server

	probeAddr string

	transientInstrument string
	transientFails      int32
	attempts            sync.Map // map[string]*atomic.Int32

	injectJoinFixture bool
	joinInstrument    string
}

func newE2ERuntime(logger *slog.Logger) (*e2eRuntime, *problem.Problem) {
	rt := &e2eRuntime{
		enabled: strings.TrimSpace(os.Getenv(envE2ETestMode)) == "1",
		logger:  logger,
	}
	if !rt.enabled {
		return rt, nil
	}
	if !hasProcessorE2ETestPosture() {
		return nil, problem.New(problem.ValidationFailed, "E2E_TEST_MODE=1 requires RUN_MODE=test or MARKET_RACCOON_MODE=test")
	}

	rt.transientInstrument = strings.TrimSpace(os.Getenv(envE2ETransientInstrument))
	if rt.transientInstrument == "" {
		rt.transientInstrument = "E2E-TRANSIENT"
	}

	rt.transientFails = 2
	if raw := strings.TrimSpace(os.Getenv(envE2ETransientFails)); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 32); err == nil && parsed >= 0 {
			rt.transientFails = int32(parsed)
		}
	}
	rt.injectJoinFixture = strings.TrimSpace(os.Getenv(envE2EInjectJoinFixture)) == "1"
	rt.joinInstrument = strings.TrimSpace(os.Getenv(envE2EJoinInstrument))
	if rt.joinInstrument == "" {
		rt.joinInstrument = "E2E-JOIN"
	}

	return rt, nil
}

func (r *e2eRuntime) startProbe() *problem.Problem {
	if !r.enabled {
		return nil
	}
	addr := resolveProcessorLoopbackProbeAddr()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return problem.Wrap(err, problem.Unavailable, "processor e2e probe listen failed")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if r.ready.Load() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("not ready"))
	})
	mux.Handle("GET /metrics", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		metrics.UpdateProcessMetrics()
		metrics.Handler().ServeHTTP(w, req)
	}))

	r.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}
	r.probeAddr = ln.Addr().String()

	go func() {
		if err := r.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			r.logger.Error("processor: e2e probe server failed", "err", err)
		}
	}()

	r.logger.Info("processor: e2e hooks enabled",
		"http_addr", r.probeAddr,
		"transient_instrument", r.transientInstrument,
		"transient_fails", r.transientFails,
		"inject_join_fixture", r.injectJoinFixture,
		"join_instrument", r.joinInstrument,
	)
	return nil
}

func hasProcessorE2ETestPosture() bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv(envProcessorRunMode)), "test") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(os.Getenv(envProcessorMode)), "test")
}

func resolveProcessorLoopbackProbeAddr() string {
	raw := strings.TrimSpace(os.Getenv(envE2EHTTPAddr))
	port := extractProcessorProbePort(raw, defaultProcessorProbePort)
	return net.JoinHostPort("127.0.0.1", port)
}

func extractProcessorProbePort(rawAddr, fallback string) string {
	rawAddr = strings.TrimSpace(rawAddr)
	if rawAddr == "" {
		return fallback
	}
	if _, port, err := net.SplitHostPort(rawAddr); err == nil {
		return validatedProcessorPortOrFallback(port, fallback)
	}
	if strings.HasPrefix(rawAddr, ":") {
		return validatedProcessorPortOrFallback(strings.TrimPrefix(rawAddr, ":"), fallback)
	}
	return validatedProcessorPortOrFallback(rawAddr, fallback)
}

func validatedProcessorPortOrFallback(port, fallback string) string {
	port = strings.TrimSpace(port)
	if port == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(port)
	if err != nil || parsed <= 0 || parsed > 65535 {
		return fallback
	}
	return strconv.Itoa(parsed)
}

func (r *e2eRuntime) markReady() {
	if !r.enabled {
		return
	}
	r.ready.Store(true)
}

func (r *e2eRuntime) shutdown(ctx context.Context) *problem.Problem {
	if !r.enabled || r.srv == nil {
		return nil
	}
	r.ready.Store(false)
	if err := r.srv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return problem.Wrap(err, problem.Unavailable, "processor e2e probe shutdown failed")
	}
	return nil
}

func (r *e2eRuntime) maybeInjectTransient(env envelope.Envelope, p *problem.Problem) *problem.Problem {
	if !r.enabled || p != nil || r.transientFails <= 0 {
		return p
	}
	if !strings.EqualFold(strings.TrimSpace(env.Instrument), strings.TrimSpace(r.transientInstrument)) {
		return p
	}

	attempt := r.attemptCounter(env).Add(1)
	if attempt <= r.transientFails {
		out := problem.New(problem.Unavailable, "e2e transient injected")
		out = problem.WithRetryable(problem.WithDetail(out, "attempt", attempt))
		return problem.WithDetail(out, "instrument", env.Instrument)
	}
	return p
}

func (r *e2eRuntime) attemptCounter(env envelope.Envelope) *atomic.Int32 {
	key := strings.TrimSpace(env.IdempotencyKey)
	if key == "" {
		key = fmt.Sprintf("%s:%s:%d", env.Type, env.Instrument, env.Seq)
	}
	if v, ok := r.attempts.Load(key); ok {
		return v.(*atomic.Int32)
	}
	counter := &atomic.Int32{}
	actual, _ := r.attempts.LoadOrStore(key, counter)
	return actual.(*atomic.Int32)
}

func (r *e2eRuntime) shouldInjectJoinFixture() bool {
	return r.enabled && r.injectJoinFixture
}
