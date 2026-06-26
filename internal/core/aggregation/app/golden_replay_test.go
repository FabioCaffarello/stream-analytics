package app_test

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/app"
	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/clock"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/replay"
)

var updateGolden = flag.Bool("update-golden", false, "update aggregation golden fixtures")

type snapshotGoldenLine struct {
	Venue      string         `json:"venue"`
	Instrument string         `json:"instrument"`
	Seq        int64          `json:"seq"`
	Bids       []domain.Level `json:"bids"`
	Asks       []domain.Level `json:"asks"`
}

type fixturePriceLevel struct {
	Price float64 `json:"price"`
	Size  float64 `json:"size"`
}

type fixtureBookDelta struct {
	Bids []fixturePriceLevel `json:"bids"`
	Asks []fixturePriceLevel `json:"asks"`
}

func TestAggregationGoldenReplayFromFixture(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "..", "..", "testdata", "fixtures", "aggregation-1000.jsonl")
	goldenPath := filepath.Join("..", "..", "..", "..", "testdata", "golden", "aggregation-snapshots-1000.jsonl")
	ensureAggregationFixture1000(t, fixturePath)

	reader, p := replay.NewReader(fixturePath)
	if p != nil {
		t.Fatalf("NewReader: %v", p)
	}
	defer func() {
		_ = reader.Close()
	}()

	pub := &fakePublisher{}
	store := &fakeStore{}
	uc := app.NewUpdateOrderBookFromEventsWithConfig(pub, store, app.UpdateConfig{
		MaxBooks:  2048,
		BookTTL:   time.Hour,
		MaxLevels: 1000,
		Clock:     clock.NewFakeClock(time.UnixMilli(0)),
	})

	for idx := 0; ; idx++ {
		rec, ok, p := reader.Next()
		if p != nil {
			t.Fatalf("Reader.Next[%d]: %v", idx, p)
		}
		if !ok {
			break
		}

		var delta fixtureBookDelta
		if err := json.Unmarshal(rec.Envelope.Payload, &delta); err != nil {
			t.Fatalf("json.Unmarshal payload[%d]: %v", idx, err)
		}

		res := uc.Execute(context.Background(), app.UpdateRequest{
			Venue:      rec.Envelope.Venue,
			Instrument: rec.Envelope.Instrument,
			Seq:        rec.Envelope.Seq,
			Bids:       toAggregationLevels(delta.Bids),
			Asks:       toAggregationLevels(delta.Asks),
		})
		if res.IsFail() {
			t.Fatalf("Execute[%d]: %v", idx, res.Problem())
		}
	}

	outPath := filepath.Join(t.TempDir(), "aggregation-snapshots-1000.jsonl")
	if err := writeSnapshotsGolden(outPath, pub.snaps); err != nil {
		t.Fatalf("writeSnapshotsGolden: %v", err)
	}

	if *updateGolden {
		copyGoldenFile(t, outPath, goldenPath)
	}
	if _, err := os.Stat(goldenPath); err != nil {
		t.Fatalf("missing golden file %s (run with -update-golden): %v", goldenPath, err)
	}
	if p := replay.CompareFixtureFiles(outPath, goldenPath); p != nil {
		t.Fatalf("golden mismatch: %v", p)
	}
}

func ensureAggregationFixture1000(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err == nil && !*updateGolden {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("MkdirAll fixture dir: %v", err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatalf("Remove fixture: %v", err)
	}

	seqByStream := make(map[string]int64, 2)
	envs := make([]envelope.Envelope, 0, 1000)

	for i := 0; i < 1000; i++ {
		instrument := "BTCUSDT"
		if i%2 == 1 {
			instrument = "ETHUSDT"
		}
		stream := "BINANCE|" + instrument
		seqByStream[stream]++
		seq := seqByStream[stream]
		ts := int64(1_710_100_000_000 + i)
		basePrice := 100.0
		if instrument == "ETHUSDT" {
			basePrice = 200.0
		}

		delta := fixtureBookDelta{
			Bids: []fixturePriceLevel{{Price: basePrice, Size: 1 + float64(i%5)/10}},
			Asks: []fixturePriceLevel{{Price: basePrice + 1, Size: 1.5 + float64(i%5)/10}},
		}
		payload, err := json.Marshal(delta)
		if err != nil {
			t.Fatalf("json.Marshal[%d]: %v", i, err)
		}

		envs = append(envs, envelope.Envelope{
			Type:           "marketdata.bookdelta",
			Version:        1,
			Venue:          "BINANCE",
			Instrument:     instrument,
			TsExchange:     ts - 1,
			TsIngest:       ts,
			Seq:            seq,
			IdempotencyKey: fmt.Sprintf("bookdelta-%s-%d", instrument, i),
			ContentType:    envelope.ContentTypeJSON,
			Payload:        payload,
		})
	}

	if p := replay.WriteFixtureFromEnvelopes(path, envs); p != nil {
		t.Fatalf("WriteFixtureFromEnvelopes fixture: %v", p)
	}
}

func toAggregationLevels(levels []fixturePriceLevel) []domain.Level {
	out := make([]domain.Level, 0, len(levels))
	for i := range levels {
		out = append(out, domain.Level{
			Price:    domain.Price(levels[i].Price),
			Quantity: domain.Quantity(levels[i].Size),
		})
	}
	return out
}

func writeSnapshotsGolden(path string, snaps []domain.SnapshotProduced) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}

	// #nosec G304 -- path is test-controlled and built from test-local inputs.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for i := range snaps {
		line := snapshotGoldenLine{
			Venue:      snaps[i].BookID.Venue,
			Instrument: snaps[i].BookID.Instrument,
			Seq:        snaps[i].Seq,
			Bids:       snaps[i].Bids,
			Asks:       snaps[i].Asks,
		}
		if err := enc.Encode(line); err != nil {
			return err
		}
	}
	return nil
}

func copyGoldenFile(t *testing.T, src, dst string) {
	t.Helper()
	// #nosec G304 -- src is test-controlled path.
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(dst), err)
	}
	// #nosec G304 -- dst is test-controlled path.
	if err := os.WriteFile(dst, raw, 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", dst, err)
	}
}
