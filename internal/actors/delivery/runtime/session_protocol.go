package deliveryruntime

import (
	"encoding/json"
	"strings"

	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
	deliveryv1 "github.com/market-raccoon/internal/shared/proto/gen/delivery/v1"
)

// ── Protocol version & limit-type constants ─────────────────────────────────

const wsProtocolVersion = 1

const (
	wsLimitTypeMaxSubscriptions = "max_subscriptions_per_connection"
	wsLimitTypeMaxSymbols       = "max_symbols_per_connection"
	wsLimitTypeMaxFrameBytes    = "max_frame_bytes"
	wsLimitTypeOutboundQueue    = "outbound_queue_size"
	wsLimitTypeRateLimit        = "rate_limit"
)

// ── Frame structs ───────────────────────────────────────────────────────────

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

type wsHelloAckFrame struct {
	Type               string   `json:"type"`
	Op                 string   `json:"op"`
	RequestID          string   `json:"request_id"`
	NegotiatedFeatures []string `json:"negotiated_features,omitempty"`
}

type wsPongFrame struct {
	Type      string `json:"type"`
	Op        string `json:"op"`
	RequestID string `json:"request_id"`
	TsClient  int64  `json:"ts_client"`
	TsServer  int64  `json:"ts_server"`
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
	SnapshotSource   string          `json:"snapshot_source,omitempty"`
	SnapshotSeq      int64           `json:"snapshot_seq,omitempty"`
	WatermarkSeq     int64           `json:"watermark_seq,omitempty"`
	SnapshotHash     string          `json:"snapshot_hash,omitempty"`
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

type wsBatchFrame struct {
	Type             string        `json:"type"`
	StreamID         string        `json:"stream_id"`
	ProtocolVersion  int           `json:"protocol_version"`
	ServerInstanceID string        `json:"server_instance_id"`
	Venue            string        `json:"venue"`
	Symbol           string        `json:"symbol"`
	Channel          string        `json:"channel"`
	BaseSeq          int64         `json:"base_seq"`
	Count            int           `json:"count"`
	TsServerBase     int64         `json:"ts_server_base"`
	TsIngestBase     int64         `json:"ts_ingest_base,omitempty"`
	Events           []wsBatchItem `json:"events"`
}

type wsBatchItem struct {
	SeqDelta      int64           `json:"dseq,omitempty"`
	PrevSeqDelta  int64           `json:"dprev,omitempty"`
	TsServerDelta int64           `json:"dts,omitempty"`
	TsIngestDelta int64           `json:"dti,omitempty"`
	Payload       json.RawMessage `json:"p"`
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

type wsLastFrame struct {
	Type           string `json:"type"`
	Op             string `json:"op"`
	RequestID      string `json:"request_id"`
	Subject        string `json:"subject"`
	Item           any    `json:"item"`
	SnapshotSource string `json:"snapshot_source,omitempty"`
}

type wsRangeFrame struct {
	Type           string `json:"type"`
	Op             string `json:"op"`
	RequestID      string `json:"request_id"`
	Subject        string `json:"subject"`
	Page           int    `json:"page"`
	Limit          int    `json:"limit"`
	Items          any    `json:"items"`
	SnapshotSource string `json:"snapshot_source,omitempty"`
	WatermarkSeq   int64  `json:"watermark_seq,omitempty"`
}

type getRangeParams struct {
	FromMs int64 `json:"from_ms"`
	ToMs   int64 `json:"to_ms"`
	Limit  int   `json:"limit"`
	Page   int   `json:"page"`
}

// ── Hello / ClientHello / Ping / Feature negotiation ────────────────────────

func (s *SessionActor) emitHello() {
	if s.cfg.Conn == nil {
		return
	}
	nowMs := s.clockNowMs()
	metrics.IncWSControlFrame("hello")
	s.writeJSON(wsHelloFrame{
		Type: "hello",
		Payload: wsHelloPayload{
			ProtoVer:        wsProtocolVersion,
			ProtocolVersion: wsProtocolVersion,
			ServerTime:      nowMs,
			ServerInstance:  s.cfg.ServerInstanceID,
			Capabilities:    s.limits.ToHelloCapabilities(s.cfg.ServerInstanceID, s.cfg.CompressionEnabled),
		},
	})
}

func (s *SessionActor) handleClientHello(cmd clientCommand) {
	s.helloSeen = true
	if len(cmd.RequestedFeatures) > 0 {
		nf, _, unknown := NegotiateFeatures(cmd.RequestedFeatures, s.cfg.CompressionEnabled)
		if len(unknown) > 0 {
			metrics.IncWSContractViolation("unknown_feature")
			s.writeProblem("hello", cmd.RequestID,
				problem.Newf(problem.ValidationFailed, "unsupported features: %s", strings.Join(unknown, ", ")))
			return
		}
		s.features = nf
	}
	s.writeJSON(wsHelloAckFrame{
		Type:               "ack",
		Op:                 "hello",
		RequestID:          cmd.RequestID,
		NegotiatedFeatures: s.features.List(),
	})
}

func (s *SessionActor) handlePing(cmd clientCommand) {
	nowMs := s.clockNowMs()
	metrics.IncWSControlFrame("pong")
	s.writeJSON(wsPongFrame{
		Type:      "pong",
		Op:        "ping",
		RequestID: strings.TrimSpace(cmd.RequestID),
		TsClient:  cmd.TsClient,
		TsServer:  s.normalizeServerTS(nowMs),
	})
}

// ── Error mapping helpers ───────────────────────────────────────────────────

func wsErrorMappingFromProblem(p *problem.Problem) (errorCode string, actionHint string) {
	if p == nil {
		return deliveryv1.ErrorCode_ERROR_CODE_UNSPECIFIED.String(), deliveryv1.ActionHint_ACTION_HINT_UNSPECIFIED.String()
	}
	overrideErrorCode := strings.TrimSpace(problemDetailString(p, "error_code"))
	overrideActionHint := strings.TrimSpace(problemDetailString(p, "action_hint"))
	if overrideErrorCode != "" || overrideActionHint != "" {
		if overrideErrorCode == "" {
			overrideErrorCode = deliveryv1.ErrorCode_ERROR_CODE_UNSPECIFIED.String()
		}
		if overrideActionHint == "" {
			overrideActionHint = deliveryv1.ActionHint_ACTION_HINT_NONE.String()
		}
		return overrideErrorCode, overrideActionHint
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

func withWSLimitProblemDetails(p *problem.Problem, limitType string, actionHint deliveryv1.ActionHint) *problem.Problem {
	if p == nil {
		return nil
	}
	out := problem.WithDetail(p, "error_code", strings.TrimSpace(limitType))
	out = problem.WithDetail(out, "action_hint", actionHint.String())
	return out
}

func problemDetailString(p *problem.Problem, key string) string {
	if p == nil || p.Details == nil {
		return ""
	}
	raw, ok := p.Details[key]
	if !ok {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func isWSLimitType(value string) bool {
	switch strings.TrimSpace(value) {
	case wsLimitTypeMaxSubscriptions, wsLimitTypeMaxSymbols, wsLimitTypeMaxFrameBytes, wsLimitTypeOutboundQueue, wsLimitTypeRateLimit:
		return true
	default:
		return false
	}
}

// ── Channel / query helpers ─────────────────────────────────────────────────

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

// ── writeProblem ────────────────────────────────────────────────────────────

func (s *SessionActor) writeProblem(op, requestID string, p *problem.Problem) {
	if p == nil {
		return
	}
	errorCode, actionHint := wsErrorMappingFromProblem(p)
	if isWSLimitType(errorCode) {
		metrics.IncWSLimitRejection(errorCode)
	}
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
