package app_test

import (
	"context"
	"testing"

	"github.com/market-raccoon/internal/core/delivery/app"
	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/core/delivery/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

type fakeRangeStore struct {
	items     []ports.RangeItem
	bySubject map[string][]ports.RangeItem
	p         *problem.Problem
	calls     []domain.Subject
}

func (f *fakeRangeStore) GetRange(_ context.Context, subject domain.Subject, _, _ int64, _ int) ([]ports.RangeItem, *problem.Problem) {
	f.calls = append(f.calls, subject)
	if f.p != nil {
		return nil, f.p
	}
	if f.bySubject != nil {
		return f.bySubject[subject.String()], nil
	}
	return f.items, nil
}

func TestSessionService_ParseSubject(t *testing.T) {
	svc := app.NewSessionService(nil)
	res := svc.ParseSubject("marketdata.trade/binance/BTC-USDT/raw")
	if res.IsFail() {
		t.Fatalf("ParseSubject failed: %v", res.Problem())
	}
}

func TestSessionService_GetRange(t *testing.T) {
	svc := app.NewSessionService(&fakeRangeStore{items: []ports.RangeItem{{Seq: 1}}})
	res := svc.GetRange(context.Background(), app.GetRangeRequest{SubjectRaw: "marketdata.trade/binance/BTC-USDT/raw", Limit: 10})
	if res.IsFail() {
		t.Fatalf("GetRange failed: %v", res.Problem())
	}
	if got := len(res.Value()); got != 1 {
		t.Fatalf("range len = %d, want 1", got)
	}
}

func TestSessionService_GetRange_invalidSubject(t *testing.T) {
	svc := app.NewSessionService(nil)
	res := svc.GetRange(context.Background(), app.GetRangeRequest{SubjectRaw: "x"})
	if !res.IsFail() {
		t.Fatal("expected failure")
	}
}

func TestSessionService_GetRange_storeUnavailable(t *testing.T) {
	svc := app.NewSessionService(nil)
	res := svc.GetRange(context.Background(), app.GetRangeRequest{SubjectRaw: "marketdata.trade/binance/BTC-USDT/raw"})
	if !res.IsFail() {
		t.Fatal("expected failure")
	}
	if got, want := res.Problem().Code, problem.NotFound; got != want {
		t.Fatalf("problem code = %s, want %s", got, want)
	}
}

func TestSessionService_GetRange_marketTypeAliasFallback(t *testing.T) {
	store := &fakeRangeStore{
		bySubject: map[string][]ports.RangeItem{
			"aggregation.candle/binance/BTCUSDT/1m": {{Seq: 42}},
		},
	}
	svc := app.NewSessionService(store)
	res := svc.GetRange(context.Background(), app.GetRangeRequest{
		SubjectRaw: "aggregation.candle/binance/BTCUSDT:SPOT/1m",
		Limit:      64,
	})
	if res.IsFail() {
		t.Fatalf("GetRange failed: %v", res.Problem())
	}
	items := res.Value()
	if got := len(items); got != 1 {
		t.Fatalf("range len = %d, want 1", got)
	}
	if got := items[0].Seq; got != 42 {
		t.Fatalf("item seq = %d, want 42", got)
	}
	if got := len(store.calls); got != 2 {
		t.Fatalf("store calls = %d, want 2", got)
	}
	if got := store.calls[0].String(); got != "aggregation.candle/binance/BTCUSDT:SPOT/1m" {
		t.Fatalf("call[0] subject = %q, want alias subject", got)
	}
	if got := store.calls[1].String(); got != "aggregation.candle/binance/BTCUSDT/1m" {
		t.Fatalf("call[1] subject = %q, want canonical fallback subject", got)
	}
}
