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
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/observability"
	"github.com/market-raccoon/internal/shared/problem"
)

const readLimitBytes = 64 * 1024
const wsKeepalivePingInterval = 20 * time.Second

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
	// SlowClientDropThreshold disconnects a session after N dropped outbound
	// events due to backpressure. 0 disables threshold-based disconnects.
	SlowClientDropThreshold int
	// Clock is optional; defaults to SystemClock.
	Clock sharedclock.Clock
	// TranscodeCache is an optional shared proto→JSON transcode cache.
	// When set, proto payloads destined for JSON clients are cached to avoid
	// redundant decode+marshal across sessions receiving the same event.
	TranscodeCache *TranscodeCache
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
	outbound     *deliveryRing
	outboundCap  int
	flushing     bool
	policy       domain.BackpressurePolicy
	priorities   map[string]int
	rateLimiter  *RateLimiter
	dropCount    int
}

type sessionKeepaliveTick struct{}

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
	case AttachConn:
		s.attachConn(msg.Conn)
	case sessionInboundText:
		s.handleInboundText(msg.Data)
	case sessionKeepaliveTick:
		s.handleKeepaliveTick()
	case GetRangeRequest:
		s.handleGetRangeRequest(msg)
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
	if s.outbound == nil {
		s.outbound = newDeliveryRing(s.outboundCap)
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
	s.attachConn(s.cfg.Conn)
}

func (s *SessionActor) attachConn(conn wsConn) {
	if conn == nil || s.closed {
		return
	}
	if s.cancelReader != nil {
		return
	}
	if s.cfg.Conn == nil {
		s.cfg.Conn = conn
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
	go s.keepaliveLoop(s.readerCtx)
	go s.readLoop()
}

func (s *SessionActor) onStopped() {
	s.closeSession()
}

func (s *SessionActor) keepaliveLoop(ctx context.Context) {
	ticker := time.NewTicker(wsKeepalivePingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.engine != nil && s.self != nil {
				s.engine.Send(s.self, sessionKeepaliveTick{})
			}
		}
	}
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

func (s *SessionActor) handleKeepaliveTick() {
	if s.closed || s.cfg.Conn == nil {
		return
	}
	// Server drives ping/pong keepalive so browser/native clients don't expire
	// on the 60s read deadline while passively subscribed.
	if err := s.cfg.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
		s.engine.Send(s.self, sessionDisconnected{})
	}
}

type clientCommand struct {
	Op        string          `json:"op"`
	Subject   string          `json:"subject"`
	RequestID string          `json:"request_id,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
}

// Pre-allocated typed structs for outbound JSON frames.
// These eliminate map[string]any allocations on the delivery hot path.

type wsAckFrame struct {
	Type      string `json:"type"`
	Op        string `json:"op"`
	RequestID string `json:"request_id"`
	Subject   string `json:"subject"`
}

type wsSnapshotFrame struct {
	Type    string          `json:"type"`
	Subject string          `json:"subject"`
	Payload json.RawMessage `json:"payload"`
	// SnapshotSource identifies server-side bootstrap source for subscribe
	// snapshot frames when synthesized from the hot snapshot provider.
	SnapshotSource string `json:"snapshot_source,omitempty"`
}

type wsLastFrame struct {
	Type      string `json:"type"`
	Op        string `json:"op"`
	RequestID string `json:"request_id"`
	Subject   string `json:"subject"`
	Item      any    `json:"item"`
	// SnapshotSource is present when the response item was synthesized from the
	// hot snapshot fallback rather than the session range store.
	SnapshotSource string `json:"snapshot_source,omitempty"`
}

type wsRangeFrame struct {
	Type      string `json:"type"`
	Op        string `json:"op"`
	RequestID string `json:"request_id"`
	Subject   string `json:"subject"`
	Page      int    `json:"page"`
	Limit     int    `json:"limit"`
	Items     any    `json:"items"`
	// SnapshotSource is present when items were synthesized from the hot
	// snapshot fallback because the requested range returned empty.
	SnapshotSource string `json:"snapshot_source,omitempty"`
}

type wsEventFrame struct {
	Type     string          `json:"type"`
	Subject  string          `json:"subject"`
	Seq      int64           `json:"seq"`
	TsIngest int64           `json:"ts_ingest"`
	Payload  json.RawMessage `json:"payload"`
}

type wsErrorProblem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type wsErrorFrame struct {
	Type      string         `json:"type"`
	Op        string         `json:"op"`
	RequestID string         `json:"request_id"`
	Problem   wsErrorProblem `json:"problem"`
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
	s.writeJSON(wsAckFrame{
		Type:      "ack",
		Op:        cmd.Op,
		RequestID: cmd.RequestID,
		Subject:   subject.String(),
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
	s.writeJSON(wsSnapshotFrame{
		Type:           "snapshot",
		Subject:        subject.String(),
		Payload:        payload,
		SnapshotSource: "hot_snapshot_fallback",
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
	s.writeJSON(wsAckFrame{
		Type:      "ack",
		Op:        cmd.Op,
		RequestID: cmd.RequestID,
		Subject:   subject.String(),
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
	var snapshotSource string
	items := append([]ports.RangeItem(nil), res.Value()...)
	sortRangeItems(items)
	if len(items) > 0 {
		item = items[len(items)-1] // highest seq after defensive sort
	} else if s.cfg.HotSnapshotProvider != nil {
		if raw, ok := s.cfg.HotSnapshotProvider.GetLatest(subject); ok && len(raw) > 0 {
			item = hotSnapshotRangeItem(raw)
			snapshotSource = "hot_snapshot_fallback"
		}
	}
	metrics.IncWSQuery("getlast", wsQueryBucket(subject.StreamType))
	s.writeJSON(wsLastFrame{
		Type:           "last",
		Op:             cmd.Op,
		RequestID:      cmd.RequestID,
		Subject:        subject.String(),
		Item:           item,
		SnapshotSource: snapshotSource,
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
	s.executeGetRange(cmd.Op, cmd.RequestID, cmd.Subject, params)
}

func (s *SessionActor) handleGetRangeRequest(req GetRangeRequest) {
	if !s.allowRateLimitedCommand("getrange", req.RequestID) {
		return
	}
	s.executeGetRange("getrange", req.RequestID, req.Subject, getRangeParams{
		FromMs: req.FromMs,
		ToMs:   req.ToMs,
		Limit:  req.Limit,
		Page:   req.Page,
	})
}

func (s *SessionActor) executeGetRange(op, requestID, subjectRaw string, params getRangeParams) {
	subRes := s.service.ParseSubject(subjectRaw)
	if subRes.IsFail() {
		metrics.IncWSQueryRejected("subject_invalid")
		s.writeProblem(op, requestID, subRes.Problem())
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
		s.writeProblem(op, requestID, problem.Newf(problem.ValidationFailed, "limit must be <= %d", maxLimit))
		return
	}
	if page > maxPage {
		metrics.IncWSQueryRejected("page_cap")
		s.writeProblem(op, requestID, problem.Newf(problem.ValidationFailed, "page must be <= %d", maxPage))
		return
	}
	queryLimit := limit * page
	if queryLimit > maxQueryLimit {
		metrics.IncWSQueryRejected("query_cap")
		s.writeProblem(op, requestID, problem.Newf(problem.ValidationFailed, "limit*page must be <= %d", maxQueryLimit))
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
		s.writeProblem(op, requestID, res.Problem())
		return
	}
	items := append([]ports.RangeItem(nil), res.Value()...)
	var snapshotSource string
	sortRangeItems(items)
	if len(items) == 0 && page == 1 && params.FromMs == 0 && params.ToMs == 0 && s.cfg.HotSnapshotProvider != nil {
		if raw, ok := s.cfg.HotSnapshotProvider.GetLatest(subject); ok && len(raw) > 0 {
			items = []ports.RangeItem{hotSnapshotRangeItem(raw)}
			snapshotSource = "hot_snapshot_fallback"
		}
	}
	items = paginateTail(items, page, limit)
	if len(items) > maxResponseItems {
		items = items[len(items)-maxResponseItems:]
	}
	metrics.IncWSQuery("getrange", wsQueryBucket(subject.StreamType))
	s.writeJSON(wsRangeFrame{
		Type:           "range",
		Op:             op,
		RequestID:      requestID,
		Subject:        subject.String(),
		Page:           page,
		Limit:          limit,
		Items:          items,
		SnapshotSource: snapshotSource,
	})
}

func hotSnapshotRangeItem(raw []byte) ports.RangeItem {
	item := ports.RangeItem{Payload: append([]byte(nil), raw...)}
	// Best-effort metadata so fallback results sort and inspect more like normal
	// range items. Aggregates payloads carry SeqLast/WindowEndTs in JSON.
	var meta struct {
		SeqLast     int64 `json:"SeqLast"`
		WindowEndTs int64 `json:"WindowEndTs"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return item
	}
	if meta.SeqLast > 0 {
		item.Seq = meta.SeqLast
	}
	if meta.WindowEndTs > 0 {
		item.TsIngest = meta.WindowEndTs
	}
	return item
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
	if s.outbound.IsFull() {
		switch s.policy {
		case domain.BackpressureDropNewest:
			if s.onDrop("queue_full") {
				return
			}
			return
		case domain.BackpressureDropOldest:
			s.outbound.DropFront()
			if s.onDrop("drop_oldest") {
				return
			}
		case domain.BackpressurePriorityDrop:
			if !s.priorityDrop(evt) {
				if s.onDrop("priority_drop_self") {
					return
				}
				return
			}
			if s.onDrop("priority_drop") {
				return
			}
			metrics.SetWSQueueDepth(s.outbound.Len())
			if s.flushing {
				return
			}
			s.flushing = true
			s.engine.Send(s.self, sessionFlushOutbound{})
			return
		default:
			if s.onDrop("queue_full") {
				return
			}
			return
		}
	}
	s.outbound.PushBack(evt)
	metrics.SetWSQueueDepth(s.outbound.Len())
	if s.flushing {
		return
	}
	s.flushing = true
	s.engine.Send(s.self, sessionFlushOutbound{})
}

func (s *SessionActor) priorityDrop(evt DeliveryEvent) bool {
	if !s.outbound.IsFull() {
		s.outbound.PushBack(evt)
		return true
	}
	incomingPri := s.eventPriority(evt.Env.Type)
	lowestIdx := -1
	lowestPri := incomingPri
	for i := 0; i < s.outbound.Len(); i++ {
		pri := s.eventPriority(s.outbound.At(i).Env.Type)
		if pri < lowestPri {
			lowestPri = pri
			lowestIdx = i
		}
	}
	if lowestIdx < 0 {
		return false
	}
	s.outbound.RemoveAt(lowestIdx)
	s.outbound.PushBack(evt)
	return true
}

func (s *SessionActor) onDrop(reason string) bool {
	metrics.IncWSDrops(reason)
	s.dropCount++
	threshold := s.cfg.SlowClientDropThreshold
	if threshold <= 0 || s.dropCount < threshold {
		return false
	}

	metrics.IncWSDrops("slow_client_disconnect")
	s.logger.Warn(
		"delivery session: slow client disconnected after drop threshold breach",
		"client_id", s.cfg.ClientID,
		"session_id", s.session.ID(),
		"drops", s.dropCount,
		"threshold", threshold,
		"reason", reason,
	)
	s.closeSession()
	return true
}

func (s *SessionActor) eventPriority(eventType string) int {
	if s.priorities == nil {
		return 0
	}
	// eventType arrives pre-normalized (lowercase, trimmed) from the ingest
	// pipeline via envelope.Validate(). Priorities map keys are also lowercase
	// (built in ensureDefaults). Direct lookup avoids per-event string allocs.
	return s.priorities[eventType]
}

func (s *SessionActor) flushOutbound() {
	if s.closed {
		s.flushing = false
		return
	}
	evt, ok := s.outbound.PopFront()
	if !ok {
		s.flushing = false
		metrics.SetWSQueueDepth(0)
		return
	}
	metrics.SetWSQueueDepth(s.outbound.Len())

	started := time.Now()
	if err := s.writeDeliveryEvent(evt); err != nil {
		s.logger.Warn("delivery session: write failed", "err", err)
		s.closeSession()
		return
	}
	metrics.ObserveWSSendLatency(time.Since(started))

	if s.outbound.Len() > 0 {
		s.engine.Send(s.self, sessionFlushOutbound{})
		return
	}
	s.flushing = false
}

func (s *SessionActor) writeDeliveryEvent(evt DeliveryEvent) *problem.Problem {
	if s.cfg.PreferProto && contracts.ProtoRolloutEnabledForEventType(evt.Env.Type) {
		raw, p := contracts.MarshalEnvelopeV1FromDomain(evt.Env)
		if p != nil {
			return p
		}
		if err := s.writeProtoDirect(websocket.BinaryMessage, raw); err != nil {
			return problem.Wrap(err, problem.Internal, "proto write failed")
		}
		observability.IncDeliveryProto()
		return nil
	}
	payload := evt.Env.Payload
	if evt.Env.ContentType == envelope.ContentTypeProto {
		if s.cfg.TranscodeCache != nil {
			cached, p := s.cfg.TranscodeCache.TranscodeProtoToJSON(
				evt.Env.Type, evt.Env.Version, evt.Env.ContentType, payload,
			)
			if p != nil {
				return p
			}
			payload = cached
		} else {
			decoded, p := codec.DecodePayload(evt.Env.Type, evt.Env.Version, evt.Env.ContentType, payload)
			if p != nil {
				return p
			}
			transcoded, err := json.Marshal(decoded)
			if err != nil {
				return problem.Wrap(err, problem.Internal, "proto→json transcode failed")
			}
			payload = json.RawMessage(transcoded)
		}
	}
	if err := s.writeJSONDirect(wsEventFrame{
		Type:     "event",
		Subject:  evt.Subject.String(),
		Seq:      evt.Env.Seq,
		TsIngest: evt.Env.TsIngest,
		Payload:  payload,
	}); err != nil {
		return problem.Wrap(err, problem.Internal, "json write failed")
	}
	observability.IncDeliveryJSON()
	return nil
}

func (s *SessionActor) writeProblem(op, requestID string, p *problem.Problem) {
	if p == nil {
		return
	}
	s.writeJSON(wsErrorFrame{
		Type:      "error",
		Op:        op,
		RequestID: requestID,
		Problem: wsErrorProblem{
			Code:    string(p.Code),
			Message: p.Message,
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
