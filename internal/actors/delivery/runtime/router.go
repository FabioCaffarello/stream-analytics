package deliveryruntime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/observability"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// RouterConfig configures the Delivery router actor.
type RouterConfig struct {
	Logger              *slog.Logger
	Timeframe           string
	EnvelopeStore       envelopeStore
	StreamCoherenceMode string
	// StreamStateTTL bounds per-stream sequence state retention.
	// Zero or negative values default to 30m.
	StreamStateTTL time.Duration
	// StreamStateSweepEvery controls inactive stream-state eviction cadence.
	// Zero or negative values default to 1m.
	StreamStateSweepEvery time.Duration
	// MaxActiveSessions bounds concurrent registered sessions.
	// 0 disables the limit.
	MaxActiveSessions int
	// MaxStreamStateEntries bounds stream-state map entries.
	// When exceeded, a forced sweep runs and oldest entries are evicted.
	// 0 defaults to 50000.
	MaxStreamStateEntries int
	// Now overrides wall-clock access for deterministic tests.
	// Nil defaults to time.Now.
	Now func() time.Time
}

type envelopeStore interface {
	StoreEnvelope(env envelope.Envelope)
}

// RouterActor owns subject routing state.
type RouterActor struct {
	cfg RouterConfig

	logger *slog.Logger

	sessions        map[string]*actor.PID
	sessionByPID    map[string]string
	sessionSubjects map[string]map[domain.Subject]struct{}
	subjectSessions map[domain.Subject]map[string]*actor.PID
	// subjectPIDs is a pre-built slice cache for the hot fan-out path.
	// Rebuilt from subjectSessions on subscribe/unsubscribe (cold path).
	subjectPIDs           map[domain.Subject][]*actor.PID
	streamState           map[string]*streamState
	streamStateTTL        time.Duration
	maxStreamStateEntries int
	now                   func() time.Time
	streamSweepEvery      time.Duration
	streamSweepRepeater   *actor.SendRepeater
	coherenceMode         string

	engine  *actor.Engine
	selfPID *actor.PID
	stopped bool
}

type streamState struct {
	lastOriginSeq   int64
	lastDeliverySeq int64
	lastSeenAt      time.Time
}

type routerSweepStreamState struct{}

const routerInstrumentMarketTypeMetaKey = "instrument_market_type"

const (
	streamCoherenceModeStickySession     = "sticky_session"
	streamCoherenceModeUpstreamSequencer = "upstream_sequencer"
	defaultRouterStreamStateTTL          = 30 * time.Minute
	defaultRouterStreamStateSweepEvery   = time.Minute
	defaultMaxStreamStateEntries         = 50000
)

func NewRouterActor(cfg RouterConfig) actor.Producer {
	return func() actor.Receiver {
		return &RouterActor{cfg: cfg}
	}
}

func (r *RouterActor) Receive(c *actor.Context) {
	r.ensureDefaults(c)

	switch msg := c.Message().(type) {
	case actor.Initialized:
	case actor.Started:
		r.onStarted()
	case actor.Stopped:
		r.onStopped()
	case actor.ActorStoppedEvent:
		r.onActorStopped(msg)
	case actor.ActorStartedEvent:
	case actor.ActorInitializedEvent:
	case RegisterSession:
		r.register(msg)
	case UnregisterSession:
		r.unregister(msg.SessionID.String())
	case SubscribeSession:
		r.subscribe(msg)
	case UnsubscribeSession:
		r.unsubscribe(msg)
	case DeliverEnvelope:
		r.handleEnvelope(msg.Envelope)
	case busEnvelopeMsg:
		r.handleEnvelope(msg.Env)
	case routerSweepStreamState:
		r.sweepStreamState()
	default:
		r.logger.Warn("delivery router: unknown message", "type", fmt.Sprintf("%T", msg))
	}
}

func (r *RouterActor) ensureDefaults(c *actor.Context) {
	if r.logger == nil {
		if r.cfg.Logger != nil {
			r.logger = r.cfg.Logger
		} else {
			r.logger = slog.Default()
		}
	}
	if r.sessions == nil {
		r.sessions = make(map[string]*actor.PID)
	}
	if r.sessionByPID == nil {
		r.sessionByPID = make(map[string]string)
	}
	if r.sessionSubjects == nil {
		r.sessionSubjects = make(map[string]map[domain.Subject]struct{})
	}
	if r.subjectSessions == nil {
		r.subjectSessions = make(map[domain.Subject]map[string]*actor.PID)
	}
	if r.subjectPIDs == nil {
		r.subjectPIDs = make(map[domain.Subject][]*actor.PID)
	}
	if r.streamState == nil {
		r.streamState = make(map[string]*streamState)
		metrics.SetDeliveryRouterStreamStateEntries(0)
		metrics.SetDeliveryRouterStreamStateActive(0)
	}
	if r.streamStateTTL <= 0 {
		r.streamStateTTL = r.cfg.StreamStateTTL
		if r.streamStateTTL <= 0 {
			r.streamStateTTL = defaultRouterStreamStateTTL
		}
	}
	if r.streamSweepEvery <= 0 {
		r.streamSweepEvery = r.cfg.StreamStateSweepEvery
		if r.streamSweepEvery <= 0 {
			r.streamSweepEvery = defaultRouterStreamStateSweepEvery
		}
	}
	if r.maxStreamStateEntries <= 0 {
		r.maxStreamStateEntries = r.cfg.MaxStreamStateEntries
		if r.maxStreamStateEntries <= 0 {
			r.maxStreamStateEntries = defaultMaxStreamStateEntries
		}
	}
	if r.now == nil {
		if r.cfg.Now != nil {
			r.now = r.cfg.Now
		} else {
			r.now = time.Now
		}
	}
	if r.coherenceMode == "" {
		r.coherenceMode = normalizeStreamCoherenceMode(r.cfg.StreamCoherenceMode)
		metrics.SetDeliveryRouterCoherenceMode(r.coherenceMode)
	}
	if r.engine == nil && c != nil {
		r.engine = c.Engine()
		r.selfPID = c.PID()
	}
}

func (r *RouterActor) onStarted() {
	if r.engine != nil && r.selfPID != nil {
		r.engine.Subscribe(r.selfPID)
		r.startStreamStateSweep()
		r.sweepStreamState()
	}
}

func (r *RouterActor) onStopped() {
	r.stopped = true
	r.stopStreamStateSweep()
	if r.engine != nil && r.selfPID != nil {
		r.engine.Unsubscribe(r.selfPID)
	}
}

func (r *RouterActor) startStreamStateSweep() {
	if r.streamSweepRepeater != nil || r.engine == nil || r.selfPID == nil || r.streamSweepEvery <= 0 {
		return
	}
	repeater := r.engine.SendRepeat(r.selfPID, routerSweepStreamState{}, r.streamSweepEvery)
	r.streamSweepRepeater = &repeater
}

func (r *RouterActor) stopStreamStateSweep() {
	if r.streamSweepRepeater == nil {
		return
	}
	r.streamSweepRepeater.Stop()
	r.streamSweepRepeater = nil
}

func (r *RouterActor) onActorStopped(evt actor.ActorStoppedEvent) {
	if evt.PID == nil || r.selfPID == nil || evt.PID.Equals(r.selfPID) {
		return
	}
	sessionID, ok := r.sessionByPID[evt.PID.String()]
	if !ok {
		return
	}
	r.unregister(sessionID)
}

func (r *RouterActor) register(msg RegisterSession) {
	id := msg.SessionID.String()
	if id == "" || msg.PID == nil {
		return
	}
	// Enforce MaxActiveSessions when registering a new (not re-registering) session.
	if _, exists := r.sessions[id]; !exists {
		if r.cfg.MaxActiveSessions > 0 && len(r.sessions) >= r.cfg.MaxActiveSessions {
			metrics.IncDeliveryRouterSessionsRejected()
			r.logger.Warn("delivery router: max active sessions reached, rejecting",
				"max", r.cfg.MaxActiveSessions, "current", len(r.sessions))
			return
		}
	}
	if previousPID, exists := r.sessions[id]; exists && previousPID != nil {
		delete(r.sessionByPID, previousPID.String())
	}
	r.sessions[id] = msg.PID
	r.sessionByPID[msg.PID.String()] = id
	if _, ok := r.sessionSubjects[id]; !ok {
		r.sessionSubjects[id] = make(map[domain.Subject]struct{})
	}
	metrics.SetDeliveryRouterSessionsActive(len(r.sessions))
}

func (r *RouterActor) unregister(sessionID string) {
	subs, ok := r.sessionSubjects[sessionID]
	if ok {
		for subject := range subs {
			r.removeSessionFromSubject(sessionID, subject)
		}
	}
	delete(r.sessionSubjects, sessionID)
	if pid, ok := r.sessions[sessionID]; ok && pid != nil {
		delete(r.sessionByPID, pid.String())
	}
	delete(r.sessions, sessionID)
	metrics.SetDeliveryRouterSessionsActive(len(r.sessions))
}

func (r *RouterActor) subscribe(msg SubscribeSession) {
	sessionID := msg.SessionID.String()
	pid, ok := r.sessions[sessionID]
	if !ok || pid == nil {
		return
	}
	if _, ok := r.sessionSubjects[sessionID]; !ok {
		r.sessionSubjects[sessionID] = make(map[domain.Subject]struct{})
	}
	if _, already := r.sessionSubjects[sessionID][msg.Subject]; already {
		return
	}
	r.sessionSubjects[sessionID][msg.Subject] = struct{}{}
	if _, ok := r.subjectSessions[msg.Subject]; !ok {
		r.subjectSessions[msg.Subject] = make(map[string]*actor.PID)
	}
	r.subjectSessions[msg.Subject][sessionID] = pid
	r.rebuildSubjectPIDs(msg.Subject)
	r.updateSubscriptionGauge()
}

func (r *RouterActor) unsubscribe(msg UnsubscribeSession) {
	sessionID := msg.SessionID.String()
	r.removeSessionFromSubject(sessionID, msg.Subject)
}

func (r *RouterActor) removeSessionFromSubject(sessionID string, subject domain.Subject) {
	if subs, ok := r.sessionSubjects[sessionID]; ok {
		delete(subs, subject)
		if len(subs) == 0 {
			delete(r.sessionSubjects, sessionID)
		}
	}
	if pids, ok := r.subjectSessions[subject]; ok {
		delete(pids, sessionID)
		if len(pids) == 0 {
			delete(r.subjectSessions, subject)
			delete(r.subjectPIDs, subject)
			delete(r.streamState, subject.String())
			metrics.SetDeliveryRouterStreamStateEntries(len(r.streamState))
		} else {
			r.rebuildSubjectPIDs(subject)
		}
	}
	r.updateSubscriptionGauge()
}

func (r *RouterActor) updateSubscriptionGauge() {
	total := 0
	for _, subs := range r.sessionSubjects {
		total += len(subs)
	}
	metrics.SetDeliveryRouterSubscriptionsActive(total)
	observability.SetTerminalWSSubscriptionsActive(int64(total))
}

// rebuildSubjectPIDs rebuilds the fan-out slice cache for a subject
// from the canonical subjectSessions map. Called on subscribe/unsubscribe
// (cold path) to keep the hot-path slice current.
func (r *RouterActor) rebuildSubjectPIDs(subject domain.Subject) {
	inner, ok := r.subjectSessions[subject]
	if !ok || len(inner) == 0 {
		delete(r.subjectPIDs, subject)
		return
	}
	pids := make([]*actor.PID, 0, len(inner))
	for _, pid := range inner {
		if pid != nil {
			pids = append(pids, pid)
		}
	}
	r.subjectPIDs[subject] = pids
}

func (r *RouterActor) handleEnvelope(env envelope.Envelope) {
	if r.stopped {
		return
	}
	_, span := otel.Tracer("market-raccoon.delivery.router").Start(context.Background(), "router.handle_envelope")
	span.SetAttributes(
		attribute.String("event.type", env.Type),
		attribute.String("event.venue", env.Venue),
		attribute.String("event.instrument", env.Instrument),
		attribute.Int64("event.seq", env.Seq),
	)
	defer span.End()
	if p := domain.ValidateEnvelopeForDelivery(env); p != nil {
		metrics.IncDeliveryRouterEventsRejected("contract_policy")
		r.logger.Warn("delivery router: envelope rejected by contract policy", "err", p)
		span.SetAttributes(attribute.Bool("event.rejected", true))
		return
	}
	if r.cfg.EnvelopeStore != nil {
		r.cfg.EnvelopeStore.StoreEnvelope(env)
	}
	timeframe := routingTimeframeForEnvelope(r.cfg.Timeframe, env)
	subject, p := domain.SubjectFromEnvelope(env, timeframe)
	if p != nil {
		metrics.IncDeliveryRouterEventsRejected("invalid_subject")
		r.logger.Warn("delivery router: invalid envelope subject", "err", p)
		return
	}
	aliasSubject, hasAlias := routingMarketTypeAliasSubject(subject, env)
	targets := r.subjectPIDs[subject]
	aliasTargets := []*actor.PID(nil)
	if hasAlias {
		aliasTargets = r.subjectPIDs[aliasSubject]
	}
	wildcardSubject := domain.Subject{}
	wildcardTargets := []*actor.PID(nil)
	if subject.IsSignal() {
		if wild, wp := domain.NewSignalSubject("*", subject.Venue, subject.Symbol, subject.Timeframe); wp == nil && wild != subject {
			wildcardSubject = wild
			wildcardTargets = r.subjectPIDs[wild]
		}
	}
	if len(targets) == 0 && len(aliasTargets) == 0 && len(wildcardTargets) == 0 {
		return
	}
	if len(targets) > 0 {
		if ok, reason := r.acceptStreamSeq(subject.String(), env.Seq); !ok {
			metrics.IncDeliveryRouterEventsRejected(reason)
			return
		}
	}
	if len(aliasTargets) > 0 {
		if ok, reason := r.acceptStreamSeq(aliasSubject.String(), env.Seq); !ok {
			metrics.IncDeliveryRouterEventsRejected(reason)
			return
		}
	}
	if len(wildcardTargets) > 0 {
		if ok, reason := r.acceptStreamSeq(wildcardSubject.String(), env.Seq); !ok {
			metrics.IncDeliveryRouterEventsRejected(reason)
			return
		}
	}
	metrics.IncDeliveryRouterEventsRouted()
	sent := map[string]struct{}(nil)
	if len(targets)+len(aliasTargets)+len(wildcardTargets) > 1 {
		sent = make(map[string]struct{}, len(targets)+len(aliasTargets)+len(wildcardTargets))
	}
	primaryEnv := env
	primaryEnv.Seq = r.nextDeliverySeq(subject.String())
	for _, pid := range targets {
		if sent != nil && pid != nil {
			sent[pid.String()] = struct{}{}
		}
		r.engine.Send(pid, DeliveryEvent{Subject: subject, Env: primaryEnv})
	}
	aliasEnv := env
	aliasSeqAssigned := false
	for _, pid := range aliasTargets {
		if pid == nil {
			continue
		}
		if sent != nil {
			if _, exists := sent[pid.String()]; exists {
				continue
			}
			sent[pid.String()] = struct{}{}
		}
		if !aliasSeqAssigned {
			aliasEnv.Seq = r.nextDeliverySeq(aliasSubject.String())
			aliasSeqAssigned = true
		}
		r.engine.Send(pid, DeliveryEvent{Subject: aliasSubject, Env: aliasEnv})
	}
	wildcardEnv := env
	wildcardSeqAssigned := false
	for _, pid := range wildcardTargets {
		if pid == nil {
			continue
		}
		if sent != nil {
			if _, exists := sent[pid.String()]; exists {
				continue
			}
			sent[pid.String()] = struct{}{}
		}
		if !wildcardSeqAssigned {
			wildcardEnv.Seq = r.nextDeliverySeq(wildcardSubject.String())
			wildcardSeqAssigned = true
		}
		r.engine.Send(pid, DeliveryEvent{Subject: wildcardSubject, Env: wildcardEnv})
	}
}

func (r *RouterActor) acceptStreamSeq(streamID string, seq int64) (bool, string) {
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		metrics.IncDeliveryRouterCoherenceViolation("seq_invalid")
		return false, "seq_invalid"
	}
	if seq <= 0 {
		metrics.IncDeliveryRouterCoherenceViolation("seq_invalid")
		return false, "seq_invalid"
	}
	now := r.now()
	state, ok := r.streamState[streamID]
	if !ok || state == nil {
		if len(r.streamState) >= r.maxStreamStateEntries {
			r.sweepStreamState()
			if len(r.streamState) >= r.maxStreamStateEntries {
				r.evictOldestStreamState()
			}
		}
		state = &streamState{}
		r.streamState[streamID] = state
		metrics.SetDeliveryRouterStreamStateEntries(len(r.streamState))
	}
	state.lastSeenAt = now
	if seq > state.lastOriginSeq {
		state.lastOriginSeq = seq
		return true, ""
	}
	metrics.IncDeliveryRouterCoherenceViolation("seq_non_monotonic")
	return false, "seq_non_monotonic"
}

func (r *RouterActor) nextDeliverySeq(streamID string) int64 {
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		return 0
	}
	now := r.now()
	state, ok := r.streamState[streamID]
	if !ok || state == nil {
		state = &streamState{}
		r.streamState[streamID] = state
	}
	next := state.lastDeliverySeq + 1
	if next <= 0 {
		next = 1
	}
	state.lastDeliverySeq = next
	state.lastSeenAt = now
	return next
}

func (r *RouterActor) sweepStreamState() {
	if len(r.streamState) == 0 {
		metrics.SetDeliveryRouterStreamStateEntries(0)
		metrics.SetDeliveryRouterStreamStateActive(0)
		return
	}
	now := r.now()
	active := 0
	evicted := 0
	for streamID, state := range r.streamState {
		if state == nil || state.lastSeenAt.IsZero() || now.Sub(state.lastSeenAt) > r.streamStateTTL {
			delete(r.streamState, streamID)
			evicted++
			continue
		}
		active++
	}
	metrics.SetDeliveryRouterStreamStateEntries(len(r.streamState))
	metrics.SetDeliveryRouterStreamStateActive(active)
	if evicted > 0 {
		metrics.AddDeliveryRouterStreamStateEvicted(evicted)
	}
}

func (r *RouterActor) evictOldestStreamState() {
	var oldestID string
	var oldestTime time.Time
	for id, state := range r.streamState {
		if state == nil {
			delete(r.streamState, id)
			metrics.AddDeliveryRouterStreamStateEvicted(1)
			metrics.SetDeliveryRouterStreamStateEntries(len(r.streamState))
			return
		}
		if oldestID == "" || state.lastSeenAt.Before(oldestTime) {
			oldestID = id
			oldestTime = state.lastSeenAt
		}
	}
	if oldestID != "" {
		delete(r.streamState, oldestID)
		metrics.AddDeliveryRouterStreamStateEvicted(1)
		metrics.SetDeliveryRouterStreamStateEntries(len(r.streamState))
	}
}

func routingTimeframeForEnvelope(defaultTimeframe string, env envelope.Envelope) string {
	timeframe := strings.TrimSpace(defaultTimeframe)
	if timeframe == "" {
		timeframe = domain.DefaultTimeframe
	}
	if !allowEnvelopeTimeframeOverride(env.Type) {
		return timeframe
	}
	if len(env.Meta) == 0 {
		return timeframe
	}
	if tf := strings.TrimSpace(env.Meta["timeframe"]); tf != "" {
		return strings.ToLower(tf)
	}
	return timeframe
}

func allowEnvelopeTimeframeOverride(eventType string) bool {
	// Timeframe-qualified streams honour envelope Meta["timeframe"] so that
	// clients can subscribe to a specific TF (e.g. /1m) and only receive
	// events for that window.  Raw marketdata (trade, bookdelta) is always
	// routed on DefaultTimeframe ("raw") because those streams have no TF.
	et := strings.ToLower(strings.TrimSpace(eventType))
	switch {
	case strings.HasPrefix(et, "insights."):
		return true
	case et == "aggregation.candle", et == "aggregation.stats":
		return true
	case et == "signal.composite", et == "signal.event":
		return true
	default:
		return false
	}
}

func routingMarketTypeAliasSubject(primary domain.Subject, env envelope.Envelope) (domain.Subject, bool) {
	if len(env.Meta) == 0 {
		return domain.Subject{}, false
	}
	marketType := strings.ToUpper(strings.TrimSpace(env.Meta[routerInstrumentMarketTypeMetaKey]))
	if marketType == "" {
		return domain.Subject{}, false
	}
	symbol := strings.TrimSpace(env.Instrument)
	if symbol == "" {
		return domain.Subject{}, false
	}
	if !strings.Contains(symbol, ":") {
		symbol = symbol + ":" + marketType
	}
	alias, p := domain.NewSubject(primary.StreamType, primary.Venue, symbol, primary.Timeframe)
	if p != nil || alias == primary {
		return domain.Subject{}, false
	}
	return alias, true
}

func normalizeStreamCoherenceMode(raw string) string {
	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case "", streamCoherenceModeStickySession:
		return streamCoherenceModeStickySession
	case streamCoherenceModeUpstreamSequencer:
		return streamCoherenceModeUpstreamSequencer
	default:
		return "unknown"
	}
}
