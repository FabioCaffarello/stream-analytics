package deliveryruntime

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/market-raccoon/internal/core/delivery/app"
	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/core/delivery/ports"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/observability"
	"github.com/market-raccoon/internal/shared/problem"
	deliveryv1 "github.com/market-raccoon/internal/shared/proto/gen/delivery/v1"
)

// ── Inbound command dispatch ────────────────────────────────────────────────

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

// ── Subscribe / Unsubscribe ─────────────────────────────────────────────────

func (s *SessionActor) handleSubscribe(cmd clientCommand) {
	if !s.allowRateLimitedCommand("subscribe", cmd.RequestID) {
		return
	}
	subject, p := s.resolveCommandSubject(cmd, "subscribe")
	if p != nil {
		s.writeProblem("subscribe", cmd.RequestID, p)
		return
	}
	alreadySubscribed := s.session.IsSubscribed(subject)
	if p := s.enforceSubscriptionLimits(subject); p != nil {
		s.writeProblem("subscribe", cmd.RequestID, p)
		return
	}
	if p := s.session.Subscribe(subject, domain.Filter{}); p != nil {
		s.writeProblem(cmd.Op, cmd.RequestID, p)
		return
	}
	if !alreadySubscribed && subject.IsSignal() {
		metrics.IncMRSignalWSActiveSubscriptions()
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
	s.emitSubscribeBackfill(subject)
}

func (s *SessionActor) emitSubscribeBackfill(subject domain.Subject) {
	if !s.supportsSubscribeBackfill() {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(subject.StreamType), "aggregation.candle") {
		return
	}
	s.emitRangeFrame(
		"backfill",
		"",
		subject,
		getRangeParams{Limit: subscribeBackfillLimit, Page: 1},
		true,
	)
}

func (s *SessionActor) supportsSubscribeBackfill() bool {
	return s.features.HasBatching()
}

func (s *SessionActor) handleUnsubscribe(cmd clientCommand) {
	subject, p := s.resolveCommandSubject(cmd, "unsubscribe")
	if p != nil {
		s.writeProblem("unsubscribe", cmd.RequestID, p)
		return
	}
	hadSignalSubscription := subject.IsSignal() && s.session.IsSubscribed(subject)
	if p := s.session.Unsubscribe(subject); p != nil {
		s.writeProblem(cmd.Op, cmd.RequestID, p)
		return
	}
	if hadSignalSubscription {
		metrics.DecMRSignalWSActiveSubscriptions()
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

// ── Snapshot emission ───────────────────────────────────────────────────────

func (s *SessionActor) emitSnapshot(subject domain.Subject) bool {
	subjectKey := subject.String()
	if s.cfg.HotSnapshotProvider != nil {
		raw, ok := s.cfg.HotSnapshotProvider.GetLatest(subject)
		if ok && len(raw) > 0 {
			payload := json.RawMessage(raw)
			if !json.Valid(payload) {
				s.logger.Warn("delivery session: invalid snapshot payload, skipping", "subject", subjectKey)
			} else {
				if s.cfg.SnapshotWireCache == nil {
					meta := s.buildStreamMeta(subject, hotSnapshotRangeItem(raw))
					s.snapshotSeq[subjectKey]++
					frame := wsSnapshotFrame{
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
					}
					if !s.frameFitsMaxBytes(frame) {
						s.onDrop(backpressureDropReasonFrameTooLarge, nil)
						return false
					}
					s.writeJSON(frame)
					metrics.IncWSQuery("snapshot", wsQueryBucket(subject.StreamType))
					return true
				}
				now := time.Now()
				if s.cfg.Clock != nil {
					now = s.cfg.Clock.Now()
				}
				cacheKey := snapshotWireCacheKey(subject, 0)
				if cached, hit := s.cfg.SnapshotWireCache.Get(cacheKey, now); hit && len(cached) > 0 {
					if err := s.writeJSONRawWithLimits(cached); err == nil {
						metrics.IncWSQuery("snapshot", wsQueryBucket(subject.StreamType))
						return true
					}
				}
				meta := s.buildStreamMeta(subject, hotSnapshotRangeItem(raw))
				frame := wsSnapshotFrame{
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
					WatermarkSeq:     meta.Seq,
					SnapshotHash:     fnvHexHash(payload),
				}
				wire, err := json.Marshal(frame)
				if err != nil {
					s.logger.Warn("delivery session: snapshot marshal failed", "subject", subjectKey, "err", err)
					return false
				}
				if err := s.writeJSONRawWithLimits(wire); err != nil {
					s.onDrop(backpressureDropReasonFrameTooLarge, nil)
					return false
				}
				if s.cfg.SnapshotWireCache != nil {
					s.cfg.SnapshotWireCache.Set(cacheKey, wire, now)
				}
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
	frame := wsSnapshotFrame{
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
	}
	if !s.frameFitsMaxBytes(frame) {
		s.onDrop(backpressureDropReasonFrameTooLarge, nil)
		return false
	}
	s.writeJSON(frame)
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

// ── GetLast / GetRange / Resync ─────────────────────────────────────────────

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
		item = items[len(items)-1]
	} else if s.cfg.HotSnapshotProvider != nil {
		if raw, ok := s.cfg.HotSnapshotProvider.GetLatest(subject); ok && len(raw) > 0 {
			item = hotSnapshotRangeItem(raw)
			snapshotSource = "hot_snapshot_fallback"
		}
	}
	metrics.IncWSQuery("getlast", wsQueryBucket(subject.StreamType))
	frame := wsLastFrame{
		Type:           "last",
		Op:             cmd.Op,
		RequestID:      cmd.RequestID,
		Subject:        subject.String(),
		Item:           item,
		SnapshotSource: snapshotSource,
	}
	if !s.frameFitsMaxBytes(frame) {
		metrics.IncWSQueryRejected("frame_too_large")
		s.writeProblem(cmd.Op, cmd.RequestID,
			withWSLimitProblemDetails(
				problem.Newf(problem.ValidationFailed, "response exceeds max_frame_bytes (%d)", s.limits.MaxFrameBytes),
				wsLimitTypeMaxFrameBytes,
				deliveryv1.ActionHint_ACTION_HINT_NONE,
			),
		)
		return
	}
	s.writeJSON(frame)
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

	s.emitRangeFrame(op, requestID, subject, params, false)
}

func (s *SessionActor) emitRangeFrame(op, requestID string, subject domain.Subject, params getRangeParams, bestEffort bool) {
	page := params.Page
	if page <= 0 {
		page = 1
	}
	limit := params.Limit
	if limit <= 0 {
		limit = defaultRangeLimit
	}
	res := s.service.GetRange(context.Background(), app.GetRangeRequest{
		SubjectRaw: subject.String(),
		FromMs:     params.FromMs,
		ToMs:       params.ToMs,
		Limit:      maxQueryLimit,
	})
	if res.IsFail() {
		if bestEffort {
			return
		}
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
	var watermarkSeq int64
	for _, it := range items {
		if it.Seq > watermarkSeq {
			watermarkSeq = it.Seq
		}
	}
	queryOp := op
	if strings.TrimSpace(queryOp) == "" {
		queryOp = "backfill"
	}
	metrics.IncWSQuery(queryOp, wsQueryBucket(subject.StreamType))
	frame := wsRangeFrame{
		Type:           "range",
		Op:             queryOp,
		RequestID:      requestID,
		Subject:        subject.String(),
		Page:           page,
		Limit:          limit,
		Items:          items,
		SnapshotSource: snapshotSource,
		WatermarkSeq:   watermarkSeq,
	}
	if !s.frameFitsMaxBytes(frame) {
		if bestEffort {
			return
		}
		metrics.IncWSQueryRejected("frame_too_large")
		s.writeProblem(op, requestID,
			withWSLimitProblemDetails(
				problem.Newf(problem.ValidationFailed, "response exceeds max_frame_bytes (%d)", s.limits.MaxFrameBytes),
				wsLimitTypeMaxFrameBytes,
				deliveryv1.ActionHint_ACTION_HINT_NONE,
			),
		)
		return
	}
	s.writeJSON(frame)
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

// ── Subject resolution ──────────────────────────────────────────────────────

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
	channel = canonicalStreamTypeForCommandChannel(channel)
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
	alreadySubscribed := s.session.IsSubscribed(subject)
	if subject.IsSignal() && s.limits.MaxSignalSubscriptions > 0 && !alreadySubscribed {
		currentSignalSubs := s.session.SignalSubscriptionCount()
		if currentSignalSubs >= s.limits.MaxSignalSubscriptions {
			metrics.IncMRSignalWSSubscriptionRejected("max_signal_subscriptions")
			return withWSLimitProblemDetails(
				problem.Newf(
					problem.ValidationFailed,
					"max signal subscriptions per connection exceeded (%d)",
					s.limits.MaxSignalSubscriptions,
				),
				wsLimitTypeMaxSubscriptions,
				deliveryv1.ActionHint_ACTION_HINT_NONE,
			)
		}
	}
	if s.limits.MaxSubscriptions > 0 {
		current := len(s.session.Subscriptions())
		if !alreadySubscribed && current >= s.limits.MaxSubscriptions {
			if subject.IsSignal() {
				metrics.IncMRSignalWSSubscriptionRejected("max_subscriptions_per_connection")
			}
			return withWSLimitProblemDetails(
				problem.Newf(problem.ValidationFailed, "max subscriptions per connection exceeded (%d)", s.limits.MaxSubscriptions),
				wsLimitTypeMaxSubscriptions,
				deliveryv1.ActionHint_ACTION_HINT_NONE,
			)
		}
	}
	if s.limits.MaxSymbolsPerConnection <= 0 {
		return nil
	}
	if alreadySubscribed {
		return nil
	}
	symbols := map[string]struct{}{}
	for _, sub := range s.session.Subscriptions() {
		symbols[sub.Subject.Symbol] = struct{}{}
	}
	symbols[subject.Symbol] = struct{}{}
	if len(symbols) > s.limits.MaxSymbolsPerConnection {
		if subject.IsSignal() {
			metrics.IncMRSignalWSSubscriptionRejected("max_symbols_per_connection")
		}
		return withWSLimitProblemDetails(
			problem.Newf(problem.ValidationFailed, "max symbols per connection exceeded (%d)", s.limits.MaxSymbolsPerConnection),
			wsLimitTypeMaxSymbols,
			deliveryv1.ActionHint_ACTION_HINT_NONE,
		)
	}
	return nil
}

func canonicalStreamTypeForCommandChannel(channel string) string {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "trade", "marketdata.trade":
		return "marketdata.trade"
	case "book_delta", "marketdata.bookdelta":
		return "marketdata.bookdelta"
	case "book_snapshot", "aggregation.snapshot":
		return "aggregation.snapshot"
	case "ticker", "marketdata.markprice":
		return "marketdata.markprice"
	case "liquidation", "marketdata.liquidation":
		return "marketdata.liquidation"
	case "stats", "aggregation.stats":
		return "aggregation.stats"
	case "candle", "aggregation.candle":
		return "aggregation.candle"
	case "tape", "aggregation.tape":
		return "aggregation.tape"
	case "heatmap_snapshot", "insights.heatmap_snapshot":
		return "insights.heatmap_snapshot"
	case "volume_profile_snapshot", "insights.volume_profile_snapshot":
		return "insights.volume_profile_snapshot"
	case "evidence", "liquidity.evidence", "insights.microstructure_evidence":
		return "liquidity.evidence"
	case "signal", "signal.event":
		return "signal"
	default:
		return channel
	}
}

// ── Rate limiting ───────────────────────────────────────────────────────────

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
	s.writeProblem(op, requestID,
		withWSLimitProblemDetails(
			problem.New(problem.Unavailable, "rate limit exceeded"),
			wsLimitTypeRateLimit,
			deliveryv1.ActionHint_ACTION_HINT_RETRY,
		),
	)
	return false
}

// ── Helpers ─────────────────────────────────────────────────────────────────

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

func hotSnapshotRangeItem(raw []byte) ports.RangeItem {
	item := ports.RangeItem{Payload: append([]byte(nil), raw...)}
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
