package jetstream

import "testing"

func TestValidateSubjectTaxonomy_Valid(t *testing.T) {
	t.Parallel()

	for _, subject := range []string{
		"aggregation.snapshot.v1.binance.BTCUSDT",
		"aggregation.orderbook_inconsistency.v1.binance.BTCUSDT",
		"quarantine.v1.binance.BTCUSDT",
		"marketdata.trade.v1.binance.BTCUSDT",
		"insights.crossvenue.trade_snapshot.v1.global.BTCUSDT",
		"insights.heatmap_snapshot.v1.binance.BTCUSDT",
		"insights.heatmap_delta.v1.binance.BTCUSDT",
		"signal.composite.v1.binance.BTCUSDT",
	} {
		if err := ValidateSubjectTaxonomy(subject); err != nil {
			t.Fatalf("subject %q should be valid: %v", subject, err)
		}
	}
}

func TestValidateSubjectTaxonomy_Invalid(t *testing.T) {
	t.Parallel()

	tests := []string{
		"quarantine.v1.venue.instrument.extra",
		"quarantine.vX.venue.instrument",
		"marketdata.trade.v1.binance.*",
		"freeprefix.v1.binance.BTCUSDT",
	}
	for _, subject := range tests {
		if err := ValidateSubjectTaxonomy(subject); err == nil {
			t.Fatalf("subject %q should be invalid", subject)
		}
	}
}

func TestValidateSubjectPattern_InvalidRootFailsFast(t *testing.T) {
	t.Parallel()

	if err := ValidateSubjectPattern("freeprefix.>"); err == nil {
		t.Fatal("expected invalid root pattern to fail")
	}
}
