package jetstream

import (
	"context"
	"strings"
	"testing"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

type mockPublisher struct {
	published []envelope.Envelope
	err       *problem.Problem
}

func (m *mockPublisher) Publish(_ context.Context, env envelope.Envelope) *problem.Problem {
	if m.err != nil {
		return m.err
	}
	m.published = append(m.published, env)
	return nil
}

func newTestArtifactPublisher(mock *mockPublisher) *ArtifactPublisher {
	return &ArtifactPublisher{
		pub:   mock,
		clock: func() int64 { return 1_710_000_000_000 },
	}
}

func bootstrapTestCodecRegistry(t *testing.T) {
	t.Helper()
	reg := codec.NewRegistry()
	if p := contracts.RegisterAggregationPayloadV1(reg); p != nil {
		t.Fatalf("RegisterAggregationPayloadV1: %v", p)
	}
	if p := codec.SetPayloadRegistry(reg); p != nil {
		t.Fatalf("SetPayloadRegistry: %v", p)
	}
}

func TestArtifactPublisher_PublishSnapshot(t *testing.T) {
	bootstrapTestCodecRegistry(t)
	mock := &mockPublisher{}
	ap := newTestArtifactPublisher(mock)

	snap := aggdomain.SnapshotProduced{
		BookID: aggdomain.BookID{Venue: "binance", Instrument: "BTC-USDT"},
		Seq:    42,
		Bids:   []aggdomain.Level{{Price: 65000, Quantity: 1.0}},
		Asks:   []aggdomain.Level{{Price: 65001, Quantity: 0.5}},
	}

	p := ap.PublishSnapshot(context.Background(), snap)
	if p != nil {
		t.Fatalf("PublishSnapshot: %v", p)
	}
	if len(mock.published) != 1 {
		t.Fatalf("expected 1 published, got %d", len(mock.published))
	}

	env := mock.published[0]
	if env.Type != "aggregation.snapshot" {
		t.Fatalf("type=%q want aggregation.snapshot", env.Type)
	}
	if env.Version != 1 {
		t.Fatalf("version=%d want 1", env.Version)
	}
	if env.Venue != "binance" {
		t.Fatalf("venue=%q want binance", env.Venue)
	}
	if env.Instrument != "BTC-USDT" {
		t.Fatalf("instrument=%q want BTC-USDT", env.Instrument)
	}
	if env.Seq != 42 {
		t.Fatalf("seq=%d want 42", env.Seq)
	}
	if env.ContentType != envelope.ContentTypeJSON {
		t.Fatalf("content_type=%q want %q", env.ContentType, envelope.ContentTypeJSON)
	}
	if strings.TrimSpace(env.IdempotencyKey) == "" {
		t.Fatal("idempotency_key must not be empty")
	}
	if len(env.Payload) == 0 {
		t.Fatal("payload must not be empty")
	}
}

func TestArtifactPublisher_PublishInconsistent(t *testing.T) {
	bootstrapTestCodecRegistry(t)
	mock := &mockPublisher{}
	ap := newTestArtifactPublisher(mock)

	evt := aggdomain.OrderBookInconsistentDetected{
		BookID: aggdomain.BookID{Venue: "bybit", Instrument: "ETH-USDT"},
		Seq:    10,
		Reason: "crossed_book",
	}

	p := ap.PublishInconsistent(context.Background(), evt)
	if p != nil {
		t.Fatalf("PublishInconsistent: %v", p)
	}
	if len(mock.published) != 1 {
		t.Fatalf("expected 1 published, got %d", len(mock.published))
	}

	env := mock.published[0]
	if env.Type != "aggregation.orderbook_inconsistency" {
		t.Fatalf("type=%q want aggregation.orderbook_inconsistency", env.Type)
	}
	if env.Venue != "bybit" {
		t.Fatalf("venue=%q want bybit", env.Venue)
	}
}

func TestArtifactPublisher_PublishCandleClosed(t *testing.T) {
	bootstrapTestCodecRegistry(t)
	mock := &mockPublisher{}
	ap := newTestArtifactPublisher(mock)

	evt := aggdomain.CandleClosed{
		Candle: aggdomain.CandleV1{
			Venue:         "binance",
			Instrument:    "BTC-USDT",
			Timeframe:     "1m",
			WindowStartTs: 1_710_000_000_000,
			WindowEndTs:   1_710_000_060_000,
			Open:          65000.0,
			High:          65100.0,
			Low:           64900.0,
			ClosePrice:    65050.0,
			Volume:        10.5,
			BuyVolume:     6.0,
			SellVolume:    4.5,
			TradeCount:    100,
			SeqFirst:      1,
			SeqLast:       100,
			IsClosed:      true,
		},
	}

	p := ap.PublishCandleClosed(context.Background(), evt)
	if p != nil {
		t.Fatalf("PublishCandleClosed: %v", p)
	}
	if len(mock.published) != 1 {
		t.Fatalf("expected 1 published, got %d", len(mock.published))
	}

	env := mock.published[0]
	if env.Type != "aggregation.candle" {
		t.Fatalf("type=%q want aggregation.candle", env.Type)
	}
	if env.Version != 1 {
		t.Fatalf("version=%d want 1", env.Version)
	}
	if env.Venue != "binance" {
		t.Fatalf("venue=%q want binance", env.Venue)
	}
	if env.Seq != 100 {
		t.Fatalf("seq=%d want 100", env.Seq)
	}
	if env.ContentType != envelope.ContentTypeJSON {
		t.Fatalf("content_type=%q want %q", env.ContentType, envelope.ContentTypeJSON)
	}
	if env.Meta == nil || env.Meta["timeframe"] != "1m" {
		t.Fatalf("meta[timeframe]=%q want %q", env.Meta["timeframe"], "1m")
	}
}

func TestArtifactPublisher_PublishCandleClosed_PropagatesMarketTypeMeta(t *testing.T) {
	bootstrapTestCodecRegistry(t)
	mock := &mockPublisher{}
	ap := newTestArtifactPublisher(mock)

	evt := aggdomain.CandleClosed{
		Candle: aggdomain.CandleV1{
			Venue:         "binance",
			Instrument:    "SOLUSDT:SPOT",
			Timeframe:     "1m",
			WindowStartTs: 1_710_000_000_000,
			WindowEndTs:   1_710_000_060_000,
			Open:          100,
			High:          101,
			Low:           99,
			ClosePrice:    100.5,
			Volume:        1,
			BuyVolume:     0.5,
			SellVolume:    0.5,
			TradeCount:    1,
			SeqFirst:      10,
			SeqLast:       20,
			IsClosed:      true,
		},
	}

	if p := ap.PublishCandleClosed(context.Background(), evt); p != nil {
		t.Fatalf("PublishCandleClosed: %v", p)
	}
	if len(mock.published) != 1 {
		t.Fatalf("expected 1 published, got %d", len(mock.published))
	}
	env := mock.published[0]
	if got, want := env.Instrument, "SOLUSDT"; got != want {
		t.Fatalf("instrument=%q want %q", got, want)
	}
	if env.Meta == nil || env.Meta["instrument_market_type"] != "SPOT" {
		t.Fatalf("meta[instrument_market_type]=%q want SPOT", env.Meta["instrument_market_type"])
	}
	if env.Meta["timeframe"] != "1m" {
		t.Fatalf("meta[timeframe]=%q want 1m", env.Meta["timeframe"])
	}
}

func TestArtifactPublisher_PublishStatsClosed(t *testing.T) {
	bootstrapTestCodecRegistry(t)
	mock := &mockPublisher{}
	ap := newTestArtifactPublisher(mock)

	evt := aggdomain.StatsWindowClosed{
		Stats: aggdomain.StatsWindowV1{
			Venue:          "binance",
			Instrument:     "BTC-USDT",
			Timeframe:      "5m",
			WindowStartTs:  1_710_000_000_000,
			WindowEndTs:    1_710_000_300_000,
			WindowMs:       300_000,
			TsIngestMs:     1_710_000_300_123,
			QualityFlags:   3,
			LiqBuyVolume:   5.0,
			LiqSellVolume:  3.0,
			LiqTotalVolume: 8.0,
			LiqCount:       12,
			SeqFirst:       1,
			SeqLast:        50,
			IsClosed:       true,
		},
	}

	p := ap.PublishStatsClosed(context.Background(), evt)
	if p != nil {
		t.Fatalf("PublishStatsClosed: %v", p)
	}
	if len(mock.published) != 1 {
		t.Fatalf("expected 1 published, got %d", len(mock.published))
	}

	env := mock.published[0]
	if env.Type != "aggregation.stats" {
		t.Fatalf("type=%q want aggregation.stats", env.Type)
	}
	if env.Venue != "binance" {
		t.Fatalf("venue=%q want binance", env.Venue)
	}
	if env.Seq != 50 {
		t.Fatalf("seq=%d want 50", env.Seq)
	}
}

func TestArtifactPublisher_PublishStatsClosed_PropagatesMarketTypeMeta(t *testing.T) {
	bootstrapTestCodecRegistry(t)
	mock := &mockPublisher{}
	ap := newTestArtifactPublisher(mock)

	evt := aggdomain.StatsWindowClosed{
		Stats: aggdomain.StatsWindowV1{
			Venue:         "binance",
			Instrument:    "SOLUSDT:USDMFUTURES",
			Timeframe:     "1m",
			WindowStartTs: 1000,
			WindowEndTs:   2000,
			SeqFirst:      1,
			SeqLast:       2,
			IsClosed:      true,
		},
	}

	if p := ap.PublishStatsClosed(context.Background(), evt); p != nil {
		t.Fatalf("PublishStatsClosed: %v", p)
	}
	if len(mock.published) != 1 {
		t.Fatalf("expected 1 published, got %d", len(mock.published))
	}
	env := mock.published[0]
	if got, want := env.Instrument, "SOLUSDT"; got != want {
		t.Fatalf("instrument=%q want %q", got, want)
	}
	if env.Meta == nil || env.Meta["instrument_market_type"] != "USDMFUTURES" {
		t.Fatalf("meta[instrument_market_type]=%q want USDMFUTURES", env.Meta["instrument_market_type"])
	}
}

func TestArtifactPublisher_PublisherError(t *testing.T) {
	bootstrapTestCodecRegistry(t)
	mock := &mockPublisher{err: problem.New(problem.Unavailable, "nats down")}
	ap := newTestArtifactPublisher(mock)

	snap := aggdomain.SnapshotProduced{
		BookID: aggdomain.BookID{Venue: "binance", Instrument: "BTC-USDT"},
		Seq:    1,
		Bids:   []aggdomain.Level{{Price: 65000, Quantity: 1.0}},
	}

	p := ap.PublishSnapshot(context.Background(), snap)
	if p == nil {
		t.Fatal("expected error from publisher")
	}
	if p.Code != problem.Unavailable {
		t.Fatalf("code=%q want %q", p.Code, problem.Unavailable)
	}
}

func TestDomainCandleToWireDTO_FieldMapping(t *testing.T) {
	evt := aggdomain.CandleClosed{
		Candle: aggdomain.CandleV1{
			Venue:         "binance",
			Instrument:    "ETH-USDT",
			Timeframe:     "15m",
			WindowStartTs: 100,
			WindowEndTs:   200,
			Open:          3000.0,
			High:          3100.0,
			Low:           2900.0,
			ClosePrice:    3050.0,
			Volume:        50.0,
			BuyVolume:     30.0,
			SellVolume:    20.0,
			TradeCount:    500,
			SeqFirst:      10,
			SeqLast:       510,
			IsClosed:      true,
		},
	}

	dto := domainCandleToWireDTO(evt)
	c := dto.Candle
	if c.Venue != "binance" || c.Instrument != "ETH-USDT" || c.Timeframe != "15m" {
		t.Fatalf("identity mismatch: %+v", c)
	}
	if c.Open != 3000.0 || c.High != 3100.0 || c.Low != 2900.0 || c.ClosePrice != 3050.0 {
		t.Fatalf("OHLC mismatch: %+v", c)
	}
	if c.Volume != 50.0 || c.BuyVolume != 30.0 || c.SellVolume != 20.0 {
		t.Fatalf("volume mismatch: %+v", c)
	}
	if c.TradeCount != 500 || c.SeqFirst != 10 || c.SeqLast != 510 || !c.IsClosed {
		t.Fatalf("meta mismatch: %+v", c)
	}
}

func TestDomainStatsToWireDTO_FieldMapping(t *testing.T) {
	evt := aggdomain.StatsWindowClosed{
		Stats: aggdomain.StatsWindowV1{
			Venue:           "bybit",
			Instrument:      "SOL-USDT",
			Timeframe:       "1h",
			WindowStartTs:   1000,
			WindowEndTs:     2000,
			WindowMs:        1000,
			TsIngestMs:      2123,
			QualityFlags:    9,
			LiqBuyVolume:    10.0,
			LiqSellVolume:   7.0,
			LiqTotalVolume:  17.0,
			LiqCount:        25,
			MarkPriceOpen:   150.0,
			MarkPriceHigh:   155.0,
			MarkPriceLow:    148.0,
			MarkPriceClose:  153.0,
			FundingRateAvg:  0.0001,
			FundingRateLast: 0.00012,
			SeqFirst:        1,
			SeqLast:         100,
			IsClosed:        true,
		},
	}

	dto := domainStatsToWireDTO(evt)
	s := dto.Stats
	if s.Venue != "bybit" || s.Instrument != "SOL-USDT" || s.Timeframe != "1h" {
		t.Fatalf("identity mismatch: %+v", s)
	}
	if s.LiqBuyVolume != 10.0 || s.LiqSellVolume != 7.0 || s.LiqTotalVolume != 17.0 || s.LiqCount != 25 {
		t.Fatalf("liquidation mismatch: %+v", s)
	}
	if s.MarkPriceOpen != 150.0 || s.MarkPriceHigh != 155.0 || s.FundingRateAvg != 0.0001 {
		t.Fatalf("markprice/funding mismatch: %+v", s)
	}
	if s.WindowMs != 1000 || s.TsIngestMs != 2123 || s.QualityFlags != 9 {
		t.Fatalf("window/quality mismatch: %+v", s)
	}
}
