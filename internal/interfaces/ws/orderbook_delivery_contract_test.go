package wsserver

import (
	"testing"

	"github.com/market-raccoon/internal/core/delivery/domain"
)

func TestOrderbookDeliverySlowClientPolicy(t *testing.T) {
	if got := domain.ShouldDropOnBackpressure(domain.BackpressureDropNewest, 0, 1); got {
		t.Fatal("queue below capacity should not drop")
	}
	if got := domain.ShouldDropOnBackpressure(domain.BackpressureDropNewest, 1, 1); !got {
		t.Fatal("queue at capacity should drop newest")
	}
}

func TestOrderbookDeliveryReplayRangeDeterministic(t *testing.T) {
	// Delegates to the WS contract e2e path for deterministic getrange behavior.
	TestWSRangeDeterminismReplay(t)
}
