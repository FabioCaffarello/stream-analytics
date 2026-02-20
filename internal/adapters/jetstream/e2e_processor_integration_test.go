//go:build integration

package jetstream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/nats-io/nats.go"
)

type shardE2EProc struct {
	proc        *processorProcess
	metricsAddr string
	configPath  string
}

func TestE2EProcessorJetStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	natsURL, cleanup := startJetStreamNATS(t)
	defer cleanup()

	repoRoot := findRepoRoot(t)
	processorBin := buildProcessorBinary(t, ctx, repoRoot)

	pub := mustPublisher(t, natsURL)
	defer func() { _ = pub.Close(context.Background()) }()

	const durableName = "processor-e2e-v1"
	seq := 1
	n := 20
	m := 10

	metricsAddr1 := reserveLocalAddr(t)
	configPath1 := writeProcessorConfig(t, natsURL, durableName, metricsAddr1)
	proc1 := startProcessorProcess(t, ctx, repoRoot, processorBin, configPath1, metricsAddr1)
	defer proc1.forceStop()
	waitReady(t, ctx, proc1, metricsAddr1)

	seq = publishRawBatch(t, pub, seq, n, "E2E-A")
	waitMetricAtLeast(t, ctx, proc1, metricsAddr1, "bus_consumed_total", map[string]string{
		"bus_type": "jetstream",
		"status":   "ok",
	}, float64(n))

	if err := proc1.stopGracefully(10 * time.Second); err != nil {
		proc1.dumpLogs(t)
		t.Fatalf("processor #1 graceful stop failed: %v", err)
	}

	seq = publishRawBatch(t, pub, seq, m, "E2E-B")

	metricsAddr2 := reserveLocalAddr(t)
	configPath2 := writeProcessorConfig(t, natsURL, durableName, metricsAddr2)
	proc2 := startProcessorProcess(t, ctx, repoRoot, processorBin, configPath2, metricsAddr2)
	defer proc2.forceStop()
	waitReady(t, ctx, proc2, metricsAddr2)

	waitMetricAtLeast(t, ctx, proc2, metricsAddr2, "bus_consumed_total", map[string]string{
		"bus_type": "jetstream",
		"status":   "ok",
	}, float64(m))

	termBefore := mustMetricValue(t, ctx, proc2, metricsAddr2, "bus_consumed_total", map[string]string{
		"bus_type": "jetstream",
		"status":   "term",
	})
	quarantineBefore := mustMetricValue(t, ctx, proc2, metricsAddr2, "ingest_quarantine_total", map[string]string{
		"reason": "decode_failed",
	})

	publishInvalidEnvelope(t, natsURL, fmt.Sprintf("poison-%d", time.Now().UnixNano()))
	waitMetricAtLeast(t, ctx, proc2, metricsAddr2, "bus_consumed_total", map[string]string{
		"bus_type": "jetstream",
		"status":   "term",
	}, termBefore+1)
	waitMetricAtLeast(t, ctx, proc2, metricsAddr2, "ingest_quarantine_total", map[string]string{
		"reason": "decode_failed",
	}, quarantineBefore+1)

	redeliveredAfterPoison := mustMetricValue(t, ctx, proc2, metricsAddr2, "bus_redelivered_total", map[string]string{
		"bus_type": "jetstream",
	})
	time.Sleep(2 * time.Second)
	redeliveredPoisonStable := mustMetricValue(t, ctx, proc2, metricsAddr2, "bus_redelivered_total", map[string]string{
		"bus_type": "jetstream",
	})
	if redeliveredPoisonStable > redeliveredAfterPoison {
		proc2.dumpLogs(t)
		t.Fatalf("poison redelivery should stay stable; got %v -> %v", redeliveredAfterPoison, redeliveredPoisonStable)
	}

	okBeforeTransient := mustMetricValue(t, ctx, proc2, metricsAddr2, "bus_consumed_total", map[string]string{
		"bus_type": "jetstream",
		"status":   "ok",
	})
	redeliveredBeforeTransient := redeliveredPoisonStable

	publishTransientEnvelope(t, pub, seq)
	waitMetricAtLeast(t, ctx, proc2, metricsAddr2, "bus_redelivered_total", map[string]string{
		"bus_type": "jetstream",
	}, redeliveredBeforeTransient+1)
	waitMetricAtLeast(t, ctx, proc2, metricsAddr2, "bus_consumed_total", map[string]string{
		"bus_type": "jetstream",
		"status":   "ok",
	}, okBeforeTransient+1)

	if err := proc2.stopGracefully(10 * time.Second); err != nil {
		proc2.dumpLogs(t)
		t.Fatalf("processor #2 graceful stop failed: %v", err)
	}
}

func TestE2EProcessorJetStream_CrossVenueJoinOptIn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	natsURL, cleanup := startJetStreamNATS(t)
	defer cleanup()

	repoRoot := findRepoRoot(t)
	processorBin := buildProcessorBinary(t, ctx, repoRoot)

	const durableName = "processor-e2e-join-v1"
	metricsAddr := reserveLocalAddr(t)
	configPath := writeProcessorConfigWithJoin(t, natsURL, durableName, metricsAddr, true)

	proc := startProcessorProcessWithEnv(t, ctx, repoRoot, processorBin, configPath, metricsAddr, map[string]string{
		"E2E_INJECT_JOIN_FIXTURE": "1",
		"E2E_JOIN_INSTRUMENT":     "E2E-JOIN",
	})
	defer proc.forceStop()
	waitReady(t, ctx, proc, metricsAddr)

	waitMetricAtLeast(t, ctx, proc, metricsAddr, "insights_snapshots_total", map[string]string{
		"venue_count_bucket": "2",
	}, 1)
	waitMetricAtLeast(t, ctx, proc, metricsAddr, "insights_state_instruments_active", map[string]string{}, 1)

	count := consumeCount(t, natsURL, "MARKETDATA", "insights.crossvenue.trade_snapshot.v1.>", 1, 10*time.Second)
	if count < 1 {
		proc.dumpLogs(t)
		t.Fatalf("expected at least one insights snapshot, got %d", count)
	}

	if err := proc.stopGracefully(10 * time.Second); err != nil {
		proc.dumpLogs(t)
		t.Fatalf("processor graceful stop failed: %v", err)
	}
}

func TestE2EProcessorJetStream_FailClosedWithoutTestRunMode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repoRoot := findRepoRoot(t)
	processorBin := buildProcessorBinary(t, ctx, repoRoot)
	metricsAddr := reserveLocalAddr(t)
	configPath := writeProcessorConfig(t, "nats://127.0.0.1:4222", "processor-e2e-failclosed", metricsAddr)

	proc := startProcessorProcessWithEnv(t, ctx, repoRoot, processorBin, configPath, metricsAddr, map[string]string{
		"RUN_MODE":            "prod",
		"MARKET_RACCOON_MODE": "prod",
	})
	defer proc.forceStop()

	err := proc.waitExit(5 * time.Second)
	if err == nil {
		proc.dumpLogs(t)
		t.Fatal("expected fail-closed exit when E2E_TEST_MODE=1 without test posture")
	}
	logs := proc.stdout.String() + "\n" + proc.stderr.String()
	if !strings.Contains(logs, "requires RUN_MODE=test or MARKET_RACCOON_MODE=test") {
		proc.dumpLogs(t)
		t.Fatalf("expected fail-closed message in logs, got:\n%s", logs)
	}
}

func TestE2EProcessorJetStream_ThreeShardsCrashRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	natsURL, cleanup := startJetStreamNATS(t)
	defer cleanup()

	repoRoot := findRepoRoot(t)
	processorBin := buildProcessorBinary(t, ctx, repoRoot)
	pub := mustPublisher(t, natsURL)
	defer func() { _ = pub.Close(context.Background()) }()

	const (
		shardCount      = 3
		perShardInitial = 12
		perShardAfter   = 6
		durableName     = "processor-e2e-sharded-v1"
	)

	processes := make([]shardE2EProc, shardCount)
	for shardIdx := 0; shardIdx < shardCount; shardIdx++ {
		metricsAddr := reserveLocalAddr(t)
		configPath := writeProcessorConfigWithShardRegistry(t, natsURL, durableName, metricsAddr, true, true, "20s")
		proc := startProcessorProcessWithEnv(t, ctx, repoRoot, processorBin, configPath, metricsAddr, map[string]string{
			"MR_ENV":                   "dev",
			"SHARD_COUNT":              strconv.Itoa(shardCount),
			"SHARD_INDEX":              strconv.Itoa(shardIdx),
			"E2E_HTTP_ADDR":            reserveLocalAddr(t),
			"E2E_TRANSIENT_INSTRUMENT": "E2E-TRANSIENT",
		})
		processes[shardIdx] = shardE2EProc{proc: proc, metricsAddr: metricsAddr, configPath: configPath}
		defer proc.forceStop()
	}
	for _, item := range processes {
		waitReady(t, ctx, item.proc, item.metricsAddr)
	}

	initialInstruments := pickInstrumentsPerShard(shardCount, perShardInitial)
	seq := 1
	for shardIdx := 0; shardIdx < shardCount; shardIdx++ {
		for _, instrument := range initialInstruments[shardIdx] {
			env := envelope.Envelope{
				Type:           "marketdata.raw",
				Version:        1,
				Venue:          "binance",
				Instrument:     instrument,
				TsExchange:     1_710_000_000_000 + int64(seq),
				TsIngest:       1_710_000_000_100 + int64(seq),
				Seq:            int64(seq),
				IdempotencyKey: fmt.Sprintf("e2e-shard-initial-%d", seq),
				ContentType:    envelope.ContentTypeJSON,
				Payload:        []byte(`{"kind":"raw"}`),
			}
			if p := pub.Publish(context.Background(), env); p != nil {
				t.Fatalf("publish initial envelope failed: %v", p)
			}
			seq++
		}
	}
	for _, item := range processes {
		waitMetricAtLeast(t, ctx, item.proc, item.metricsAddr, "bus_consumed_total", map[string]string{
			"bus_type": "jetstream",
			"status":   "ok",
		}, 1)
	}

	// Crash shard 1 process, publish additional shard-1 routed envelopes,
	// then restart the same shard to validate convergence.
	crashed := processes[1]
	if err := crashed.proc.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("kill shard 1 processor failed: %v", err)
	}
	_ = crashed.proc.waitExit(5 * time.Second)
	time.Sleep(32 * time.Second) // wait for shard lease TTL (30s) to expire before restart

	afterInstruments := pickInstrumentsForShard(shardCount, 1, perShardAfter, 10_000)
	for _, instrument := range afterInstruments {
		env := envelope.Envelope{
			Type:           "marketdata.raw",
			Version:        1,
			Venue:          "binance",
			Instrument:     instrument,
			TsExchange:     1_710_001_000_000 + int64(seq),
			TsIngest:       1_710_001_000_100 + int64(seq),
			Seq:            int64(seq),
			IdempotencyKey: fmt.Sprintf("e2e-shard-restart-%d", seq),
			ContentType:    envelope.ContentTypeJSON,
			Payload:        []byte(`{"kind":"raw"}`),
		}
		if p := pub.Publish(context.Background(), env); p != nil {
			t.Fatalf("publish restart envelope failed: %v", p)
		}
		seq++
	}
	restarted := startProcessorProcessWithEnv(t, ctx, repoRoot, processorBin, crashed.configPath, crashed.metricsAddr, map[string]string{
		"MR_ENV":        "dev",
		"SHARD_COUNT":   strconv.Itoa(shardCount),
		"SHARD_INDEX":   "1",
		"E2E_HTTP_ADDR": reserveLocalAddr(t),
	})
	processes[1].proc = restarted
	defer restarted.forceStop()
	waitReady(t, ctx, restarted, crashed.metricsAddr)
}

type processorProcess struct {
	cmd *exec.Cmd

	stdout bytes.Buffer
	stderr bytes.Buffer

	done chan error

	mu      sync.Mutex
	exited  bool
	exitErr error
}

func startProcessorProcess(t *testing.T, ctx context.Context, repoRoot, binPath, configPath, metricsAddr string) *processorProcess {
	t.Helper()
	return startProcessorProcessWithEnv(t, ctx, repoRoot, binPath, configPath, metricsAddr, nil)
}

func startProcessorProcessWithEnv(
	t *testing.T,
	ctx context.Context,
	repoRoot, binPath, configPath, metricsAddr string,
	extraEnv map[string]string,
) *processorProcess {
	t.Helper()

	p := &processorProcess{
		done: make(chan error, 1),
	}
	cmd := exec.CommandContext(ctx, binPath, "-config", configPath, "-bus", "jetstream", "-log-level", "debug")
	cmd.Dir = repoRoot
	cmd.Stdout = &p.stdout
	cmd.Stderr = &p.stderr
	envOverrides := map[string]string{
		"E2E_TEST_MODE":            "1",
		"E2E_TRANSIENT_INSTRUMENT": "E2E-TRANSIENT",
		"E2E_TRANSIENT_FAILS":      "2",
		"RUN_MODE":                 "test",
	}
	for key, val := range extraEnv {
		envOverrides[key] = val
	}
	cmd.Env = withEnvOverrides(envOverrides)
	p.cmd = cmd

	if err := cmd.Start(); err != nil {
		t.Fatalf("start processor failed: %v", err)
	}
	go func() {
		p.done <- cmd.Wait()
	}()
	return p
}

func withEnvOverrides(overrides map[string]string) []string {
	if len(overrides) == 0 {
		return os.Environ()
	}
	base := os.Environ()
	filtered := make([]string, 0, len(base)+len(overrides))
	for _, item := range base {
		if i := strings.IndexByte(item, '='); i > 0 {
			if _, exists := overrides[item[:i]]; exists {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		filtered = append(filtered, key+"="+overrides[key])
	}
	return filtered
}

func (p *processorProcess) pollExit() (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exited {
		return true, p.exitErr
	}
	select {
	case err := <-p.done:
		p.exited = true
		p.exitErr = err
		return true, err
	default:
		return false, nil
	}
}

func (p *processorProcess) stopGracefully(timeout time.Duration) error {
	if exited, err := p.pollExit(); exited {
		return err
	}
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	select {
	case err := <-p.done:
		p.mu.Lock()
		p.exited = true
		p.exitErr = err
		p.mu.Unlock()
		return err
	case <-deadline.C:
		_ = p.cmd.Process.Kill()
		err := <-p.done
		p.mu.Lock()
		p.exited = true
		p.exitErr = err
		p.mu.Unlock()
		return fmt.Errorf("timeout waiting graceful stop: %w", err)
	}
}

func (p *processorProcess) forceStop() {
	_ = p.stopGracefully(2 * time.Second)
}

func (p *processorProcess) waitExit(timeout time.Duration) error {
	if exited, err := p.pollExit(); exited {
		return err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-p.done:
		p.mu.Lock()
		p.exited = true
		p.exitErr = err
		p.mu.Unlock()
		return err
	case <-timer.C:
		return fmt.Errorf("timeout waiting process exit")
	}
}

func (p *processorProcess) dumpLogs(t *testing.T) {
	t.Helper()
	stdout := p.stdout.String()
	stderr := p.stderr.String()
	if strings.TrimSpace(stdout) != "" {
		t.Logf("processor stdout:\n%s", stdout)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Logf("processor stderr:\n%s", stderr)
	}
}

func waitReady(t *testing.T, ctx context.Context, proc *processorProcess, metricsAddr string) {
	t.Helper()

	client := &http.Client{Timeout: 800 * time.Millisecond}
	url := "http://" + metricsAddr + "/readyz"
	for {
		if exited, err := proc.pollExit(); exited {
			proc.dumpLogs(t)
			t.Fatalf("processor exited before readyz: %v", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("build readyz request failed: %v", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if ctx.Err() != nil {
			proc.dumpLogs(t)
			t.Fatalf("timeout waiting readyz")
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func waitMetricAtLeast(
	t *testing.T,
	ctx context.Context,
	proc *processorProcess,
	metricsAddr, metricName string,
	labels map[string]string,
	minValue float64,
) {
	t.Helper()

	var last float64
	for {
		if exited, err := proc.pollExit(); exited {
			proc.dumpLogs(t)
			t.Fatalf("processor exited while waiting metric %s: %v", metricName, err)
		}

		value, err := scrapeMetricValue(ctx, metricsAddr, metricName, labels)
		if err == nil {
			last = value
			if value >= minValue {
				return
			}
		}
		if ctx.Err() != nil {
			proc.dumpLogs(t)
			t.Fatalf("timeout waiting metric %s >= %v (last=%v)", metricName, minValue, last)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func mustMetricValue(
	t *testing.T,
	ctx context.Context,
	proc *processorProcess,
	metricsAddr, metricName string,
	labels map[string]string,
) float64 {
	t.Helper()
	value, err := scrapeMetricValue(ctx, metricsAddr, metricName, labels)
	if err != nil {
		proc.dumpLogs(t)
		t.Fatalf("scrape metric %s failed: %v", metricName, err)
	}
	return value
}

func scrapeMetricValue(ctx context.Context, metricsAddr, metricName string, labels map[string]string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+metricsAddr+"/metrics", nil)
	if err != nil {
		return 0, err
	}

	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("metrics status=%d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		series := fields[0]
		valueRaw := fields[len(fields)-1]

		name := series
		if i := strings.IndexByte(series, '{'); i >= 0 {
			name = series[:i]
		}
		if name != metricName {
			continue
		}

		matched := true
		for key, val := range labels {
			if !strings.Contains(series, fmt.Sprintf(`%s="%s"`, key, val)) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}

		v, err := strconv.ParseFloat(valueRaw, 64)
		if err != nil {
			return 0, err
		}
		return v, nil
	}
	return 0, nil
}

func publishRawBatch(t *testing.T, pub *Publisher, seqStart, count int, instrumentPrefix string) int {
	t.Helper()
	next := seqStart
	for i := 0; i < count; i++ {
		seq := next
		next++
		inst := fmt.Sprintf("%s-%03d", instrumentPrefix, i)
		env := envelope.Envelope{
			Type:           "marketdata.raw",
			Version:        1,
			Venue:          "binance",
			Instrument:     inst,
			TsExchange:     1_710_000_000_000 + int64(seq),
			TsIngest:       1_710_000_000_100 + int64(seq),
			Seq:            int64(seq),
			IdempotencyKey: fmt.Sprintf("e2e-%s-%d", instrumentPrefix, seq),
			ContentType:    envelope.ContentTypeJSON,
			Payload:        []byte(`{"kind":"raw"}`),
		}
		if p := pub.Publish(context.Background(), env); p != nil {
			t.Fatalf("publish raw[%d] failed: %v", i, p)
		}
	}
	return next
}

func publishTransientEnvelope(t *testing.T, pub *Publisher, seq int) {
	t.Helper()
	env := envelope.Envelope{
		Type:           "marketdata.raw",
		Version:        1,
		Venue:          "binance",
		Instrument:     "E2E-TRANSIENT",
		TsExchange:     1_710_000_010_000 + int64(seq),
		TsIngest:       1_710_000_010_100 + int64(seq),
		Seq:            int64(seq),
		IdempotencyKey: fmt.Sprintf("e2e-transient-%d", seq),
		ContentType:    envelope.ContentTypeJSON,
		Payload:        []byte(`{"kind":"transient"}`),
	}
	if p := pub.Publish(context.Background(), env); p != nil {
		t.Fatalf("publish transient failed: %v", p)
	}
}

func publishInvalidEnvelope(t *testing.T, natsURL, msgID string) {
	t.Helper()

	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatalf("nats connect failed: %v", err)
	}
	defer nc.Close()

	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("jetstream context failed: %v", err)
	}

	msg := nats.NewMsg("marketdata.raw.v1.binance.E2EPOISON")
	msg.Header.Set(nats.MsgIdHdr, msgID)
	msg.Data = []byte("{invalid-envelope")
	if _, err := js.PublishMsg(msg); err != nil {
		t.Fatalf("publish invalid envelope failed: %v", err)
	}
}

func buildProcessorBinary(t *testing.T, ctx context.Context, repoRoot string) string {
	t.Helper()

	outPath := filepath.Join(t.TempDir(), "processor-e2e")
	cmd := exec.CommandContext(ctx, "go", "build", "-o", outPath, "./cmd/processor")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build processor binary failed: %v\n%s", err, string(output))
	}
	return outPath
}

func writeProcessorConfig(t *testing.T, natsURL, durable, httpAddr string) string {
	t.Helper()
	return writeProcessorConfigWithJoinAndShardRegistry(t, natsURL, durable, httpAddr, false, false, false, "")
}

func writeProcessorConfigWithJoin(t *testing.T, natsURL, durable, httpAddr string, enableJoin bool) string {
	t.Helper()
	return writeProcessorConfigWithJoinAndShardRegistry(t, natsURL, durable, httpAddr, enableJoin, false, false, "")
}

func writeProcessorConfigWithShardRegistry(
	t *testing.T,
	natsURL, durable, httpAddr string,
	enabled bool,
	strict bool,
	grace string,
) string {
	t.Helper()
	return writeProcessorConfigWithJoinAndShardRegistry(t, natsURL, durable, httpAddr, false, enabled, strict, grace)
}

func writeProcessorConfigWithJoinAndShardRegistry(
	t *testing.T,
	natsURL, durable, httpAddr string,
	enableJoin bool,
	enableShardRegistry bool,
	shardRegistryStrict bool,
	shardRegistryGrace string,
) string {
	t.Helper()

	insightsJSON := `"insights": {
      "enable_crossvenue_join": false,
      "enable_spread_signal": false,
      "join_trades_subject": "marketdata.trade.v1.>",
      "snapshot_subject_prefix": "",
      "max_instruments": 10000,
      "ttl": "1h",
      "min_venues": 2,
      "min_spread_bps": 0,
      "rounding_mode": "half_even",
      "sweep_every_n": 1024,
      "sweep_every": "30s"
    }`
	if enableJoin {
		insightsJSON = `"insights": {
      "enable_crossvenue_join": true,
      "enable_spread_signal": false,
      "join_trades_subject": "marketdata.trade.v1.>",
      "snapshot_subject_prefix": "insights.crossvenue.trade_snapshot.v1",
      "max_instruments": 10000,
      "ttl": "1h",
      "min_venues": 2,
      "min_spread_bps": 0,
      "rounding_mode": "half_even",
      "sweep_every_n": 1024,
      "sweep_every": "30s"
	    }`
	}

	shardJSON := ``
	if enableShardRegistry {
		graceVal := shardRegistryGrace
		if strings.TrimSpace(graceVal) == "" {
			graceVal = "60s"
		}
		shardJSON = fmt.Sprintf(`,
	  "shard": {
	    "registry": {
	      "enabled": true,
	      "strict": %t,
	      "topology_grace": %q
	    }
	  }`, shardRegistryStrict, graceVal)
	}

	cfg := fmt.Sprintf(`{
	  "bus": {"type": "jetstream"},
  "jetstream": {
    "url": %q,
    "stream_name": "MARKETDATA",
    "consumer_durable": %q,
    "ack_wait": "2s",
    "max_ack_pending": 1024,
    "max_deliver": 20,
    "deliver_policy": "all",
    "filter_subjects": ["marketdata.>"],
    "dedup_window": "5m",
    "max_age": "24h",
    "max_bytes": "10GB"
  },
  "log": {"level": "debug", "format": "text"},
  "http": {
    "addr": %q,
    "shutdown_timeout": "5s",
    "guardian_shutdown_timeout": "4s",
    "publisher_flush_timeout": "2s"
  },
	  "processor": {
	    "bus_capacity": 1024,
	    %s
	  }
	  %s
	}
	`, natsURL, durable, httpAddr, insightsJSON, shardJSON)

	path := filepath.Join(t.TempDir(), "processor-e2e.json")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write processor config failed: %v", err)
	}
	return path
}

func reserveLocalAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local addr failed: %v", err)
	}
	defer l.Close()
	return l.Addr().String()
}

func findRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	cur := wd
	for {
		if _, err := os.Stat(filepath.Join(cur, "go.work")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			t.Fatalf("could not locate repository root from %s", wd)
		}
		cur = parent
	}
}

func pickInstrumentsPerShard(shardCount, perShard int) map[int][]string {
	out := make(map[int][]string, shardCount)
	for shard := 0; shard < shardCount; shard++ {
		out[shard] = pickInstrumentsForShard(shardCount, shard, perShard, shard*100_000)
	}
	return out
}

func pickInstrumentsForShard(shardCount, shardID, needed, start int) []string {
	if needed <= 0 {
		return nil
	}
	out := make([]string, 0, needed)
	for i := start; len(out) < needed; i++ {
		instrument := fmt.Sprintf("E2E-S%06d", i)
		subject := fmt.Sprintf("marketdata.raw.v1.binance.%s", instrument)
		if ShardGroup(ShardKey(subject), shardCount) == shardID {
			out = append(out, instrument)
		}
	}
	return out
}
