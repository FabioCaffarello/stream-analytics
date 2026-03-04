package main

import (
	"context"
	"testing"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type spyArtifactPublisher struct {
	candleCalls int
	statsCalls  int
	tapeCalls   int
}

func (s *spyArtifactPublisher) PublishSnapshot(context.Context, aggdomain.SnapshotProduced) *problem.Problem {
	return nil
}

func (s *spyArtifactPublisher) PublishInconsistent(context.Context, aggdomain.OrderBookInconsistentDetected) *problem.Problem {
	return nil
}

func (s *spyArtifactPublisher) PublishCandleClosed(_ context.Context, _ aggdomain.CandleClosed) *problem.Problem {
	s.candleCalls++
	return nil
}

func (s *spyArtifactPublisher) PublishStatsClosed(_ context.Context, _ aggdomain.StatsWindowClosed) *problem.Problem {
	s.statsCalls++
	return nil
}

func (s *spyArtifactPublisher) PublishTapeClosed(_ context.Context, _ aggdomain.TapeClosed) *problem.Problem {
	s.tapeCalls++
	return nil
}

type spyCandleStore struct{ calls int }

func (s *spyCandleStore) SaveCandle(context.Context, aggdomain.CandleClosed) *problem.Problem {
	s.calls++
	return nil
}

type spyStatsStore struct{ calls int }

func (s *spyStatsStore) SaveStats(context.Context, aggdomain.StatsWindowClosed) *problem.Problem {
	s.calls++
	return nil
}

func TestSubMinuteRolloutGate_ScopedMatching(t *testing.T) {
	gate := newSubMinuteRolloutGate(config.ProcessorSubMinuteRolloutConfig{
		Enabled:     true,
		Venues:      []string{"binance"},
		Instruments: []string{"BTCUSDT"},
	})
	if !gate.allows("BINANCE", "BTCUSDT:SPOT", "1s") {
		t.Fatal("expected scoped gate to allow matching venue/instrument for 1s")
	}
	if gate.allows("BYBIT", "BTCUSDT:SPOT", "1s") {
		t.Fatal("expected scoped gate to block non-matching venue for 1s")
	}
	if gate.allows("BINANCE", "ETHUSDT:SPOT", "5s") {
		t.Fatal("expected scoped gate to block non-matching instrument for 5s")
	}
	if !gate.allows("BYBIT", "ETHUSDT", "1m") {
		t.Fatal("expected non sub-minute timeframe to bypass rollout gate")
	}
}

func TestSubMinuteFilteringPublisher_BlocksSubMinuteAndCountsDrop(t *testing.T) {
	next := &spyArtifactPublisher{}
	wrapped := &subMinuteFilteringArtifactPublisher{
		next: next,
		gate: newSubMinuteRolloutGate(config.ProcessorSubMinuteRolloutConfig{
			Enabled: false,
		}),
	}

	beforeDrops := testutil.ToFloat64(metrics.IngestDropTotal.WithLabelValues("subminute_rollout_blocked"))
	if p := wrapped.PublishCandleClosed(context.Background(), aggdomain.CandleClosed{
		Candle: aggdomain.CandleV1{
			Venue:      "BINANCE",
			Instrument: "BTCUSDT:SPOT",
			Timeframe:  "1s",
		},
	}); p != nil {
		t.Fatalf("unexpected problem on sub-minute blocked publish: %v", p)
	}
	if next.candleCalls != 0 {
		t.Fatalf("candle publish calls=%d want=0 for blocked 1s", next.candleCalls)
	}
	afterDrops := testutil.ToFloat64(metrics.IngestDropTotal.WithLabelValues("subminute_rollout_blocked"))
	if diff := afterDrops - beforeDrops; diff != 1 {
		t.Fatalf("subminute_rollout_blocked drops delta=%f want=1", diff)
	}

	if p := wrapped.PublishStatsClosed(context.Background(), aggdomain.StatsWindowClosed{
		Stats: aggdomain.StatsWindowV1{
			Venue:      "BINANCE",
			Instrument: "BTCUSDT:SPOT",
			Timeframe:  "1m",
		},
	}); p != nil {
		t.Fatalf("unexpected problem on non sub-minute publish: %v", p)
	}
	if next.statsCalls != 1 {
		t.Fatalf("stats publish calls=%d want=1 for 1m", next.statsCalls)
	}
}

func TestSubMinuteFilteringStores_BlockSubMinuteOnly(t *testing.T) {
	candleStore := &spyCandleStore{}
	statsStore := &spyStatsStore{}

	candleWrapped := &subMinuteFilteringCandleStore{
		next: candleStore,
		gate: newSubMinuteRolloutGate(config.ProcessorSubMinuteRolloutConfig{
			Enabled: false,
		}),
	}
	statsWrapped := &subMinuteFilteringStatsStore{
		next: statsStore,
		gate: newSubMinuteRolloutGate(config.ProcessorSubMinuteRolloutConfig{
			Enabled: false,
		}),
	}

	_ = candleWrapped.SaveCandle(context.Background(), aggdomain.CandleClosed{
		Candle: aggdomain.CandleV1{
			Venue:      "BINANCE",
			Instrument: "BTCUSDT:SPOT",
			Timeframe:  "1s",
		},
	})
	_ = statsWrapped.SaveStats(context.Background(), aggdomain.StatsWindowClosed{
		Stats: aggdomain.StatsWindowV1{
			Venue:      "BINANCE",
			Instrument: "BTCUSDT:SPOT",
			Timeframe:  "5s",
		},
	})
	if candleStore.calls != 0 {
		t.Fatalf("candle store calls=%d want=0 for blocked 1s", candleStore.calls)
	}
	if statsStore.calls != 0 {
		t.Fatalf("stats store calls=%d want=0 for blocked 5s", statsStore.calls)
	}

	_ = candleWrapped.SaveCandle(context.Background(), aggdomain.CandleClosed{
		Candle: aggdomain.CandleV1{
			Venue:      "BINANCE",
			Instrument: "BTCUSDT:SPOT",
			Timeframe:  "1m",
		},
	})
	_ = statsWrapped.SaveStats(context.Background(), aggdomain.StatsWindowClosed{
		Stats: aggdomain.StatsWindowV1{
			Venue:      "BINANCE",
			Instrument: "BTCUSDT:SPOT",
			Timeframe:  "1m",
		},
	})
	if candleStore.calls != 1 {
		t.Fatalf("candle store calls=%d want=1 for 1m", candleStore.calls)
	}
	if statsStore.calls != 1 {
		t.Fatalf("stats store calls=%d want=1 for 1m", statsStore.calls)
	}
}
