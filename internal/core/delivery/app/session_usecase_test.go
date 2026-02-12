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
	items []ports.RangeItem
	p     *problem.Problem
}

func (f *fakeRangeStore) GetRange(_ context.Context, _ domain.Subject, _, _ int64, _ int) ([]ports.RangeItem, *problem.Problem) {
	return f.items, f.p
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
