package deliveryruntime

import (
	"fmt"
	"log/slog"

	"github.com/anthdm/hollywood/actor"
	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/shared/envelope"
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

	engine  *actor.Engine
	selfPID *actor.PID
	stopped bool
}

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
		}
	}
}

func (r *RouterActor) handleEnvelope(env envelope.Envelope) {
	if r.stopped {
		return
	}
	if p := domain.ValidateEnvelopeForDelivery(env); p != nil {
		r.logger.Warn("delivery router: envelope rejected by contract policy", "err", p)
		return
	}
	if r.cfg.EnvelopeStore != nil {
		r.cfg.EnvelopeStore.StoreEnvelope(env)
	}
	timeframe := r.cfg.Timeframe
	if tf, ok := env.Meta["timeframe"]; ok && tf != "" {
		timeframe = tf
	}
	if timeframe == "" {
		timeframe = domain.DefaultTimeframe
	}
	subject, p := domain.SubjectFromEnvelope(env, timeframe)
	if p != nil {
		r.logger.Warn("delivery router: invalid envelope subject", "err", p)
		return
	}
	targets, ok := r.subjectSessions[subject]
	if !ok {
		return
	}
	for _, pid := range targets {
		if pid != nil {
			r.engine.Send(pid, DeliveryEvent{Subject: subject, Env: env})
		}
	}
}
