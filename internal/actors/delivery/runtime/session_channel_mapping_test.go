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
		t.Fatalf("legacy channel passthrough, got %q", got)
	}
	if got := canonicalStreamTypeForCommandChannel("evidence.microstructure_evidence"); got != "evidence.microstructure_evidence" {
		t.Fatalf("canonical evidence channel passthrough, got %q", got)
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

func TestCanonicalStreamTypeForCommandChannelAnalyticsPrimitives(t *testing.T) {
	cases := map[string]string{
		"open_interest":  "marketdata.open_interest",
		"oi":             "aggregation.oi",
		"delta_volume":   "aggregation.delta_volume",
		"cvd":            "aggregation.cvd",
		"bar_stats":      "aggregation.bar_stats",
		"aggregation.oi": "aggregation.oi",
	}
	for input, want := range cases {
		if got := canonicalStreamTypeForCommandChannel(input); got != want {
			t.Fatalf("channel=%q stream type=%q want=%q", input, got, want)
		}
	}
}

func TestChannelEnumAndNameForOpenInterest(t *testing.T) {
	if got := channelEnumFromStreamType("marketdata.open_interest"); got != deliveryv1.Channel_CHANNEL_OPEN_INTEREST {
		t.Fatalf("channel enum = %v, want %v", got, deliveryv1.Channel_CHANNEL_OPEN_INTEREST)
	}
	if got := channelEnumFromStreamType("aggregation.oi"); got != deliveryv1.Channel_CHANNEL_OPEN_INTEREST {
		t.Fatalf("channel enum = %v, want %v", got, deliveryv1.Channel_CHANNEL_OPEN_INTEREST)
	}
	if name := channelName(deliveryv1.Channel_CHANNEL_OPEN_INTEREST, ""); name != "open_interest" {
		t.Fatalf("channel name = %q, want open_interest", name)
	}
}

func TestChannelNameForAnalyticsFallbacks(t *testing.T) {
	if got := channelName(deliveryv1.Channel_CHANNEL_UNSPECIFIED, "aggregation.delta_volume"); got != "delta_volume" {
		t.Fatalf("channel name = %q, want delta_volume", got)
	}
	if got := channelName(deliveryv1.Channel_CHANNEL_UNSPECIFIED, "aggregation.cvd"); got != "cvd" {
		t.Fatalf("channel name = %q, want cvd", got)
	}
	if got := channelName(deliveryv1.Channel_CHANNEL_UNSPECIFIED, "aggregation.bar_stats"); got != "bar_stats" {
		t.Fatalf("channel name = %q, want bar_stats", got)
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

	t.Run("legacy regime evidence stream type rejected", func(t *testing.T) {
		sub, p := domain.ParseSubject("insights.regime_evidence/binance/BTC-USDT/1m")
		if p != nil {
			t.Fatalf("parse: %v", p)
		}
		if got := rejectLegacyCutoverSubject(sub); got == nil {
			t.Fatal("expected legacy regime evidence subject rejection")
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
