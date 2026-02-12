package mdruntime

import (
	"fmt"
	"sort"
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
	byWSStream               map[string]uint64
	byTicker                 map[string]uint64
	depthGapsBySymbol        map[string]uint64
	lastDepthFinalBySymbol   map[string]int64
	depthGapsTotal           uint64
	backpressureDropsTotal   uint64
	wsReconnectTotal         uint64
	wsDisconnectByReason     map[string]uint64
	wsConnectionUptimeSecs   float64

	lastSampleAt map[string]time.Time
	sampleWindow time.Duration
}

func newParserTelemetry() *parserTelemetry {
	return &parserTelemetry{
		byEvent:                  make(map[string]uint64),
		bySkipReason:             make(map[string]uint64),
		byExchangeEventAndSkip:   make(map[string]uint64),
		parseErrorsByProblemCode: make(map[string]uint64),
		byWSStream:               make(map[string]uint64),
		byTicker:                 make(map[string]uint64),
		depthGapsBySymbol:        make(map[string]uint64),
		lastDepthFinalBySymbol:   make(map[string]int64),
		wsDisconnectByReason:     make(map[string]uint64),
		lastSampleAt:             make(map[string]time.Time),
		sampleWindow:             30 * time.Second,
	}
}

func (t *parserTelemetry) recordIngest(eventType, ticker, wsStream string) {
	t.total++
	t.ingested++
	t.byEvent[normalizeLabel(eventType, "unknown")]++
	t.byTicker[normalizeLabel(ticker, "unknown")]++
	if wsStream != "" {
		t.byWSStream[wsStream]++
	}
}

func (t *parserTelemetry) recordSkip(exchange, eventType, reason, problemCode, ticker, wsStream string) {
	t.total++
	t.skipped++

	event := normalizeLabel(eventType, "unknown")
	skipReason := normalizeLabel(reason, "skip_unspecified")
	code := normalizeLabel(problemCode, "none")
	ex := normalizeLabel(exchange, "unknown")

	t.byEvent[event]++
	t.bySkipReason[skipReason]++
	t.byExchangeEventAndSkip[fmt.Sprintf("%s|%s|%s", ex, event, skipReason)]++
	t.byTicker[normalizeLabel(ticker, "unknown")]++
	if wsStream != "" {
		t.byWSStream[wsStream]++
	}
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

func (t *parserTelemetry) recordDepthSequence(symbol string, first, final int64) (gap bool, lastFinal int64) {
	sym := normalizeLabel(symbol, "unknown")
	lastFinal, seen := t.lastDepthFinalBySymbol[sym]
	if seen && first > lastFinal+1 {
		gap = true
		t.depthGapsTotal++
		t.depthGapsBySymbol[sym]++
	}
	if !seen || final > lastFinal {
		t.lastDepthFinalBySymbol[sym] = final
	}
	return gap, lastFinal
}

func (t *parserTelemetry) recordBackpressureDrops(n uint64) {
	t.backpressureDropsTotal += n
}

func (t *parserTelemetry) recordReconnect(reason string, uptimeSec float64) {
	t.wsReconnectTotal++
	r := normalizeLabel(reason, "unknown")
	t.wsDisconnectByReason[r]++
	if uptimeSec > 0 {
		t.wsConnectionUptimeSecs += uptimeSec
	}
}

func normalizeLabel(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func (t *parserTelemetry) topWSStreams(n int) map[string]uint64 {
	return topCounts(t.byWSStream, n)
}

func (t *parserTelemetry) topTickerSharePercent(n int) map[string]float64 {
	if t.total == 0 {
		return map[string]float64{}
	}
	top := topCounts(t.byTicker, n)
	out := make(map[string]float64, len(top))
	for k, v := range top {
		out[k] = float64(v) * 100.0 / float64(t.total)
	}
	return out
}

func topCounts(m map[string]uint64, n int) map[string]uint64 {
	type kv struct {
		k string
		v uint64
	}
	items := make([]kv, 0, len(m))
	for k, v := range m {
		items = append(items, kv{k: k, v: v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].v == items[j].v {
			return items[i].k < items[j].k
		}
		return items[i].v > items[j].v
	})
	if n > len(items) {
		n = len(items)
	}
	out := make(map[string]uint64, n)
	for i := 0; i < n; i++ {
		out[items[i].k] = items[i].v
	}
	return out
}
