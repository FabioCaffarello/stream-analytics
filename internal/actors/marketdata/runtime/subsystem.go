package mdruntime

import (
	"context"
	"fmt"
	"log/slog"

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
	if s.cfg.ParseMessage == nil {
		s.logger.Debug("mdruntime: no ParseMessage configured — dropping message",
			"exchange", msg.Exchange,
			"endpoint", msg.Endpoint,
			"bytes", len(msg.Data),
		)
		return
	}

	req, skip := s.cfg.ParseMessage(msg)
	if skip {
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

	resp := res.Value()
	s.logger.Debug("mdruntime: ingested",
		"topic", resp.Published.Topic,
		"seq", resp.Seq,
		"exchange", msg.Exchange,
	)
}

func (s *SubsystemActor) handleError(c *actor.Context, msg *ws.WsError) {
	s.logger.Error("mdruntime: ws error",
		"exchange", msg.Exchange,
		"endpoint", msg.Endpoint,
		"kind", msg.Kind,
		"err", msg.Err,
	)

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
			"endpoint", msg.Endpoint,
		)
	case "error":
		s.logger.Error("mdruntime: ws state error",
			"status", msg.Status,
			"exchange", msg.Exchange,
			"endpoint", msg.Endpoint,
			"err", msg.Err,
		)
	default:
		s.logger.Debug("mdruntime: ws state",
			"status", msg.Status,
			"exchange", msg.Exchange,
			"endpoint", msg.Endpoint,
		)
	}
}
