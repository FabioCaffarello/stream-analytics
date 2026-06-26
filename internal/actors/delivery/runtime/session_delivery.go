package deliveryruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/contracts"
	"github.com/FabioCaffarello/stream-analytics/internal/core/delivery/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/core/delivery/ports"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/codec"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/metrics"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/observability"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	deliveryv1 "github.com/FabioCaffarello/stream-analytics/internal/shared/proto/gen/delivery/v1"
	"github.com/gorilla/websocket"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

func observeTradeTapeWireBudget(venue, eventType string, bytes int) {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "marketdata.trade":
		metrics.ObserveMRTradeWireBytes(venue, "trade", bytes)
	case "aggregation.tape":
		metrics.ObserveMRTradeWireBytes(venue, "tape", bytes)
	}
}

// ── Flush / write paths ─────────────────────────────────────────────────────

func (s *SessionActor) flushOutbound() {
	if s.closed {
		s.flushing = false
		return
	}
	drained := 0
	for drained < wsFlushBatchSize {
		if s.features.HasBatching() && !s.cfg.PreferProto {
			started := time.Now()
			sent, err := s.writeDeliveryBatchFromQueue(wsFlushBatchSize - drained)
			if err != nil {
				s.logger.Warn("delivery session: batched write failed", "err", err)
				s.closeSession()
				return
			}
			if sent > 0 {
				drained += sent
				metrics.ObserveWSSendLatency(time.Since(started))
				continue
			}
		}

		evt, ok := s.outbound.PopFront()
		if !ok {
			s.flushing = false
			metrics.SetWSQueueDepth(0)
			metrics.SetWSTenantQueueDepth(s.cfg.TenantID, 0)
			return
		}
		drained++
		metrics.SetWSQueueDepth(s.outbound.Len())
		metrics.SetWSTenantQueueDepth(s.cfg.TenantID, s.outbound.Len())

		started := time.Now()
		if err := s.writeDeliveryEvent(evt); err != nil {
			s.logger.Warn("delivery session: write failed", "err", err)
			s.closeSession()
			return
		}
		metrics.ObserveWSSendLatency(time.Since(started))
	}
	if s.outbound.Len() > 0 {
		s.engine.Send(s.self, sessionFlushOutbound{})
		return
	}
	s.flushing = false
}

func (s *SessionActor) writeDeliveryBatchFromQueue(maxItems int) (int, *problem.Problem) {
	if maxItems < 2 || s.outbound == nil || s.outbound.Len() < 2 {
		return 0, nil
	}
	if maxItems > wsFlushBatchSize {
		maxItems = wsFlushBatchSize
	}
	if maxItems > s.outbound.Len() {
		maxItems = s.outbound.Len()
	}
	if maxItems < 2 {
		return 0, nil
	}

	first := s.outbound.At(0)
	subjectKey := first.Subject.String()
	if _, pending := s.deferredSnapshotSubjects[subjectKey]; pending {
		if s.emitHotSnapshot(first.Subject) {
			delete(s.deferredSnapshotSubjects, subjectKey)
		}
	}
	channel := channelName(channelEnumFromStreamType(first.Subject.StreamType), first.Subject.StreamType)

	candidateCount := 1
	for candidateCount < maxItems {
		next := s.outbound.At(candidateCount)
		if next.Subject != first.Subject {
			break
		}
		candidateCount++
	}
	if candidateCount < 2 {
		return 0, nil
	}

	basePrev := s.lastDeliveredSeq[subjectKey]
	for i := 0; i < candidateCount; i++ {
		evt := s.outbound.At(i)
		meta := s.buildStreamMeta(evt.Subject, ports.RangeItem{
			Seq:      evt.Env.Seq,
			TsIngest: evt.Env.TsIngest,
		})
		payload, p := s.prepareJSONPayload(evt.Env)
		if p != nil {
			metrics.IncWSSerializeErrors()
			observability.IncTerminalWSSerializeError()
			return 0, p
		}
		prevSeq := basePrev
		if i > 0 {
			prevSeq = s.batchPrepared[i-1].seq
		}
		s.batchPrepared[i] = preparedBatchEvent{
			subject:  evt.Subject,
			env:      evt.Env,
			channel:  channel,
			seq:      evt.Env.Seq,
			prevSeq:  prevSeq,
			tsIngest: evt.Env.TsIngest,
			tsServer: meta.GetTsServer(),
			payload:  payload,
		}
	}

	for count := candidateCount; count >= 2; count-- {
		baseSeq := s.batchPrepared[0].seq
		baseTsServer := s.batchPrepared[0].tsServer
		baseTsIngest := s.batchPrepared[0].tsIngest
		basePrevSeq := s.batchPrepared[0].prevSeq

		for i := 0; i < count; i++ {
			item := &s.batchItems[i]
			prep := s.batchPrepared[i]
			item.SeqDelta = prep.seq - baseSeq
			item.PrevSeqDelta = prep.prevSeq - basePrevSeq
			item.TsServerDelta = prep.tsServer - baseTsServer
			item.TsIngestDelta = prep.tsIngest - baseTsIngest
			item.Payload = prep.payload
		}

		frame := wsBatchFrame{
			Type:             "batch",
			StreamID:         subjectKey,
			ProtocolVersion:  wsProtocolVersion,
			ServerInstanceID: s.cfg.ServerInstanceID,
			Venue:            first.Subject.Venue,
			Symbol:           first.Subject.Symbol,
			Channel:          channel,
			BaseSeq:          baseSeq,
			Count:            count,
			TsServerBase:     baseTsServer,
			TsIngestBase:     baseTsIngest,
			Events:           s.batchItems[:count],
		}
		raw, err := json.Marshal(frame)
		if err != nil {
			return 0, problem.Wrap(err, problem.Internal, "batch marshal failed")
		}
		applyCompression, wireSize := s.planWireCompression(raw)
		if s.limits.MaxFrameBytes > 0 && wireSize > s.limits.MaxFrameBytes {
			continue
		}
		if err := s.writeJSONRaw(raw, applyCompression, wireSize); err != nil {
			return 0, problem.Wrap(err, problem.Internal, "batch write failed")
		}
		metrics.ObserveWSWireBytes(channel, wireSize)

		for i := 0; i < count; i++ {
			prep := s.batchPrepared[i]
			_, _ = s.outbound.PopFront()
			s.lastDeliveredSeq[subjectKey] = prep.seq
			metrics.IncWSMessagesOut(channel)
			metrics.IncWSTenantMessagesOut(s.cfg.TenantID, channel)
			metrics.AddWSBytesOut(channel, len(prep.payload))
			observeTradeTapeWireBudget(prep.subject.Venue, prep.env.Type, len(prep.payload))
			s.messagesOut++
			lag := prep.tsServer - prep.tsIngest
			s.lastLagMs = lag
			metrics.SetWSLag(channel, lag)
			metrics.ObserveWSPublishToDeliverLatency(channel, time.Duration(maxInt64(0, lag))*time.Millisecond)
			if json.Valid(prep.payload) {
				s.lastSnapshot[subjectKey] = sessionSnapshotEntry{
					Seq:      prep.seq,
					TsServer: prep.tsServer,
					Venue:    prep.subject.Venue,
					Symbol:   prep.subject.Symbol,
					Channel:  channel,
					Payload:  append(json.RawMessage(nil), prep.payload...),
				}
			}
			observability.RecordTerminalWSDelivery(
				subjectKey,
				prep.subject.Venue,
				prep.subject.Symbol,
				channel,
				prep.seq,
				prep.tsIngest,
				prep.tsServer,
				lag,
			)
			observability.IncDeliveryJSON()
		}

		metrics.IncWSBatchFrames()
		metrics.AddWSBatchEvents(count)
		metrics.SetWSQueueDepth(s.outbound.Len())
		metrics.SetWSTenantQueueDepth(s.cfg.TenantID, s.outbound.Len())
		return count, nil
	}
	return 0, problem.Newf(
		problem.ValidationFailed,
		"batch candidate does not fit max_frame_bytes: stream_id=%s candidates=%d max_frame_bytes=%d",
		subjectKey,
		candidateCount,
		s.limits.MaxFrameBytes,
	)
}

func (s *SessionActor) prepareJSONPayload(env envelope.Envelope) (json.RawMessage, *problem.Problem) {
	payload := env.Payload
	if env.ContentType != envelope.ContentTypeProto {
		return payload, nil
	}
	if s.cfg.TranscodeCache != nil {
		cached, p := s.cfg.TranscodeCache.TranscodeProtoToJSON(
			env.Type, env.Version, env.ContentType, payload,
		)
		if p != nil {
			return nil, p
		}
		return cached, nil
	}
	decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, payload)
	if p != nil {
		return nil, p
	}
	transcoded, err := json.Marshal(decoded)
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "proto→json transcode failed")
	}
	return json.RawMessage(transcoded), nil
}

func prepareSignalFramePayload(payload json.RawMessage) (wsSignalPayload, *problem.Problem) {
	var in struct {
		Kind           string          `json:"kind"`
		Type           string          `json:"type"`
		Venue          string          `json:"venue"`
		Instrument     string          `json:"instrument"`
		Symbol         string          `json:"symbol"`
		Timeframe      string          `json:"timeframe"`
		SignalID       string          `json:"signal_id"`
		RuleID         string          `json:"rule_id"`
		RuleVersion    string          `json:"rule_version"`
		Severity       string          `json:"severity"`
		Confidence     float64         `json:"confidence"`
		Evidence       json.RawMessage `json:"evidence"`
		Features       json.RawMessage `json:"features"`
		EvidenceIDs    []string        `json:"evidence_ids"`
		Explain        []string        `json:"explain"`
		CorrelationIDs []string        `json:"correlation_ids"`
		CorrelationID  string          `json:"correlation_id"`
		InputWatermark []struct {
			Venue    string `json:"venue"`
			Symbol   string `json:"symbol"`
			SeqStart int64  `json:"seq_start"`
			SeqEnd   int64  `json:"seq_end"`
		} `json:"input_watermark"`
		RegimeKind     string  `json:"regime_kind"`
		RegimeStrength float64 `json:"regime_strength"`
		Reason         string  `json:"reason"`
		Explanation    string  `json:"explanation"`
	}
	if err := json.Unmarshal(payload, &in); err != nil {
		return wsSignalPayload{}, problem.Wrap(err, problem.Internal, "signal payload decode failed")
	}
	kind := strings.TrimSpace(in.Kind)
	if kind == "" {
		kind = strings.TrimSpace(in.Type)
	}
	instrument := strings.TrimSpace(in.Instrument)
	if instrument == "" {
		instrument = strings.TrimSpace(in.Symbol)
	}
	evidence := in.Evidence
	if len(evidence) == 0 {
		evidence = in.Features
	}
	if len(evidence) == 0 {
		evidence = json.RawMessage("[]")
	}
	explain := normalizedStringList(in.Explain)
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		reason = strings.TrimSpace(in.Explanation)
	}
	if reason == "" && len(explain) > 0 {
		reason = explain[0]
	}
	correlationIDs := normalizedStringList(append(in.CorrelationIDs, in.CorrelationID))
	evidenceIDs := normalizedStringList(in.EvidenceIDs)
	if len(evidenceIDs) == 0 {
		evidenceIDs = evidenceIDsFromWatermark(in.InputWatermark)
	}
	return wsSignalPayload{
		Kind:           strings.ToLower(strings.TrimSpace(kind)),
		Venue:          strings.ToLower(strings.TrimSpace(in.Venue)),
		Instrument:     instrument,
		Timeframe:      strings.ToLower(strings.TrimSpace(in.Timeframe)),
		SignalID:       strings.TrimSpace(in.SignalID),
		RuleID:         strings.TrimSpace(in.RuleID),
		RuleVersion:    strings.TrimSpace(in.RuleVersion),
		Severity:       strings.ToLower(strings.TrimSpace(in.Severity)),
		Confidence:     in.Confidence,
		Evidence:       evidence,
		EvidenceIDs:    evidenceIDs,
		Explain:        explain,
		CorrelationIDs: correlationIDs,
		Regime:         strings.ToLower(strings.TrimSpace(in.RegimeKind)),
		RegimeStrength: in.RegimeStrength,
		Reason:         reason,
	}, nil
}

func normalizedStringList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for i := range in {
		v := strings.TrimSpace(in[i])
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func evidenceIDsFromWatermark(in []struct {
	Venue    string `json:"venue"`
	Symbol   string `json:"symbol"`
	SeqStart int64  `json:"seq_start"`
	SeqEnd   int64  `json:"seq_end"`
}) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for i := range in {
		venue := strings.ToUpper(strings.TrimSpace(in[i].Venue))
		symbol := strings.ToUpper(strings.TrimSpace(in[i].Symbol))
		if venue == "" || symbol == "" {
			continue
		}
		if in[i].SeqStart <= 0 || in[i].SeqEnd <= 0 {
			continue
		}
		if in[i].SeqEnd < in[i].SeqStart {
			continue
		}
		out = append(out, fmt.Sprintf("%s|%s|%d-%d", venue, symbol, in[i].SeqStart, in[i].SeqEnd))
	}
	return normalizedStringList(out)
}

func (s *SessionActor) writeDeliveryEvent(evt DeliveryEvent) *problem.Problem {
	_, span := otel.Tracer("stream-analytics.delivery.session").Start(context.Background(), "session.write_delivery_event")
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
	if _, pending := s.deferredSnapshotSubjects[subjectKey]; pending {
		if s.emitHotSnapshot(evt.Subject) {
			delete(s.deferredSnapshotSubjects, subjectKey)
		}
	}
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
		applyCompression, wireSize := s.planWireCompression(raw)
		if s.limits.MaxFrameBytes > 0 && wireSize > s.limits.MaxFrameBytes {
			s.onDrop(backpressureDropReasonFrameTooLarge, &evt)
			return nil
		}
		if err := s.writeBinaryRaw(raw, applyCompression, wireSize); err != nil {
			span.RecordError(err)
			return problem.Wrap(err, problem.Internal, "proto write failed")
		}
		s.lastDeliveredSeq[subjectKey] = evt.Env.Seq
		metrics.IncWSMessagesOut(channel)
		metrics.IncWSTenantMessagesOut(s.cfg.TenantID, channel)
		metrics.AddWSBytesOut(channel, len(raw))
		metrics.ObserveWSWireBytes(channel, wireSize)
		observeTradeTapeWireBudget(evt.Subject.Venue, evt.Env.Type, len(raw))
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
	payload, p := s.prepareJSONPayload(evt.Env)
	if p != nil {
		metrics.IncWSSerializeErrors()
		observability.IncTerminalWSSerializeError()
		span.RecordError(p)
		return p
	}
	if evt.Subject.IsSignal() {
		signalPayload, p := prepareSignalFramePayload(payload)
		if p != nil {
			metrics.IncWSSerializeErrors()
			observability.IncTerminalWSSerializeError()
			span.RecordError(p)
			return p
		}
		frame := wsSignalFrame{
			Type:     "signal",
			Subject:  subjectKey,
			Seq:      evt.Env.Seq,
			TsServer: meta.GetTsServer(),
			Payload:  signalPayload,
		}
		raw, marshalErr := json.Marshal(frame)
		if marshalErr != nil {
			metrics.IncWSSerializeErrors()
			observability.IncTerminalWSSerializeError()
			span.RecordError(marshalErr)
			return problem.Wrap(marshalErr, problem.Internal, "signal frame marshal failed")
		}
		applyCompression, wireSize := s.planWireCompression(raw)
		if s.limits.MaxFrameBytes > 0 && wireSize > s.limits.MaxFrameBytes {
			s.onDrop(backpressureDropReasonFrameTooLarge, &evt)
			return nil
		}
		if applyCompression {
			if err := s.writeJSONRaw(raw, applyCompression, wireSize); err != nil {
				span.RecordError(err)
				return problem.Wrap(err, problem.Internal, "signal frame write failed")
			}
		} else if err := s.writeJSONDirect(frame); err != nil {
			span.RecordError(err)
			return problem.Wrap(err, problem.Internal, "signal frame write failed")
		}
		s.lastDeliveredSeq[subjectKey] = evt.Env.Seq
		metrics.IncWSMessagesOut("signal")
		metrics.IncWSTenantMessagesOut(s.cfg.TenantID, "signal")
		metrics.AddWSBytesOut("signal", len(payload))
		metrics.ObserveWSWireBytes("signal", wireSize)
		metrics.IncMRSignalWSDelivered(signalPayload.Kind, signalPayload.Venue, signalPayload.Instrument)
		s.messagesOut++
		lag := frame.TsServer - evt.Env.TsIngest
		s.lastLagMs = lag
		metrics.SetWSLag("signal", lag)
		metrics.ObserveWSPublishToDeliverLatency("signal", time.Duration(maxInt64(0, lag))*time.Millisecond)
		if json.Valid(payload) {
			s.lastSnapshot[subjectKey] = sessionSnapshotEntry{
				Seq:      frame.Seq,
				TsServer: frame.TsServer,
				Venue:    evt.Subject.Venue,
				Symbol:   evt.Subject.Symbol,
				Channel:  "signal",
				Payload:  append(json.RawMessage(nil), payload...),
			}
		}
		observability.RecordTerminalWSDelivery(
			subjectKey,
			evt.Subject.Venue,
			evt.Subject.Symbol,
			"signal",
			frame.Seq,
			evt.Env.TsIngest,
			frame.TsServer,
			lag,
		)
		observability.IncDeliveryJSON()
		return nil
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
	raw, marshalErr := json.Marshal(frame)
	if marshalErr != nil {
		metrics.IncWSSerializeErrors()
		observability.IncTerminalWSSerializeError()
		span.RecordError(marshalErr)
		return problem.Wrap(marshalErr, problem.Internal, "json marshal failed")
	}
	applyCompression, wireSize := s.planWireCompression(raw)
	if s.limits.MaxFrameBytes > 0 && wireSize > s.limits.MaxFrameBytes {
		s.onDrop(backpressureDropReasonFrameTooLarge, &evt)
		return nil
	}
	if applyCompression {
		if err := s.writeJSONRaw(raw, applyCompression, wireSize); err != nil {
			span.RecordError(err)
			return problem.Wrap(err, problem.Internal, "json write failed")
		}
	} else {
		if err := s.writeJSONDirect(frame); err != nil {
			span.RecordError(err)
			return problem.Wrap(err, problem.Internal, "json write failed")
		}
	}
	s.lastDeliveredSeq[subjectKey] = evt.Env.Seq
	metrics.IncWSMessagesOut(channel)
	metrics.IncWSTenantMessagesOut(s.cfg.TenantID, channel)
	metrics.AddWSBytesOut(channel, len(payload))
	metrics.ObserveWSWireBytes(channel, wireSize)
	observeTradeTapeWireBudget(evt.Subject.Venue, evt.Env.Type, len(payload))
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

// ── Wire write helpers ──────────────────────────────────────────────────────

func (s *SessionActor) writeJSON(v any) {
	if err := s.writeJSONDirect(v); err != nil {
		s.logger.Warn("delivery session: write failed", "err", err)
		s.closeSession()
	}
}

func (s *SessionActor) frameFitsMaxBytes(v any) bool {
	if s.limits.MaxFrameBytes <= 0 {
		return true
	}
	raw, err := json.Marshal(v)
	if err != nil {
		s.logger.Warn("delivery session: marshal frame size check failed", "err", err)
		return false
	}
	_, wireSize := s.planWireCompression(raw)
	return wireSize <= s.limits.MaxFrameBytes
}

func (s *SessionActor) writeJSONDirect(v any) error {
	if s.cfg.Conn == nil {
		return nil
	}
	if s.features.HasCompression() {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		applyCompression, wireSize := s.planWireCompression(raw)
		if applyCompression {
			return s.writeJSONRaw(raw, applyCompression, wireSize)
		}
		if c, ok := s.cfg.Conn.(interface{ EnableWriteCompression(bool) }); ok {
			c.EnableWriteCompression(false)
		}
		return s.cfg.Conn.WriteJSON(v)
	}
	return s.cfg.Conn.WriteJSON(v)
}

func (s *SessionActor) writeJSONRawWithLimits(raw []byte) error {
	applyCompression, wireSize := s.planWireCompression(raw)
	if s.limits.MaxFrameBytes > 0 && wireSize > s.limits.MaxFrameBytes {
		return fmt.Errorf("frame exceeds max_frame_bytes (%d)", s.limits.MaxFrameBytes)
	}
	return s.writeJSONRaw(raw, applyCompression, wireSize)
}

func (s *SessionActor) writeJSONRaw(raw []byte, applyCompression bool, wireSize int) error {
	return s.writeRawMessage(websocket.TextMessage, raw, applyCompression, wireSize)
}

func (s *SessionActor) writeBinaryRaw(raw []byte, applyCompression bool, wireSize int) error {
	return s.writeRawMessage(websocket.BinaryMessage, raw, applyCompression, wireSize)
}

func (s *SessionActor) writeRawMessage(messageType int, payload []byte, applyCompression bool, wireSize int) error {
	if s.cfg.Conn == nil {
		return nil
	}
	if c, ok := s.cfg.Conn.(interface{ EnableWriteCompression(bool) }); ok {
		c.EnableWriteCompression(applyCompression)
	}
	if err := s.cfg.Conn.WriteMessage(messageType, payload); err != nil {
		return err
	}
	if applyCompression {
		metrics.IncWSCompressApplied()
		metrics.AddWSCompressBytesIn(len(payload))
		metrics.AddWSCompressBytesOut(wireSize)
	}
	return nil
}

// ── Compression ─────────────────────────────────────────────────────────────

func (s *SessionActor) planWireCompression(payload []byte) (bool, int) {
	wireSize := len(payload)
	if wireSize <= 0 {
		return false, wireSize
	}
	if !s.features.HasCompression() || wireSize < s.limits.CompressThresholdBytes {
		return false, wireSize
	}
	if _, ok := s.cfg.Conn.(interface{ EnableWriteCompression(bool) }); !ok {
		return false, wireSize
	}
	compressed := s.estimateCompressedSize(payload)
	if compressed <= 0 || compressed >= wireSize {
		return false, wireSize
	}
	return true, compressed
}

func (s *SessionActor) estimateCompressedSize(payload []byte) int {
	if len(payload) == 0 || s.compressWriter == nil {
		return len(payload)
	}
	s.compressBuf.Reset()
	s.compressWriter.Reset(&s.compressBuf)
	if _, err := s.compressWriter.Write(payload); err != nil {
		return len(payload)
	}
	if err := s.compressWriter.Close(); err != nil {
		return len(payload)
	}
	return s.compressBuf.Len()
}

// ── Backpressure level ──────────────────────────────────────────────────────

func (s *SessionActor) computeBackpressureLevel() (level int, action string) {
	strategy := s.bpStrategy
	if strategy.criticalRatio <= 0 {
		strategy = defaultBackpressureStrategy()
	}
	return strategy.queueLevel(s.outbound.Len(), s.limits.OutboundQueueSize)
}

// ── Stream metadata builder ─────────────────────────────────────────────────

func (s *SessionActor) buildStreamMeta(subject domain.Subject, item ports.RangeItem) *deliveryv1.StreamMeta {
	nowMs := s.clockNowMs()
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
