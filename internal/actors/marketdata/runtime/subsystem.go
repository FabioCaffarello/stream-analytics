package mdruntime

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/market-raccoon/internal/actors/marketdata/ws"
	runtime "github.com/market-raccoon/internal/actors/runtime"
	"github.com/market-raccoon/internal/core/marketdata/app"
	"github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

// SubsystemConfig configures the MarketDataSubsystemActor.
type SubsystemConfig struct {
	// Subsystem is the guardian subsystem key for this actor instance.
	// Default: runtime.SubsystemMarketData.
	Subsystem runtime.Subsystem

	// Logger is used for structured logging.  Defaults to slog.Default().
	Logger *slog.Logger

	// Service is the marketdata BC facade.  Required.
	Service *app.MarketDataService

	// ParseMessage converts a raw WS message into an IngestRequest.
	// If nil, all messages are silently skipped (safe default for tests that
	// inject messages directly without a live exchange connection).
	ParseMessage ParseFunc

	// ParseMessageV2 is an optional parser with telemetry metadata.
	// When provided, it takes precedence over ParseMessage.
	ParseMessageV2 ParseFuncV2

	// ManagerConfig defines how the ws.Manager pools connections.
	// When non-nil, the actor spawns a ws.Manager child on startup.
	// When nil, no manager is spawned; the caller is responsible for feeding
	// messages directly (useful in tests and cmd/processor).
	ManagerConfig *ws.ManagerConfig

	// OnStarted is called with the actor's own PID immediately after startup.
	// It is invoked synchronously within the actor's mailbox, so it must not
	// block.  Useful in cmd wiring to obtain the PID for direct message
	// injection (e.g., fake feed goroutine).
	OnStarted func(selfPID *actor.PID)

	// spawnManagerFn overrides the ws.Manager spawn for testing.
	// Unexported; set via WithSpawnManagerFn option.
	spawnManagerFn func(c *actor.Context, selfPID *actor.PID) *actor.PID

	// BackpressureBufferSize is the bounded queue size between WS and ingest.
	BackpressureBufferSize int
	// BackpressurePolicy controls eviction strategy when queue is full.
	BackpressurePolicy string
}

// SubsystemActor bridges the ws.Manager actor layer with the core
// marketdata ingest use case.
//
// Message protocol (received):
//   - actor.Started  — spawns ws.Manager child if plan is configured.
//   - *ws.WsMessage  — parses and ingests via core use case.
//   - *ws.WsError    — forwards as runtime.ChildFailed to parent (Guardian).
//   - *ws.WsState    — logs lifecycle transitions.
//   - actor.Stopped  — no-op; engine cleans up children.
type SubsystemActor struct {
	cfg        SubsystemConfig
	logger     *slog.Logger
	managerPID *actor.PID
	telemetry  *parserTelemetry
	queue      *wsQueue
	engine     *actor.Engine
	selfPID    *actor.PID

	lastMessageAt   time.Time
	lastPublishAt   time.Time
	wsConnected     bool
	wsConnectedByID map[string]bool
	backpressureOn  bool
	lastHeartbeatAt time.Time
}

type publishTick struct {
	At time.Time
}

// NewSubsystemActor returns a hollywood actor.Producer for the
// MarketDataSubsystemActor using the given config.
func NewSubsystemActor(cfg SubsystemConfig) actor.Producer {
	return func() actor.Receiver {
		return &SubsystemActor{cfg: cfg}
	}
}

// WithSpawnManagerFn is a test-only option that overrides the manager spawn
// function.  Pass nil to suppress manager spawning entirely.
func WithSpawnManagerFn(fn func(c *actor.Context, selfPID *actor.PID) *actor.PID) func(*SubsystemConfig) {
	return func(cfg *SubsystemConfig) {
		cfg.spawnManagerFn = fn
	}
}

// Receive handles actor messages.
func (s *SubsystemActor) Receive(c *actor.Context) {
	s.ensureDefaults()
	switch msg := c.Message().(type) {
	case actor.Initialized:
		// nothing; engine lifecycle preamble.
	case actor.Started:
		s.onStarted(c)
	case actor.Stopped:
		if s.queue != nil {
			s.queue.Close()
		}
	case *ws.WsMessage:
		s.handleMessage(c, msg)
	case *ws.WsError:
		s.handleError(c, msg)
	case *ws.WsState:
		s.handleState(c, msg)
	case publishTick:
		s.lastPublishAt = msg.At
		s.emitSubsystemHeartbeat(c, false)
	default:
		s.logger.Warn("mdruntime: unknown message", "type", fmt.Sprintf("%T", msg))
	}
}

// ensureDefaults fills in zero-value fields.
func (s *SubsystemActor) ensureDefaults() {
	if s.cfg.Subsystem == "" {
		s.cfg.Subsystem = runtime.SubsystemMarketData
	}
	if s.logger == nil {
		if s.cfg.Logger != nil {
			s.logger = s.cfg.Logger
		} else {
			s.logger = slog.Default()
		}
	}
	if s.telemetry == nil {
		s.telemetry = newParserTelemetry()
	}
	if s.queue == nil {
		policy := normalizeBackpressurePolicy(s.cfg.BackpressurePolicy)
		s.queue = newWSQueue(s.cfg.BackpressureBufferSize, policy)
	}
	if s.wsConnectedByID == nil {
		s.wsConnectedByID = make(map[string]bool)
	}
}

func (s *SubsystemActor) onStarted(c *actor.Context) {
	s.engine = c.Engine()
	s.selfPID = c.PID()
	s.logger.Info("mdruntime: subsystem started")
	if s.cfg.OnStarted != nil {
		s.cfg.OnStarted(c.PID())
	}
	go s.runIngestWorker()

	spawnFn := s.cfg.spawnManagerFn
	if spawnFn == nil && s.cfg.ManagerConfig != nil {
		spawnFn = s.defaultSpawnManager
	}
	if spawnFn == nil {
		s.logger.Debug("mdruntime: no manager plan configured — operating without ws.Manager")
		return
	}
	s.managerPID = spawnFn(c, c.PID())
	if s.managerPID != nil {
		s.logger.Info("mdruntime: ws.Manager spawned", "pid", s.managerPID.String())
	}
}

func (s *SubsystemActor) defaultSpawnManager(c *actor.Context, selfPID *actor.PID) *actor.PID {
	cfg := *s.cfg.ManagerConfig
	cfg.SendTo = selfPID

	pid := c.SpawnChild(
		ws.NewManager(cfg),
		"ws-manager",
		actor.WithID("ws-manager"),
	)
	return pid
}

func (s *SubsystemActor) handleMessage(c *actor.Context, msg *ws.WsMessage) {
	s.lastMessageAt = msg.RecvAt
	s.emitSubsystemHeartbeat(c, false)
	dropped, entered := s.queue.Enqueue(msg)
	metrics.SetBackpressureQueueDepth(msg.Exchange, s.queue.Len())
	if entered && !s.backpressureOn {
		s.backpressureOn = true
		s.logger.Warn("mdruntime: entering backpressure mode",
			"policy", normalizeBackpressurePolicy(s.cfg.BackpressurePolicy),
			"buffer_size", s.cfg.BackpressureBufferSize,
		)
	}
	if dropped > 0 {
		s.telemetry.recordBackpressureDrops(uint64(dropped))
		metrics.IncBackpressureDrops(string(normalizeBackpressurePolicy(s.cfg.BackpressurePolicy)), dropped)
	}
}

func (s *SubsystemActor) runIngestWorker() {
	for {
		msg, ok := s.queue.Pop()
		if !ok {
			return
		}
		s.processMessage(msg)
	}
}

func (s *SubsystemActor) processMessage(msg *ws.WsMessage) {
	if s.cfg.ParseMessageV2 == nil && s.cfg.ParseMessage == nil {
		metrics.IncWSMessageReceived(msg.Exchange, "unknown")
		metrics.ObserveIngest(msg.Exchange, "UNKNOWN", "unknown", "validation_failed", 0)
		s.logger.Debug("mdruntime: no ParseMessage configured — dropping message",
			"exchange", msg.Exchange,
			"endpoint", msg.Endpoint,
			"bytes", len(msg.Data),
		)
		s.telemetry.recordSkip(msg.Exchange, "unknown", "parse_nil", "", "", "")
		s.logProgress()
		return
	}

	var (
		req  app.IngestRequest
		skip bool
		meta ParseMeta
	)
	if s.cfg.ParseMessageV2 != nil {
		req, skip, meta = s.cfg.ParseMessageV2(msg)
	} else {
		req, skip = s.cfg.ParseMessage(msg)
		meta = ParseMeta{EventType: req.EventType}
		if skip {
			meta.SkipReason = "skip_unspecified"
		}
	}

	if skip {
		eventType := meta.EventType
		if eventType == "" {
			eventType = "unknown"
		}
		instrument := req.Instrument
		if instrument == "" {
			instrument = "UNKNOWN"
		}
		metrics.IncWSMessageReceived(msg.Exchange, eventType)
		metrics.ObserveIngest(msg.Exchange, instrument, eventType, "validation_failed", 0)
		s.telemetry.recordSkip(msg.Exchange, meta.EventType, meta.SkipReason, meta.ProblemCode, meta.Ticker, meta.WSStream)
		if meta.SkipReason == "parse_error" && s.telemetry.shouldSample(time.Now(), meta.ProblemCode) {
			s.logger.Warn("mdruntime: parse skip sampled",
				"exchange", msg.Exchange,
				"bucket_id", msg.BucketID,
				"consumer_id", msg.ConsumerID,
				"endpoint", msg.Endpoint,
				"event_type", normalizeLabel(meta.EventType, "unknown"),
				"skip_reason", normalizeLabel(meta.SkipReason, "skip_unspecified"),
				"problem_code", normalizeLabel(meta.ProblemCode, "none"),
				"problem_message", meta.ProblemMessage,
				"payload_sample", truncatePayload(msg.Data, 256),
			)
		}
		s.logProgress()
		return
	}

	if req.EventType == "marketdata.bookdelta" {
		if depth, ok := req.Payload.(domain.BookDeltaV1); ok && depth.FirstID > 0 && depth.FinalID > 0 {
			if gap, lastFinal := s.telemetry.recordDepthSequence(req.Instrument, depth.FirstID, depth.FinalID); gap {
				s.logger.Warn("mdruntime: depth gap detected",
					"instrument", req.Instrument,
					"first_update_id", depth.FirstID,
					"final_update_id", depth.FinalID,
					"last_final_update_id", lastFinal,
				)
			}
		}
	}

	startedAt := time.Now()
	res := s.cfg.Service.Ingest.Execute(context.Background(), req)
	metrics.IngestStreamsActive.Set(float64(s.cfg.Service.Ingest.ActiveStreams()))
	if res.IsFail() {
		p := res.Problem()
		status := "failed"
		switch p.Code {
		case problem.Duplicate:
			status = "duplicate"
		case problem.OutOfOrder:
			status = "out_of_order"
		case problem.ValidationFailed, problem.InvalidArgument:
			status = "validation_failed"
		}
		metrics.IncWSMessageReceived(msg.Exchange, req.EventType)
		metrics.ObserveIngest(msg.Exchange, req.Instrument, req.EventType, status, time.Since(startedAt))
		s.logger.Warn("mdruntime: ingest failed",
			"code", p.Code,
			"msg", p.Message,
			"exchange", msg.Exchange,
			"endpoint", msg.Endpoint,
			"retryable", p.Retryable,
		)
		return
	}

	wsStream := req.Metadata["ws_stream"]
	ticker := req.Instrument
	if req.Metadata["instrument_pair"] != "" {
		ticker = req.Metadata["instrument_pair"]
	}
	metrics.IncWSMessageReceived(msg.Exchange, req.EventType)
	metrics.ObserveIngest(msg.Exchange, req.Instrument, req.EventType, "ok", time.Since(startedAt))
	s.telemetry.recordIngest(req.EventType, ticker, wsStream)
	s.logProgress()
	if s.engine != nil && s.selfPID != nil {
		s.engine.Send(s.selfPID, publishTick{At: time.Now()})
	}

	resp := res.Value()
	s.logger.Debug("mdruntime: ingested",
		"topic", resp.Published.Topic,
		"seq", resp.Seq,
		"exchange", msg.Exchange,
	)
}

func (s *SubsystemActor) logProgress() {
	if s.telemetry.shouldEmitProgress() {
		s.logger.Info("mdruntime: message counters",
			"total", s.telemetry.total,
			"ingested", s.telemetry.ingested,
			"skipped", s.telemetry.skipped,
			"by_event", s.telemetry.byEvent,
			"skip_by_reason", s.telemetry.bySkipReason,
			"skip_by_exchange_event_reason", s.telemetry.byExchangeEventAndSkip,
			"parse_error_by_code", s.telemetry.parseErrorsByProblemCode,
			"depth_gaps_total", s.telemetry.depthGapsTotal,
			"depth_gaps_by_symbol", s.telemetry.depthGapsBySymbol,
			"ws_backpressure_drops_total", s.telemetry.backpressureDropsTotal,
			"ws_reconnect_total", s.telemetry.wsReconnectTotal,
			"ws_disconnect_reason", s.telemetry.wsDisconnectByReason,
			"ws_connection_uptime_seconds", s.telemetry.wsConnectionUptimeSecs,
			"top_ws_streams", s.telemetry.topWSStreams(5),
			"top_ticker_share_pct", s.telemetry.topTickerSharePercent(5),
		)
	}
}

func (s *SubsystemActor) handleError(c *actor.Context, msg *ws.WsError) {
	metrics.IncWSError(msg.Exchange, msg.Kind)
	s.logger.Error("mdruntime: ws error",
		"exchange", msg.Exchange,
		"bucket_id", msg.BucketID,
		"consumer_id", msg.ConsumerID,
		"endpoint", msg.Endpoint,
		"kind", msg.Kind,
		"err", msg.Err,
	)

	if !isEscalationWorthyWsError(msg.Kind) {
		s.logger.Warn("mdruntime: transient ws error; keeping subsystem alive",
			"kind", msg.Kind,
			"exchange", msg.Exchange,
			"bucket_id", msg.BucketID,
			"consumer_id", msg.ConsumerID,
		)
		return
	}

	if c.Parent() == nil {
		return
	}
	c.Send(c.Parent(), runtime.ChildFailed{
		Subsystem: s.cfg.Subsystem,
		Kind:      msg.Kind,
		Err:       msg.Err,
	})
}

func (s *SubsystemActor) handleState(c *actor.Context, msg *ws.WsState) {
	if msg.Status == "reconnecting" {
		s.telemetry.recordReconnect(msg.Reason, msg.UptimeSec)
		metrics.IncWSReconnect(msg.Exchange, msg.Reason)
	}

	switch msg.Status {
	case "connected", "subscribed":
		s.wsConnectedByID[msg.ConsumerID] = true
	case "error", "closed":
		delete(s.wsConnectedByID, msg.ConsumerID)
	}
	s.wsConnected = len(s.wsConnectedByID) > 0
	metrics.SetWSConnectionsActive(msg.Exchange, len(s.wsConnectedByID))
	s.emitSubsystemHeartbeat(c, true)

	// Avoid log spam for high-frequency reconnects; log at appropriate levels.
	switch msg.Status {
	case "connected", "subscribed":
		s.logger.Info("mdruntime: ws state",
			"status", msg.Status,
			"exchange", msg.Exchange,
			"bucket_id", msg.BucketID,
			"consumer_id", msg.ConsumerID,
			"endpoint", msg.Endpoint,
		)
	case "error":
		s.logger.Error("mdruntime: ws state error",
			"status", msg.Status,
			"exchange", msg.Exchange,
			"bucket_id", msg.BucketID,
			"consumer_id", msg.ConsumerID,
			"endpoint", msg.Endpoint,
			"err", msg.Err,
		)
	default:
		s.logger.Debug("mdruntime: ws state",
			"status", msg.Status,
			"exchange", msg.Exchange,
			"bucket_id", msg.BucketID,
			"consumer_id", msg.ConsumerID,
			"endpoint", msg.Endpoint,
		)
	}
}

func (s *SubsystemActor) emitSubsystemHeartbeat(c *actor.Context, force bool) {
	if c == nil || c.Parent() == nil {
		return
	}
	now := time.Now()
	if !force && !s.lastHeartbeatAt.IsZero() && now.Sub(s.lastHeartbeatAt) < time.Second {
		return
	}
	s.lastHeartbeatAt = now
	c.Send(c.Parent(), runtime.SubsystemHeartbeat{
		Subsystem:     s.cfg.Subsystem,
		Connected:     s.wsConnected,
		LastMessageAt: s.lastMessageAt,
		LastPublishAt: s.lastPublishAt,
	})
}

func isEscalationWorthyWsError(kind string) bool {
	switch kind {
	case "dial", "subscribe", "read", "pingpong", "heartbeat":
		return false
	default:
		return true
	}
}

func truncatePayload(b []byte, max int) string {
	if len(b) == 0 {
		return ""
	}
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "...(truncated)"
}
