package timescale_test

import (
	"context"
	"testing"

	"github.com/market-raccoon/internal/adapters/storage/timescale"
	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/shared/envelope"
)

func TestDeliveryRangeStore_GetRange(t *testing.T) {
	store := timescale.NewDeliveryRangeStore(10)
	store.StoreEnvelope(envelope.Envelope{
		Type:       "aggregation.snapshot",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        10,
		TsIngest:   100,
		Payload:    []byte("a"),
	})
	store.StoreEnvelope(envelope.Envelope{
		Type:       "aggregation.snapshot",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Seq:        11,
		TsIngest:   101,
		Payload:    []byte("b"),
	})

	sub, p := domain.ParseSubject("aggregation.snapshot/binance/BTCUSDT/raw")
	if p != nil {
		t.Fatalf("ParseSubject: %v", p)
	}
	items, pp := store.GetRange(context.Background(), sub, 0, 0, 10)
	if pp != nil {
		t.Fatalf("GetRange: %v", pp)
	}
	if got := len(items); got != 2 {
		t.Fatalf("items len=%d want=2", got)
	}
	if got, want := items[0].Seq, int64(10); got != want {
		t.Fatalf("first seq=%d want=%d", got, want)
	}
}

func TestPgRangeStore_NilPool_GracefulFallback(t *testing.T) {
	store := timescale.NewPgRangeStore(nil, 10)
	sub, p := domain.ParseSubject("aggregation.snapshot/binance/BTCUSDT/raw")
	if p != nil {
		t.Fatalf("ParseSubject: %v", p)
	}
	items, pp := store.GetRange(context.Background(), sub, 0, 0, 10)
	if pp != nil {
		t.Fatalf("GetRange: %v", pp)
	}
	if len(items) != 0 {
		t.Fatalf("items len=%d want=0", len(items))
	}
	store.StoreEnvelope(envelope.Envelope{
		Type:       "aggregation.snapshot",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Seq:        1,
		TsIngest:   1,
		Payload:    []byte(`{"ok":true}`),
	})
}

func TestDeliveryRangeStore_MetaTimeframe(t *testing.T) {
	store := timescale.NewDeliveryRangeStore(10)

	// Envelope with Meta["timeframe"] = "5m" (as heatmap/vpvr snapshots produce).
	store.StoreEnvelope(envelope.Envelope{
		Type:       "insights.heatmap_snapshot",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        1,
		TsIngest:   100,
		Meta:       map[string]string{"timeframe": "5m"},
		Payload:    []byte(`{"cells":[]}`),
	})

	// Envelope WITHOUT Meta["timeframe"] falls back to "raw".
	store.StoreEnvelope(envelope.Envelope{
		Type:       "marketdata.trade",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        2,
		TsIngest:   101,
		Payload:    []byte(`{"price":100}`),
	})

	// Query with timeframe "5m" — must find the heatmap envelope.
	sub5m, p := domain.ParseSubject("insights.heatmap_snapshot/binance/BTCUSDT/5m")
	if p != nil {
		t.Fatalf("ParseSubject 5m: %v", p)
	}
	items, pp := store.GetRange(context.Background(), sub5m, 0, 0, 10)
	if pp != nil {
		t.Fatalf("GetRange 5m: %v", pp)
	}
	if got := len(items); got != 1 {
		t.Fatalf("5m items len=%d want=1", got)
	}
	if got := items[0].Seq; got != 1 {
		t.Fatalf("5m first seq=%d want=1", got)
	}

	// Query with timeframe "raw" — must find the trade envelope.
	subRaw, p := domain.ParseSubject("marketdata.trade/binance/BTCUSDT/raw")
	if p != nil {
		t.Fatalf("ParseSubject raw: %v", p)
	}
	items, pp = store.GetRange(context.Background(), subRaw, 0, 0, 10)
	if pp != nil {
		t.Fatalf("GetRange raw: %v", pp)
	}
	if got := len(items); got != 1 {
		t.Fatalf("raw items len=%d want=1", got)
	}
	if got := items[0].Seq; got != 2 {
		t.Fatalf("raw first seq=%d want=2", got)
	}
}
