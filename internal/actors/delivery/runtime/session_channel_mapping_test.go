package deliveryruntime

import (
	"testing"

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
	if got := canonicalStreamTypeForCommandChannel("insights.microstructure_evidence"); got != "liquidity.evidence" {
		t.Fatalf("legacy passthrough = %q, want liquidity.evidence", got)
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
