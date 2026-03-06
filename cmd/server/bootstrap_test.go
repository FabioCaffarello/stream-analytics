package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	deliverydomain "github.com/market-raccoon/internal/core/delivery/domain"
	deliveryports "github.com/market-raccoon/internal/core/delivery/ports"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestBuildServerFactories_DeliveryEnabledIncludesFactory(t *testing.T) {
	factory := func() actor.Receiver { return nil }
	factories := buildServerFactories(true, factory)
	if _, ok := factories[actorruntime.SubsystemDelivery]; !ok {
		t.Fatal("expected SubsystemDelivery factory when delivery is enabled")
	}
}

func TestBuildServerFactories_DeliveryDisabledExcludesFactory(t *testing.T) {
	factory := func() actor.Receiver { return nil }
	factories := buildServerFactories(false, factory)
	if _, ok := factories[actorruntime.SubsystemDelivery]; ok {
		t.Fatal("did not expect SubsystemDelivery factory when delivery is disabled")
	}
}

func TestConfigLoad_DeliveryEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "server.jsonc")
	raw := `{
  "delivery": {
    "enabled": true,
    "max_sessions": 128,
    "backpressure_policy": "drop_newest",
    "nats": {
      "consumer_durable": "delivery-test",
      "filter_subjects": ["marketdata.>"]
    }
  }
}`
	if err := os.WriteFile(cfgPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, p := config.Load(cfgPath)
	if p != nil {
		t.Fatalf("config load failed: %v", p)
	}
	if !cfg.Delivery.Enabled {
		t.Fatal("expected delivery.enabled=true")
	}
	if cfg.Delivery.MaxSessions != 128 {
		t.Fatalf("max_sessions=%d want=128", cfg.Delivery.MaxSessions)
	}
	if p := cfg.Validate(); p != nil {
		t.Fatalf("config validate failed: %v", p)
	}
}

type stubRangeStore struct {
	items []deliveryports.RangeItem
}

func (s stubRangeStore) GetRange(_ context.Context, _ deliverydomain.Subject, _, _ int64, _ int) ([]deliveryports.RangeItem, *problem.Problem) {
	return append([]deliveryports.RangeItem(nil), s.items...), nil
}

func (s stubRangeStore) StoreEnvelope(_ envelope.Envelope) {}

func TestRangeStoreHotSnapshotProvider_GetLatest_CandleNewest(t *testing.T) {
	subject, p := deliverydomain.ParseSubject("aggregation.candle/binance/BTCUSDT:SPOT/raw")
	if p != nil {
		t.Fatalf("parse subject: %v", p)
	}
	provider := newRangeStoreHotSnapshotProvider(stubRangeStore{
		items: []deliveryports.RangeItem{
			{Seq: 10, TsIngest: time.Now().Add(-5 * time.Minute).UnixMilli(), Payload: []byte(`{"v":"old"}`)},
			{Seq: 8, TsIngest: time.Now().Add(-2 * time.Minute).UnixMilli(), Payload: []byte(`{"v":"older-seq"}`)},
			{Seq: 11, TsIngest: time.Now().Add(-2 * time.Minute).UnixMilli(), Payload: []byte(`{"v":"new"}`)},
		},
	})
	raw, ok := provider.GetLatest(subject)
	if !ok {
		t.Fatal("expected snapshot payload")
	}
	if got, want := string(raw), `{"v":"new"}`; got != want {
		t.Fatalf("payload=%s want=%s", got, want)
	}
}

func TestRangeStoreHotSnapshotProvider_GetLatest_UnsupportedStream(t *testing.T) {
	subject, p := deliverydomain.ParseSubject("marketdata.trade/binance/BTCUSDT:SPOT/raw")
	if p != nil {
		t.Fatalf("parse subject: %v", p)
	}
	provider := newRangeStoreHotSnapshotProvider(stubRangeStore{
		items: []deliveryports.RangeItem{{Seq: 1, TsIngest: time.Now().UnixMilli(), Payload: []byte(`{"x":1}`)}},
	})
	if raw, ok := provider.GetLatest(subject); ok || raw != nil {
		t.Fatalf("expected unsupported stream to return no snapshot, got ok=%v raw=%q", ok, string(raw))
	}
}

func TestRangeStoreHotSnapshotProvider_GetLatest_AliasFallsBackToCanonicalSymbol(t *testing.T) {
	subjectAlias, p := deliverydomain.ParseSubject("aggregation.candle/binance/BTCUSDT:SPOT/raw")
	if p != nil {
		t.Fatalf("parse alias subject: %v", p)
	}
	subjectCanonical, p := deliverydomain.ParseSubject("aggregation.candle/binance/BTCUSDT/raw")
	if p != nil {
		t.Fatalf("parse canonical subject: %v", p)
	}

	provider := newRangeStoreHotSnapshotProvider(aliasAwareStubRangeStore{
		bySubject: map[string][]deliveryports.RangeItem{
			subjectCanonical.String(): {
				{Seq: 42, TsIngest: time.Now().UnixMilli(), Payload: []byte(`{"v":"canonical"}`)},
			},
		},
	})
	raw, ok := provider.GetLatest(subjectAlias)
	if !ok {
		t.Fatal("expected alias fallback snapshot payload")
	}
	if got, want := string(raw), `{"v":"canonical"}`; got != want {
		t.Fatalf("payload=%s want=%s", got, want)
	}
}

type fixedSnapshotProvider struct {
	bySubject map[string][]byte
	calls     int
}

func (p *fixedSnapshotProvider) GetLatest(subject deliverydomain.Subject) ([]byte, bool) {
	p.calls++
	raw, ok := p.bySubject[subject.String()]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), raw...), true
}

func TestBoundedSnapshotCacheProvider_UsesTTLAndSizeBounds(t *testing.T) {
	subA, p := deliverydomain.ParseSubject("aggregation.candle/binance/BTCUSDT/raw")
	if p != nil {
		t.Fatalf("parse subject A: %v", p)
	}
	subB, p := deliverydomain.ParseSubject("aggregation.candle/binance/ETHUSDT/raw")
	if p != nil {
		t.Fatalf("parse subject B: %v", p)
	}
	subC, p := deliverydomain.ParseSubject("aggregation.candle/binance/SOLUSDT/raw")
	if p != nil {
		t.Fatalf("parse subject C: %v", p)
	}

	now := time.Unix(1000, 0)
	base := &fixedSnapshotProvider{bySubject: map[string][]byte{
		subA.String(): []byte(`{"v":"a"}`),
		subB.String(): []byte(`{"v":"b"}`),
		subC.String(): []byte(`{"v":"c"}`),
	}}
	cache := newBoundedSnapshotCacheProvider(base, time.Second, 2)
	cached, ok := cache.(*boundedSnapshotCacheProvider)
	if !ok {
		t.Fatalf("provider type=%T want *boundedSnapshotCacheProvider", cache)
	}
	cached.clock = func() time.Time { return now }

	_, _ = cached.GetLatest(subA)
	_, _ = cached.GetLatest(subA) // hit from cache
	if got, want := base.calls, 1; got != want {
		t.Fatalf("base calls after cache hit=%d want=%d", got, want)
	}

	_, _ = cached.GetLatest(subB)
	_, _ = cached.GetLatest(subC) // max entries=2, should evict oldest
	if got := testutil.ToFloat64(metrics.DeliveryWSSnapshotCacheEntries); got != 2 {
		t.Fatalf("snapshot cache entries gauge=%f want=2", got)
	}
	_, _ = cached.GetLatest(subA) // re-fetch after eviction
	if got, want := base.calls, 4; got != want {
		t.Fatalf("base calls after eviction=%d want=%d", got, want)
	}

	now = now.Add(2 * time.Second) // TTL expiry
	_, _ = cached.GetLatest(subB)
	if got, want := base.calls, 5; got != want {
		t.Fatalf("base calls after ttl expiry=%d want=%d", got, want)
	}
}

type aliasAwareStubRangeStore struct {
	bySubject map[string][]deliveryports.RangeItem
}

func (s aliasAwareStubRangeStore) GetRange(_ context.Context, subject deliverydomain.Subject, _, _ int64, _ int) ([]deliveryports.RangeItem, *problem.Problem) {
	items := s.bySubject[subject.String()]
	return append([]deliveryports.RangeItem(nil), items...), nil
}

func (s aliasAwareStubRangeStore) StoreEnvelope(_ envelope.Envelope) {}

type spyCandleReader struct {
	rangeCalls int
}

func (s *spyCandleReader) GetCandleRange(context.Context, string, string, string, int64, int64, int) ([]aggdomain.CandleV1, *problem.Problem) {
	s.rangeCalls++
	return []aggdomain.CandleV1{{Timeframe: "1m"}}, nil
}

func (s *spyCandleReader) GetCandleTimestamps(context.Context, string, string, string, int64, int64) ([]int64, *problem.Problem) {
	return []int64{1}, nil
}

func (s *spyCandleReader) GetFirstCandle(context.Context, string, string, string) (*aggdomain.CandleV1, *problem.Problem) {
	c := &aggdomain.CandleV1{Timeframe: "1m"}
	return c, nil
}

func (s *spyCandleReader) GetLastCandle(context.Context, string, string, string) (*aggdomain.CandleV1, *problem.Problem) {
	c := &aggdomain.CandleV1{Timeframe: "1m"}
	return c, nil
}

type spyStatsReader struct {
	rangeCalls int
}

func (s *spyStatsReader) GetStatsTimestamps(context.Context, string, string, string, int64, int64) ([]int64, *problem.Problem) {
	return []int64{1}, nil
}

func (s *spyStatsReader) GetStatsRange(context.Context, string, string, string, int64, int64, int) ([]aggdomain.StatsWindowV1, *problem.Problem) {
	s.rangeCalls++
	return []aggdomain.StatsWindowV1{{Timeframe: "1m"}}, nil
}

func (s *spyStatsReader) GetFirstStats(context.Context, string, string, string) (*aggdomain.StatsWindowV1, *problem.Problem) {
	st := &aggdomain.StatsWindowV1{Timeframe: "1m"}
	return st, nil
}

func (s *spyStatsReader) GetLastStats(context.Context, string, string, string) (*aggdomain.StatsWindowV1, *problem.Problem) {
	st := &aggdomain.StatsWindowV1{Timeframe: "1m"}
	return st, nil
}

func TestSubMinuteFilteringRangeStore_BlocksSubMinuteAndAllowsHigherTF(t *testing.T) {
	sub1s, p := deliverydomain.ParseSubject("aggregation.candle/binance/BTCUSDT/1s")
	if p != nil {
		t.Fatalf("parse 1s subject: %v", p)
	}
	sub1m, p := deliverydomain.ParseSubject("aggregation.candle/binance/BTCUSDT/1m")
	if p != nil {
		t.Fatalf("parse 1m subject: %v", p)
	}

	next := stubRangeStore{
		items: []deliveryports.RangeItem{{Seq: 1, TsIngest: 1, Payload: []byte(`{"ok":true}`)}},
	}
	store := subMinuteFilteringRangeStore{
		next: next,
		gate: newSubMinuteRolloutGate(config.ProcessorSubMinuteRolloutConfig{
			Enabled: false,
		}),
	}

	before := testutil.ToFloat64(metrics.WSQueryRejectedTotal.WithLabelValues("subminute_rollout_blocked"))
	items, p := store.GetRange(context.Background(), sub1s, 0, 0, 10)
	if p != nil {
		t.Fatalf("blocked 1s should not fail query path: %v", p)
	}
	if len(items) != 0 {
		t.Fatalf("blocked 1s items=%d want=0", len(items))
	}
	after := testutil.ToFloat64(metrics.WSQueryRejectedTotal.WithLabelValues("subminute_rollout_blocked"))
	if diff := after - before; diff != 1 {
		t.Fatalf("ws_query_rejected{subminute_rollout_blocked} delta=%f want=1", diff)
	}

	items, p = store.GetRange(context.Background(), sub1m, 0, 0, 10)
	if p != nil {
		t.Fatalf("1m query failed: %v", p)
	}
	if len(items) != 1 {
		t.Fatalf("1m items=%d want=1", len(items))
	}
}

func TestSubMinuteFilteringRangeStore_ScopedAllowlistByVenueAndInstrument(t *testing.T) {
	subAllowed, p := deliverydomain.ParseSubject("aggregation.candle/binance/BTCUSDT:SPOT/1s")
	if p != nil {
		t.Fatalf("parse allowed subject: %v", p)
	}
	subWrongVenue, p := deliverydomain.ParseSubject("aggregation.candle/bybit/BTCUSDT:SPOT/1s")
	if p != nil {
		t.Fatalf("parse wrong-venue subject: %v", p)
	}
	subWrongInstrument, p := deliverydomain.ParseSubject("aggregation.candle/binance/ETHUSDT:SPOT/1s")
	if p != nil {
		t.Fatalf("parse wrong-instrument subject: %v", p)
	}

	next := stubRangeStore{
		items: []deliveryports.RangeItem{{Seq: 1, TsIngest: 1, Payload: []byte(`{"ok":true}`)}},
	}
	store := subMinuteFilteringRangeStore{
		next: next,
		gate: newSubMinuteRolloutGate(config.ProcessorSubMinuteRolloutConfig{
			Enabled:     true,
			Venues:      []string{"binance"},
			Instruments: []string{"BTCUSDT"},
		}),
	}

	items, p := store.GetRange(context.Background(), subAllowed, 0, 0, 10)
	if p != nil {
		t.Fatalf("allowed 1s query failed: %v", p)
	}
	if len(items) != 1 {
		t.Fatalf("allowed 1s items=%d want=1", len(items))
	}

	items, p = store.GetRange(context.Background(), subWrongVenue, 0, 0, 10)
	if p != nil {
		t.Fatalf("wrong-venue 1s query should not fail: %v", p)
	}
	if len(items) != 0 {
		t.Fatalf("wrong-venue 1s items=%d want=0", len(items))
	}

	items, p = store.GetRange(context.Background(), subWrongInstrument, 0, 0, 10)
	if p != nil {
		t.Fatalf("wrong-instrument 1s query should not fail: %v", p)
	}
	if len(items) != 0 {
		t.Fatalf("wrong-instrument 1s items=%d want=0", len(items))
	}
}

func TestSubMinuteFilteringCandleReader_BlocksSubMinuteRange(t *testing.T) {
	next := &spyCandleReader{}
	reader := subMinuteFilteringCandleReader{
		next: next,
		gate: newSubMinuteRolloutGate(config.ProcessorSubMinuteRolloutConfig{
			Enabled: false,
		}),
	}
	items, p := reader.GetCandleRange(context.Background(), "binance", "BTCUSDT", "1s", 0, 10, 100)
	if p != nil {
		t.Fatalf("blocked 1s query should not fail: %v", p)
	}
	if len(items) != 0 {
		t.Fatalf("blocked 1s items=%d want=0", len(items))
	}
	if next.rangeCalls != 0 {
		t.Fatalf("next candle reader calls=%d want=0 for blocked 1s", next.rangeCalls)
	}

	items, p = reader.GetCandleRange(context.Background(), "binance", "BTCUSDT", "1m", 0, 10, 100)
	if p != nil {
		t.Fatalf("1m query should pass: %v", p)
	}
	if len(items) != 1 {
		t.Fatalf("1m items=%d want=1", len(items))
	}
	if next.rangeCalls != 1 {
		t.Fatalf("next candle reader calls=%d want=1 for 1m", next.rangeCalls)
	}
}

func TestSubMinuteFilteringStatsReader_BlocksSubMinuteRange(t *testing.T) {
	next := &spyStatsReader{}
	reader := subMinuteFilteringStatsReader{
		next: next,
		gate: newSubMinuteRolloutGate(config.ProcessorSubMinuteRolloutConfig{
			Enabled: false,
		}),
	}
	items, p := reader.GetStatsRange(context.Background(), "binance", "BTCUSDT", "5s", 0, 10, 100)
	if p != nil {
		t.Fatalf("blocked 5s query should not fail: %v", p)
	}
	if len(items) != 0 {
		t.Fatalf("blocked 5s items=%d want=0", len(items))
	}
	if next.rangeCalls != 0 {
		t.Fatalf("next stats reader calls=%d want=0 for blocked 5s", next.rangeCalls)
	}

	items, p = reader.GetStatsRange(context.Background(), "binance", "BTCUSDT", "1m", 0, 10, 100)
	if p != nil {
		t.Fatalf("1m query should pass: %v", p)
	}
	if len(items) != 1 {
		t.Fatalf("1m items=%d want=1", len(items))
	}
	if next.rangeCalls != 1 {
		t.Fatalf("next stats reader calls=%d want=1 for 1m", next.rangeCalls)
	}
}
