package deliveryruntime

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/anthdm/hollywood/actor"
	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
)

// RouterConfig configures the Delivery router actor.
type RouterConfig struct {
	Logger        *slog.Logger
	Timeframe     string
	EnvelopeStore envelopeStore
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
	subjectPIDs map[domain.Subject][]*actor.PID

	engine  *actor.Engine
	selfPID *actor.PID
	stopped bool
}

const routerInstrumentMarketTypeMetaKey = "instrument_market_type"

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
	if r.engine == nil && c != nil {
		r.engine = c.Engine()
		r.selfPID = c.PID()
	}
}

func (r *RouterActor) onStarted() {
	if r.engine != nil && r.selfPID != nil {
		r.engine.Subscribe(r.selfPID)
	}
}

func (r *RouterActor) onStopped() {
	r.stopped = true
	if r.engine != nil && r.selfPID != nil {
		r.engine.Unsubscribe(r.selfPID)
	}
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
	if previousPID, exists := r.sessions[id]; exists && previousPID != nil {
		delete(r.sessionByPID, previousPID.String())
	}
	r.sessions[id] = msg.PID
	r.sessionByPID[msg.PID.String()] = id
	if _, ok := r.sessionSubjects[id]; !ok {
		r.sessionSubjects[id] = make(map[domain.Subject]struct{})
	}
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
	if p := domain.ValidateEnvelopeForDelivery(env); p != nil {
		metrics.IncDeliveryRouterEventsRejected("contract_policy")
		r.logger.Warn("delivery router: envelope rejected by contract policy", "err", p)
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
	if len(targets) == 0 && len(aliasTargets) == 0 {
		return
	}
	metrics.IncDeliveryRouterEventsRouted()
	sent := map[string]struct{}(nil)
	if len(targets) > 0 && len(aliasTargets) > 0 {
		sent = make(map[string]struct{}, len(targets))
	}
	for _, pid := range targets {
		if sent != nil && pid != nil {
			sent[pid.String()] = struct{}{}
		}
		r.engine.Send(pid, DeliveryEvent{Subject: subject, Env: env})
	}
	for _, pid := range aliasTargets {
		if pid == nil {
			continue
		}
		if sent != nil {
			if _, exists := sent[pid.String()]; exists {
				continue
			}
		}
		r.engine.Send(pid, DeliveryEvent{Subject: aliasSubject, Env: env})
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
	// WS contract for marketdata and aggregation candle/stats routes on `/raw`.
	// Insights streams are timeframe-routed and should honor envelope metadata.
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(eventType)), "insights.")
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
