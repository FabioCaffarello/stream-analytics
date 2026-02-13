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
