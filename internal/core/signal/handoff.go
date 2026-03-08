package signal

// # Strategy Handoff Contract
//
// The signal→strategy handoff is the boundary where detection/composition
// outputs become strategy inputs. This file documents the contract.
//
// ## From Signal Detection (Tier 1)
//
// Emission.Event (marketmodel.SignalEvent) carries:
//   - SignalID:       unique per detection, deterministic (FNV-1a hash)
//   - CorrelationID:  links back to source evidence
//   - CorrelationIDs: ordered set of all correlated identifiers
//   - Type:           detection type (regime_change, liquidity_collapse, etc.)
//   - Confidence:     [0,1] detection confidence
//   - Severity:       low|medium|high|critical
//
// Strategy maps these to IntentInput:
//   - IntentInput.SignalID      ← SignalEvent.SignalID
//   - IntentInput.CorrelationID ← SignalEvent.CorrelationID
//   - IntentInput.Kind          ← SignalEvent.Type
//   - IntentInput.Confidence    ← SignalEvent.Confidence
//   - IntentInput.Venue         ← SignalEvent.Venue
//   - IntentInput.Instrument    ← SignalEvent.Symbol
//   - IntentInput.TsServer      ← SignalEvent.TsServer
//
// ## From Signal Composition (Tier 2)
//
// CompositeSignalV1 carries:
//   - SignalID:       unique per composition, prefixed "csig_"
//   - CorrelationID:  links back to source evidence stream
//   - Kind:           evidence type that triggered composition
//   - Confidence:     boosted confidence after regime/cross-venue rules
//   - Severity:       inherited from source evidence
//
// Strategy maps these to IntentInput:
//   - IntentInput.SignalID      ← CompositeSignalV1.SignalID
//   - IntentInput.CorrelationID ← CompositeSignalV1.CorrelationID
//   - IntentInput.Kind          ← CompositeSignalV1.Kind
//   - IntentInput.Confidence    ← CompositeSignalV1.Confidence
//   - IntentInput.Venue         ← CompositeSignalV1.Venue
//   - IntentInput.Instrument    ← CompositeSignalV1.Instrument
//   - IntentInput.TsServer      ← CompositeSignalV1.TsServer
//
// ## Invariants
//
//   - Strategy NEVER imports signal or signals — it consumes IntentInput only.
//   - The orchestrator (actor layer) is responsible for mapping signal outputs
//     to IntentInput and feeding the IntentPlanner.
//   - ParentSignalIDs in IntentProvenance always contains exactly the SignalID(s)
//     that triggered the intent.
//   - Signal modules NEVER issue buy/sell directives (ADR-0008 boundary).

// SignalToIntentMapping documents the field mapping from detection emission
// to strategy intent input. This type is not instantiated at runtime — it
// exists purely as a compile-time documentation anchor.
type SignalToIntentMapping struct {
	// SignalID maps to IntentInput.SignalID
	SignalID string
	// CorrelationID maps to IntentInput.CorrelationID
	CorrelationID string
	// Type maps to IntentInput.Kind
	Type string
	// Confidence maps to IntentInput.Confidence
	Confidence float64
	// Venue maps to IntentInput.Venue
	Venue string
	// Symbol maps to IntentInput.Instrument
	Symbol string
	// TsServer maps to IntentInput.TsServer
	TsServer int64
}
