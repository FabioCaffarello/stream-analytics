package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/market-raccoon/internal/core/evidence/domain"
)

const lelGoldenPath = "testdata/lel_golden_replay.jsonl"

func TestLELGoldenReplayDeterministic(t *testing.T) {
	t.Parallel()

	run := func() []byte {
		cfg := DefaultRuleConfig()
		cfg.CooldownMs = 0
		engine := NewLELEngine(DefaultLELEngineConfig(),
			NewLELBookImbalanceRule(cfg),
			NewLELAbsorptionRule(cfg),
			NewLELSweepRule(cfg),
			NewLELThinningRule(cfg),
			NewLELSpreadRegimeRule(cfg),
		)
		events := syntheticLELEvents(500)
		var out bytes.Buffer
		for i := range events {
			emitted := engine.OnEvent(events[i])
			for j := range emitted {
				raw, err := json.Marshal(emitted[j])
				if err != nil {
					t.Fatalf("marshal output: %v", err)
				}
				out.Write(raw)
				out.WriteByte('\n')
			}
		}
		return out.Bytes()
	}

	first := run()
	second := run()
	if !bytes.Equal(first, second) {
		t.Fatal("replay output is not byte-identical across runs")
	}

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(lelGoldenPath), 0o750); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(lelGoldenPath, first, 0o600); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}

	want, err := os.ReadFile(lelGoldenPath) // #nosec G304 -- repository-owned deterministic fixture.
	if err != nil {
		t.Fatalf("read golden %s: %v (run with UPDATE_GOLDEN=1)", lelGoldenPath, err)
	}
	if !bytes.Equal(first, want) {
		t.Fatalf("golden mismatch for %s (run with UPDATE_GOLDEN=1)", lelGoldenPath)
	}
}

func syntheticLELEvents(n int) []domain.LELEvent {
	out := make([]domain.LELEvent, 0, n)
	venue := "binance"
	symbol := "BTC-USDT"
	for i := 1; i <= n; i++ {
		ts := int64(i) * 1000
		seq := int64(i)
		if i%2 == 0 {
			spread := 4.0
			bestBid := 100.0
			bestAsk := 100.04
			bidDepth := 500.0
			askDepth := 500.0
			bidLevels := 20
			askLevels := 20

			if i >= 40 && i <= 60 {
				bidDepth = 900
				askDepth = 100
			}
			if i >= 120 && i <= 130 {
				bidDepth = 100
				askDepth = 100
				bidLevels = 8
				askLevels = 8
			}
			if i == 200 {
				bestAsk = 101.50
				spread = 149
			}
			if i == 260 {
				bidLevels = 10
				bidDepth = 120
			}

			out = append(out, domain.LELEvent{
				Kind:      domain.LELEventKindSnapshot,
				Venue:     venue,
				Symbol:    symbol,
				TsServer:  ts,
				Seq:       seq,
				BestBid:   bestBid,
				BestAsk:   bestAsk,
				SpreadBPS: spread,
				BidDepth:  bidDepth,
				AskDepth:  askDepth,
				BidLevels: bidLevels,
				AskLevels: askLevels,
			})
			continue
		}

		totalVolume := 8.0
		if i >= 301 && i <= 309 {
			totalVolume = 120.0
		}
		out = append(out, domain.LELEvent{
			Kind:          domain.LELEventKindTape,
			Venue:         venue,
			Symbol:        symbol,
			TsServer:      ts,
			Seq:           seq,
			TradeCount:    10,
			BuyVolume:     totalVolume * 0.6,
			SellVolume:    totalVolume * 0.4,
			TotalVolume:   totalVolume,
			VwapPrice:     100.1,
			MaxPrice:      100.5,
			MinPrice:      99.8,
			Rate:          4,
			Imbalance:     0.2,
			WindowStartTs: ts - 1000,
			WindowEndTs:   ts,
		})
	}
	return out
}
