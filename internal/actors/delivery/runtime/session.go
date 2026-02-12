package deliveryruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
	"github.com/market-raccoon/internal/core/delivery/app"
	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/core/delivery/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

const readLimitBytes = 64 * 1024

type wsConn interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteJSON(v any) error
	SetReadLimit(limit int64)
	SetReadDeadline(t time.Time) error
	SetPongHandler(h func(string) error)
	Close() error
}

type SessionConfig struct {
	Logger     *slog.Logger
	RouterPID  *actor.PID
	Conn       wsConn
	RangeStore ports.RangeStore
}

type SessionActor struct {
	cfg SessionConfig

	logger *slog.Logger
	engine *actor.Engine
	self   *actor.PID

	session *domain.Session
	service *app.SessionService

	readerCtx    context.Context
	cancelReader context.CancelFunc
	closed       bool
}

func NewSessionActor(cfg SessionConfig) actor.Producer {
	return func() actor.Receiver {
		return &SessionActor{cfg: cfg}
	}
}

func (s *SessionActor) Receive(c *actor.Context) {
	s.ensureDefaults(c)

	switch msg := c.Message().(type) {
	case actor.Initialized:
	case actor.Started:
		s.onStarted()
	case actor.Stopped:
		s.onStopped()
	case sessionInboundText:
		s.handleInboundText(msg.Data)
	case sessionDisconnected:
		s.closeSession()
	case DeliveryEvent:
		s.writeData(msg)
	default:
		s.logger.Warn("delivery session: unknown message", "type", fmt.Sprintf("%T", msg))
	}
}

func (s *SessionActor) ensureDefaults(c *actor.Context) {
	if s.logger == nil {
		if s.cfg.Logger != nil {
			s.logger = s.cfg.Logger
		} else {
			s.logger = slog.Default()
		}
	}
	if s.engine == nil && c != nil {
		s.engine = c.Engine()
		s.self = c.PID()
	}
	if s.session == nil {
		s.session = domain.NewSession()
	}
	if s.service == nil {
		s.service = app.NewSessionService(s.cfg.RangeStore)
	}
}

func (s *SessionActor) onStarted() {
	if s.cfg.RouterPID != nil {
		s.engine.Send(s.cfg.RouterPID, RegisterSession{SessionID: s.session.ID(), PID: s.self})
	}
	if s.cfg.Conn == nil {
		return
	}
	s.readerCtx, s.cancelReader = context.WithCancel(context.Background())
	s.cfg.Conn.SetReadLimit(readLimitBytes)
	if err := s.cfg.Conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
		s.logger.Warn("delivery session: set read deadline failed", "err", err)
	}
	s.cfg.Conn.SetPongHandler(func(string) error {
		return s.cfg.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})
	go s.readLoop()
}

func (s *SessionActor) onStopped() {
	s.closeSession()
}

func (s *SessionActor) readLoop() {
	for {
		select {
		case <-s.readerCtx.Done():
			return
		default:
			messageType, data, err := s.cfg.Conn.ReadMessage()
			if err != nil {
				s.engine.Send(s.self, sessionDisconnected{})
				return
			}
			if messageType != websocket.TextMessage {
				continue
			}
			s.engine.Send(s.self, sessionInboundText{Data: data})
		}
	}
}

type clientCommand struct {
	Op        string          `json:"op"`
	Subject   string          `json:"subject"`
	RequestID string          `json:"request_id,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
}

type getRangeParams struct {
	FromMs int64 `json:"from_ms"`
	ToMs   int64 `json:"to_ms"`
	Limit  int   `json:"limit"`
}

func (s *SessionActor) handleInboundText(data []byte) {
	var cmd clientCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		s.writeProblem(cmd.Op, cmd.RequestID, problem.Wrap(err, problem.ValidationFailed, "invalid JSON payload"))
		return
	}
	switch cmd.Op {
	case "subscribe":
		s.handleSubscribe(cmd)
	case "unsubscribe":
		s.handleUnsubscribe(cmd)
	case "getrange":
		s.handleGetRange(cmd)
	default:
		s.writeProblem(cmd.Op, cmd.RequestID, problem.Newf(problem.ValidationFailed, "unsupported op %q", cmd.Op))
	}
}

func (s *SessionActor) handleSubscribe(cmd clientCommand) {
	subRes := s.service.ParseSubject(cmd.Subject)
	if subRes.IsFail() {
		s.writeProblem(cmd.Op, cmd.RequestID, subRes.Problem())
		return
	}
	subject := subRes.Value()
	if p := s.session.Subscribe(subject, domain.Filter{}); p != nil {
		s.writeProblem(cmd.Op, cmd.RequestID, p)
		return
	}
	if s.cfg.RouterPID != nil {
		s.engine.Send(s.cfg.RouterPID, SubscribeSession{SessionID: s.session.ID(), Subject: subject})
	}
	s.writeJSON(map[string]any{
		"type":       "ack",
		"op":         cmd.Op,
		"request_id": cmd.RequestID,
		"subject":    subject.String(),
	})
}

func (s *SessionActor) handleUnsubscribe(cmd clientCommand) {
	subRes := s.service.ParseSubject(cmd.Subject)
	if subRes.IsFail() {
		s.writeProblem(cmd.Op, cmd.RequestID, subRes.Problem())
		return
	}
	subject := subRes.Value()
	if p := s.session.Unsubscribe(subject); p != nil {
		s.writeProblem(cmd.Op, cmd.RequestID, p)
		return
	}
	if s.cfg.RouterPID != nil {
		s.engine.Send(s.cfg.RouterPID, UnsubscribeSession{SessionID: s.session.ID(), Subject: subject})
	}
	s.writeJSON(map[string]any{
		"type":       "ack",
		"op":         cmd.Op,
		"request_id": cmd.RequestID,
		"subject":    subject.String(),
	})
}

func (s *SessionActor) handleGetRange(cmd clientCommand) {
	var params getRangeParams
	if len(cmd.Params) != 0 {
		if err := json.Unmarshal(cmd.Params, &params); err != nil {
			s.writeProblem(cmd.Op, cmd.RequestID, problem.Wrap(err, problem.ValidationFailed, "invalid getrange params"))
			return
		}
	}
	res := s.service.GetRange(context.Background(), app.GetRangeRequest{
		SubjectRaw: cmd.Subject,
		FromMs:     params.FromMs,
		ToMs:       params.ToMs,
		Limit:      params.Limit,
	})
	if res.IsFail() {
		s.writeProblem(cmd.Op, cmd.RequestID, res.Problem())
		return
	}
	s.writeJSON(map[string]any{
		"type":       "range",
		"op":         cmd.Op,
		"request_id": cmd.RequestID,
		"subject":    cmd.Subject,
		"items":      res.Value(),
	})
}

func (s *SessionActor) writeData(evt DeliveryEvent) {
	s.writeJSON(map[string]any{
		"type":      "event",
		"subject":   evt.Subject.String(),
		"seq":       evt.Env.Seq,
		"ts_ingest": evt.Env.TsIngest,
		"payload":   evt.Env.Payload,
	})
}

func (s *SessionActor) writeProblem(op, requestID string, p *problem.Problem) {
	if p == nil {
		return
	}
	s.writeJSON(map[string]any{
		"type":       "error",
		"op":         op,
		"request_id": requestID,
		"problem": map[string]any{
			"code":    p.Code,
			"message": p.Message,
		},
	})
}

func (s *SessionActor) writeJSON(v any) {
	if s.cfg.Conn == nil {
		return
	}
	if err := s.cfg.Conn.WriteJSON(v); err != nil {
		s.logger.Warn("delivery session: write failed", "err", err)
		s.closeSession()
	}
}

func (s *SessionActor) closeSession() {
	if s.closed {
		return
	}
	s.closed = true
	if s.cancelReader != nil {
		s.cancelReader()
	}
	// Explicitly emit unsubscribe for each tracked subject before unregister.
	// Unregister remains the idempotent safety net.
	for _, sub := range s.session.Subscriptions() {
		if s.cfg.RouterPID != nil {
			s.engine.Send(s.cfg.RouterPID, UnsubscribeSession{
				SessionID: s.session.ID(),
				Subject:   sub.Subject,
			})
		}
	}
	if s.cfg.RouterPID != nil {
		s.engine.Send(s.cfg.RouterPID, UnregisterSession{SessionID: s.session.ID()})
	}
	if s.cfg.Conn != nil {
		_ = s.cfg.Conn.Close()
	}
}
