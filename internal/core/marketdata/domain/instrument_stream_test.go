package domain_test

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

func newStream(t *testing.T) *domain.InstrumentStream {
	t.Helper()
	window, p := domain.NewDedupWindow(1024)
	if p != nil {
		t.Fatalf("NewDedupWindow: %s", p)
	}
	s, p := domain.NewInstrumentStream("binance", "BTC/USDT", window)
	if p != nil {
		t.Fatalf("NewInstrumentStream: %s", p)
	}
	return s
}

func buildTrade(t *testing.T, s *domain.InstrumentStream, seq int64, tsIngest int64) {
	t.Helper()
	_, p := s.BuildEnvelope(
		domain.EventType("marketdata.trade"),
		domain.SchemaVersion(1),
		domain.Timestamp(tsIngest-5),
		domain.Timestamp(tsIngest),
		domain.Sequence(seq),
		"application/json",
		[]byte(`{"trade_id":"t1"}`),
		"",
	)
	if p != nil {
		t.Fatalf("BuildEnvelope seq=%d: %s", seq, p)
	}
}

func TestInstrumentStream_normalize(t *testing.T) {
	window, p := domain.NewDedupWindow(128)
	if p != nil {
		t.Fatalf("NewDedupWindow: %s", p)
	}
	s, p := domain.NewInstrumentStream("  binance  ", "BTC/USDT", window)
	if p != nil {
		t.Fatalf("unexpected problem: %s", p)
	}
	id := s.ID()
	if id.Venue.String() != "BINANCE" {
		t.Errorf("Venue = %q; want BINANCE", id.Venue)
	}
	if id.Instrument.String() != "BTCUSDT" {
		t.Errorf("Instrument = %q; want BTCUSDT", id.Instrument)
	}
	if id.MarketType.String() != domain.MarketTypeSpot.String() {
		t.Errorf("MarketType = %q; want %q", id.MarketType, domain.MarketTypeSpot.String())
	}
}

func TestInstrumentStream_withMarketType(t *testing.T) {
	window, p := domain.NewDedupWindow(128)
	if p != nil {
		t.Fatalf("NewDedupWindow: %s", p)
	}
	s, p := domain.NewInstrumentStreamWithMarketType("binance", "BTC/USDT", "USD_M_FUTURES", window)
	if p != nil {
		t.Fatalf("unexpected problem: %s", p)
	}
	id := s.ID()
	if id.MarketType.String() != domain.MarketTypeUSDMFutures.String() {
		t.Errorf("MarketType = %q; want %q", id.MarketType, domain.MarketTypeUSDMFutures.String())
	}
	if got := id.SequencerInstrumentKey(); got != "BTCUSDT:USD_M_FUTURES" {
		t.Errorf("SequencerInstrumentKey = %q; want BTCUSDT:USD_M_FUTURES", got)
	}
}

func TestInstrumentStream_seqMonotonic(t *testing.T) {
	s := newStream(t)
	buildTrade(t, s, 1, 1710000001000)
	buildTrade(t, s, 2, 1710000002000)

	// seq 2 again → OUT_OF_ORDER
	_, p := s.BuildEnvelope(
		domain.EventType("marketdata.trade"),
		domain.SchemaVersion(1),
		domain.Timestamp(1710000002000),
		domain.Timestamp(1710000003000),
		domain.Sequence(2),
		"application/json",
		[]byte(`{"trade_id":"t2"}`),
		"",
	)
	if p == nil {
		t.Fatal("expected OUT_OF_ORDER problem, got nil")
	}
	if p.Code != problem.OutOfOrder {
		t.Errorf("code = %s; want OUT_OF_ORDER", p.Code)
	}
}

func TestInstrumentStream_envelopeValid(t *testing.T) {
	s := newStream(t)
	env, p := s.BuildEnvelope(
		domain.EventType("marketdata.trade"),
		domain.SchemaVersion(1),
		domain.Timestamp(1710000000000),
		domain.Timestamp(1710000005000),
		domain.Sequence(1),
		"application/json",
		[]byte(`{"trade_id":"abc"}`),
		"",
	)
	if p != nil {
		t.Fatalf("BuildEnvelope: %s", p)
	}
	if vp := env.Validate(); vp != nil {
		t.Errorf("envelope.Validate() failed: %s", vp)
	}
	if env.Type != "marketdata.trade" {
		t.Errorf("Type = %q; want marketdata.trade", env.Type)
	}
	if env.Venue != "BINANCE" {
		t.Errorf("Venue = %q; want BINANCE", env.Venue)
	}
}

func TestInstrumentStream_dedupCacheEviction(t *testing.T) {
	// Verify that the stream doesn't grow unbounded — just ensure we can
	// process more than dedupCacheMax events without panic.
	s := newStream(t)
	for i := int64(1); i <= 1100; i++ {
		_, p := s.BuildEnvelope(
			domain.EventType("marketdata.trade"),
			domain.SchemaVersion(1),
			domain.Timestamp(i*1000),
			domain.Timestamp(i*1000+5),
			domain.Sequence(i),
			"application/json",
			[]byte(`{"trade_id":"x"}`),
			"",
		)
		if p != nil {
			t.Fatalf("seq %d: %s", i, p)
		}
	}
}

func TestNewInstrumentStream_emptyVenue(t *testing.T) {
	window, p := domain.NewDedupWindow(16)
	if p != nil {
		t.Fatalf("NewDedupWindow: %s", p)
	}
	_, p = domain.NewInstrumentStream("", "BTC-PERP", window)
	if p == nil {
		t.Fatal("expected problem for empty venue")
	}
	if p.Code != problem.ValidationFailed {
		t.Errorf("code = %s; want VALIDATION_FAILED", p.Code)
	}
}

func TestValueObjects(t *testing.T) {
	t.Run("SchemaVersion rejects 0", func(t *testing.T) {
		_, p := domain.NewSchemaVersion(0)
		if p == nil {
			t.Error("expected problem")
		}
	})
	t.Run("SchemaVersion accepts 1", func(t *testing.T) {
		v, p := domain.NewSchemaVersion(1)
		if p != nil || int(v) != 1 {
			t.Errorf("unexpected: v=%d p=%v", v, p)
		}
	})
	t.Run("Sequence rejects negative", func(t *testing.T) {
		_, p := domain.NewSequence(-1)
		if p == nil {
			t.Error("expected problem")
		}
	})
	t.Run("Timestamp rejects zero", func(t *testing.T) {
		_, p := domain.NewTimestamp(0)
		if p == nil {
			t.Error("expected problem")
		}
	})
	t.Run("DedupWindow rejects zero", func(t *testing.T) {
		_, p := domain.NewDedupWindow(0)
		if p == nil {
			t.Error("expected problem")
		}
	})
	t.Run("DedupWindow accepts positive", func(t *testing.T) {
		v, p := domain.NewDedupWindow(64)
		if p != nil || v.Size() != 64 {
			t.Errorf("unexpected: v=%d p=%v", v, p)
		}
	})
}

func TestInstrumentStream_healthState(t *testing.T) {
	s := newStream(t)
	healthy := s.Health()
	if !healthy.IsHealthy || healthy.State != domain.StreamHealthy {
		t.Fatalf("expected healthy stream, got %+v", healthy)
	}

	_, _ = s.BuildEnvelope(
		domain.EventType("marketdata.trade"),
		domain.SchemaVersion(1),
		domain.Timestamp(1710000001000),
		domain.Timestamp(1710000001005),
		domain.Sequence(1),
		"application/json",
		[]byte(`{"trade_id":"t2"}`),
		"",
	)
	_, _ = s.BuildEnvelope(
		domain.EventType("marketdata.trade"),
		domain.SchemaVersion(1),
		domain.Timestamp(1710000002000),
		domain.Timestamp(1710000003000),
		domain.Sequence(1),
		"application/json",
		[]byte(`{"trade_id":"t2"}`),
		"",
	)
	degraded := s.Health()
	if degraded.IsHealthy || degraded.State != domain.StreamNeedsAttention {
		t.Fatalf("expected degraded stream after out-of-order, got %+v", degraded)
	}
}

func TestInstrumentStream_sourceIdempotencyKeyDedupsAcrossSeq(t *testing.T) {
	s := newStream(t)
	_, p := s.BuildEnvelope(
		domain.EventType("marketdata.trade"),
		domain.SchemaVersion(1),
		domain.Timestamp(1710000001000),
		domain.Timestamp(1710000001005),
		domain.Sequence(1),
		"application/json",
		[]byte(`{"trade_id":"42"}`),
		"venue=BINANCE|instrument=BTCUSDT|trade_id=42",
	)
	if p != nil {
		t.Fatalf("first BuildEnvelope failed: %v", p)
	}

	_, p = s.BuildEnvelope(
		domain.EventType("marketdata.trade"),
		domain.SchemaVersion(1),
		domain.Timestamp(1710000002000),
		domain.Timestamp(1710000002005),
		domain.Sequence(2),
		"application/json",
		[]byte(`{"trade_id":"42"}`),
		"venue=BINANCE|instrument=BTCUSDT|trade_id=42",
	)
	if p == nil {
		t.Fatal("expected duplicate problem")
	}
	if p.Code != problem.Duplicate {
		t.Fatalf("problem code = %s, want %s", p.Code, problem.Duplicate)
	}
}
