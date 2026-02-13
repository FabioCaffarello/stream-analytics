package domain

import (
	"fmt"
	"testing"
)

// TestSeenOrdCompaction verifies that the seenOrd slice backing array does not
// grow unbounded after repeated evictions.  This is a white-box test because
// seenOrd is unexported.
func TestSeenOrdCompaction(t *testing.T) {
	const windowSize = 64
	window, p := NewDedupWindow(windowSize)
	if p != nil {
		t.Fatalf("NewDedupWindow: %s", p)
	}
	s, p := NewInstrumentStream("binance", "BTCUSDT", window)
	if p != nil {
		t.Fatalf("NewInstrumentStream: %s", p)
	}

	// Push many more entries than window size to trigger repeated evictions.
	const totalEntries = windowSize * 20
	for i := int64(1); i <= totalEntries; i++ {
		_, bp := s.BuildEnvelope(
			EventType("marketdata.trade"),
			SchemaVersion(1),
			Timestamp(i*1000),
			Timestamp(i*1000+5),
			Sequence(i),
			"application/json",
			[]byte(`{"trade_id":"x"}`),
			fmt.Sprintf("key-%d", i),
		)
		if bp != nil {
			t.Fatalf("seq %d: %s", i, bp)
		}
	}

	// After all evictions, seenOrd length must equal windowSize.
	if got := len(s.seenOrd); got != windowSize {
		t.Fatalf("len(seenOrd) = %d; want %d", got, windowSize)
	}

	// The backing array capacity must be bounded — no more than 2x window.
	if got := cap(s.seenOrd); got > 2*windowSize {
		t.Fatalf("cap(seenOrd) = %d; want <= %d (2x window)", got, 2*windowSize)
	}
}
