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
)

// SubsystemConfig configures the MarketDataSubsystemActor.
type SubsystemConfig struct {
	// Logger is used for structured logging.  Defaults to slog.Default().
	Logger *slog.Logger

	// Ingest is the marketdata ingest use case.  Required.
	Ingest *app.IngestMarketData

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
		// engine handles child cleanup; nothing to do.
	case *ws.WsMessage:
		s.handleMessage(c, msg)
	case *ws.WsError:
		s.handleError(c, msg)
	case *ws.WsState:
		s.handleState(msg)
	default:
		s.logger.Warn("mdruntime: unknown message", "type", fmt.Sprintf("%T", msg))
	}
}

// ensureDefaults fills in zero-value fields.
func (s *SubsystemActor) ensureDefaults() {
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
}

func (s *SubsystemActor) onStarted(c *actor.Context) {
	s.logger.Info("mdruntime: subsystem started")
	if s.cfg.OnStarted != nil {
		s.cfg.OnStarted(c.PID())
	}

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
	if s.cfg.ParseMessageV2 == nil && s.cfg.ParseMessage == nil {
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

	res := s.cfg.Ingest.Execute(context.Background(), req)
	if res.IsFail() {
		p := res.Problem()
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
	s.telemetry.recordIngest(req.EventType, ticker, wsStream)
	s.logProgress()

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
			"top_ws_streams", s.telemetry.topWSStreams(5),
			"top_ticker_share_pct", s.telemetry.topTickerSharePercent(5),
		)
	}
}

func (s *SubsystemActor) handleError(c *actor.Context, msg *ws.WsError) {
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
		Subsystem: runtime.SubsystemMarketData,
		Kind:      msg.Kind,
		Err:       msg.Err,
	})
}

func (s *SubsystemActor) handleState(msg *ws.WsState) {
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
