package mdruntime

import (
	"sort"
	"strings"
	"time"
)

// maxTelemetryKeys caps high-cardinality per-symbol/per-ticker maps to
// prevent unbounded memory growth.  The first maxTelemetryKeys unique
// keys are tracked; subsequent new keys are silently skipped.
const maxTelemetryKeys = 2048

type parserTelemetry struct {
	total    uint64
	ingested uint64
	skipped  uint64
	// expectedSkipTotal tracks skips that are expected by exchange protocol
	// semantics (for example Bybit delta ticker messages without markPrice).
	expectedSkipTotal uint64
	// unexpectedSkipTotal tracks skips that warrant closer inspection.
	unexpectedSkipTotal uint64

	byEvent                  map[string]uint64
	bySkipReason             map[string]uint64
	byExpectedSkipReason     map[string]uint64
	byUnexpectedSkipReason   map[string]uint64
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
		byExpectedSkipReason:     make(map[string]uint64),
		byUnexpectedSkipReason:   make(map[string]uint64),
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
	incCapped(t.byTicker, normalizeLabel(ticker, "unknown"), maxTelemetryKeys)
	if bucket := normalizeWSStreamLabel(wsStream); bucket != "" {
		t.byWSStream[bucket]++
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
	if isExpectedSkipReason(event, skipReason, wsStream) {
		t.expectedSkipTotal++
		incCapped(t.byExpectedSkipReason, skipReason, maxTelemetryKeys)
	} else {
		t.unexpectedSkipTotal++
		incCapped(t.byUnexpectedSkipReason, skipReason, maxTelemetryKeys)
		t.byExchangeEventAndSkip[ex+"|"+event+"|"+skipReason]++
	}
	incCapped(t.byTicker, normalizeLabel(ticker, "unknown"), maxTelemetryKeys)
	if bucket := normalizeWSStreamLabel(wsStream); bucket != "" {
		t.byWSStream[bucket]++
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

func (t *parserTelemetry) recordDepthSequence(symbol string, first, final, prevFinal int64) (gap bool, lastFinal int64) {
	sym := normalizeLabel(symbol, "unknown")
	lastFinal, seen := t.lastDepthFinalBySymbol[sym]
	if seen {
		// When prevFinal (pu) is available, use it for gap detection.
		// For Binance Futures, firstID (U) is a global counter much larger
		// than finalID (u), so comparing first vs lastFinal is always a
		// false positive.  prevFinal == lastFinal means no gap.
		if prevFinal > 0 {
			gap = prevFinal != lastFinal
		} else {
			gap = first > lastFinal+1
		}
		if gap {
			t.depthGapsTotal++
			incCapped(t.depthGapsBySymbol, sym, maxTelemetryKeys)
		}
	}
	if seen || len(t.lastDepthFinalBySymbol) < maxTelemetryKeys {
		if !seen || final > lastFinal {
			t.lastDepthFinalBySymbol[sym] = final
		}
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

func normalizeWSStreamLabel(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "@") {
		parts := strings.Split(s, "@")
		if len(parts) < 2 {
			return "unknown"
		}
		switch strings.ToLower(parts[1]) {
		case "aggtrade":
			return "aggtrade"
		case "depth":
			return "depth"
		case "trade":
			return "trade"
		case "markprice":
			return "markprice"
		case "forceorder":
			return "liquidation"
		default:
			return "other"
		}
	}
	switch {
	case strings.HasPrefix(strings.ToLower(s), "publictrade."):
		return "aggtrade"
	case strings.HasPrefix(strings.ToLower(s), "orderbook."):
		return "depth"
	case strings.EqualFold(s, "trades"):
		return "aggtrade"
	case strings.EqualFold(s, "l2book"):
		return "depth"
	case strings.HasPrefix(strings.ToLower(s), "trade."):
		return "trade"
	case strings.EqualFold(s, "match"), strings.EqualFold(s, "last_match"):
		return "trade"
	case strings.EqualFold(s, "snapshot"), strings.EqualFold(s, "l2update"):
		return "depth"
	case strings.EqualFold(s, "ticker"):
		return "ticker"
	case strings.EqualFold(s, "markPrice"):
		return "markprice"
	case strings.EqualFold(s, "forceOrder"):
		return "liquidation"
	case strings.EqualFold(s, "subscriptions"),
		strings.EqualFold(s, "subscriptionresponse"),
		strings.EqualFold(s, "subscribe"),
		strings.EqualFold(s, "ping"),
		strings.EqualFold(s, "pong"),
		strings.EqualFold(s, "heartbeat"),
		strings.EqualFold(s, "error"):
		return "control"
	case strings.HasPrefix(strings.ToLower(s), "tickers."):
		return "ticker"
	case strings.HasPrefix(strings.ToLower(s), "liquidation."):
		return "liquidation"
	case strings.HasPrefix(strings.ToLower(s), "allliquidation."):
		return "liquidation"
	default:
		return "other"
	}
}

func isExpectedSkipReason(eventType, skipReason, wsStream string) bool {
	switch strings.ToLower(strings.TrimSpace(skipReason)) {
	case "control_event":
		return true
	case "markprice_unavailable":
		event := strings.ToLower(strings.TrimSpace(eventType))
		if event == "ticker" || event == "marketdata.markprice" {
			return true
		}
		stream := normalizeWSStreamLabel(wsStream)
		return stream == "ticker" || stream == "markprice"
	default:
		return false
	}
}

func (t *parserTelemetry) topSkipReasons(n int) map[string]uint64 {
	return topCounts(t.bySkipReason, n)
}

func (t *parserTelemetry) topExpectedSkipReasons(n int) map[string]uint64 {
	return topCounts(t.byExpectedSkipReason, n)
}

func (t *parserTelemetry) topUnexpectedSkipReasons(n int) map[string]uint64 {
	return topCounts(t.byUnexpectedSkipReason, n)
}

func (t *parserTelemetry) topExchangeEventSkips(n int) map[string]uint64 {
	return topCounts(t.byExchangeEventAndSkip, n)
}

func (t *parserTelemetry) topWSStreams(n int) map[string]uint64 {
	return topCounts(t.byWSStream, n)
}

func (t *parserTelemetry) topTickerSharePercent(n int) map[string]float64 {
	if t.total == 0 {
		return map[string]float64{}
	}
	filtered := make(map[string]uint64, len(t.byTicker))
	var total uint64
	for ticker, count := range t.byTicker {
		if ticker == "unknown" || count == 0 {
			continue
		}
		filtered[ticker] = count
		total += count
	}
	if total == 0 {
		return map[string]float64{}
	}
	top := topCounts(filtered, n)
	out := make(map[string]float64, len(top))
	for k, v := range top {
		out[k] = float64(v) * 100.0 / float64(total)
	}
	return out
}

// incCapped increments m[key] only if the key already exists or the map has
// room below maxKeys.  This prevents unbounded cardinality growth while still
// tracking the first maxKeys unique keys faithfully.
func incCapped(m map[string]uint64, key string, maxKeys int) {
	if _, ok := m[key]; ok {
		m[key]++
		return
	}
	if len(m) < maxKeys {
		m[key] = 1
	}
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
