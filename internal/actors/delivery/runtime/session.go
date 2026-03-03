package deliveryruntime

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
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
	deliveryv1 "github.com/market-raccoon/internal/shared/proto/gen/delivery/v1"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const readLimitBytes = 64 * 1024
const wsKeepalivePingInterval = 20 * time.Second
const wsMetricsCadence = 5 * time.Second
const wsProtocolVersion = 1

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
	TenantID   string
	RangeStore ports.RangeStore
	// ServerInstanceID identifies the running server process in protocol frames.
	ServerInstanceID string
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
	// MaxSubscriptions bounds active subscriptions per websocket session.
	// 0 disables the limit.
	MaxSubscriptions int
	// MaxSymbolsPerConnection bounds active unique symbols per session.
	// 0 disables the limit.
	MaxSymbolsPerConnection int
	// MaxFrameBytes is the maximum outbound frame size in bytes.
	// 0 defaults to readLimitBytes.
	MaxFrameBytes int
	// Clock is optional; defaults to SystemClock.
	Clock sharedclock.Clock
	// TranscodeCache is an optional shared proto→JSON transcode cache.
	// When set, proto payloads destined for JSON clients are cached to avoid
	// redundant decode+marshal across sessions receiving the same event.
	TranscodeCache *TranscodeCache
	// OnClosed is invoked once when session is closed.
	OnClosed func()
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
	helloSeen    bool
	lastLagMs    int64
	messagesOut  int64
	lastSnapshot map[string]sessionSnapshotEntry

	// F3: per-subject snapshot counter (1-indexed, incremented on each snapshot emit).
	snapshotSeq map[string]int64
	// F3: per-subject last delivered event seq for prev_seq chaining.
	lastDeliveredSeq map[string]int64

	// F4: client-advertised requested features from ClientHello.
	clientFeatures []string
	// F4: resolved max frame bytes for proto frame size guard.
	maxFrameBytes int

	// F5: peak queue depth since last metrics emission (reset after each emission).
	queueHighWatermark int
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
	if strings.TrimSpace(s.cfg.ServerInstanceID) == "" {
		s.cfg.ServerInstanceID = "unknown"
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
	if s.lastSnapshot == nil {
		s.lastSnapshot = make(map[string]sessionSnapshotEntry)
	}
	if s.snapshotSeq == nil {
		s.snapshotSeq = make(map[string]int64)
	}
	if s.lastDeliveredSeq == nil {
		s.lastDeliveredSeq = make(map[string]int64)
	}
	if s.maxFrameBytes <= 0 {
		s.maxFrameBytes = s.cfg.MaxFrameBytes
		if s.maxFrameBytes <= 0 {
			s.maxFrameBytes = readLimitBytes
		}
	}
}

func (s *SessionActor) onStarted() {
	if s.cfg.RouterPID != nil {
		s.engine.Send(s.cfg.RouterPID, RegisterSession{SessionID: s.session.ID(), PID: s.self})
	}
	metrics.IncWSClientsConnected()
	metrics.IncWSTenantConnectionsActive(s.cfg.TenantID)
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

func (s *SessionActor) emitHello() {
	if s.cfg.Conn == nil {
		return
	}
	nowMs := time.Now().UnixMilli()
	if s.cfg.Clock != nil {
		nowMs = s.cfg.Clock.Now().UnixMilli()
	}
	var rl *wsHelloRateLimit
	if s.cfg.RateLimit.Enabled {
		rl = &wsHelloRateLimit{
			Enabled:       true,
			MaxPerSecond:  s.cfg.RateLimit.MaxPerSecond,
			BurstCapacity: s.cfg.RateLimit.BurstCapacity,
		}
	}
	queueSize := s.outboundCap
	if queueSize <= 0 {
		queueSize = 256
	}
	maxFrameBytes := s.cfg.MaxFrameBytes
	if maxFrameBytes <= 0 {
		maxFrameBytes = readLimitBytes
	}
	metrics.IncWSControlFrame("hello")
	s.writeJSON(wsHelloFrame{
		Type: "hello",
		Payload: wsHelloPayload{
			ProtoVer:        wsProtocolVersion,
			ProtocolVersion: wsProtocolVersion,
			ServerTime:      nowMs,
			ServerInstance:  s.cfg.ServerInstanceID,
			Capabilities: wsHelloCapabilities{
				Topics: []string{
					"marketdata.trade",
					"marketdata.bookdelta",
					"aggregation.snapshot",
					"aggregation.stats",
					"aggregation.candle",
					"insights.heatmap_snapshot",
					"insights.volume_profile_snapshot",
				},
				Venues: []string{
					"binance",
					"bybit",
					"coinbase",
					"kraken",
					"hyperliquid",
				},
				MaxSubscriptionsPerConn: s.cfg.MaxSubscriptions,
				MaxSymbolsPerConnection: s.cfg.MaxSymbolsPerConnection,
				MaxFrameBytes:           maxFrameBytes,
				OutboundQueueSize:       queueSize,
				MetricsCadenceMs:        int(wsMetricsCadence.Milliseconds()),
				KeepaliveIntervalMs:     int(wsKeepalivePingInterval.Milliseconds()),
				RateLimit:               rl,
				SupportedFeatures:       []string{"batching", "snapshot_hash", "prev_seq"},
			},
		},
	})
}

func (s *SessionActor) onStopped() {
	s.closeSession()
}

func (s *SessionActor) keepaliveLoop(ctx context.Context) {
	pingTicker := time.NewTicker(wsKeepalivePingInterval)
	metricsTicker := time.NewTicker(wsMetricsCadence)
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

func (s *SessionActor) handleMetricsTick() {
	if s.closed || s.cfg.Conn == nil || s.session == nil {
		return
	}
	snapshot := observability.SnapshotTerminalWSState(1)
	serializeErrors := saturatingUint64ToInt64(snapshot.SerializeErrorsTotal)
	if serializeErrors < 0 {
		serializeErrors = 0
	}
	resyncTotal := saturatingUint64ToInt64(snapshot.ResyncTotal)
	if resyncTotal < 0 {
		resyncTotal = 0
	}
	msgOut := s.messagesOut
	if msgOut < 0 {
		msgOut = 0
	}
	lag := s.lastLagMs
	if lag < 0 {
		lag = 0
	}
	bpLevel, bpAction := s.computeBackpressureLevel()
	hwm := s.queueHighWatermark
	s.queueHighWatermark = 0 // reset after emission
	metrics.IncWSControlFrame("metrics")
	s.writeJSON(wsMetricsFrame{
		Type: "metrics",
		Payload: wsMetricsPayload{
			WSDroppedTotal:            int64(s.dropCount),
			WSQueueLen:                s.outbound.Len(),
			WSLagMs:                   lag,
			PublishToDeliverLatencyMs: lag,
			SerializeErrorsTotal:      serializeErrors,
			ResyncTotal:               resyncTotal,
			ActiveSubscriptions:       len(s.session.Subscriptions()),
			MessagesOutTotal:          msgOut,
			BackpressureLevel:         bpLevel,
			RecommendedAction:         bpAction,
			QueueCapacity:             s.outboundCap,
			QueueHighWatermark:        hwm,
		},
	})
}

func (s *SessionActor) computeBackpressureLevel() (level int, action string) {
	if s.outboundCap <= 0 {
		return 0, "none"
	}
	ratio := float64(s.outbound.Len()) / float64(s.outboundCap)
	switch {
	case ratio >= 0.95:
		return 3, "reconnect"
	case ratio >= 0.75:
		return 2, "reduce_subscriptions"
	case ratio >= 0.50:
		return 1, "none"
	default:
		return 0, "none"
	}
}

type clientCommand struct {
	Type              string          `json:"type,omitempty"`
	Op                string          `json:"op,omitempty"`
	Subject           string          `json:"subject,omitempty"`
	StreamID          string          `json:"stream_id,omitempty"`
	RequestID         string          `json:"request_id,omitempty"`
	Venue             string          `json:"venue,omitempty"`
	Symbol            string          `json:"symbol,omitempty"`
	Channel           string          `json:"channel,omitempty"`
	Depth             uint32          `json:"depth,omitempty"`
	Aggregation       string          `json:"aggregation,omitempty"`
	LastSeq           int64           `json:"last_seq,omitempty"`
	TsClient          int64           `json:"ts_client,omitempty"`
	Params            json.RawMessage `json:"params,omitempty"`
	RequestedFeatures []string        `json:"requested_features,omitempty"`
}

// Pre-allocated typed structs for outbound JSON frames.
// These eliminate map[string]any allocations on the delivery hot path.

type wsAckFrame struct {
	Type      string `json:"type"`
	Op        string `json:"op"`
	RequestID string `json:"request_id"`
	Subject   string `json:"subject"`
}

type wsHelloRateLimit struct {
	Enabled       bool `json:"enabled"`
	MaxPerSecond  int  `json:"max_per_second,omitempty"`
	BurstCapacity int  `json:"burst_capacity,omitempty"`
}

type wsHelloCapabilities struct {
	Topics                  []string          `json:"topics"`
	Venues                  []string          `json:"venues"`
	Symbols                 []string          `json:"symbols,omitempty"`
	MaxSubscriptionsPerConn int               `json:"max_subscriptions_per_connection,omitempty"`
	MaxSymbolsPerConnection int               `json:"max_symbols_per_connection,omitempty"`
	MaxFrameBytes           int               `json:"max_frame_bytes,omitempty"`
	OutboundQueueSize       int               `json:"outbound_queue_size,omitempty"`
	MetricsCadenceMs        int               `json:"metrics_cadence_ms,omitempty"`
	KeepaliveIntervalMs     int               `json:"keepalive_interval_ms,omitempty"`
	RateLimit               *wsHelloRateLimit `json:"rate_limit,omitempty"`
	SupportedFeatures       []string          `json:"supported_features,omitempty"`
}

type wsHelloPayload struct {
	ProtoVer        int                 `json:"proto_ver"`
	ProtocolVersion int                 `json:"protocol_version"`
	ServerTime      int64               `json:"server_time"`
	ServerInstance  string              `json:"server_instance_id"`
	Capabilities    wsHelloCapabilities `json:"capabilities"`
}

type wsHelloFrame struct {
	Type    string         `json:"type"`
	Payload wsHelloPayload `json:"payload"`
}

type wsSnapshotFrame struct {
	Type             string          `json:"type"`
	Subject          string          `json:"subject"`
	StreamID         string          `json:"stream_id,omitempty"`
	ProtocolVersion  int             `json:"protocol_version,omitempty"`
	ServerInstanceID string          `json:"server_instance_id,omitempty"`
	Seq              int64           `json:"seq,omitempty"`
	TsServer         int64           `json:"ts_server,omitempty"`
	Venue            string          `json:"venue,omitempty"`
	Symbol           string          `json:"symbol,omitempty"`
	Channel          string          `json:"channel,omitempty"`
	Payload          json.RawMessage `json:"payload"`
	// SnapshotSource identifies server-side bootstrap source for subscribe
	// snapshot frames when synthesized from the hot snapshot provider.
	SnapshotSource string `json:"snapshot_source,omitempty"`
	// SnapshotSeq is per-session per-subject snapshot counter (1-indexed).
	SnapshotSeq int64 `json:"snapshot_seq,omitempty"`
	// WatermarkSeq is the highest confirmed upstream seq at snapshot time.
	WatermarkSeq int64 `json:"watermark_seq,omitempty"`
	// SnapshotHash is FNV-1a hex digest of payload for integrity checking.
	SnapshotHash string `json:"snapshot_hash,omitempty"`
}

type wsMetricsPayload struct {
	WSDroppedTotal            int64  `json:"ws_dropped_total"`
	WSQueueLen                int    `json:"ws_queue_len"`
	WSLagMs                   int64  `json:"ws_lag_ms"`
	PublishToDeliverLatencyMs int64  `json:"publish_to_deliver_latency_ms"`
	SerializeErrorsTotal      int64  `json:"serialize_errors_total"`
	ResyncTotal               int64  `json:"resync_total"`
	ActiveSubscriptions       int    `json:"active_subscriptions"`
	MessagesOutTotal          int64  `json:"messages_out_total"`
	BackpressureLevel         int    `json:"backpressure_level,omitempty"`
	RecommendedAction         string `json:"recommended_action,omitempty"`
	QueueCapacity             int    `json:"queue_capacity,omitempty"`
	QueueHighWatermark        int    `json:"queue_high_watermark,omitempty"`
}

type wsMetricsFrame struct {
	Type    string           `json:"type"`
	Payload wsMetricsPayload `json:"payload"`
}

type wsPongFrame struct {
	Type      string `json:"type"`
	Op        string `json:"op"`
	RequestID string `json:"request_id"`
	TsClient  int64  `json:"ts_client"`
	TsServer  int64  `json:"ts_server"`
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
	Type             string          `json:"type"`
	Subject          string          `json:"subject"`
	StreamID         string          `json:"stream_id"`
	ProtocolVersion  int             `json:"protocol_version"`
	ServerInstanceID string          `json:"server_instance_id"`
	Seq              int64           `json:"seq"`
	PrevSeq          int64           `json:"prev_seq,omitempty"`
	TsIngest         int64           `json:"ts_ingest"`
	TsServer         int64           `json:"ts_server"`
	Venue            string          `json:"venue"`
	Symbol           string          `json:"symbol"`
	Channel          string          `json:"channel"`
	Payload          json.RawMessage `json:"payload"`
}

type wsErrorProblem struct {
	Code       string `json:"code"`
	ErrorCode  string `json:"error_code,omitempty"`
	ActionHint string `json:"action_hint,omitempty"`
	Message    string `json:"message"`
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
	op := strings.ToLower(strings.TrimSpace(cmd.Op))
	if op == "" {
		op = strings.ToLower(strings.TrimSpace(cmd.Type))
	}
	switch op {
	case "hello":
		s.handleClientHello(cmd)
	case "subscribe":
		s.handleSubscribe(cmd)
	case "unsubscribe":
		s.handleUnsubscribe(cmd)
	case "ping":
		s.handlePing(cmd)
	case "resync":
		s.handleResync(cmd)
	case "getlast":
		s.handleGetLast(cmd)
	case "getrange":
		s.handleGetRange(cmd)
	default:
		s.writeProblem(op, cmd.RequestID, problem.Newf(problem.ValidationFailed, "unsupported op %q", op))
	}
}

// supportedFeatures is the canonical set of features the server can negotiate.
var supportedFeatures = map[string]struct{}{
	"batching":      {},
	"snapshot_hash": {},
	"prev_seq":      {},
}

// validateRequestedFeatures partitions requested features into valid and unknown,
// deduplicating and normalizing to lowercase.
func validateRequestedFeatures(requested []string) (valid, unknown []string) {
	seen := make(map[string]struct{}, len(requested))
	for _, raw := range requested {
		f := strings.ToLower(strings.TrimSpace(raw))
		if f == "" {
			continue
		}
		if _, dup := seen[f]; dup {
			continue
		}
		seen[f] = struct{}{}
		if _, ok := supportedFeatures[f]; ok {
			valid = append(valid, f)
		} else {
			unknown = append(unknown, f)
		}
	}
	return valid, unknown
}

func (s *SessionActor) handleClientHello(cmd clientCommand) {
	s.helloSeen = true
	if len(cmd.RequestedFeatures) > 0 {
		valid, unknown := validateRequestedFeatures(cmd.RequestedFeatures)
		if len(unknown) > 0 {
			metrics.IncWSContractViolation("unknown_feature")
			s.writeProblem("hello", cmd.RequestID,
				problem.Newf(problem.ValidationFailed, "unsupported features: %s", strings.Join(unknown, ", ")))
			return
		}
		s.clientFeatures = valid
	}
	s.writeJSON(wsHelloAckFrame{
		Type:               "ack",
		Op:                 "hello",
		RequestID:          cmd.RequestID,
		NegotiatedFeatures: s.clientFeatures,
	})
}

type wsHelloAckFrame struct {
	Type               string   `json:"type"`
	Op                 string   `json:"op"`
	RequestID          string   `json:"request_id"`
	NegotiatedFeatures []string `json:"negotiated_features,omitempty"`
}

func (s *SessionActor) handlePing(cmd clientCommand) {
	nowMs := time.Now().UnixMilli()
	if s.cfg.Clock != nil {
		nowMs = s.cfg.Clock.Now().UnixMilli()
	}
	metrics.IncWSControlFrame("pong")
	s.writeJSON(wsPongFrame{
		Type:      "pong",
		Op:        "ping",
		RequestID: strings.TrimSpace(cmd.RequestID),
		TsClient:  cmd.TsClient,
		TsServer:  s.normalizeServerTS(nowMs),
	})
}

func (s *SessionActor) handleResync(cmd clientCommand) {
	if !s.allowRateLimitedCommand("resync", cmd.RequestID) {
		return
	}
	subject, p := s.resolveCommandSubject(cmd, "resync")
	if p != nil {
		metrics.IncWSResyncRejected("subject_invalid")
		s.writeProblem("resync", cmd.RequestID, p)
		return
	}
	if !s.session.IsSubscribed(subject) {
		metrics.IncWSResyncRejected("not_subscribed")
		s.writeProblem("resync", cmd.RequestID, problem.Newf(problem.NotFound, "not subscribed to stream %q", subject.String()))
		return
	}
	if !s.emitSnapshot(subject) {
		metrics.IncWSResyncRejected("snapshot_unavailable")
		s.writeProblem("resync", cmd.RequestID, problem.New(problem.NotFound, "snapshot unavailable for requested stream"))
		return
	}
	metrics.IncWSResync()
	observability.IncTerminalWSResync(subject.String())
	metrics.IncWSControlFrame("ack_resync")
	s.writeJSON(wsAckFrame{
		Type:      "ack",
		Op:        "resync",
		RequestID: cmd.RequestID,
		Subject:   subject.String(),
	})
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

func (s *SessionActor) resolveCommandSubject(cmd clientCommand, op string) (domain.Subject, *problem.Problem) {
	if raw := strings.TrimSpace(cmd.Subject); raw != "" {
		subRes := s.service.ParseSubject(raw)
		if subRes.IsFail() {
			return domain.Subject{}, subRes.Problem()
		}
		return subRes.Value(), nil
	}
	if raw := strings.TrimSpace(cmd.StreamID); raw != "" {
		subRes := s.service.ParseSubject(raw)
		if subRes.IsFail() {
			return domain.Subject{}, subRes.Problem()
		}
		return subRes.Value(), nil
	}
	if strings.TrimSpace(cmd.Venue) == "" || strings.TrimSpace(cmd.Symbol) == "" || strings.TrimSpace(cmd.Channel) == "" {
		return domain.Subject{}, problem.Newf(problem.ValidationFailed, "%s requires stream_id or (venue,symbol,channel)", op)
	}
	channel := strings.ToLower(strings.TrimSpace(cmd.Channel))
	timeframe := "raw"
	if agg := strings.TrimSpace(cmd.Aggregation); agg != "" {
		timeframe = strings.ToLower(agg)
	}
	subject, p := domain.NewSubject(channel, cmd.Venue, cmd.Symbol, timeframe)
	if p != nil {
		return domain.Subject{}, p
	}
	return subject, nil
}

func (s *SessionActor) enforceSubscriptionLimits(subject domain.Subject) *problem.Problem {
	if s.cfg.MaxSubscriptions > 0 {
		current := len(s.session.Subscriptions())
		if !s.session.IsSubscribed(subject) && current >= s.cfg.MaxSubscriptions {
			return problem.Newf(problem.ValidationFailed, "max subscriptions per connection exceeded (%d)", s.cfg.MaxSubscriptions)
		}
	}
	if s.cfg.MaxSymbolsPerConnection <= 0 {
		return nil
	}
	if s.session.IsSubscribed(subject) {
		return nil
	}
	symbols := map[string]struct{}{}
	for _, sub := range s.session.Subscriptions() {
		symbols[sub.Subject.Symbol] = struct{}{}
	}
	symbols[subject.Symbol] = struct{}{}
	if len(symbols) > s.cfg.MaxSymbolsPerConnection {
		return problem.Newf(problem.ValidationFailed, "max symbols per connection exceeded (%d)", s.cfg.MaxSymbolsPerConnection)
	}
	return nil
}

func (s *SessionActor) handleSubscribe(cmd clientCommand) {
	if !s.allowRateLimitedCommand("subscribe", cmd.RequestID) {
		return
	}
	subject, p := s.resolveCommandSubject(cmd, "subscribe")
	if p != nil {
		s.writeProblem("subscribe", cmd.RequestID, p)
		return
	}
	if p := s.enforceSubscriptionLimits(subject); p != nil {
		s.writeProblem("subscribe", cmd.RequestID, p)
		return
	}
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

func (s *SessionActor) emitSnapshot(subject domain.Subject) bool {
	subjectKey := subject.String()
	if s.cfg.HotSnapshotProvider != nil {
		raw, ok := s.cfg.HotSnapshotProvider.GetLatest(subject)
		if ok && len(raw) > 0 {
			payload := json.RawMessage(raw)
			if !json.Valid(payload) {
				s.logger.Warn("delivery session: invalid snapshot payload, skipping", "subject", subjectKey)
			} else {
				meta := s.buildStreamMeta(subject, hotSnapshotRangeItem(raw))
				s.snapshotSeq[subjectKey]++
				s.writeJSON(wsSnapshotFrame{
					Type:             "snapshot",
					Subject:          subjectKey,
					StreamID:         subjectKey,
					ProtocolVersion:  wsProtocolVersion,
					ServerInstanceID: s.cfg.ServerInstanceID,
					Seq:              meta.Seq,
					TsServer:         meta.TsServer,
					Venue:            meta.Venue,
					Symbol:           meta.Symbol,
					Channel:          channelName(meta.Channel, subject.StreamType),
					Payload:          payload,
					SnapshotSource:   "hot_snapshot_fallback",
					SnapshotSeq:      s.snapshotSeq[subjectKey],
					WatermarkSeq:     meta.Seq,
					SnapshotHash:     fnvHexHash(payload),
				})
				metrics.IncWSQuery("snapshot", wsQueryBucket(subject.StreamType))
				return true
			}
		}
	}
	entry, ok := s.lastSnapshot[subjectKey]
	if !ok || len(entry.Payload) == 0 || !json.Valid(entry.Payload) {
		return false
	}
	tsServer := s.normalizeServerTS(entry.TsServer)
	payloadCopy := append(json.RawMessage(nil), entry.Payload...)
	s.snapshotSeq[subjectKey]++
	s.writeJSON(wsSnapshotFrame{
		Type:             "snapshot",
		Subject:          subjectKey,
		StreamID:         subjectKey,
		ProtocolVersion:  wsProtocolVersion,
		ServerInstanceID: s.cfg.ServerInstanceID,
		Seq:              entry.Seq,
		TsServer:         tsServer,
		Venue:            entry.Venue,
		Symbol:           entry.Symbol,
		Channel:          entry.Channel,
		Payload:          payloadCopy,
		SnapshotSource:   "session_last_event",
		SnapshotSeq:      s.snapshotSeq[subjectKey],
		WatermarkSeq:     entry.Seq,
		SnapshotHash:     fnvHexHash(payloadCopy),
	})
	metrics.IncWSQuery("snapshot", wsQueryBucket(subject.StreamType))
	return true
}

func fnvHexHash(data []byte) string {
	h := fnv.New64a()
	_, _ = h.Write(data)
	var buf [8]byte
	b := h.Sum(buf[:0])
	return hex.EncodeToString(b)
}

func (s *SessionActor) handleUnsubscribe(cmd clientCommand) {
	subject, p := s.resolveCommandSubject(cmd, "unsubscribe")
	if p != nil {
		s.writeProblem("unsubscribe", cmd.RequestID, p)
		return
	}
	if p := s.session.Unsubscribe(subject); p != nil {
		s.writeProblem(cmd.Op, cmd.RequestID, p)
		return
	}
	subjectKey := subject.String()
	delete(s.lastSnapshot, subjectKey)
	delete(s.snapshotSeq, subjectKey)
	delete(s.lastDeliveredSeq, subjectKey)
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
	subject, p := s.resolveCommandSubject(cmd, "getlast")
	if p != nil {
		metrics.IncWSQueryRejected("subject_invalid")
		s.writeProblem(cmd.Op, cmd.RequestID, p)
		return
	}
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
	if !s.allowRateLimitedCommand("getrange", cmd.RequestID) {
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
	subject, p := s.resolveCommandSubject(cmd, "getrange")
	if p != nil {
		metrics.IncWSQueryRejected("subject_invalid")
		s.writeProblem(cmd.Op, cmd.RequestID, p)
		return
	}
	s.executeGetRange(cmd.Op, cmd.RequestID, subject.String(), params)
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

func (s *SessionActor) buildStreamMeta(subject domain.Subject, item ports.RangeItem) *deliveryv1.StreamMeta {
	nowMs := time.Now().UnixMilli()
	if s.cfg.Clock != nil {
		nowMs = s.cfg.Clock.Now().UnixMilli()
	}
	return &deliveryv1.StreamMeta{
		ProtocolVersion:  deliveryv1.WireProtocolVersion_WIRE_PROTOCOL_VERSION_V1,
		ServerInstanceId: s.cfg.ServerInstanceID,
		StreamId:         subject.String(),
		Seq:              item.Seq,
		TsServer:         s.normalizeServerTS(nowMs),
		Venue:            subject.Venue,
		Symbol:           subject.Symbol,
		Channel:          channelEnumFromStreamType(subject.StreamType),
	}
}

func channelEnumFromStreamType(streamType string) deliveryv1.Channel {
	switch strings.ToLower(strings.TrimSpace(streamType)) {
	case "marketdata.trade":
		return deliveryv1.Channel_CHANNEL_TRADE
	case "marketdata.bookdelta":
		return deliveryv1.Channel_CHANNEL_BOOK_DELTA
	case "aggregation.snapshot":
		return deliveryv1.Channel_CHANNEL_BOOK_SNAPSHOT
	case "marketdata.markprice":
		return deliveryv1.Channel_CHANNEL_TICKER
	case "aggregation.stats":
		return deliveryv1.Channel_CHANNEL_STATS
	case "aggregation.candle":
		return deliveryv1.Channel_CHANNEL_CANDLE
	case "marketdata.liquidation":
		return deliveryv1.Channel_CHANNEL_LIQUIDATION
	case "insights.heatmap_snapshot":
		return deliveryv1.Channel_CHANNEL_HEATMAP_SNAPSHOT
	case "insights.volume_profile_snapshot":
		return deliveryv1.Channel_CHANNEL_VOLUME_PROFILE_SNAPSHOT
	default:
		return deliveryv1.Channel_CHANNEL_UNSPECIFIED
	}
}

func channelName(ch deliveryv1.Channel, fallback string) string {
	switch ch {
	case deliveryv1.Channel_CHANNEL_TRADE:
		return "trade"
	case deliveryv1.Channel_CHANNEL_BOOK_DELTA:
		return "book_delta"
	case deliveryv1.Channel_CHANNEL_BOOK_SNAPSHOT:
		return "book_snapshot"
	case deliveryv1.Channel_CHANNEL_TICKER:
		return "ticker"
	case deliveryv1.Channel_CHANNEL_FUNDING:
		return "funding"
	case deliveryv1.Channel_CHANNEL_OPEN_INTEREST:
		return "open_interest"
	case deliveryv1.Channel_CHANNEL_LIQUIDATION:
		return "liquidation"
	case deliveryv1.Channel_CHANNEL_STATS:
		return "stats"
	case deliveryv1.Channel_CHANNEL_CANDLE:
		return "candle"
	case deliveryv1.Channel_CHANNEL_HEATMAP_SNAPSHOT:
		return "heatmap_snapshot"
	case deliveryv1.Channel_CHANNEL_VOLUME_PROFILE_SNAPSHOT:
		return "volume_profile_snapshot"
	default:
		if strings.TrimSpace(fallback) == "" {
			return "unknown"
		}
		return strings.ToLower(strings.TrimSpace(fallback))
	}
}

func (s *SessionActor) allowRateLimitedCommand(op, requestID string) bool {
	switch op {
	case "subscribe", "getrange", "resync", "ping":
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
			if s.onDrop("queue_full", &evt) {
				return
			}
			return
		case domain.BackpressureDropOldest:
			s.outbound.DropFront()
			if s.onDrop("drop_oldest", &evt) {
				return
			}
		case domain.BackpressurePriorityDrop:
			if !s.priorityDrop(evt) {
				if s.onDrop("priority_drop_self", &evt) {
					return
				}
				return
			}
			if s.onDrop("priority_drop", &evt) {
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
			if s.onDrop("queue_full", &evt) {
				return
			}
			return
		}
	}
	s.outbound.PushBack(evt)
	qLen := s.outbound.Len()
	metrics.SetWSQueueDepth(qLen)
	metrics.SetWSTenantQueueDepth(s.cfg.TenantID, qLen)
	if qLen > s.queueHighWatermark {
		s.queueHighWatermark = qLen
	}
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

func (s *SessionActor) onDrop(reason string, evt *DeliveryEvent) bool {
	metrics.IncWSDrops(reason)
	metrics.IncWSTenantDrop(s.cfg.TenantID, reason)
	channel := "unknown"
	streamID := "unknown"
	venue := "unknown"
	symbol := "unknown"
	if evt != nil {
		streamID = evt.Subject.String()
		venue = evt.Subject.Venue
		symbol = evt.Subject.Symbol
		channel = channelName(channelEnumFromStreamType(evt.Subject.StreamType), evt.Subject.StreamType)
	}
	metrics.IncWSDropped(reason, channel)
	observability.RecordTerminalWSDrop(streamID, venue, symbol, channel, reason)
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
	_, span := otel.Tracer("market-raccoon.delivery.session").Start(context.Background(), "session.write_delivery_event")
	span.SetAttributes(
		attribute.String("stream.id", evt.Subject.String()),
		attribute.String("stream.type", evt.Subject.StreamType),
		attribute.String("stream.venue", evt.Subject.Venue),
		attribute.String("stream.symbol", evt.Subject.Symbol),
		attribute.Int64("event.seq", evt.Env.Seq),
	)
	defer span.End()

	meta := s.buildStreamMeta(evt.Subject, ports.RangeItem{
		Seq:      evt.Env.Seq,
		TsIngest: evt.Env.TsIngest,
	})
	subjectKey := evt.Subject.String()
	channel := channelName(meta.GetChannel(), evt.Subject.StreamType)
	prevSeq := s.lastDeliveredSeq[subjectKey]
	if s.cfg.PreferProto && contracts.ProtoRolloutEnabledForEventType(evt.Env.Type) {
		env := evt.Env
		if env.Meta == nil {
			env.Meta = map[string]string{}
		}
		env.Meta["protocol_version"] = fmt.Sprintf("%d", wsProtocolVersion)
		env.Meta["server_instance_id"] = s.cfg.ServerInstanceID
		env.Meta["stream_id"] = meta.GetStreamId()
		env.Meta["channel"] = channel
		env.Meta["ts_server"] = fmt.Sprintf("%d", meta.GetTsServer())
		if prevSeq > 0 {
			env.Meta["prev_seq"] = fmt.Sprintf("%d", prevSeq)
		}
		raw, p := contracts.MarshalEnvelopeV1FromDomain(env)
		if p != nil {
			metrics.IncWSSerializeErrors()
			observability.IncTerminalWSSerializeError()
			span.RecordError(p)
			return p
		}
		// F4: frame size guard for proto path.
		if s.maxFrameBytes > 0 && len(raw) > s.maxFrameBytes {
			metrics.IncWSDrops("frame_too_large")
			return nil
		}
		if err := s.writeProtoDirect(websocket.BinaryMessage, raw); err != nil {
			span.RecordError(err)
			return problem.Wrap(err, problem.Internal, "proto write failed")
		}
		s.lastDeliveredSeq[subjectKey] = evt.Env.Seq
		metrics.IncWSMessagesOut(channel)
		metrics.IncWSTenantMessagesOut(s.cfg.TenantID, channel)
		metrics.AddWSBytesOut(channel, len(raw))
		s.messagesOut++
		observability.IncDeliveryProto()
		lag := meta.GetTsServer() - evt.Env.TsIngest
		s.lastLagMs = lag
		metrics.SetWSLag(channel, lag)
		metrics.ObserveWSPublishToDeliverLatency(channel, time.Duration(maxInt64(0, lag))*time.Millisecond)
		observability.RecordTerminalWSDelivery(
			meta.GetStreamId(),
			meta.GetVenue(),
			meta.GetSymbol(),
			channel,
			meta.GetSeq(),
			evt.Env.TsIngest,
			meta.GetTsServer(),
			lag,
		)
		return nil
	}
	payload := evt.Env.Payload
	if evt.Env.ContentType == envelope.ContentTypeProto {
		if s.cfg.TranscodeCache != nil {
			cached, p := s.cfg.TranscodeCache.TranscodeProtoToJSON(
				evt.Env.Type, evt.Env.Version, evt.Env.ContentType, payload,
			)
			if p != nil {
				metrics.IncWSSerializeErrors()
				observability.IncTerminalWSSerializeError()
				span.RecordError(p)
				return p
			}
			payload = cached
		} else {
			decoded, p := codec.DecodePayload(evt.Env.Type, evt.Env.Version, evt.Env.ContentType, payload)
			if p != nil {
				metrics.IncWSSerializeErrors()
				observability.IncTerminalWSSerializeError()
				span.RecordError(p)
				return p
			}
			transcoded, err := json.Marshal(decoded)
			if err != nil {
				metrics.IncWSSerializeErrors()
				observability.IncTerminalWSSerializeError()
				span.RecordError(err)
				return problem.Wrap(err, problem.Internal, "proto→json transcode failed")
			}
			payload = json.RawMessage(transcoded)
		}
	}
	frame := wsEventFrame{
		Type:             "event",
		Subject:          subjectKey,
		StreamID:         meta.GetStreamId(),
		ProtocolVersion:  wsProtocolVersion,
		ServerInstanceID: s.cfg.ServerInstanceID,
		Seq:              evt.Env.Seq,
		PrevSeq:          prevSeq,
		TsIngest:         evt.Env.TsIngest,
		TsServer:         meta.GetTsServer(),
		Venue:            evt.Subject.Venue,
		Symbol:           evt.Subject.Symbol,
		Channel:          channel,
		Payload:          payload,
	}
	// F4: frame size guard for JSON path (mirrors proto path guard above).
	if s.maxFrameBytes > 0 {
		raw, marshalErr := json.Marshal(frame)
		if marshalErr != nil {
			metrics.IncWSSerializeErrors()
			observability.IncTerminalWSSerializeError()
			span.RecordError(marshalErr)
			return problem.Wrap(marshalErr, problem.Internal, "json marshal failed")
		}
		if len(raw) > s.maxFrameBytes {
			metrics.IncWSDrops("frame_too_large")
			metrics.IncWSDropped("frame_too_large", channel)
			return nil
		}
	}
	if err := s.writeJSONDirect(frame); err != nil {
		span.RecordError(err)
		return problem.Wrap(err, problem.Internal, "json write failed")
	}
	s.lastDeliveredSeq[subjectKey] = evt.Env.Seq
	metrics.IncWSMessagesOut(channel)
	metrics.IncWSTenantMessagesOut(s.cfg.TenantID, channel)
	metrics.AddWSBytesOut(channel, len(payload))
	s.messagesOut++
	lag := frame.TsServer - evt.Env.TsIngest
	s.lastLagMs = lag
	metrics.SetWSLag(channel, lag)
	metrics.ObserveWSPublishToDeliverLatency(channel, time.Duration(maxInt64(0, lag))*time.Millisecond)
	if json.Valid(payload) {
		s.lastSnapshot[subjectKey] = sessionSnapshotEntry{
			Seq:      frame.Seq,
			TsServer: frame.TsServer,
			Venue:    frame.Venue,
			Symbol:   frame.Symbol,
			Channel:  frame.Channel,
			Payload:  append(json.RawMessage(nil), payload...),
		}
	}
	observability.RecordTerminalWSDelivery(
		frame.StreamID,
		frame.Venue,
		frame.Symbol,
		channel,
		frame.Seq,
		frame.TsIngest,
		frame.TsServer,
		lag,
	)
	observability.IncDeliveryJSON()
	return nil
}

func (s *SessionActor) writeProblem(op, requestID string, p *problem.Problem) {
	if p == nil {
		return
	}
	errorCode, actionHint := wsErrorMappingFromProblem(p)
	s.writeJSON(wsErrorFrame{
		Type:      "error",
		Op:        op,
		RequestID: requestID,
		Problem: wsErrorProblem{
			Code:       string(p.Code),
			ErrorCode:  errorCode,
			ActionHint: actionHint,
			Message:    p.Message,
		},
	})
}

func wsErrorMappingFromProblem(p *problem.Problem) (errorCode string, actionHint string) {
	if p == nil {
		return deliveryv1.ErrorCode_ERROR_CODE_UNSPECIFIED.String(), deliveryv1.ActionHint_ACTION_HINT_UNSPECIFIED.String()
	}
	switch p.Code {
	case problem.ValidationFailed, problem.InvalidArgument:
		return deliveryv1.ErrorCode_ERROR_CODE_VALIDATION.String(), deliveryv1.ActionHint_ACTION_HINT_NONE.String()
	case problem.NotFound:
		hint := deliveryv1.ActionHint_ACTION_HINT_NONE.String()
		if p.Retryable {
			hint = deliveryv1.ActionHint_ACTION_HINT_RETRY.String()
		}
		return deliveryv1.ErrorCode_ERROR_CODE_NOT_FOUND.String(), hint
	case problem.Unavailable:
		return deliveryv1.ErrorCode_ERROR_CODE_RATE_LIMITED.String(), deliveryv1.ActionHint_ACTION_HINT_RETRY.String()
	case problem.Conflict:
		return deliveryv1.ErrorCode_ERROR_CODE_RESYNC_REQUIRED.String(), deliveryv1.ActionHint_ACTION_HINT_RESYNC.String()
	case problem.IntegrityViolation:
		return deliveryv1.ErrorCode_ERROR_CODE_RESYNC_REQUIRED.String(), deliveryv1.ActionHint_ACTION_HINT_RESUBSCRIBE.String()
	case problem.Internal:
		return deliveryv1.ErrorCode_ERROR_CODE_INTERNAL.String(), deliveryv1.ActionHint_ACTION_HINT_RECONNECT.String()
	default:
		return deliveryv1.ErrorCode_ERROR_CODE_INTERNAL.String(), deliveryv1.ActionHint_ACTION_HINT_RECONNECT.String()
	}
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

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func saturatingUint64ToInt64(v uint64) int64 {
	max := uint64(^uint64(0) >> 1)
	if v > max {
		return int64(max)
	}
	return int64(v)
}

func (s *SessionActor) normalizeServerTS(ts int64) int64 {
	if ts > 0 {
		return ts
	}
	metrics.IncWSContractViolation("missing_ts_server")
	nowMs := time.Now().UnixMilli()
	if s != nil && s.cfg.Clock != nil {
		nowMs = s.cfg.Clock.Now().UnixMilli()
	}
	if nowMs <= 0 {
		nowMs = 1
	}
	return nowMs
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
	metrics.DecWSTenantConnectionsActive(s.cfg.TenantID)
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
	if s.cfg.OnClosed != nil {
		s.cfg.OnClosed()
	}
	s.lastSnapshot = nil
	s.snapshotSeq = nil
	s.lastDeliveredSeq = nil
}
