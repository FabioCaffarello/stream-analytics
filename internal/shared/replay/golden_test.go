package replay

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/envelope"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
)

func TestGoldenReplay(t *testing.T) {
	mustBootstrapPayloadRegistry(t)

	inputPath := filepath.Join("testdata", "fixtures", "input-mini.jsonl")
	goldenPath := filepath.Join("testdata", "golden", "output-mini.jsonl")
	updateGolden := shouldUpdateGolden()

	ensureMiniInputFixture(t, inputPath, 50, updateGolden)

	outputPath, summary := runReplayToPath(t, inputPath)
	if summary.InputCount != 50 {
		t.Fatalf("input count=%d want=50", summary.InputCount)
	}

	if updateGolden {
		copyFile(t, outputPath, goldenPath)
		return
	}

	if p := CompareFixtureFiles(outputPath, goldenPath); p != nil {
		t.Fatalf("golden mismatch: %v", p)
	}
}

func TestGoldenReplayByteStable50Runs(t *testing.T) {
	mustBootstrapPayloadRegistry(t)

	inputPath := filepath.Join("testdata", "fixtures", "input-mini.jsonl")
	ensureMiniInputFixture(t, inputPath, 50, true)

	var expected string
	for i := 0; i < 50; i++ {
		outputPath, _ := runReplayToPath(t, inputPath)
		// #nosec G304 -- outputPath is generated via t.TempDir.
		data, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("read output[%d]: %v", i, err)
		}
		h := sharedhash.HashBytes(data)
		if i == 0 {
			expected = h
			continue
		}
		if h != expected {
			t.Fatalf("sha mismatch run=%d got=%s want=%s", i, h, expected)
		}
	}
}

func runReplayToPath(t *testing.T, inputPath string) (string, ReplaySummary) {
	t.Helper()
	fakeClock := clock.NewFakeClock(time.UnixMilli(0))
	player, p := NewPlayer(inputPath, fakeClock)
	if p != nil {
		t.Fatalf("NewPlayer: %v", p)
	}

	capture := &CapturePublisher{}
	summary, p := player.Replay(context.Background(), capture.Publish)
	if p != nil {
		t.Fatalf("Replay: %v", p)
	}

	outputPath := filepath.Join(t.TempDir(), "output-mini.jsonl")
	if p := WriteFixtureFromEnvelopes(outputPath, capture.Envelopes()); p != nil {
		t.Fatalf("WriteFixtureFromEnvelopes: %v", p)
	}
	return outputPath, summary
}

func ensureMiniInputFixture(t *testing.T, inputPath string, n int, allowCreate bool) {
	t.Helper()
	if _, err := os.Stat(inputPath); err == nil {
		return
	}
	if !allowCreate {
		t.Fatalf("missing input fixture: %s (run with UPDATE_GOLDEN=1 to create)", inputPath)
	}
	if err := os.MkdirAll(filepath.Dir(inputPath), 0o750); err != nil {
		t.Fatalf("mkdir input fixture dir: %v", err)
	}

	envs := make([]envelope.Envelope, 0, n)
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			envs = append(envs, buildJSONFixtureEnvelope(t, i))
			continue
		}
		envs = append(envs, buildProtoFixtureEnvelope(t, i))
	}

	if p := WriteFixtureFromEnvelopes(inputPath, envs); p != nil {
		t.Fatalf("write input fixture: %v", p)
	}
}

func shouldUpdateGolden() bool {
	raw := os.Getenv("UPDATE_GOLDEN")
	if raw == "" {
		return false
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return false
	}
	return v == 1
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	// #nosec G304 -- src/dst are explicit test paths.
	in, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read src %s: %v", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		t.Fatalf("mkdir dst dir: %v", err)
	}
	// #nosec G304 -- src/dst are explicit test paths.
	if err := os.WriteFile(dst, in, 0o600); err != nil {
		t.Fatalf("write dst %s: %v", dst, err)
	}
}
