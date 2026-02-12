package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/market-raccoon/internal/core/marketdata/app"
	"github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

// --- fakes ---

type fakeSequencer struct {
	n     int64
	calls []string
}

func (f *fakeSequencer) Next(venue, instrument string) (int64, *problem.Problem) {
	f.n++
	f.calls = append(f.calls, venue+"|"+instrument)
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
	if resp.Published.Envelope.ContentType != envelope.ContentTypeJSON {
		t.Errorf("ContentType = %q; want %q", resp.Published.Envelope.ContentType, envelope.ContentTypeJSON)
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

func TestIngest_metadataPropagatesToEnvelope(t *testing.T) {
	clk := clock.NewFakeClock(time.Now())
	uc, _, _ := newUC(clk)
	req := validReq()
	req.Metadata = map[string]string{
		"exchange":   "binance",
		"ws_stream":  "btcusdt@aggTrade",
		"instrument": "BTC-USDT",
	}

	r := uc.Execute(context.Background(), req)
	if r.IsFail() {
		t.Fatal(r.Problem())
	}

	env := r.Value().Published.Envelope
	if env.Meta["exchange"] != "binance" {
		t.Fatalf("meta exchange = %q, want binance", env.Meta["exchange"])
	}
	if env.Meta["ws_stream"] != "btcusdt@aggTrade" {
		t.Fatalf("meta ws_stream = %q, want btcusdt@aggTrade", env.Meta["ws_stream"])
	}
}

func TestIngest_publishContentTypeJSON(t *testing.T) {
	clk := clock.NewFakeClock(time.Now())
	seq := &fakeSequencer{}
	pub := &fakePublisher{}
	uc := app.NewIngestMarketDataWithConfig(clk, seq, pub, app.IngestConfig{
		DedupWindowSize:    64,
		MaxStreams:         16,
		StreamTTL:          time.Hour,
		PublishContentType: envelope.ContentTypeJSON,
	})

	r := uc.Execute(context.Background(), validReq())
	if r.IsFail() {
		t.Fatalf("ingest failed: %v", r.Problem())
	}
	env := r.Value().Published.Envelope
	if env.ContentType != envelope.ContentTypeJSON {
		t.Fatalf("content_type = %q, want %q", env.ContentType, envelope.ContentTypeJSON)
	}
	decodedAny, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
	if p != nil {
		t.Fatalf("DecodePayload(JSON): %v", p)
	}
	if _, ok := decodedAny.(domain.TradeTickV1); !ok {
		t.Fatalf("decoded payload type = %T, want %T", decodedAny, domain.TradeTickV1{})
	}
}

func TestIngest_publishContentTypeProtobuf(t *testing.T) {
	clk := clock.NewFakeClock(time.Now())
	seq := &fakeSequencer{}
	pub := &fakePublisher{}
	uc := app.NewIngestMarketDataWithConfig(clk, seq, pub, app.IngestConfig{
		DedupWindowSize:    64,
		MaxStreams:         16,
		StreamTTL:          time.Hour,
		PublishContentType: envelope.ContentTypeProto,
	})

	r := uc.Execute(context.Background(), validReq())
	if r.IsFail() {
		t.Fatalf("ingest failed: %v", r.Problem())
	}
	env := r.Value().Published.Envelope
	if env.ContentType != envelope.ContentTypeProto {
		t.Fatalf("content_type = %q, want %q", env.ContentType, envelope.ContentTypeProto)
	}
	decodedAny, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
	if p != nil {
		t.Fatalf("DecodePayload(PROTO): %v", p)
	}
	if _, ok := decodedAny.(domain.TradeTickV1); !ok {
		t.Fatalf("decoded payload type = %T, want %T", decodedAny, domain.TradeTickV1{})
	}
}

func TestIngest_boundedStreamsEvictsOldest(t *testing.T) {
	clk := clock.NewFakeClock(time.Now())
	seq := &fakeSequencer{}
	pub := &fakePublisher{}
	uc := app.NewIngestMarketDataWithConfig(clk, seq, pub, app.IngestConfig{
		DedupWindowSize: 64,
		MaxStreams:      1,
		StreamTTL:       time.Hour,
	})

	req := validReq()
	req.Instrument = "BTC/USDT"
	if r := uc.Execute(context.Background(), req); r.IsFail() {
		t.Fatalf("first ingest failed: %v", r.Problem())
	}
	req.Instrument = "ETH/USDT"
	clk.Advance(time.Millisecond)
	if r := uc.Execute(context.Background(), req); r.IsFail() {
		t.Fatalf("second ingest failed: %v", r.Problem())
	}

	if got := uc.ActiveStreams(); got != 1 {
		t.Fatalf("active streams=%d want=1", got)
	}
}

func TestIngest_ThrottledSweepDoesNotRunEveryRequest(t *testing.T) {
	clk := clock.NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	seq := &fakeSequencer{}
	pub := &fakePublisher{}
	uc := app.NewIngestMarketDataWithConfig(clk, seq, pub, app.IngestConfig{
		DedupWindowSize: 64,
		MaxStreams:      16,
		StreamTTL:       10 * time.Millisecond,
	})

	req := validReq()
	req.Instrument = "BTC/USDT"
	if r := uc.Execute(context.Background(), req); r.IsFail() {
		t.Fatalf("first ingest failed: %v", r.Problem())
	}
	req.Instrument = "ETH/USDT"
	if r := uc.Execute(context.Background(), req); r.IsFail() {
		t.Fatalf("second ingest failed: %v", r.Problem())
	}

	clk.Advance(20 * time.Millisecond)
	req.Instrument = "SOL/USDT"
	if r := uc.Execute(context.Background(), req); r.IsFail() {
		t.Fatalf("third ingest failed: %v", r.Problem())
	}

	if got := uc.ActiveStreams(); got < 2 {
		t.Fatalf("expected no full sweep on each request, active streams=%d", got)
	}
}

func TestIngest_StreamIdentityIncludesMarketType(t *testing.T) {
	clk := clock.NewFakeClock(time.Now())
	seq := &fakeSequencer{}
	pub := &fakePublisher{}
	uc := app.NewIngestMarketData(clk, seq, pub)

	reqSpot := validReq()
	reqSpot.MarketType = "SPOT"
	if r := uc.Execute(context.Background(), reqSpot); r.IsFail() {
		t.Fatalf("spot ingest failed: %v", r.Problem())
	}

	reqFutures := validReq()
	reqFutures.MarketType = "USD_M_FUTURES"
	clk.Advance(time.Millisecond)
	if r := uc.Execute(context.Background(), reqFutures); r.IsFail() {
		t.Fatalf("futures ingest failed: %v", r.Problem())
	}

	if got := uc.ActiveStreams(); got != 2 {
		t.Fatalf("active streams=%d want=2", got)
	}
	if len(seq.calls) != 2 {
		t.Fatalf("sequencer calls=%d want=2", len(seq.calls))
	}
	if seq.calls[0] != "BINANCE|BTCUSDT:SPOT" {
		t.Fatalf("sequencer call[0]=%q want BINANCE|BTCUSDT:SPOT", seq.calls[0])
	}
	if seq.calls[1] != "BINANCE|BTCUSDT:USD_M_FUTURES" {
		t.Fatalf("sequencer call[1]=%q want BINANCE|BTCUSDT:USD_M_FUTURES", seq.calls[1])
	}
}
