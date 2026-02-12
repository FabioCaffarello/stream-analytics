package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/market-raccoon/internal/core/marketdata/app"
	"github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

// --- fakes ---

type fakeSequencer struct{ n int64 }

func (f *fakeSequencer) Next(_, _ string) (int64, *problem.Problem) {
	f.n++
	return f.n, nil
}

type fakePublisher struct{ published []envelope.Envelope }

func (f *fakePublisher) Publish(_ context.Context, env envelope.Envelope) *problem.Problem {
	f.published = append(f.published, env)
	return nil
}

type failPublisher struct{}

func (failPublisher) Publish(_ context.Context, _ envelope.Envelope) *problem.Problem {
	return problem.New(problem.Internal, "bus unavailable")
}

// --- helpers ---

func newUC(clk *clock.FakeClock) (*app.IngestMarketData, *fakeSequencer, *fakePublisher) {
	seq := &fakeSequencer{}
	pub := &fakePublisher{}
	uc := app.NewIngestMarketData(clk, seq, pub)
	return uc, seq, pub
}

func validReq() app.IngestRequest {
	return app.IngestRequest{
		Venue:      "binance",
		Instrument: "BTC/USDT",
		EventType:  "marketdata.trade",
		Version:    1,
		TsExchange: time.Now().UnixMilli(),
		Payload:    domain.TradeTickV1{Price: 50_000, Size: 1.0, Side: "buy", TradeID: "t1"},
	}
}

// --- tests ---

func TestIngest_success(t *testing.T) {
	clk := clock.NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	uc, _, pub := newUC(clk)

	r := uc.Execute(context.Background(), validReq())
	if r.IsFail() {
		t.Fatalf("unexpected failure: %s", r.Problem())
	}

	resp := r.Value()
	if resp.Seq != 1 {
		t.Errorf("Seq = %d; want 1", resp.Seq)
	}
	if resp.Published.Envelope.IdempotencyKey == "" {
		t.Error("IdempotencyKey must not be empty")
	}
	if resp.Published.Topic == "" {
		t.Error("TopicKey must not be empty")
	}
	if len(pub.published) != 1 {
		t.Errorf("published %d events; want 1", len(pub.published))
	}
}

func TestIngest_seqMonotonic(t *testing.T) {
	clk := clock.NewFakeClock(time.Now())
	uc, _, pub := newUC(clk)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		clk.Advance(time.Millisecond)
		r := uc.Execute(ctx, validReq())
		if r.IsFail() {
			t.Fatalf("event %d failed: %s", i, r.Problem())
		}
	}
	if len(pub.published) != 5 {
		t.Errorf("expected 5 published; got %d", len(pub.published))
	}
	for i := 1; i < len(pub.published); i++ {
		if pub.published[i].Seq <= pub.published[i-1].Seq {
			t.Errorf("seq not monotonic at index %d", i)
		}
	}
}

func TestIngest_missingVenue(t *testing.T) {
	clk := clock.NewFakeClock(time.Now())
	uc, _, _ := newUC(clk)
	req := validReq()
	req.Venue = ""
	r := uc.Execute(context.Background(), req)
	if r.IsOk() {
		t.Fatal("expected failure for empty venue")
	}
	if r.Problem().Code != problem.ValidationFailed {
		t.Errorf("code = %s; want VALIDATION_FAILED", r.Problem().Code)
	}
}

func TestIngest_nilPayload(t *testing.T) {
	clk := clock.NewFakeClock(time.Now())
	uc, _, _ := newUC(clk)
	req := validReq()
	req.Payload = nil
	r := uc.Execute(context.Background(), req)
	if r.IsOk() {
		t.Fatal("expected failure for nil payload")
	}
}

func TestIngest_publisherFailure(t *testing.T) {
	clk := clock.NewFakeClock(time.Now())
	seq := &fakeSequencer{}
	uc := app.NewIngestMarketData(clk, seq, failPublisher{})
	r := uc.Execute(context.Background(), validReq())
	if r.IsOk() {
		t.Fatal("expected failure when publisher fails")
	}
	if r.Problem().Code != problem.Internal {
		t.Errorf("code = %s; want INTERNAL", r.Problem().Code)
	}
}

func TestIngest_topicKeyFormat(t *testing.T) {
	clk := clock.NewFakeClock(time.Now())
	uc, _, _ := newUC(clk)
	r := uc.Execute(context.Background(), validReq())
	if r.IsFail() {
		t.Fatal(r.Problem())
	}
	// topic: <type>.<venue>.<instrument>
	want := "marketdata.trade.binance.btcusdt"
	if got := r.Value().Published.Topic; got != want {
		t.Errorf("TopicKey = %q; want %q", got, want)
	}
}
