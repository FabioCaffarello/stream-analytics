package envelope

import (
	"fmt"
	"strings"
	"testing"
	"unicode"
)

func TestSubjectFromEnvelope_EquivalentToLegacy(t *testing.T) {
	tests := []Envelope{
		{Type: "marketdata.trade", Version: 1, Venue: "BINANCE", Instrument: "BTCUSDT"},
		{Type: " marketdata.bookdelta ", Version: 2, Venue: " bybit ", Instrument: "btc-usdt"},
		{Type: "", Version: 0, Venue: "", Instrument: ""},
		{Type: "INSIGHTS.VOLUME_PROFILE_FINAL", Version: -7, Venue: "Deribit", Instrument: "  eth_perp  "},
		{Type: "marketdata.trade", Version: 9, Venue: "KRAKEN", Instrument: "AÇAO-1"},
	}

	for _, env := range tests {
		env := env
		t.Run(fmt.Sprintf("%s/%s/%s/%d", env.Type, env.Venue, env.Instrument, env.Version), func(t *testing.T) {
			got := SubjectFromEnvelope(env)
			want := legacySubjectFromEnvelope(env)
			if got != want {
				t.Fatalf("subject mismatch: got=%q want=%q", got, want)
			}
		})
	}
}

func TestNormalizeSubjectInstrument_FastPath(t *testing.T) {
	raw := "BTCUSDT123"
	if got := normalizeSubjectInstrument(raw); got != raw {
		t.Fatalf("normalizeSubjectInstrument(%q)=%q want=%q", raw, got, raw)
	}
}

func legacySubjectFromEnvelope(env Envelope) string {
	eventType := strings.ToLower(strings.TrimSpace(env.Type))
	if eventType == "" {
		eventType = "unknown"
	}

	version := env.Version
	if version <= 0 {
		version = 1
	}

	venue := strings.ToLower(strings.TrimSpace(env.Venue))
	if venue == "" {
		venue = "unknown"
	}

	instrument := legacyNormalizeSubjectInstrument(env.Instrument)
	return fmt.Sprintf("%s.v%d.%s.%s", eventType, version, venue, instrument)
}

func legacyNormalizeSubjectInstrument(instrument string) string {
	raw := strings.ToUpper(strings.TrimSpace(instrument))
	if raw == "" {
		return "UNKNOWN"
	}

	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "UNKNOWN"
	}
	return b.String()
}
