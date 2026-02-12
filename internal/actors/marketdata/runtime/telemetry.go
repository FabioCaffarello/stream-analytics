package mdruntime

import (
	"fmt"
	"time"
)

type parserTelemetry struct {
	total    uint64
	ingested uint64
	skipped  uint64

	byEvent                  map[string]uint64
	bySkipReason             map[string]uint64
	byExchangeEventAndSkip   map[string]uint64
	parseErrorsByProblemCode map[string]uint64

	lastSampleAt map[string]time.Time
	sampleWindow time.Duration
}

func newParserTelemetry() *parserTelemetry {
	return &parserTelemetry{
		byEvent:                  make(map[string]uint64),
		bySkipReason:             make(map[string]uint64),
		byExchangeEventAndSkip:   make(map[string]uint64),
		parseErrorsByProblemCode: make(map[string]uint64),
		lastSampleAt:             make(map[string]time.Time),
		sampleWindow:             30 * time.Second,
	}
}

func (t *parserTelemetry) recordIngest(eventType string) {
	t.total++
	t.ingested++
	t.byEvent[normalizeLabel(eventType, "unknown")]++
}

func (t *parserTelemetry) recordSkip(exchange, eventType, reason, problemCode string) {
	t.total++
	t.skipped++

	event := normalizeLabel(eventType, "unknown")
	skipReason := normalizeLabel(reason, "skip_unspecified")
	code := normalizeLabel(problemCode, "none")
	ex := normalizeLabel(exchange, "unknown")

	t.byEvent[event]++
	t.bySkipReason[skipReason]++
	t.byExchangeEventAndSkip[fmt.Sprintf("%s|%s|%s", ex, event, skipReason)]++
	if skipReason == "parse_error" {
		t.parseErrorsByProblemCode[code]++
	}
}

func (t *parserTelemetry) shouldSample(now time.Time, key string) bool {
	key = normalizeLabel(key, "default")
	last, ok := t.lastSampleAt[key]
	if !ok || now.Sub(last) >= t.sampleWindow {
		t.lastSampleAt[key] = now
		return true
	}
	return false
}

func (t *parserTelemetry) shouldEmitProgress() bool {
	return t.total > 0 && t.total%100 == 0
}

func normalizeLabel(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
