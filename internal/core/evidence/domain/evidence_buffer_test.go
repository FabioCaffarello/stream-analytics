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
	if got := items[0].Explanation; got != "e-2" {
		t.Fatalf("oldest explanation=%q want=e-2", got)
	}
	if got := items[len(items)-1].Explanation; got != "e-1001" {
		t.Fatalf("newest explanation=%q want=e-1001", got)
	}
}

func makeEvidenceEvent(typ domain.EvidenceType, seq int64, explanation string) domain.EvidenceEvent {
	return domain.EvidenceEvent{
		Type:        typ,
		TsServer:    seq,
		Venue:       "binance",
		Symbol:      "BTCUSDT",
		StreamID:    "binance/BTCUSDT/book_delta",
		Seq:         seq,
		Severity:    domain.SeverityMedium,
		Confidence:  0.8,
		Features:    []domain.EvidenceFeature{{Key: "x", Value: 1}},
		Explanation: explanation,
		RuleVersion: domain.RuleVersionV0,
		InputWatermark: domain.InputWatermark{
			SeqStart: seq,
			SeqEnd:   seq,
		},
	}
}
