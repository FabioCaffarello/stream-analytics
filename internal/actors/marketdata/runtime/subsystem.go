package mdruntime

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/actors/marketdata/ws"
	runtime "github.com/FabioCaffarello/stream-analytics/internal/actors/runtime"
	"github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/app"
	"github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/domain"
	marketmodel "github.com/FabioCaffarello/stream-analytics/internal/core/marketmodel"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/metrics"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/naming"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	"github.com/anthdm/hollywood/actor"
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

	// ParseBatch is an optional batch parser for broadcast channels.
	// When set and returns a non-nil slice, the runtime processes each
	// request individually. When it returns nil, the message falls through
	// to ParseMessage/ParseMessageV2.
	ParseBatch ParseFuncBatch

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

	lmNormalizer    *app.NormalizeMarkPriceLiquidation
	adapterRegistry *marketmodel.AdapterRegistry
	canonicalState  *marketmodel.StateStore
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
	if s.lmNormalizer == nil {
		s.lmNormalizer = app.NewNormalizeMarkPriceLiquidation(app.NormalizeMarkPriceLiquidationConfig{})
	}
	if s.adapterRegistry == nil {
		s.adapterRegistry = marketmodel.NewAdapterRegistry()
	}
	if s.canonicalState == nil {
		s.canonicalState = marketmodel.NewStateStore(marketmodel.StateStoreConfig{
			MaxEntries: 20_000,
			TTL:        time.Hour,
		})
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
	// Batch parsing path: broadcast channels (e.g., HyperLiquid allMids).
	if s.cfg.ParseBatch != nil {
		reqs, err := s.cfg.ParseBatch(msg)
		if err != nil {
			metrics.IncWSMessageReceived(msg.Exchange, "batch_parse_error")
			s.logger.Warn("mdruntime: batch parse error",
				"exchange", msg.Exchange,
				"err", err,
			)
			s.logProgress()
			return
		}
		if reqs != nil {
			for i := range reqs {
				s.ingestSingleRequest(msg, reqs[i])
			}
			s.logProgress()
			return
		}
		// reqs == nil → not handled, fall through to single-message parse.
	}

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
		if !isExpectedSkipReason(meta.EventType, meta.SkipReason, meta.ProblemCode, meta.WSStream) &&
			s.telemetry.shouldSample(time.Now(), meta.SkipReason+"|"+meta.ProblemCode) {
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

	req, duplicate := s.normalizeMarkPriceLiquidation(msg, req)
	if duplicate {
		metrics.IncWSMessageReceived(msg.Exchange, req.EventType)
		metrics.ObserveIngest(msg.Exchange, req.Instrument, req.EventType, "duplicate", 0)
		s.telemetry.recordSkip(msg.Exchange, req.EventType, "duplicate_normalized", string(problem.Duplicate), req.Instrument, req.Metadata["ws_stream"])
		s.logProgress()
		return
	}
	req, p := s.canonicalizeRequest(req, msg.RecvAt.UnixMilli())
	if p != nil {
		metrics.IncCanonicalizationError(req.Venue, "normalize_"+req.EventType)
		metrics.IncWSMessageReceived(msg.Exchange, req.EventType)
		metrics.ObserveIngest(msg.Exchange, req.Instrument, req.EventType, "validation_failed", 0)
		s.telemetry.recordSkip(msg.Exchange, req.EventType, "canonicalization_error", string(p.Code), req.Instrument, req.Metadata["ws_stream"])
		if !isExpectedSkipReason(req.EventType, "canonicalization_error", string(p.Code), req.Metadata["ws_stream"]) &&
			s.telemetry.shouldSample(time.Now(), "canonicalization_error|"+string(p.Code)) {
			s.logger.Warn("mdruntime: canonicalization skip sampled",
				"exchange", msg.Exchange,
				"event_type", req.EventType,
				"venue", req.Venue,
				"instrument", req.Instrument,
				"problem_code", p.Code,
				"problem_message", p.Message,
			)
		}
		s.logProgress()
		return
	}
	metrics.IncCanonicalEvent(marketmodel.ChannelFromEventType(req.EventType), req.Venue)

	if req.EventType == "marketdata.bookdelta" {
		if depth, ok := req.Payload.(domain.BookDeltaV1); ok && depth.FirstID > 0 && depth.FinalID > 0 && !depth.IsSnapshot {
			if gap, lastFinal := s.telemetry.recordDepthSequence(req.Instrument, depth.FirstID, depth.FinalID, depth.PrevFinal); gap {
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
			if req.EventType == "marketdata.trade" {
				metrics.IncMRTradeDuplicate(req.Venue)
			}
		case problem.OutOfOrder:
			status = "out_of_order"
			if req.EventType == "marketdata.trade" {
				metrics.IncMRTradeOutOfOrder(req.Venue, req.Instrument)
			}
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

// ingestSingleRequest processes one IngestRequest from a batch parse result.
func (s *SubsystemActor) ingestSingleRequest(msg *ws.WsMessage, req app.IngestRequest) {
	req, duplicate := s.normalizeMarkPriceLiquidation(msg, req)
	if duplicate {
		metrics.IncWSMessageReceived(msg.Exchange, req.EventType)
		metrics.ObserveIngest(msg.Exchange, req.Instrument, req.EventType, "duplicate", 0)
		s.telemetry.recordSkip(msg.Exchange, req.EventType, "duplicate_normalized", string(problem.Duplicate), req.Instrument, req.Metadata["ws_stream"])
		return
	}
	var p *problem.Problem
	req, p = s.canonicalizeRequest(req, msg.RecvAt.UnixMilli())
	if p != nil {
		metrics.IncCanonicalizationError(req.Venue, "normalize_"+req.EventType)
		metrics.IncWSMessageReceived(msg.Exchange, req.EventType)
		metrics.ObserveIngest(msg.Exchange, req.Instrument, req.EventType, "validation_failed", 0)
		s.telemetry.recordSkip(msg.Exchange, req.EventType, "canonicalization_error", string(p.Code), req.Instrument, req.Metadata["ws_stream"])
		if !isExpectedSkipReason(req.EventType, "canonicalization_error", string(p.Code), req.Metadata["ws_stream"]) &&
			s.telemetry.shouldSample(time.Now(), "canonicalization_error|"+string(p.Code)) {
			s.logger.Warn("mdruntime: canonicalization skip sampled",
				"exchange", msg.Exchange,
				"event_type", req.EventType,
				"venue", req.Venue,
				"instrument", req.Instrument,
				"problem_code", p.Code,
				"problem_message", p.Message,
			)
		}
		return
	}
	metrics.IncCanonicalEvent(marketmodel.ChannelFromEventType(req.EventType), req.Venue)

	startedAt := time.Now()
	res := s.cfg.Service.Ingest.Execute(context.Background(), req)
	metrics.IngestStreamsActive.Set(float64(s.cfg.Service.Ingest.ActiveStreams()))
	if res.IsFail() {
		p := res.Problem()
		status := "failed"
		switch p.Code {
		case problem.Duplicate:
			status = "duplicate"
			if req.EventType == "marketdata.trade" {
				metrics.IncMRTradeDuplicate(req.Venue)
			}
		case problem.OutOfOrder:
			status = "out_of_order"
			if req.EventType == "marketdata.trade" {
				metrics.IncMRTradeOutOfOrder(req.Venue, req.Instrument)
			}
		case problem.ValidationFailed, problem.InvalidArgument:
			status = "validation_failed"
		}
		metrics.IncWSMessageReceived(msg.Exchange, req.EventType)
		metrics.ObserveIngest(msg.Exchange, req.Instrument, req.EventType, status, time.Since(startedAt))
		return
	}

	metrics.IncWSMessageReceived(msg.Exchange, req.EventType)
	metrics.ObserveIngest(msg.Exchange, req.Instrument, req.EventType, "ok", time.Since(startedAt))
	s.telemetry.recordIngest(req.EventType, req.Instrument, req.Metadata["ws_stream"])
	if s.engine != nil && s.selfPID != nil {
		s.engine.Send(s.selfPID, publishTick{At: time.Now()})
	}
}

func (s *SubsystemActor) normalizeMarkPriceLiquidation(msg *ws.WsMessage, req app.IngestRequest) (app.IngestRequest, bool) {
	eventType := naming.NormalizeEventType(req.EventType)
	if eventType != "marketdata.markprice" && eventType != "marketdata.liquidation" {
		return req, false
	}
	if s.lmNormalizer == nil {
		return req, false
	}

	tsIngest := msg.RecvAt.UnixMilli()
	if tsIngest <= 0 {
		tsIngest = time.Now().UnixMilli()
	}
	normReq := app.NormalizeMarkPriceLiquidationRequest{
		Venue:             req.Venue,
		Instrument:        req.Instrument,
		EventType:         req.EventType,
		Version:           req.Version,
		TsExchange:        req.TsExchange,
		TsIngest:          tsIngest,
		SourceIdempotency: req.IdempotencyKey,
	}
	switch eventType {
	case "marketdata.markprice":
		switch p := req.Payload.(type) {
		case domain.MarkPriceTickV1:
			payload := p
			normReq.MarkPricePayload = &payload
		case *domain.MarkPriceTickV1:
			normReq.MarkPricePayload = p
		default:
			s.logger.Warn("mdruntime: markprice payload type mismatch",
				"payload_type", fmt.Sprintf("%T", req.Payload),
			)
			return req, false
		}
	case "marketdata.liquidation":
		switch p := req.Payload.(type) {
		case domain.LiquidationTickV1:
			payload := p
			normReq.LiquidationPayload = &payload
		case *domain.LiquidationTickV1:
			normReq.LiquidationPayload = p
		default:
			s.logger.Warn("mdruntime: liquidation payload type mismatch",
				"payload_type", fmt.Sprintf("%T", req.Payload),
			)
			return req, false
		}
	}

	res := s.lmNormalizer.Execute(context.Background(), normReq)
	if res.IsFail() {
		s.logger.Warn("mdruntime: lm normalization failed",
			"event_type", req.EventType,
			"problem", res.Problem(),
		)
		return req, false
	}
	out := res.Value()
	req.Venue = out.Venue
	req.Instrument = out.Instrument
	req.EventType = out.EventType
	req.Version = out.Version
	req.IdempotencyKey = out.DedupKey
	if out.MarkPrice != nil {
		req.Payload = *out.MarkPrice
	}
	if out.Liquidation != nil {
		req.Payload = *out.Liquidation
	}
	return req, out.IsDuplicate
}

func (s *SubsystemActor) canonicalizeRequest(req app.IngestRequest, fallbackTS int64) (app.IngestRequest, *problem.Problem) {
	if req.Venue == "" || req.Instrument == "" {
		return req, problem.New(problem.ValidationFailed, "canonicalization requires venue and instrument")
	}
	adapter := s.adapterRegistry.Resolve(req.Venue)
	symbol, p := marketmodel.NewSymbol(req.Instrument)
	if p != nil {
		return req, p
	}
	fallback := marketmodel.ServerTS(fallbackTS)
	if req.TsExchange > 0 {
		fallback = marketmodel.ServerTS(req.TsExchange)
	}
	switch req.EventType {
	case "marketdata.trade":
		switch in := req.Payload.(type) {
		case domain.TradeTickV1:
			out, p := marketmodel.NormalizeTrade(adapter, symbol, in, fallback)
			if p != nil {
				return req, p
			}
			req.Payload = out
			req.TsExchange = out.Timestamp
			return req, nil
		case *domain.TradeTickV1:
			if in == nil {
				return req, problem.New(problem.ValidationFailed, "trade payload must not be nil")
			}
			out, p := marketmodel.NormalizeTrade(adapter, symbol, *in, fallback)
			if p != nil {
				return req, p
			}
			req.Payload = out
			req.TsExchange = out.Timestamp
			return req, nil
		default:
			return req, problem.Newf(problem.ValidationFailed, "unsupported trade payload type %T", req.Payload)
		}
	case "marketdata.bookdelta":
		switch in := req.Payload.(type) {
		case domain.BookDeltaV1:
			out, p := marketmodel.NormalizeBookDelta(adapter, symbol, in, fallback)
			if p != nil {
				return req, p
			}
			if p := s.trackCanonicalBookState(req.Venue, req.Instrument, out); p != nil {
				return req, p
			}
			req.Payload = out
			req.TsExchange = out.Timestamp
			return req, nil
		case *domain.BookDeltaV1:
			if in == nil {
				return req, problem.New(problem.ValidationFailed, "bookdelta payload must not be nil")
			}
			out, p := marketmodel.NormalizeBookDelta(adapter, symbol, *in, fallback)
			if p != nil {
				return req, p
			}
			if p := s.trackCanonicalBookState(req.Venue, req.Instrument, out); p != nil {
				return req, p
			}
			req.Payload = out
			req.TsExchange = out.Timestamp
			return req, nil
		default:
			return req, problem.Newf(problem.ValidationFailed, "unsupported bookdelta payload type %T", req.Payload)
		}
	case "marketdata.markprice":
		switch in := req.Payload.(type) {
		case domain.MarkPriceTickV1:
			out := domain.MarkPriceTickV1{
				MarkPrice:   adapter.Precision(symbol).NormalizePrice(in.MarkPrice),
				IndexPrice:  adapter.Precision(symbol).NormalizePrice(in.IndexPrice),
				FundingRate: in.FundingRate,
				Timestamp:   adapter.NormalizeTimestamp(in.Timestamp, fallback).UnixMilli(),
			}
			req.Payload = out
			req.TsExchange = out.Timestamp
			return req, nil
		case *domain.MarkPriceTickV1:
			if in == nil {
				return req, problem.New(problem.ValidationFailed, "markprice payload must not be nil")
			}
			out := domain.MarkPriceTickV1{
				MarkPrice:   adapter.Precision(symbol).NormalizePrice(in.MarkPrice),
				IndexPrice:  adapter.Precision(symbol).NormalizePrice(in.IndexPrice),
				FundingRate: in.FundingRate,
				Timestamp:   adapter.NormalizeTimestamp(in.Timestamp, fallback).UnixMilli(),
			}
			req.Payload = out
			req.TsExchange = out.Timestamp
			return req, nil
		default:
			return req, problem.Newf(problem.ValidationFailed, "unsupported markprice payload type %T", req.Payload)
		}
	case "marketdata.liquidation":
		switch in := req.Payload.(type) {
		case domain.LiquidationTickV1:
			side, p := adapter.NormalizeSide(string(in.Side))
			if p != nil {
				return req, p
			}
			out := domain.LiquidationTickV1{
				Side:      string(side),
				Price:     adapter.Precision(symbol).NormalizePrice(in.Price),
				Size:      adapter.Precision(symbol).NormalizeSize(in.Size),
				Timestamp: adapter.NormalizeTimestamp(in.Timestamp, fallback).UnixMilli(),
			}
			req.Payload = out
			req.TsExchange = out.Timestamp
			return req, nil
		case *domain.LiquidationTickV1:
			if in == nil {
				return req, problem.New(problem.ValidationFailed, "liquidation payload must not be nil")
			}
			side, p := adapter.NormalizeSide(string(in.Side))
			if p != nil {
				return req, p
			}
			out := domain.LiquidationTickV1{
				Side:      string(side),
				Price:     adapter.Precision(symbol).NormalizePrice(in.Price),
				Size:      adapter.Precision(symbol).NormalizeSize(in.Size),
				Timestamp: adapter.NormalizeTimestamp(in.Timestamp, fallback).UnixMilli(),
			}
			req.Payload = out
			req.TsExchange = out.Timestamp
			return req, nil
		default:
			return req, problem.Newf(problem.ValidationFailed, "unsupported liquidation payload type %T", req.Payload)
		}
	default:
		return req, nil
	}
}

func (s *SubsystemActor) trackCanonicalBookState(venue, instrument string, delta domain.BookDeltaV1) *problem.Problem {
	if s.canonicalState == nil {
		return nil
	}
	key, p := marketmodel.NewStreamKey(venue, instrument, marketmodel.ChannelBookDelta)
	if p != nil {
		return p
	}
	seqVal := delta.FinalID
	if seqVal <= 0 {
		seqVal = delta.Timestamp
	}
	if seqVal <= 0 {
		seqVal = 1
	}
	seq := marketmodel.Seq(seqVal)
	if delta.IsSnapshot {
		return s.canonicalState.UpsertSnapshot(key, seq, marketmodel.BookSnapshot{
			Bids:      delta.Bids,
			Asks:      delta.Asks,
			Timestamp: delta.Timestamp,
		})
	}
	if _, p := s.canonicalState.ApplyDelta(key, seq, delta, delta.Timestamp); p != nil {
		if p.Code == problem.NotFound {
			return s.canonicalState.UpsertSnapshot(key, seq, marketmodel.BookSnapshot{
				Bids:      delta.Bids,
				Asks:      delta.Asks,
				Timestamp: delta.Timestamp,
			})
		}
		return p
	}
	return nil
}

func (s *SubsystemActor) logProgress() {
	if !s.telemetry.shouldEmitProgress() {
		return
	}
	topN := 5
	s.logger.Info("mdruntime: message counters",
		"subsystem", s.cfg.Subsystem,
		"sample_kind", "progress_topn",
		"sample_window_seconds", int(s.telemetry.sampleWindow/time.Second),
		"top_n", topN,
		"total", s.telemetry.total,
		"ingested", s.telemetry.ingested,
		"skipped", s.telemetry.skipped,
		"by_event_top", topCounts(s.telemetry.byEvent, topN),
		"skip_by_reason_top", s.telemetry.topSkipReasons(topN),
		"skip_expected_total", s.telemetry.expectedSkipTotal,
		"skip_expected_by_reason_top", s.telemetry.topExpectedSkipReasons(3),
		"skip_unexpected_total", s.telemetry.unexpectedSkipTotal,
		"skip_unexpected_by_reason_top", s.telemetry.topUnexpectedSkipReasons(topN),
		"skip_by_exchange_event_reason_top", s.telemetry.topExchangeEventSkips(topN),
		"parse_error_by_code_top", topCounts(s.telemetry.parseErrorsByProblemCode, topN),
		"depth_gaps_total", s.telemetry.depthGapsTotal,
		"depth_gaps_by_symbol_top", topCounts(s.telemetry.depthGapsBySymbol, topN),
		"ws_backpressure_drops_total", s.telemetry.backpressureDropsTotal,
		"ws_reconnect_total", s.telemetry.wsReconnectTotal,
		"ws_disconnect_reason_top", topCounts(s.telemetry.wsDisconnectByReason, topN),
		"ws_connection_uptime_seconds", s.telemetry.wsConnectionUptimeSecs,
		"top_ws_streams", s.telemetry.topWSStreams(topN),
		"top_ticker_share_pct", s.telemetry.topTickerSharePercent(topN),
	)

	if s.telemetry.unexpectedSkipTotal > 0 && s.telemetry.shouldSample(time.Now(), "top-unexpected-skips") {
		s.logger.Warn("mdruntime: top unexpected skips sampled",
			"subsystem", s.cfg.Subsystem,
			"sample_kind", "unexpected_skip_topn",
			"sample_window_seconds", int(s.telemetry.sampleWindow/time.Second),
			"top_n", topN,
			"skip_unexpected_total", s.telemetry.unexpectedSkipTotal,
			"skip_unexpected_by_reason_top", s.telemetry.topUnexpectedSkipReasons(topN),
			"skip_by_exchange_event_reason_top", s.telemetry.topExchangeEventSkips(topN),
			"ws_reconnect_total", s.telemetry.wsReconnectTotal,
			"ws_backpressure_drops_total", s.telemetry.backpressureDropsTotal,
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
