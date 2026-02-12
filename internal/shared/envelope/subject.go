package envelope

import (
	"fmt"
	"strings"
	"unicode"
)

// SubjectFromEnvelope returns the canonical JetStream subject:
// {type}.v{version}.{venue_lower}.{instrument_alnum_upper}
// Example: marketdata.trade.v1.binance.BTCUSDT
func SubjectFromEnvelope(env Envelope) string {
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

	instrument := normalizeSubjectInstrument(env.Instrument)
	return fmt.Sprintf("%s.v%d.%s.%s", eventType, version, venue, instrument)
}

func normalizeSubjectInstrument(instrument string) string {
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
