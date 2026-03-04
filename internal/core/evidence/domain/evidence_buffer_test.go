package domain_test

import (
	"fmt"
	"testing"

	"github.com/market-raccoon/internal/core/evidence/domain"
)

func TestEvidenceBuffer_PushOverwritesOldestAtCap(t *testing.T) {
	policy, p := domain.NewEvidenceBufferPolicy(1000)
	if p != nil {
		t.Fatalf("policy validation failed: %v", p)
	}
	buf := domain.NewEvidenceBuffer(policy)

	for i := 1; i <= 1001; i++ {
		overwritten, p := buf.Push(makeEvidenceEvent(domain.Absorption, int64(i), fmt.Sprintf("e-%d", i)))
		if p != nil {
			t.Fatalf("push %d failed: %v", i, p)
		}
		if i <= 1000 && overwritten {
			t.Fatalf("push %d unexpectedly overwrote", i)
		}
		if i == 1001 && !overwritten {
			t.Fatal("push 1001 should overwrite oldest entry")
		}
	}

	if got := buf.Size(domain.Absorption); got != 1000 {
		t.Fatalf("buffer size=%d want=1000", got)
	}

	items := buf.List(domain.Absorption)
	if got := len(items); got != 1000 {
		t.Fatalf("list length=%d want=1000", got)
	}
	if got := items[0].Reason; got != "e-2" {
		t.Fatalf("oldest reason=%q want=e-2", got)
	}
	if got := items[len(items)-1].Reason; got != "e-1001" {
		t.Fatalf("newest reason=%q want=e-1001", got)
	}
}

func makeEvidenceEvent(kind domain.EvidenceKind, seq int64, reason string) domain.EvidenceEvent {
	return domain.EvidenceEvent{
		Kind:        kind,
		TsServer:    seq,
		Venue:       "binance",
		Symbol:      "BTCUSDT",
		Severity:    domain.SeverityMedium,
		Confidence:  0.8,
		Features:    []string{"x"},
		FeatureVals: []float64{1},
		Reason:      reason,
		SeqTrigger:  seq,
	}
}
