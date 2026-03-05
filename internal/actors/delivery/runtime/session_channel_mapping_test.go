package deliveryruntime

import (
	"testing"

	"github.com/market-raccoon/internal/core/delivery/domain"
	deliveryv1 "github.com/market-raccoon/internal/shared/proto/gen/delivery/v1"
)

func TestChannelEnumFromStreamTypeEvidence(t *testing.T) {
	got := channelEnumFromStreamType("liquidity.evidence")
	if got != deliveryv1.Channel_CHANNEL_EVIDENCE {
		t.Fatalf("channel enum = %v, want %v", got, deliveryv1.Channel_CHANNEL_EVIDENCE)
	}
	if name := channelName(got, ""); name != "evidence" {
		t.Fatalf("channel name = %q, want evidence", name)
	}
}

func TestCanonicalStreamTypeForCommandChannelEvidence(t *testing.T) {
	if got := canonicalStreamTypeForCommandChannel("evidence"); got != "liquidity.evidence" {
		t.Fatalf("stream type = %q, want liquidity.evidence", got)
	}
	if got := canonicalStreamTypeForCommandChannel("liquidity.evidence"); got != "liquidity.evidence" {
		t.Fatalf("stream type passthrough = %q, want liquidity.evidence", got)
	}
	if got := canonicalStreamTypeForCommandChannel("insights.microstructure_evidence"); got != "insights.microstructure_evidence" {
		t.Fatalf("legacy channel should not canonicalize, got %q", got)
	}
}

func TestChannelNameTapeFallback(t *testing.T) {
	if got := channelName(deliveryv1.Channel_CHANNEL_UNSPECIFIED, "aggregation.tape"); got != "tape" {
		t.Fatalf("channel name = %q, want tape", got)
	}
}

func TestCanonicalStreamTypeForCommandChannelTape(t *testing.T) {
	if got := canonicalStreamTypeForCommandChannel("tape"); got != "aggregation.tape" {
		t.Fatalf("stream type = %q, want aggregation.tape", got)
	}
	if got := canonicalStreamTypeForCommandChannel("aggregation.tape"); got != "aggregation.tape" {
		t.Fatalf("stream type passthrough = %q, want aggregation.tape", got)
	}
}

func TestRejectLegacyCutoverSubject(t *testing.T) {
	t.Run("legacy evidence stream type rejected", func(t *testing.T) {
		sub, p := domain.ParseSubject("insights.microstructure_evidence/binance/BTC-USDT/raw")
		if p != nil {
			t.Fatalf("parse: %v", p)
		}
		if got := rejectLegacyCutoverSubject(sub); got == nil {
			t.Fatal("expected legacy evidence subject rejection")
		}
	})

	t.Run("legacy signal kind rejected", func(t *testing.T) {
		sub, p := domain.ParseSubject("signal/composite/binance/BTC-USDT/1m")
		if p != nil {
			t.Fatalf("parse: %v", p)
		}
		if got := rejectLegacyCutoverSubject(sub); got == nil {
			t.Fatal("expected legacy signal subject rejection")
		}
	})

	t.Run("canonical signal kind accepted", func(t *testing.T) {
		sub, p := domain.ParseSubject("signal/absorption/binance/BTC-USDT/1m")
		if p != nil {
			t.Fatalf("parse: %v", p)
		}
		if got := rejectLegacyCutoverSubject(sub); got != nil {
			t.Fatalf("unexpected rejection: %v", got)
		}
	})
}
