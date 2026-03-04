package deliveryruntime

import (
	"testing"

	deliveryv1 "github.com/market-raccoon/internal/shared/proto/gen/delivery/v1"
)

func TestChannelEnumFromStreamTypeEvidence(t *testing.T) {
	got := channelEnumFromStreamType("insights.microstructure_evidence")
	if got != deliveryv1.Channel_CHANNEL_EVIDENCE {
		t.Fatalf("channel enum = %v, want %v", got, deliveryv1.Channel_CHANNEL_EVIDENCE)
	}
	if name := channelName(got, ""); name != "evidence" {
		t.Fatalf("channel name = %q, want evidence", name)
	}
}

func TestCanonicalStreamTypeForCommandChannelEvidence(t *testing.T) {
	if got := canonicalStreamTypeForCommandChannel("evidence"); got != "insights.microstructure_evidence" {
		t.Fatalf("stream type = %q, want insights.microstructure_evidence", got)
	}
	if got := canonicalStreamTypeForCommandChannel("insights.microstructure_evidence"); got != "insights.microstructure_evidence" {
		t.Fatalf("stream type passthrough = %q, want insights.microstructure_evidence", got)
	}
}
