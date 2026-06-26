package deliveryruntime

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/core/delivery/app"
	"github.com/FabioCaffarello/stream-analytics/internal/core/delivery/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/core/delivery/ports"
	sharedclock "github.com/FabioCaffarello/stream-analytics/internal/shared/clock"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/metrics"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/observability"
	"github.com/anthdm/hollywood/actor"
)

// ── Constants ───────────────────────────────────────────────────────────────

const readLimitBytes = 64 * 1024
const wsKeepalivePingInterval = 20 * time.Second
const wsMetricsCadence = 5 * time.Second
const wsFlushBatchSize = 32
const wsCompressThresholdBytes = 1024

const (
	defaultRangeLimit      = 100
	maxLimit               = 1000
	maxPage                = 100
	maxQueryLimit          = 20000
	maxResponseItems       = 1000
	subscribeBackfillLimit = 64
)

// ── Interfaces ──────────────────────────────────────────────────────────────

type wsConn interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteJSON(v any) error
	WriteMessage(messageType int, data []byte) error
	SetReadLimit(limit int64)
	SetReadDeadline(t time.Time) error
	SetPongHandler(h func(string) error)
	Close() error
}

// HotSnapshotProvider returns latest snapshot bytes for a subject.
type HotSnapshotProvider interface {
	GetLatest(subject domain.Subject) ([]byte, bool)
}

// ── SessionConfig ───────────────────────────────────────────────────────────

type SessionConfig struct {
	Logger              *slog.Logger
	RouterPID           *actor.PID
	Conn                wsConn
	ClientID            string
	TenantID            string
	RangeStore          ports.RangeStore
	ServerInstanceID    string
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
	// MaxSubscriptions bounds active subscriptions per websocket session.
	// 0 disables the limit.
	MaxSubscriptions int
	// MaxSignalSubscriptions bounds active signal subscriptions per session.
	// 0 disables the signal-specific limit.
	MaxSignalSubscriptions int
	// MaxSymbolsPerConnection bounds active unique symbols per session.
	// 0 disables the limit.
	MaxSymbolsPerConnection int
	// MaxFrameBytes is the maximum outbound frame size in bytes.
	// 0 defaults to readLimitBytes.
	MaxFrameBytes int
	// KeepaliveInterval controls periodic websocket pings.
	// 0 defaults to wsKeepalivePingInterval.
	KeepaliveInterval time.Duration
	// MetricsCadence controls periodic metrics frame emission.
	// 0 defaults to wsMetricsCadence.
	MetricsCadence time.Duration
	// Clock is optional; defaults to SystemClock.
	Clock sharedclock.Clock
	// TranscodeCache is an optional shared proto→JSON transcode cache.
	TranscodeCache *TranscodeCache
	// SnapshotWireCache caches pre-encoded snapshot frames shared across sessions.
	SnapshotWireCache *SnapshotWireCache
	// CompressionEnabled toggles websocket write compression support.
	CompressionEnabled bool
	// RequireClientHello gates subscribe/resync/getrange behind a client hello.
	// When true, these commands are rejected until the client sends a hello frame.
	RequireClientHello bool
	// OnClosed is invoked once when session is closed.
	OnClosed func()
}

// ── SessionActor ────────────────────────────────────────────────────────────

type SessionActor struct {
	cfg SessionConfig

	logger *slog.Logger
	engine *actor.Engine
	self   *actor.PID

	session *domain.Session
	service *app.SessionService

	limits   EffectiveLimits
	features NegotiatedFeatures

	readerCtx    context.Context
	cancelReader context.CancelFunc
	started      bool
	closed       bool
	outbound     *deliveryRing
	flushing     bool
	policy       domain.BackpressurePolicy
	priorities   map[string]int
	rateLimiter  *RateLimiter
	dropCount    int
	helloSeen    bool
	lastLagMs    int64
	messagesOut  int64
	lastSnapshot map[string]sessionSnapshotEntry

	// Per-subject snapshot counter (1-indexed, incremented on each snapshot emit).
	snapshotSeq map[string]int64
	// Per-subject last delivered event seq for prev_seq chaining.
	lastDeliveredSeq map[string]int64
	// deferredSnapshotSubjects tracks subjects where snapshot was unavailable at
	// subscribe time. The snapshot is re-attempted before the first event delivery.
	deferredSnapshotSubjects map[string]struct{}
	// Per-session resync counter (total resyncs across all subjects).
	resyncCount int64

	// Peak queue depth since last metrics emission (reset after each emission).
	queueHighWatermark int
	bpStrategy         backpressureStrategy
	bpDropSamples      map[backpressureDropSampleKey]int
	bpDropSampleDrops  int
	bpDropSampleWindow int

	compressBuf    bytes.Buffer
	compressWriter *flate.Writer

	batchPrepared []preparedBatchEvent
	batchItems    []wsBatchItem
}

type sessionKeepaliveTick struct{}
type sessionMetricsTick struct{}

type sessionSnapshotEntry struct {
	Seq      int64
	TsServer int64
	Venue    string
	Symbol   string
	Channel  string
	Payload  json.RawMessage
}

type preparedBatchEvent struct {
	subject  domain.Subject
	env      envelope.Envelope
	channel  string
	seq      int64
	prevSeq  int64
	tsIngest int64
	tsServer int64
	payload  json.RawMessage
}

// ── Constructor ─────────────────────────────────────────────────────────────

func NewSessionActor(cfg SessionConfig) actor.Producer {
	return func() actor.Receiver {
		return &SessionActor{cfg: cfg}
	}
}

// ── Receive ─────────────────────────────────────────────────────────────────

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
	case sessionMetricsTick:
		s.handleMetricsTick()
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

// ── Init / lifecycle ────────────────────────────────────────────────────────

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
		s.logger = s.logger.With(
			"connection_id", s.session.ID().String(),
			"user_id", strings.TrimSpace(s.cfg.ClientID),
			"tenant_id", strings.TrimSpace(s.cfg.TenantID),
		)
	}
	if s.service == nil {
		s.service = app.NewSessionService(s.cfg.RangeStore)
	}
	if s.limits.MaxFrameBytes <= 0 {
		s.limits = NewEffectiveLimits(s.cfg)
	}
	if s.outbound == nil {
		s.outbound = newDeliveryRing(s.limits.OutboundQueueSize)
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
	if s.bpStrategy.sampleFlushEvery <= 0 {
		s.bpStrategy = defaultBackpressureStrategy()
	}
	if s.bpDropSamples == nil {
		s.bpDropSamples = make(map[backpressureDropSampleKey]int)
	}
	if strings.TrimSpace(s.cfg.ServerInstanceID) == "" {
		s.cfg.ServerInstanceID = "unknown"
	}
	if s.rateLimiter == nil && s.limits.RateLimit.Enabled {
		burst := s.limits.RateLimit.BurstCapacity
		if burst <= 0 {
			burst = 200
		}
		rate := s.limits.RateLimit.MaxPerSecond
		if rate <= 0 {
			rate = 100
		}
		s.rateLimiter = NewRateLimiter(burst, rate, s.cfg.Clock)
	}
	if s.lastSnapshot == nil {
		s.lastSnapshot = make(map[string]sessionSnapshotEntry)
	}
	if s.snapshotSeq == nil {
		s.snapshotSeq = make(map[string]int64)
	}
	if s.lastDeliveredSeq == nil {
		s.lastDeliveredSeq = make(map[string]int64)
	}
	if s.deferredSnapshotSubjects == nil {
		s.deferredSnapshotSubjects = make(map[string]struct{})
	}
	if s.compressWriter == nil {
		if w, err := flate.NewWriter(&s.compressBuf, flate.BestSpeed); err == nil {
			s.compressWriter = w
		}
	}
	if s.batchPrepared == nil {
		s.batchPrepared = make([]preparedBatchEvent, wsFlushBatchSize)
	}
	if s.batchItems == nil {
		s.batchItems = make([]wsBatchItem, wsFlushBatchSize)
	}
	s.limits.EmitMetrics()
	metrics.SetWSQueueCapacity(s.limits.OutboundQueueSize)
	metrics.SetWSBackpressureLevel(0)
	metrics.SetWSQueueHighWatermark(0)
}

func (s *SessionActor) onStarted() {
	s.started = true
	metrics.IncWSClientsConnected()
	metrics.IncWSTenantConnectionsActive(s.cfg.TenantID)
	observability.IncSessionsActive()
	if s.cfg.PreferProto {
		observability.IncPreferProtoSessions()
	}
	if s.cfg.RouterPID != nil {
		s.engine.Send(s.cfg.RouterPID, RegisterSession{SessionID: s.session.ID(), PID: s.self})
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
	s.emitHello()
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
	pingTicker := time.NewTicker(time.Duration(s.limits.KeepaliveIntervalMs) * time.Millisecond)
	metricsTicker := time.NewTicker(time.Duration(s.limits.MetricsCadenceMs) * time.Millisecond)
	defer pingTicker.Stop()
	defer metricsTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-pingTicker.C:
			if s.engine != nil && s.self != nil {
				s.engine.Send(s.self, sessionKeepaliveTick{})
			}
		case <-metricsTicker.C:
			if s.engine != nil && s.self != nil {
				s.engine.Send(s.self, sessionMetricsTick{})
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
			if messageType != 1 { // websocket.TextMessage == 1
				continue
			}
			s.engine.Send(s.self, sessionInboundText{Data: data})
		}
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
	s.flushBackpressureDropSamples(true)
	if s.started {
		metrics.DecWSClientsConnected()
		metrics.DecWSTenantConnectionsActive(s.cfg.TenantID)
		observability.DecSessionsActive()
		if s.cfg.PreferProto {
			observability.DecPreferProtoSessions()
		}
		metrics.SetWSQueueDepth(0)
		metrics.SetWSTenantQueueDepth(s.cfg.TenantID, 0)
		metrics.SetWSBackpressureLevel(0)
		metrics.SetWSQueueHighWatermark(0)
		for _, sub := range s.session.Subscriptions() {
			if sub.Subject.IsSignal() {
				metrics.DecMRSignalWSActiveSubscriptions()
			}
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
	}
	if s.cfg.Conn != nil {
		_ = s.cfg.Conn.Close()
	}
	if s.cfg.OnClosed != nil {
		s.cfg.OnClosed()
	}
	s.lastSnapshot = nil
	s.snapshotSeq = nil
	s.lastDeliveredSeq = nil
	s.deferredSnapshotSubjects = nil
}
