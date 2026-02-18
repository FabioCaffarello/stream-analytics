package deliveryruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
	"github.com/market-raccoon/internal/core/delivery/app"
	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/core/delivery/ports"
	sharedclock "github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/observability"
	"github.com/market-raccoon/internal/shared/problem"
)

const readLimitBytes = 64 * 1024

const (
	defaultRangeLimit = 100
	maxLimit          = 1000
	maxPage           = 100
	maxQueryLimit     = 20000
	maxResponseItems  = 1000
)

type wsConn interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteJSON(v any) error
	WriteMessage(messageType int, data []byte) error
	SetReadLimit(limit int64)
	SetReadDeadline(t time.Time) error
	SetPongHandler(h func(string) error)
	Close() error
}

type SessionConfig struct {
	Logger     *slog.Logger
	RouterPID  *actor.PID
	Conn       wsConn
	ClientID   string
	RangeStore ports.RangeStore
	// HotSnapshotProvider returns latest snapshot bytes for a subject.
	HotSnapshotProvider HotSnapshotProvider
	// OutboundQueueSize bounds queued delivery events per session.
	OutboundQueueSize int
	// BackpressurePolicy controls behavior when outbound queue is full.
	BackpressurePolicy domain.BackpressurePolicy
	// BackpressurePriorities overrides default event priorities.
	BackpressurePriorities map[string]int
	// PreferProto toggles protobuf wire frames for outbound delivery events.
	PreferProto bool
	// RateLimit enables per-session token bucket rate limiting for read commands.
	RateLimit RateLimitConfig
	// Clock is optional; defaults to SystemClock.
	Clock sharedclock.Clock
}

type HotSnapshotProvider interface {
	GetLatest(subject domain.Subject) ([]byte, bool)
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
	outbound     []DeliveryEvent
	outboundCap  int
	flushing     bool
	policy       domain.BackpressurePolicy
	priorities   map[string]int
	rateLimiter  *RateLimiter
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
		s.enqueueDelivery(msg)
	case sessionFlushOutbound:
		s.flushOutbound()
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
	if s.outboundCap <= 0 {
		s.outboundCap = s.cfg.OutboundQueueSize
		if s.outboundCap <= 0 {
			s.outboundCap = 256
		}
	}
	if s.policy == "" {
		s.policy = domain.NormalizeBackpressurePolicy(s.cfg.BackpressurePolicy)
	}
	if s.priorities == nil {
		s.priorities = domain.DefaultBackpressurePriorities()
		for key, pri := range s.cfg.BackpressurePriorities {
			s.priorities[strings.ToLower(strings.TrimSpace(key))] = pri
		}
	}
	if s.cfg.Clock == nil {
		s.cfg.Clock = sharedclock.NewSystemClock()
	}
	if s.rateLimiter == nil && s.cfg.RateLimit.Enabled {
		burst := s.cfg.RateLimit.BurstCapacity
		if burst <= 0 {
			burst = 200
		}
		rate := s.cfg.RateLimit.MaxPerSecond
		if rate <= 0 {
			rate = 100
		}
		s.rateLimiter = NewRateLimiter(burst, rate, s.cfg.Clock)
	}
}

func (s *SessionActor) onStarted() {
	if s.cfg.RouterPID != nil {
		s.engine.Send(s.cfg.RouterPID, RegisterSession{SessionID: s.session.ID(), PID: s.self})
	}
	metrics.IncWSClientsConnected()
	observability.IncSessionsActive()
	if s.cfg.PreferProto {
		observability.IncPreferProtoSessions()
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
	Page   int   `json:"page"`
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
	case "getlast":
		s.handleGetLast(cmd)
	case "getrange":
		s.handleGetRange(cmd)
	default:
		s.writeProblem(cmd.Op, cmd.RequestID, problem.Newf(problem.ValidationFailed, "unsupported op %q", cmd.Op))
	}
}

func paginateTail(items []ports.RangeItem, page, limit int) []ports.RangeItem {
	if limit <= 0 {
		return items
	}
	if page <= 0 {
		page = 1
	}
	n := len(items)
	end := n - (page-1)*limit
	if end <= 0 {
		return []ports.RangeItem{}
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	return items[start:end]
}

func sortRangeItems(items []ports.RangeItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Seq != items[j].Seq {
			return items[i].Seq < items[j].Seq
		}
		if items[i].TsIngest != items[j].TsIngest {
			return items[i].TsIngest < items[j].TsIngest
		}
		return bytes.Compare(items[i].Payload, items[j].Payload) < 0
	})
}

func (s *SessionActor) handleSubscribe(cmd clientCommand) {
	if !s.allowRateLimitedCommand(cmd.Op, cmd.RequestID) {
		return
	}
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
	s.emitSnapshot(subject)
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

func (s *SessionActor) emitSnapshot(subject domain.Subject) {
	if s.cfg.HotSnapshotProvider == nil {
		return
	}
	raw, ok := s.cfg.HotSnapshotProvider.GetLatest(subject)
	if !ok || len(raw) == 0 {
		return
	}
	payload := json.RawMessage(raw)
	if !json.Valid(payload) {
		s.logger.Warn("delivery session: invalid snapshot payload, skipping", "subject", subject.String())
		return
	}
	s.writeJSON(map[string]any{
		"type":    "snapshot",
		"subject": subject.String(),
		"payload": payload,
	})
	metrics.IncWSQuery("snapshot", wsQueryBucket(subject.StreamType))
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

func (s *SessionActor) handleGetLast(cmd clientCommand) {
	subRes := s.service.ParseSubject(cmd.Subject)
	if subRes.IsFail() {
		metrics.IncWSQueryRejected("subject_invalid")
		s.writeProblem(cmd.Op, cmd.RequestID, subRes.Problem())
		return
	}
	subject := subRes.Value()
	res := s.service.GetRange(context.Background(), app.GetRangeRequest{
		SubjectRaw: subject.String(),
		Limit:      maxQueryLimit,
	})
	if res.IsFail() {
		metrics.IncWSQueryRejected("range_failed")
		s.writeProblem(cmd.Op, cmd.RequestID, res.Problem())
		return
	}
	var item any
	items := append([]ports.RangeItem(nil), res.Value()...)
	sortRangeItems(items)
	if len(items) > 0 {
		item = items[len(items)-1] // highest seq after defensive sort
	}
	metrics.IncWSQuery("getlast", wsQueryBucket(subject.StreamType))
	s.writeJSON(map[string]any{
		"type":       "last",
		"op":         cmd.Op,
		"request_id": cmd.RequestID,
		"subject":    subject.String(),
		"item":       item,
	})
}

func (s *SessionActor) handleGetRange(cmd clientCommand) {
	if !s.allowRateLimitedCommand(cmd.Op, cmd.RequestID) {
		return
	}
	var params getRangeParams
	if len(cmd.Params) != 0 {
		if err := json.Unmarshal(cmd.Params, &params); err != nil {
			metrics.IncWSQueryRejected("params_invalid")
			s.writeProblem(cmd.Op, cmd.RequestID, problem.Wrap(err, problem.ValidationFailed, "invalid getrange params"))
			return
		}
	}
	subRes := s.service.ParseSubject(cmd.Subject)
	if subRes.IsFail() {
		metrics.IncWSQueryRejected("subject_invalid")
		s.writeProblem(cmd.Op, cmd.RequestID, subRes.Problem())
		return
	}
	subject := subRes.Value()

	page := params.Page
	if page <= 0 {
		page = 1
	}
	limit := params.Limit
	if limit <= 0 {
		limit = defaultRangeLimit
	}
	if limit > maxLimit {
		metrics.IncWSQueryRejected("limit_cap")
		s.writeProblem(cmd.Op, cmd.RequestID, problem.Newf(problem.ValidationFailed, "limit must be <= %d", maxLimit))
		return
	}
	if page > maxPage {
		metrics.IncWSQueryRejected("page_cap")
		s.writeProblem(cmd.Op, cmd.RequestID, problem.Newf(problem.ValidationFailed, "page must be <= %d", maxPage))
		return
	}
	queryLimit := limit * page
	if queryLimit > maxQueryLimit {
		metrics.IncWSQueryRejected("query_cap")
		s.writeProblem(cmd.Op, cmd.RequestID, problem.Newf(problem.ValidationFailed, "limit*page must be <= %d", maxQueryLimit))
		return
	}

	// Defensive window: fetch bounded superset, then sort/paginate in-memory.
	// This avoids relying on implicit store ordering semantics.
	res := s.service.GetRange(context.Background(), app.GetRangeRequest{
		SubjectRaw: subject.String(),
		FromMs:     params.FromMs,
		ToMs:       params.ToMs,
		Limit:      maxQueryLimit,
	})
	if res.IsFail() {
		metrics.IncWSQueryRejected("range_failed")
		s.writeProblem(cmd.Op, cmd.RequestID, res.Problem())
		return
	}
	items := append([]ports.RangeItem(nil), res.Value()...)
	sortRangeItems(items)
	items = paginateTail(items, page, limit)
	if len(items) > maxResponseItems {
		items = items[len(items)-maxResponseItems:]
	}
	metrics.IncWSQuery("getrange", wsQueryBucket(subject.StreamType))
	s.writeJSON(map[string]any{
		"type":       "range",
		"op":         cmd.Op,
		"request_id": cmd.RequestID,
		"subject":    subject.String(),
		"page":       page,
		"limit":      limit,
		"items":      items,
	})
}

func (s *SessionActor) allowRateLimitedCommand(op, requestID string) bool {
	switch op {
	case "subscribe", "getrange":
	default:
		return true
	}
	if s.rateLimiter == nil {
		return true
	}
	if s.rateLimiter.Allow() {
		return true
	}
	metrics.IncWSQueryRejected("rate_limited")
	s.writeProblem(op, requestID, problem.New(problem.Unavailable, "rate limit exceeded"))
	return false
}

func (s *SessionActor) enqueueDelivery(evt DeliveryEvent) {
	if len(s.outbound) >= s.outboundCap {
		switch s.policy {
		case domain.BackpressureDropNewest:
			metrics.IncWSDrops("queue_full")
			return
		case domain.BackpressureDropOldest:
			s.outbound = s.outbound[1:]
			metrics.IncWSDrops("drop_oldest")
		case domain.BackpressurePriorityDrop:
			if !s.priorityDrop(evt) {
				metrics.IncWSDrops("priority_drop_self")
				return
			}
			metrics.SetWSQueueDepth(len(s.outbound))
			if s.flushing {
				return
			}
			s.flushing = true
			s.engine.Send(s.self, sessionFlushOutbound{})
			return
		default:
			metrics.IncWSDrops("queue_full")
			return
		}
	}
	s.outbound = append(s.outbound, evt)
	metrics.SetWSQueueDepth(len(s.outbound))
	if s.flushing {
		return
	}
	s.flushing = true
	s.engine.Send(s.self, sessionFlushOutbound{})
}

func (s *SessionActor) priorityDrop(evt DeliveryEvent) bool {
	if len(s.outbound) < s.outboundCap {
		s.outbound = append(s.outbound, evt)
		return true
	}
	incomingPri := s.eventPriority(evt.Env.Type)
	lowestIdx := -1
	lowestPri := incomingPri
	for i, queued := range s.outbound {
		pri := s.eventPriority(queued.Env.Type)
		if pri < lowestPri {
			lowestPri = pri
			lowestIdx = i
		}
	}
	if lowestIdx < 0 {
		return false
	}
	s.outbound = append(s.outbound[:lowestIdx], s.outbound[lowestIdx+1:]...)
	s.outbound = append(s.outbound, evt)
	metrics.IncWSDrops("priority_drop")
	return true
}

func (s *SessionActor) eventPriority(eventType string) int {
	if s.priorities == nil {
		return 0
	}
	return s.priorities[strings.ToLower(strings.TrimSpace(eventType))]
}

func (s *SessionActor) flushOutbound() {
	if s.closed {
		s.flushing = false
		return
	}
	if len(s.outbound) == 0 {
		s.flushing = false
		metrics.SetWSQueueDepth(0)
		return
	}
	evt := s.outbound[0]
	s.outbound = s.outbound[1:]
	metrics.SetWSQueueDepth(len(s.outbound))

	started := time.Now()
	if err := s.writeDeliveryEvent(evt); err != nil {
		s.logger.Warn("delivery session: write failed", "err", err)
		s.closeSession()
		return
	}
	metrics.ObserveWSSendLatency(time.Since(started))

	if len(s.outbound) > 0 {
		s.engine.Send(s.self, sessionFlushOutbound{})
		return
	}
	s.flushing = false
}

func (s *SessionActor) writeDeliveryEvent(evt DeliveryEvent) error {
	if s.cfg.PreferProto && contracts.ProtoRolloutEnabledForEventType(evt.Env.Type) {
		raw, p := contracts.MarshalEnvelopeV1FromDomain(evt.Env)
		if p != nil {
			return p
		}
		if err := s.writeProtoDirect(websocket.BinaryMessage, raw); err != nil {
			return err
		}
		observability.IncDeliveryProto()
		return nil
	}
	if err := s.writeJSONDirect(map[string]any{
		"type":      "event",
		"subject":   evt.Subject.String(),
		"seq":       evt.Env.Seq,
		"ts_ingest": evt.Env.TsIngest,
		"payload":   evt.Env.Payload,
	}); err != nil {
		return err
	}
	observability.IncDeliveryJSON()
	return nil
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
	if err := s.writeJSONDirect(v); err != nil {
		s.logger.Warn("delivery session: write failed", "err", err)
		s.closeSession()
	}
}

func (s *SessionActor) writeJSONDirect(v any) error {
	if s.cfg.Conn == nil {
		return nil
	}
	return s.cfg.Conn.WriteJSON(v)
}

func (s *SessionActor) writeProtoDirect(messageType int, payload []byte) error {
	if s.cfg.Conn == nil {
		return nil
	}
	return s.cfg.Conn.WriteMessage(messageType, payload)
}

func wsQueryBucket(streamType string) string {
	switch {
	case strings.HasPrefix(streamType, "marketdata."):
		return "marketdata"
	case strings.HasPrefix(streamType, "aggregation."):
		return "aggregation"
	case strings.HasPrefix(streamType, "insights."):
		return "insights"
	default:
		return "unknown"
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
	metrics.DecWSClientsConnected()
	observability.DecSessionsActive()
	if s.cfg.PreferProto {
		observability.DecPreferProtoSessions()
	}
	metrics.SetWSQueueDepth(0)
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
