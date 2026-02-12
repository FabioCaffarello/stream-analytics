package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
)

// e2eRuntime adds process-level hooks used only in integration tests.
// It is impossible to enable unless E2E_TEST_MODE=1.
type e2eRuntime struct {
	enabled bool
	logger  *slog.Logger

	ready atomic.Bool
	srv   *http.Server

	transientInstrument string
	transientFails      int32
	attempts            sync.Map // map[string]*atomic.Int32
}

func newE2ERuntime(logger *slog.Logger) *e2eRuntime {
	rt := &e2eRuntime{
		enabled: strings.TrimSpace(os.Getenv(envE2ETestMode)) == "1",
		logger:  logger,
	}
	if !rt.enabled {
		return rt
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

	return rt
}

func (r *e2eRuntime) startProbe() *problem.Problem {
	if !r.enabled {
		return nil
	}
	addr := strings.TrimSpace(os.Getenv(envE2EHTTPAddr))
	if addr == "" {
		addr = "127.0.0.1:18082"
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
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}
	go func() {
		if err := r.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			r.logger.Error("processor: e2e probe server failed", "err", err)
		}
	}()

	r.logger.Info("processor: e2e hooks enabled",
		"http_addr", addr,
		"transient_instrument", r.transientInstrument,
		"transient_fails", r.transientFails,
	)
	return nil
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
