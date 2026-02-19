package envelope

import (
	"strconv"
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
	return eventType + ".v" + strconv.Itoa(version) + "." + venue + "." + instrument
}

func normalizeSubjectInstrument(instrument string) string {
	raw := strings.ToUpper(strings.TrimSpace(instrument))
	if raw == "" {
		return "UNKNOWN"
	}
	if isUpperAlphaNumeric(raw) {
		return raw
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

func isUpperAlphaNumeric(s string) bool {
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch >= 'A' && ch <= 'Z' {
			continue
		}
		if ch >= '0' && ch <= '9' {
			continue
		}
		return false
	}
	return true
}
