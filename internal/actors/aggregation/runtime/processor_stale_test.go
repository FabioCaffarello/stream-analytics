package aggruntime

import (
	"math"
	"testing"
	"time"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestStaleDuration_UpdateResetsToZero(t *testing.T) {
	now := time.Unix(100, 0)
	key := aggdomain.BookID{Venue: "binance", Instrument: "BTCUSDT"}
	p := &ProcessorSubsystemActor{
		activeOrderBooks:      map[aggdomain.BookID]struct{}{key: {}},
		lastOrderBookUpdateAt: map[aggdomain.BookID]time.Time{key: now},
	}
	p.emitOrderBookStaleDurations(now)

	got := testutil.ToFloat64(metrics.MROrderBookStaleDurationSeconds.WithLabelValues("binance", metrics.InstrumentBucket("BTCUSDT")))
	if got != 0 {
		t.Fatalf("stale duration got=%f want=0", got)
	}
}

func TestStaleDuration_IncreasesWithoutUpdate(t *testing.T) {
	now := time.Unix(100, 0)
	key := aggdomain.BookID{Venue: "binance", Instrument: "BTCUSDT"}
	p := &ProcessorSubsystemActor{
		activeOrderBooks:      map[aggdomain.BookID]struct{}{key: {}},
		lastOrderBookUpdateAt: map[aggdomain.BookID]time.Time{key: now.Add(-5 * time.Second)},
	}
	p.emitOrderBookStaleDurations(now)

	got := testutil.ToFloat64(metrics.MROrderBookStaleDurationSeconds.WithLabelValues("binance", metrics.InstrumentBucket("BTCUSDT")))
	if math.Abs(got-5) > 0.01 {
		t.Fatalf("stale duration got=%f want~=5", got)
	}
}
