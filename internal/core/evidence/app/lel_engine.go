package app

import (
	"math"
	"sort"
	"strings"

	"github.com/market-raccoon/internal/core/evidence/domain"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/naming"
)

// LELEngineConfig configures deterministic bounded LEL behavior.
type LELEngineConfig struct {
	MaxStreamsPerRule int
	MaxStreamsGlobal  int
	StreamTTLMillis   int64
}

// DefaultLELEngineConfig returns production defaults.
func DefaultLELEngineConfig() LELEngineConfig {
	return LELEngineConfig{
		MaxStreamsPerRule: 256,
		MaxStreamsGlobal:  1024,
		StreamTTLMillis:   10 * 60 * 1000,
	}
}

// LELEngineStats reports bounded state counters.
type LELEngineStats struct {
	TotalStreams int
	TotalEmitted int64
	TotalEvicted int64
	RuleStreams  map[string]int
}

// LELEngine orchestrates all LEL rules over snapshot+tape inputs.
type LELEngine struct {
	cfg          LELEngineConfig
	rules        []domain.LELRule
	store        *EvidenceStateStore
	totalEmitted int64
	totalEvicted int64
}

// NewLELEngine constructs a deterministic bounded LEL engine.
func NewLELEngine(cfg LELEngineConfig, rules ...domain.LELRule) *LELEngine {
	if cfg.MaxStreamsGlobal <= 0 {
		cfg.MaxStreamsGlobal = 1024
	}
	if cfg.StreamTTLMillis <= 0 {
		cfg.StreamTTLMillis = 10 * 60 * 1000
	}
	if cfg.MaxStreamsPerRule <= 0 {
		cfg.MaxStreamsPerRule = 256
	}
	return &LELEngine{
		cfg:   cfg,
		rules: rules,
		store: NewEvidenceStateStore(EvidenceStateStoreConfig{
			MaxEntries: cfg.MaxStreamsGlobal,
			TTLMillis:  cfg.StreamTTLMillis,
		}),
	}
}

// OnEvent evaluates one LEL input against all rules.
func (e *LELEngine) OnEvent(event domain.LELEvent) []domain.LiquidityEvidence {
	if e == nil {
		return nil
	}
	normalized := normalizeLELEvent(event)
	if normalized.Seq <= 0 || normalized.TsServer <= 0 {
		metrics.IncLELEvidenceDropped("invalid_event")
		return nil
	}
	switch normalized.Kind {
	case domain.LELEventKindSnapshot:
		metrics.IncLELInputProcessed("snapshot")
	case domain.LELEventKindTape:
		metrics.IncLELInputProcessed("tape")
	default:
		metrics.IncLELEvidenceDropped("invalid_kind")
		return nil
	}

	obs := e.store.Observe(normalized.StreamID, normalized.Seq, normalized.TsServer)
	for i := range obs.Evictions {
		for j := range e.rules {
			e.rules[j].EvictStream(obs.Evictions[i].StreamID)
		}
		metrics.IncLELStateEvicted(obs.Evictions[i].Reason)
		e.totalEvicted++
	}
	metrics.SetLELStateEntries(e.store.Len())
	if !obs.Accepted {
		metrics.IncLELEvidenceDropped(obs.Reason)
		return nil
	}
	if obs.PrevTs > 0 && normalized.TsServer >= obs.PrevTs {
		metrics.ObserveLELEvalLatency(float64(normalized.TsServer-obs.PrevTs) / 1000.0)
	} else {
		metrics.ObserveLELEvalLatency(0)
	}

	out := make([]domain.LiquidityEvidence, 0, len(e.rules))
	for i := range e.rules {
		ruleEvents := e.rules[i].OnEvent(normalized)
		for j := range ruleEvents {
			ev := normalizeLiquidityEvidence(normalized, ruleEvents[j])
			if p := ev.Validate(); p != nil {
				metrics.IncLELEvidenceDropped("invalid_evidence")
				continue
			}
			out = append(out, ev)
			metrics.IncLELEvidenceEmitted(string(ev.EvidenceType), string(ev.Severity), ev.Venue)
		}
	}
	e.totalEmitted += int64(len(out))
	return out
}

// Stats returns current bounded state metrics.
func (e *LELEngine) Stats() LELEngineStats {
	ruleStreams := make(map[string]int, len(e.rules))
	for i := range e.rules {
		ruleStreams[e.rules[i].Name()] = e.rules[i].StreamCount()
	}
	return LELEngineStats{
		TotalStreams: e.store.Len(),
		TotalEmitted: e.totalEmitted,
		TotalEvicted: e.totalEvicted,
		RuleStreams:  ruleStreams,
	}
}

func normalizeLELEvent(event domain.LELEvent) domain.LELEvent {
	event.Venue = naming.CanonicalVenue(event.Venue)
	event.Symbol = naming.CanonicalInstrument(event.Symbol)
	if strings.TrimSpace(event.StreamID) == "" {
		event.StreamID = event.Venue + "|" + event.Symbol
	}
	return event
}

func normalizeLiquidityEvidence(input domain.LELEvent, ev domain.LiquidityEvidence) domain.LiquidityEvidence {
	if ev.EvidenceType == "" {
		ev.EvidenceType = domain.LiquidityEvidenceTypeThinning
	}
	if ev.TsIngestMs <= 0 {
		ev.TsIngestMs = input.TsServer
	}
	if strings.TrimSpace(ev.Venue) == "" {
		ev.Venue = input.Venue
	}
	if strings.TrimSpace(ev.Symbol) == "" {
		ev.Symbol = input.Symbol
	}
	ev.Venue = naming.CanonicalVenue(ev.Venue)
	ev.Symbol = naming.CanonicalInstrument(ev.Symbol)
	if strings.TrimSpace(ev.StreamID) == "" {
		ev.StreamID = input.StreamID
	}
	if ev.Seq <= 0 {
		ev.Seq = input.Seq
	}
	if ev.WindowMs <= 0 {
		if input.Kind == domain.LELEventKindTape && input.WindowEndTs > input.WindowStartTs {
			ev.WindowMs = input.WindowEndTs - input.WindowStartTs
		}
	}
	if ev.WindowMs <= 0 {
		ev.WindowMs = 1
	}
	if ev.Version <= 0 {
		ev.Version = domain.LiquidityEvidenceVersion
	}
	if ev.Watermark.SeqStart <= 0 {
		ev.Watermark.SeqStart = input.Seq
	}
	if ev.Watermark.SeqEnd <= 0 {
		ev.Watermark.SeqEnd = input.Seq
	}
	ev.Metrics = normalizeMetrics(ev.Metrics)
	ev.Explain = normalizeExplain(ev.Explain)
	return ev
}

func normalizeMetrics(in []domain.LiquidityEvidenceMetric) []domain.LiquidityEvidenceMetric {
	if len(in) == 0 {
		return nil
	}
	out := make([]domain.LiquidityEvidenceMetric, 0, len(in))
	for i := range in {
		key := strings.TrimSpace(in[i].Key)
		if key == "" || math.IsNaN(in[i].Value) || math.IsInf(in[i].Value, 0) {
			continue
		}
		out = append(out, domain.LiquidityEvidenceMetric{Key: key, Value: in[i].Value})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	dedup := out[:0]
	for i := range out {
		if i > 0 && out[i].Key == out[i-1].Key {
			continue
		}
		dedup = append(dedup, out[i])
	}
	if len(dedup) > 8 {
		dedup = dedup[:8]
	}
	return dedup
}

func normalizeExplain(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for i := range in {
		s := strings.TrimSpace(in[i])
		if s == "" {
			continue
		}
		if len(s) > 120 {
			s = s[:120]
		}
		out = append(out, s)
	}
	if len(out) > 4 {
		out = out[:4]
	}
	return out
}
