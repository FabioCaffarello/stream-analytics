//go:build integration

package main

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
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestE2EConsumerMultiExchange(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	repoRoot := findRepoRoot(t)
	consumerBin := buildConsumerBinary(t, ctx, repoRoot)
	configPath := writeConsumerConfig(t)

	probeAddr1 := reserveLocalAddr(t)
	proc1 := startConsumerProcess(t, ctx, repoRoot, consumerBin, configPath, probeAddr1)
	defer proc1.forceStop()

	waitReady(t, ctx, proc1, probeAddr1)
	waitMetricSeriesContains(t, ctx, proc1, probeAddr1, "ws_connections_active", `venue="binance"`)
	waitMetricSeriesContains(t, ctx, proc1, probeAddr1, "ws_connections_active", `venue="bybit"`)

	if err := proc1.stopGracefully(10 * time.Second); err != nil {
		proc1.dumpLogs(t)
		t.Fatalf("consumer #1 graceful stop failed: %v", err)
	}

	probeAddr2 := reserveLocalAddr(t)
	proc2 := startConsumerProcess(t, ctx, repoRoot, consumerBin, configPath, probeAddr2)
	defer proc2.forceStop()

	waitReady(t, ctx, proc2, probeAddr2)
	waitMetricSeriesContains(t, ctx, proc2, probeAddr2, "ws_connections_active", `venue="binance"`)
	waitMetricSeriesContains(t, ctx, proc2, probeAddr2, "ws_connections_active", `venue="bybit"`)

	if err := proc2.stopGracefully(10 * time.Second); err != nil {
		proc2.dumpLogs(t)
		t.Fatalf("consumer #2 graceful stop failed: %v", err)
	}
}

func TestE2EConsumerFailClosedWithoutTestRunMode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repoRoot := findRepoRoot(t)
	consumerBin := buildConsumerBinary(t, ctx, repoRoot)
	configPath := writeConsumerConfig(t)
	probeAddr := reserveLocalAddr(t)

	proc := startConsumerProcessWithEnv(t, ctx, repoRoot, consumerBin, configPath, probeAddr, map[string]string{
		"RUN_MODE":            "prod",
		"MARKET_RACCOON_MODE": "prod",
		"E2E_TEST_MODE":       "1",
		"E2E_HTTP_ADDR":       probeAddr,
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

type consumerProcess struct {
	cmd *exec.Cmd

	stdout bytes.Buffer
	stderr bytes.Buffer

	done chan error

	mu      sync.Mutex
	exited  bool
	exitErr error
}

func startConsumerProcess(t *testing.T, ctx context.Context, repoRoot, binPath, configPath, probeAddr string) *consumerProcess {
	t.Helper()
	return startConsumerProcessWithEnv(t, ctx, repoRoot, binPath, configPath, probeAddr, nil)
}

func startConsumerProcessWithEnv(
	t *testing.T,
	ctx context.Context,
	repoRoot, binPath, configPath, probeAddr string,
	extraEnv map[string]string,
) *consumerProcess {
	t.Helper()

	p := &consumerProcess{
		done: make(chan error, 1),
	}
	cmd := exec.CommandContext(ctx, binPath, "-config", configPath, "-log-level", "debug")
	cmd.Dir = repoRoot
	cmd.Stdout = &p.stdout
	cmd.Stderr = &p.stderr
	envOverrides := map[string]string{
		"E2E_TEST_MODE": "1",
		"E2E_HTTP_ADDR": probeAddr,
		"RUN_MODE":      "test",
	}
	for key, val := range extraEnv {
		envOverrides[key] = val
	}
	cmd.Env = withEnvOverrides(envOverrides)
	p.cmd = cmd

	if err := cmd.Start(); err != nil {
		t.Fatalf("start consumer failed: %v", err)
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

func (p *consumerProcess) pollExit() (bool, error) {
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

func (p *consumerProcess) stopGracefully(timeout time.Duration) error {
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

func (p *consumerProcess) forceStop() {
	_ = p.stopGracefully(2 * time.Second)
}

func (p *consumerProcess) waitExit(timeout time.Duration) error {
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

func (p *consumerProcess) dumpLogs(t *testing.T) {
	t.Helper()
	stdout := p.stdout.String()
	stderr := p.stderr.String()
	if strings.TrimSpace(stdout) != "" {
		t.Logf("consumer stdout:\n%s", stdout)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Logf("consumer stderr:\n%s", stderr)
	}
}

func waitReady(t *testing.T, ctx context.Context, proc *consumerProcess, probeAddr string) {
	t.Helper()

	client := &http.Client{Timeout: 800 * time.Millisecond}
	url := "http://" + probeAddr + "/readyz"
	for {
		if exited, err := proc.pollExit(); exited {
			proc.dumpLogs(t)
			t.Fatalf("consumer exited before readyz: %v", err)
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

func waitMetricSeriesContains(
	t *testing.T,
	ctx context.Context,
	proc *consumerProcess,
	probeAddr, metricName string,
	labelFragment string,
) {
	t.Helper()

	for {
		if exited, err := proc.pollExit(); exited {
			proc.dumpLogs(t)
			t.Fatalf("consumer exited while waiting metric %s: %v", metricName, err)
		}

		body, err := scrapeMetrics(ctx, probeAddr)
		if err == nil && hasMetricSeriesWithLabel(body, metricName, labelFragment) {
			return
		}
		if ctx.Err() != nil {
			proc.dumpLogs(t)
			t.Fatalf("timeout waiting metric %s with %s", metricName, labelFragment)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func scrapeMetrics(ctx context.Context, probeAddr string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+probeAddr+"/metrics", nil)
	if err != nil {
		return "", err
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metrics status=%d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func hasMetricSeriesWithLabel(body, metricName, labelFragment string) bool {
	lines := strings.Split(body, "\n")
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
		name := series
		if i := strings.IndexByte(series, '{'); i >= 0 {
			name = series[:i]
		}
		if name != metricName {
			continue
		}
		if strings.Contains(series, labelFragment) {
			return true
		}
	}
	return false
}

func buildConsumerBinary(t *testing.T, ctx context.Context, repoRoot string) string {
	t.Helper()

	outPath := filepath.Join(t.TempDir(), "consumer-e2e")
	cmd := exec.CommandContext(ctx, "go", "build", "-o", outPath, "./cmd/consumer")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build consumer binary failed: %v\n%s", err, string(output))
	}
	return outPath
}

func writeConsumerConfig(t *testing.T) string {
	t.Helper()
	cfg := `{
  "bus": {"type": "inmemory"},
  "log": {"level": "debug", "format": "text"},
  "http": {"shutdown_timeout": "3s"},
  "consumer": {
    "streams_per_ticker": 2,
    "max_streams_per_websocket": 200,
    "max_websockets": 5,
    "max_websocket_lifetime": "45m",
    "respawn_overlap": "5s",
    "exchanges": [
      {
        "name": "binance",
        "type": "binance",
        "base_url": "wss://example.invalid/binance",
        "tickers": ["BTC-USDT"],
        "market_type": "SPOT"
      },
      {
        "name": "bybit",
        "type": "bybit",
        "base_url": "wss://example.invalid/bybit",
        "tickers": ["BTC-USDT"],
        "market_type": "SPOT"
      }
    ]
  },
  "marketdata": {"publish_content_type": "application/json"}
}
`

	path := filepath.Join(t.TempDir(), "consumer-e2e.json")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write consumer config failed: %v", err)
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
