package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anthdm/hollywood/actor"
	ws "github.com/market-raccoon/internal/actors/marketdata/ws"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	envConsumerE2ETestMode = "E2E_TEST_MODE"
	envConsumerE2EHTTPAddr = "E2E_HTTP_ADDR"
	envConsumerProbeAddr   = "PROBE_ADDR"
	envRunMode             = "RUN_MODE"
	envMarketRaccoonMode   = "MARKET_RACCOON_MODE"
	defaultProbePort       = "18083"
)

// e2eRuntime adds process-level hooks used only in integration tests.
// It is impossible to enable unless E2E_TEST_MODE=1.
type e2eRuntime struct {
	enabled bool
	logger  *slog.Logger

	engine      *actor.Engine
	guardianPID *actor.PID

	probeListener net.Listener
	probeAddr     string
	srv           *http.Server

	feedCtx    context.Context
	feedCancel context.CancelFunc
	feedWG     sync.WaitGroup

	mu sync.Mutex
}

func newE2ERuntime(logger *slog.Logger) (*e2eRuntime, *problem.Problem) {
	enabled := strings.TrimSpace(os.Getenv(envConsumerE2ETestMode)) == "1"
	if enabled && !hasE2ETestPosture() {
		return nil, problem.New(problem.ValidationFailed, "E2E_TEST_MODE=1 requires RUN_MODE=test or MARKET_RACCOON_MODE=test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &e2eRuntime{
		enabled:    enabled,
		logger:     logger,
		feedCtx:    ctx,
		feedCancel: cancel,
	}, nil
}

func (r *e2eRuntime) isEnabled() bool { return r != nil && r.enabled }

func (r *e2eRuntime) bindEngine(engine *actor.Engine) {
	if !r.isEnabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.engine = engine
}

func (r *e2eRuntime) bindGuardian(guardianPID *actor.PID) {
	if !r.isEnabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.guardianPID = guardianPID
}

func (r *e2eRuntime) subsystemStartedHook(exchangeType, exchangeName string) func(*actor.PID) {
	if !r.isEnabled() {
		return nil
	}
	exType := strings.ToLower(strings.TrimSpace(exchangeType))
	exName := strings.ToLower(strings.TrimSpace(exchangeName))
	return func(selfPID *actor.PID) {
		if selfPID == nil {
			return
		}
		r.startFeedLoop(exType, exName, selfPID)
	}
}

func (r *e2eRuntime) startProbe() *problem.Problem {
	if !r.isEnabled() {
		return nil
	}
	addr := resolveLoopbackProbeAddr()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return problem.Wrap(err, problem.Unavailable, "consumer e2e probe listen failed")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		ready, pending, p := r.guardianReady()
		if p != nil {
			w.WriteHeader(http.StatusGatewayTimeout)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ready": false,
				"error": p.Message,
			})
			return
		}
		if !ready {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ready":   false,
				"pending": pending,
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ready": true})
	})
	mux.Handle("GET /metrics", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		metrics.UpdateProcessMetrics()
		metrics.Handler().ServeHTTP(w, req)
	}))

	r.mu.Lock()
	r.probeListener = ln
	r.probeAddr = ln.Addr().String()
	r.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}
	srv := r.srv
	r.mu.Unlock()

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			r.logger.Error("consumer: e2e probe server failed", "err", err)
		}
	}()

	r.logger.Info("consumer: e2e hooks enabled", "http_addr", r.probeAddr)
	return nil
}

func hasE2ETestPosture() bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv(envRunMode)), "test") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(os.Getenv(envMarketRaccoonMode)), "test")
}

func resolveLoopbackProbeAddr() string {
	raw := strings.TrimSpace(os.Getenv(envConsumerE2EHTTPAddr))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv(envConsumerProbeAddr))
	}
	port := extractProbePort(raw, defaultProbePort)
	return net.JoinHostPort("127.0.0.1", port)
}

func extractProbePort(rawAddr, fallback string) string {
	rawAddr = strings.TrimSpace(rawAddr)
	if rawAddr == "" {
		return fallback
	}

	if _, port, err := net.SplitHostPort(rawAddr); err == nil {
		return validatedPortOrFallback(port, fallback)
	}
	if strings.HasPrefix(rawAddr, ":") {
		return validatedPortOrFallback(strings.TrimPrefix(rawAddr, ":"), fallback)
	}
	return validatedPortOrFallback(rawAddr, fallback)
}

func validatedPortOrFallback(port, fallback string) string {
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

func (r *e2eRuntime) shutdown(ctx context.Context) *problem.Problem {
	if !r.isEnabled() {
		return nil
	}
	r.feedCancel()
	done := make(chan struct{})
	go func() {
		r.feedWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		return problem.Wrap(ctx.Err(), problem.Unavailable, "consumer e2e feed shutdown timeout")
	}

	r.mu.Lock()
	srv := r.srv
	r.mu.Unlock()
	if srv != nil {
		if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return problem.Wrap(err, problem.Unavailable, "consumer e2e probe shutdown failed")
		}
	}
	return nil
}

func (r *e2eRuntime) guardianReady() (bool, []actorruntime.Subsystem, *problem.Problem) {
	r.mu.Lock()
	engine := r.engine
	guardianPID := r.guardianPID
	r.mu.Unlock()
	if engine == nil || guardianPID == nil {
		return false, nil, problem.New(problem.Unavailable, "guardian not initialized")
	}
	resp := engine.Request(guardianPID, actorruntime.ReadyQuery{}, 1500*time.Millisecond)
	result, err := resp.Result()
	if err != nil {
		return false, nil, problem.Wrap(err, problem.Unavailable, "readyz timeout")
	}
	rr, ok := result.(actorruntime.ReadyResponse)
	if !ok {
		return false, nil, problem.New(problem.Internal, "unexpected ready response type")
	}
	return rr.Ready, rr.Pending, nil
}

func (r *e2eRuntime) startFeedLoop(exchangeType, exchangeName string, subsystemPID *actor.PID) {
	r.mu.Lock()
	engine := r.engine
	ctx := r.feedCtx
	r.mu.Unlock()
	if engine == nil || subsystemPID == nil {
		return
	}

	r.feedWG.Add(1)
	go func() {
		defer r.feedWG.Done()

		consumerID := "e2e-" + exchangeName
		endpoint := "e2e://" + exchangeName
		engine.Send(subsystemPID, &ws.WsState{
			Exchange:   exchangeName,
			BucketID:   0,
			ConsumerID: consumerID,
			Endpoint:   endpoint,
			Status:     "connected",
			At:         time.Now(),
		})

		sendBatch := func() {
			for _, payload := range e2ePayloadsForExchange(exchangeType) {
				engine.Send(subsystemPID, &ws.WsMessage{
					Exchange:   exchangeName,
					BucketID:   0,
					ConsumerID: consumerID,
					Endpoint:   endpoint,
					Data:       []byte(payload),
					RecvAt:     time.Now(),
				})
			}
		}

		sendBatch()
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		deadline := time.NewTimer(1500 * time.Millisecond)
		defer deadline.Stop()
		for {
			select {
			case <-ctx.Done():
				engine.Send(subsystemPID, &ws.WsState{
					Exchange:   exchangeName,
					BucketID:   0,
					ConsumerID: consumerID,
					Endpoint:   endpoint,
					Status:     "closed",
					At:         time.Now(),
				})
				return
			case <-deadline.C:
				return
			case <-ticker.C:
				sendBatch()
			}
		}
	}()
}

func e2ePayloadsForExchange(exchangeType string) []string {
	switch exchangeType {
	case "bybit":
		return []string{
			`{"topic":"publicTrade.BTCUSDT","type":"snapshot","ts":1710000001000,"data":[{"T":1710000001001,"s":"BTCUSDT","S":"Buy","v":"0.010","p":"65000.50","i":"123456"}]}`,
			`{"topic":"orderbook.50.BTCUSDT","type":"delta","ts":1710000010000,"data":{"s":"BTCUSDT","b":[["64999.9","1.2"]],"a":[["65000.1","2.3"]],"u":105,"seq":101,"cts":1710000010001}}`,
		}
	default:
		return []string{
			`{"stream":"btcusdt@aggTrade","data":{"e":"aggTrade","E":1710000001000,"T":1710000002000,"s":"BTCUSDT","a":12345,"p":"42000.10","q":"0.200","m":true}}`,
			`{"stream":"btcusdt@depth@100ms","data":{"e":"depthUpdate","E":1710000010000,"s":"BTCUSDT","U":101,"u":105,"pu":100,"b":[["41999.9","1.2"]],"a":[["42000.2","2.3"]]}}`,
		}
	}
}
