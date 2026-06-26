package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	insightsapp "github.com/FabioCaffarello/stream-analytics/internal/core/insights/app"
	sharedhash "github.com/FabioCaffarello/stream-analytics/internal/shared/hash"
)

type heatmapFixtureRow struct {
	EventType  string  `json:"event_type"`
	Venue      string  `json:"venue"`
	Instrument string  `json:"instrument"`
	Timeframe  string  `json:"timeframe"`
	TickSize   float64 `json:"tick_size"`
	Price      float64 `json:"price"`
	Size       float64 `json:"size"`
	Side       string  `json:"side"`
	TsIngest   int64   `json:"ts_ingest"`
	Seq        int64   `json:"seq"`
}

func TestReplayHeatmapGolden1000(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "testdata", "fixtures", "heatmap-1000.jsonl")
	goldenPath := filepath.Join("..", "..", "testdata", "golden", "heatmap-replayed-1000.jsonl")
	ensureHeatmapFixture1000(t, fixturePath)

	outputPath := filepath.Join(t.TempDir(), "heatmap-replayed-1000.jsonl")
	if err := replayHeatmapFixtureToFile(context.Background(), fixturePath, outputPath); err != nil {
		t.Fatalf("replay heatmap fixture: %v", err)
	}
	if *updateGolden {
		copyFile(t, outputPath, goldenPath)
	}
	if _, err := os.Stat(goldenPath); err != nil {
		t.Fatalf("missing golden file %s (run with -update-golden): %v", goldenPath, err)
	}
	assertFilesEqual(t, outputPath, goldenPath)
}

func TestHeatmapReplayByteStable50Runs(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "testdata", "fixtures", "heatmap-1000.jsonl")
	ensureHeatmapFixture1000(t, fixturePath)

	var expected string
	for i := 0; i < 50; i++ {
		outPath := filepath.Join(t.TempDir(), "heatmap-output.jsonl")
		if err := replayHeatmapFixtureToFile(context.Background(), fixturePath, outPath); err != nil {
			t.Fatalf("replay run[%d]: %v", i, err)
		}
		// #nosec G304 -- outPath is test-controlled (t.TempDir).
		raw, err := os.ReadFile(outPath)
		if err != nil {
			t.Fatalf("read output[%d]: %v", i, err)
		}
		h := sharedhash.HashBytes(raw)
		if i == 0 {
			expected = h
			continue
		}
		if h != expected {
			t.Fatalf("byte stability mismatch run=%d got=%s want=%s", i, h, expected)
		}
	}
}

func replayHeatmapFixtureToFile(ctx context.Context, fixturePath, outPath string) error {
	uc := insightsapp.NewBuildHeatmap()
	// #nosec G304 -- fixturePath is a test-controlled path.
	in, err := os.Open(fixturePath)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	// #nosec G304 -- outPath is a test-controlled path.
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	enc := json.NewEncoder(out)

	for sc.Scan() {
		var row heatmapFixtureRow
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			return err
		}
		res := uc.Execute(ctx, insightsapp.BuildHeatmapRequest{
			EventType:  row.EventType,
			Venue:      row.Venue,
			Instrument: row.Instrument,
			Timeframe:  row.Timeframe,
			TickSize:   row.TickSize,
			Price:      row.Price,
			Size:       row.Size,
			Side:       row.Side,
			TsIngest:   row.TsIngest,
			Seq:        row.Seq,
		})
		if res.IsFail() {
			return fmt.Errorf("build heatmap failed: %v", res.Problem())
		}
		v := res.Value()
		record := map[string]any{
			"idempotency_key": v.IdempotencyKey,
			"artifact":        v.Artifact,
		}
		if err := enc.Encode(record); err != nil {
			return err
		}
	}
	return sc.Err()
}

func ensureHeatmapFixture1000(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil && !*updateGolden {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("MkdirAll fixture dir: %v", err)
	}
	// #nosec G304 -- path is a test-controlled path.
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)

	for i := 0; i < 1000; i++ {
		eventType := "marketdata.trade"
		if i%3 == 0 {
			eventType = "marketdata.bookdelta"
		}
		instrument := "BTC-USDT"
		if i%5 == 0 {
			instrument = "ETH-USDT"
		}
		side := "buy"
		if i%2 == 1 {
			side = "sell"
		}
		row := heatmapFixtureRow{
			EventType:  eventType,
			Venue:      "binance",
			Instrument: instrument,
			Timeframe:  "1m",
			TickSize:   0.5,
			Price:      100 + float64(i%32)*0.5,
			Size:       0.1 + float64((i%10)+1)*0.2,
			Side:       side,
			TsIngest:   1_710_000_000_000 + int64(i*50),
			Seq:        int64(i + 1),
		}
		if err := enc.Encode(row); err != nil {
			t.Fatalf("encode fixture row[%d]: %v", i, err)
		}
	}
}

func assertFilesEqual(t *testing.T, actualPath, expectedPath string) {
	t.Helper()
	// #nosec G304 -- actualPath is a test-controlled path.
	actual, err := os.ReadFile(actualPath)
	if err != nil {
		t.Fatalf("read actual: %v", err)
	}
	// #nosec G304 -- expectedPath is a test-controlled path.
	expected, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read expected: %v", err)
	}
	if string(actual) != string(expected) {
		t.Fatalf("golden mismatch: actual=%s expected=%s", actualPath, expectedPath)
	}
}
