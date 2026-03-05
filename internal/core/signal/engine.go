package signal

import (
	"sort"
	"strconv"
	"strings"

	evidencedomain "github.com/market-raccoon/internal/core/evidence/domain"
	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/problem"
)

type EngineConfig struct {
	Store StateStoreConfig
	Rules RulesConfig
}

func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		Store: DefaultStateStoreConfig(),
		Rules: DefaultRulesConfig(),
	}
}

type SignalEngine struct {
	store    *SignalStateStore
	rules    []SignalRule
	combiner FeatureCombiner
	emitter  SignalEmitter
}

func NewSignalEngine(cfg EngineConfig, emitter SignalEmitter, rules ...SignalRule) *SignalEngine {
	selectedRules := rules
	if len(selectedRules) == 0 {
		selectedRules = BuildV0Rules(cfg.Rules)
	}
	return &SignalEngine{
		store:    NewSignalStateStore(cfg.Store),
		rules:    selectedRules,
		combiner: FeatureCombiner{},
		emitter:  emitter,
	}
}

func (e *SignalEngine) SetEmitter(emitter SignalEmitter) {
	if e == nil {
		return
	}
	e.emitter = emitter
}

func (e *SignalEngine) StoreEntries() int {
	if e == nil || e.store == nil {
		return 0
	}
	return e.store.StreamEntries()
}

func (e *SignalEngine) OnMarketEvent(obs MarketObservation) ([]EvictionReason, *problem.Problem) {
	if e == nil || e.store == nil {
		return nil, problem.New(problem.ValidationFailed, "signal engine is nil")
	}
	return e.store.ObserveMarket(obs)
}

func (e *SignalEngine) OnEvidenceEvent(
	key marketmodel.StreamKey,
	tenant string,
	event evidencedomain.EvidenceEvent,
) ([]Emission, []EvictionReason, []string, []string, int64, *problem.Problem) {
	if e == nil || e.store == nil {
		return nil, nil, nil, nil, 0, problem.New(problem.ValidationFailed, "signal engine is nil")
	}
	tenant = normalizedTenant(tenant)
	snapshot, accepted, evictions, p := e.store.ObserveEvidence(key, tenant, event)
	if p != nil {
		return nil, evictions, nil, nil, 0, p
	}
	if !accepted {
		return nil, evictions, nil, nil, 0, nil
	}
	evalSpan := evidenceEvalSpan(event, snapshot)

	emissions := make([]Emission, 0, len(e.rules))
	dedupTypes := make([]string, 0, len(e.rules))
	rateLimitedTypes := make([]string, 0, len(e.rules))
	for i := range e.rules {
		out, ok := e.rules[i].Evaluate(RuleInput{
			Tenant:    tenant,
			StreamKey: key,
			Evidence:  event,
			Snapshot:  snapshot,
		})
		if !ok {
			continue
		}
		features := e.combiner.MergeSorted(out.Features)
		if len(features) == 0 {
			continue
		}
		watermark := signalInputWatermark(key, snapshot, event)
		explain := explainFragments(out, event)
		signalEvent := marketmodel.SignalEvent{
			Type:           strings.TrimSpace(out.Type),
			TsServer:       event.TsServer,
			Scope:          out.Scope,
			Severity:       strings.ToLower(strings.TrimSpace(out.Severity)),
			Confidence:     out.Confidence,
			Features:       features,
			Explanation:    primaryExplanation(explain),
			Explain:        explain,
			RuleID:         normalizedRuleID(out.RuleID, e.rules[i].Name()),
			RuleVersion:    strings.TrimSpace(out.RuleVersion),
			InputWatermark: watermark,
		}
		if signalEvent.Scope == marketmodel.SignalScopeStream {
			signalEvent.Venue = string(key.Venue)
			signalEvent.Symbol = string(key.Symbol)
		}
		signalEvent.CorrelationID = correlationID(signalEvent)
		signalEvent.CorrelationIDs = mergeUniqueStrings(signalEvent.CorrelationID, evidenceCorrelationID(event))
		signalEvent.SignalID = signalID(tenant, key, signalEvent)
		if p := signalEvent.Validate(); p != nil {
			return nil, evictions, dedupTypes, rateLimitedTypes, evalSpan, p
		}
		fingerprint := signalFingerprint(signalEvent)
		if e.store.IsDuplicate(key, tenant, signalEvent.Type, fingerprint, signalEvent.TsServer) {
			dedupTypes = append(dedupTypes, signalEvent.Type)
			continue
		}
		if !e.store.AllowTenantEmission(tenant, signalEvent.TsServer) {
			rateLimitedTypes = append(rateLimitedTypes, signalEvent.Type)
			continue
		}
		emission := Emission{
			Tenant:     tenant,
			StreamKey:  key,
			Seq:        e.store.NextSignalSeq(key, tenant),
			Event:      signalEvent,
			RuleName:   e.rules[i].Name(),
			EvalSpanMs: evalSpan,
		}
		if e.emitter != nil {
			if p := e.emitter.Emit(emission); p != nil {
				return nil, evictions, dedupTypes, rateLimitedTypes, evalSpan, p
			}
		}
		emissions = append(emissions, emission)
	}
	sort.SliceStable(emissions, func(i, j int) bool {
		if emissions[i].Event.Type == emissions[j].Event.Type {
			return emissions[i].Seq < emissions[j].Seq
		}
		return emissions[i].Event.Type < emissions[j].Event.Type
	})
	return emissions, evictions, dedupTypes, rateLimitedTypes, evalSpan, nil
}

func correlationID(ev marketmodel.SignalEvent) string {
	parts := make([]string, 0, 16)
	parts = append(parts,
		ev.Type,
		strconv.FormatInt(ev.TsServer, 10),
		string(ev.Scope),
		ev.Venue,
		ev.Symbol,
		ev.Severity,
		strconv.FormatFloat(ev.Confidence, 'f', 6, 64),
		ev.RuleID,
		ev.RuleVersion,
	)
	parts = append(parts, ev.Explain...)
	for i := range ev.Features {
		parts = append(parts, ev.Features[i].Key, strconv.FormatFloat(ev.Features[i].Value, 'f', 6, 64))
	}
	for i := range ev.InputWatermark {
		parts = append(parts,
			ev.InputWatermark[i].Venue,
			ev.InputWatermark[i].Symbol,
			strconv.FormatInt(ev.InputWatermark[i].SeqStart, 10),
			strconv.FormatInt(ev.InputWatermark[i].SeqEnd, 10),
		)
	}
	return sharedhash.HashFieldsFast(parts...)
}

func signalFingerprint(ev marketmodel.SignalEvent) string {
	parts := []string{
		ev.Type,
		ev.Severity,
		strconv.FormatFloat(ev.Confidence, 'f', 6, 64),
		ev.RuleID,
		ev.RuleVersion,
		ev.Explanation,
	}
	parts = append(parts, ev.Explain...)
	for i := range ev.Features {
		parts = append(parts, ev.Features[i].Key, strconv.FormatFloat(ev.Features[i].Value, 'f', 6, 64))
	}
	for i := range ev.InputWatermark {
		parts = append(parts,
			ev.InputWatermark[i].Venue,
			ev.InputWatermark[i].Symbol,
			strconv.FormatInt(ev.InputWatermark[i].SeqStart, 10),
			strconv.FormatInt(ev.InputWatermark[i].SeqEnd, 10),
		)
	}
	return sharedhash.HashFieldsFast(parts...)
}

func signalID(tenant string, key marketmodel.StreamKey, ev marketmodel.SignalEvent) string {
	parts := make([]string, 0, 32)
	parts = append(parts,
		tenant,
		string(key.Venue),
		string(key.Symbol),
		ev.Type,
		strconv.FormatInt(ev.TsServer, 10),
		ev.RuleID,
		ev.RuleVersion,
	)
	parts = append(parts, ev.CorrelationIDs...)
	for i := range ev.InputWatermark {
		parts = append(parts,
			ev.InputWatermark[i].Venue,
			ev.InputWatermark[i].Symbol,
			strconv.FormatInt(ev.InputWatermark[i].SeqStart, 10),
			strconv.FormatInt(ev.InputWatermark[i].SeqEnd, 10),
		)
	}
	return "sig_" + sharedhash.HashFieldsFast(parts...)
}

func evidenceCorrelationID(event evidencedomain.EvidenceEvent) string {
	return "evidence:" + sharedhash.HashFieldsFast(
		string(event.Type),
		event.Venue,
		event.Symbol,
		event.StreamID,
		strconv.FormatInt(event.Seq, 10),
		strconv.FormatInt(event.TsServer, 10),
	)
}

func explainFragments(out RuleOutput, event evidencedomain.EvidenceEvent) []string {
	candidates := make([]string, 0, len(out.Explain)+2)
	candidates = append(candidates, out.Explain...)
	candidates = append(candidates, out.Explanation, event.Explanation)
	outFragments := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for i := range candidates {
		fragment := strings.TrimSpace(candidates[i])
		if fragment == "" {
			continue
		}
		if _, ok := seen[fragment]; ok {
			continue
		}
		seen[fragment] = struct{}{}
		outFragments = append(outFragments, fragment)
	}
	if len(outFragments) == 0 {
		return []string{"signal rule matched deterministic thresholds"}
	}
	return outFragments
}

func primaryExplanation(in []string) string {
	if len(in) == 0 {
		return ""
	}
	return strings.TrimSpace(in[0])
}

func normalizedRuleID(ruleID, fallbackName string) string {
	ruleID = strings.ToLower(strings.TrimSpace(ruleID))
	if ruleID == "" {
		ruleID = strings.ToLower(strings.TrimSpace(fallbackName))
	}
	ruleID = strings.ReplaceAll(ruleID, " ", "_")
	if ruleID == "" {
		return "signal_rule"
	}
	return ruleID
}

func mergeUniqueStrings(values ...string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for i := range values {
		v := strings.TrimSpace(values[i])
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func evidenceEvalSpan(event evidencedomain.EvidenceEvent, snapshot StreamSnapshot) int64 {
	if len(snapshot.EvidenceHistory) == 0 {
		return 0
	}
	evalSpan := event.TsServer - snapshot.EvidenceHistory[0].TsServer
	if evalSpan < 0 {
		return -evalSpan
	}
	return evalSpan
}

func signalInputWatermark(
	key marketmodel.StreamKey,
	snapshot StreamSnapshot,
	event evidencedomain.EvidenceEvent,
) []marketmodel.SignalInputSeqRange {
	seqStart := max64(snapshot.WatermarkStart, 1)
	if event.InputWatermark.SeqStart > 0 && event.InputWatermark.SeqStart < seqStart {
		seqStart = event.InputWatermark.SeqStart
	}
	seqEnd := max64(snapshot.WatermarkEnd, event.Seq)
	if event.InputWatermark.SeqEnd > seqEnd {
		seqEnd = event.InputWatermark.SeqEnd
	}
	if seqEnd < seqStart {
		seqEnd = seqStart
	}
	return []marketmodel.SignalInputSeqRange{{
		Venue:    string(key.Venue),
		Symbol:   string(key.Symbol),
		SeqStart: seqStart,
		SeqEnd:   seqEnd,
	}}
}
