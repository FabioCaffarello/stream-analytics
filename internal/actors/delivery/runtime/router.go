package deliveryruntime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/anthdm/hollywood/actor"
	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/shared/envelope"
)

// RouterConfig configures the Delivery router actor.
type RouterConfig struct {
	Logger     *slog.Logger
	EnvelopeCh <-chan envelope.Envelope
	Timeframe  string
}

// RouterActor owns subject routing state.
type RouterActor struct {
	cfg RouterConfig

	logger *slog.Logger

	sessions        map[string]*actor.PID
	sessionSubjects map[string]map[domain.Subject]struct{}
	subjectSessions map[domain.Subject]map[string]*actor.PID

	engine     *actor.Engine
	selfPID    *actor.PID
	consumeCtx context.Context
	cancel     context.CancelFunc
	stopped    bool
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
	case RegisterSession:
		r.register(msg)
	case UnregisterSession:
		r.unregister(msg.SessionID.String())
	case SubscribeSession:
		r.subscribe(msg)
	case UnsubscribeSession:
		r.unsubscribe(msg)
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
	if r.cfg.EnvelopeCh == nil || r.engine == nil || r.selfPID == nil {
		return
	}
	r.consumeCtx, r.cancel = context.WithCancel(context.Background())
	go r.consumeLoop()
}

func (r *RouterActor) onStopped() {
	r.stopped = true
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *RouterActor) consumeLoop() {
	for {
		select {
		case <-r.consumeCtx.Done():
			return
		case env, ok := <-r.cfg.EnvelopeCh:
			if !ok {
				return
			}
			r.engine.Send(r.selfPID, busEnvelopeMsg{Env: env})
		}
	}
}

func (r *RouterActor) register(msg RegisterSession) {
	id := msg.SessionID.String()
	if id == "" || msg.PID == nil {
		return
	}
	r.sessions[id] = msg.PID
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
	timeframe := r.cfg.Timeframe
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
