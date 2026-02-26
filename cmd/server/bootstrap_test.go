package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	deliverydomain "github.com/market-raccoon/internal/core/delivery/domain"
	deliveryports "github.com/market-raccoon/internal/core/delivery/ports"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/problem"
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

type aliasAwareStubRangeStore struct {
	bySubject map[string][]deliveryports.RangeItem
}

func (s aliasAwareStubRangeStore) GetRange(_ context.Context, subject deliverydomain.Subject, _, _ int64, _ int) ([]deliveryports.RangeItem, *problem.Problem) {
	items := s.bySubject[subject.String()]
	return append([]deliveryports.RangeItem(nil), items...), nil
}
