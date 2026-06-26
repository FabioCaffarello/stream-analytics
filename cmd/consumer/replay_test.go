package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/contracts"
	mdapp "github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/app"
	mddomain "github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/clock"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/codec"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/replay"
)

var updateGolden = flag.Bool("update-golden", false, "update replay golden fixtures")

func TestReplayEnvelopeToIngestRequest_DefaultsMarketType(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}

	payload, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, mddomain.TradeTickV1{
		Price:     1,
		Size:      1,
		Side:      "buy",
		TradeID:   "x",
		Timestamp: 1_710_000_000_000,
	})
	if p != nil {
		t.Fatalf("EncodePayload: %v", p)
	}

	req, p := replayEnvelopeToIngestRequest(envelope.Envelope{
		Type:           "marketdata.trade",
		Version:        1,
		Venue:          "binance",
		Instrument:     "BTC-USDT",
		TsExchange:     1_710_000_000_000,
		TsIngest:       1_710_000_000_001,
		Seq:            1,
		IdempotencyKey: "idem-1",
		ContentType:    envelope.ContentTypeJSON,
		Payload:        payload,
	})
	if p != nil {
		t.Fatalf("replayEnvelopeToIngestRequest: %v", p)
	}
	if req.MarketType != "SPOT" {
		t.Fatalf("MarketType=%q want=SPOT", req.MarketType)
	}
	if req.Metadata["instrument_market_type"] != "SPOT" {
		t.Fatalf("metadata instrument_market_type=%q want=SPOT", req.Metadata["instrument_market_type"])
	}
}

func TestReplayIngestGolden1000(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}

	fixturePath := filepath.Join("..", "..", "testdata", "fixtures", "ingest-1000.jsonl")
	goldenPath := filepath.Join("..", "..", "testdata", "golden", "ingest-replayed-1000.jsonl")

	ensureReplayFixture1000(t, fixturePath)

	fakeClock := clock.NewFakeClock(time.UnixMilli(0))
	replaySeq := replay.NewReplaySequencer()
	capture := &replay.CapturePublisher{}
	ingest := mdapp.NewIngestMarketData(fakeClock, replaySeq, capture)

	player, p := replay.NewPlayer(fixturePath, fakeClock, contracts.BootstrapPayloadCodecRegistry)
	if p != nil {
		t.Fatalf("NewPlayer: %v", p)
	}
	player.SetReplaySequencer(replaySeq)

	summary, p := player.Replay(context.Background(), func(ctx context.Context, env envelope.Envelope) *problem.Problem {
		req, pp := replayEnvelopeToIngestRequest(env)
		if pp != nil {
			return pp
		}
		res := ingest.Execute(ctx, req)
		if res.IsFail() {
			return res.Problem()
		}
		return nil
	})
	if p != nil {
		t.Fatalf("Replay: %v", p)
	}
	if summary.InputCount != 1000 {
		t.Fatalf("summary.InputCount=%d want=1000", summary.InputCount)
	}

	outPath := filepath.Join(t.TempDir(), "ingest-replayed-1000.jsonl")
	if p := replay.WriteFixtureFromEnvelopes(outPath, capture.Envelopes()); p != nil {
		t.Fatalf("WriteFixtureFromEnvelopes: %v", p)
	}

	if *updateGolden {
		copyFile(t, outPath, goldenPath)
	}
	if _, err := os.Stat(goldenPath); err != nil {
		t.Fatalf("missing golden file %s (run with -update-golden): %v", goldenPath, err)
	}
	if p := replay.CompareFixtureFiles(outPath, goldenPath); p != nil {
		t.Fatalf("golden mismatch: %v", p)
	}
}

func ensureReplayFixture1000(t *testing.T, path string) {
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

	seqByStream := make(map[string]int64, 4)
	envs := make([]envelope.Envelope, 0, 1000)

	for i := 0; i < 1000; i++ {
		venue := "binance"
		if i%2 == 1 {
			venue = "bybit"
		}
		instrument := "BTC-USDT"
		if (i/2)%2 == 1 {
			instrument = "ETH-USDT"
		}
		streamKey := venue + "|" + instrument
		seqByStream[streamKey]++
		seq := seqByStream[streamKey]
		ts := int64(1_710_000_000_000 + i)

		payload, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, mddomain.TradeTickV1{
			Price:     100 + float64(i%50),
			Size:      0.1 + float64(i%10)/10,
			Side:      "buy",
			TradeID:   fmt.Sprintf("%s-trade-%d", streamKey, i),
			Timestamp: ts - 2,
		})
		if p != nil {
			t.Fatalf("EncodePayload[%d]: %v", i, p)
		}

		envs = append(envs, envelope.Envelope{
			Type:           "marketdata.trade",
			Version:        1,
			Venue:          venue,
			Instrument:     instrument,
			TsExchange:     ts - 1,
			TsIngest:       ts,
			Seq:            seq,
			IdempotencyKey: fmt.Sprintf("%s-idem-%d", streamKey, i),
			ContentType:    envelope.ContentTypeJSON,
			Meta: map[string]string{
				"instrument_market_type": "SPOT",
				"exchange":               venue,
			},
			Payload: payload,
		})
	}

	if p := replay.WriteFixtureFromEnvelopes(path, envs); p != nil {
		t.Fatalf("WriteFixtureFromEnvelopes fixture: %v", p)
	}
}

func copyFile(t *testing.T, src, dst string) {
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
