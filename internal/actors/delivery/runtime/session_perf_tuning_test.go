package deliveryruntime

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/market-raccoon/internal/core/delivery/domain"
	sharedclock "github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestBatchingRespectsMaxFrameBytes(t *testing.T) {
	s, conn := newPerfSession(t, SessionConfig{
		MaxFrameBytes: 640,
	})
	s.helloSeen = true
	s.features, _, _ = NegotiateFeatures([]string{"batching"}, false)

	subject := mustParseSubjectForSession(t, "marketdata.trade/binance/BTC-USDT/raw")
	for i := 0; i < 3; i++ {
		s.outbound.PushBack(makePerfEvent(subject, "marketdata.trade", int64(100+i), 1700000000000+int64(i), 120))
	}
	s.flushing = true
	s.flushOutbound()

	totalEvents := 0
	for _, msg := range drainWrites(conn) {
		if got := encodedWriteLen(t, msg); got > s.limits.MaxFrameBytes {
			t.Fatalf("frame len=%d exceeds max_frame_bytes=%d", got, s.limits.MaxFrameBytes)
		}
		totalEvents += deliveredEventsInWrite(t, msg)
	}
	if totalEvents != 3 {
		t.Fatalf("delivered events=%d want=3", totalEvents)
	}
}

func TestBatchingBaseSeqAndCountCorrect(t *testing.T) {
	s, conn := newPerfSession(t, SessionConfig{
		MaxFrameBytes: 64 * 1024,
	})
	s.helloSeen = true
	s.features, _, _ = NegotiateFeatures([]string{"batching"}, false)

	subject := mustParseSubjectForSession(t, "marketdata.trade/binance/BTC-USDT/raw")
	s.outbound.PushBack(makePerfEvent(subject, "marketdata.trade", 10, 1700000000010, 24))
	s.outbound.PushBack(makePerfEvent(subject, "marketdata.trade", 11, 1700000000011, 24))
	s.outbound.PushBack(makePerfEvent(subject, "marketdata.trade", 12, 1700000000012, 24))

	s.flushing = true
	s.flushOutbound()

	writes := drainWrites(conn)
	if len(writes) != 1 {
		t.Fatalf("writes=%d want=1", len(writes))
	}
	bin, ok := writes[0].(fakeBinaryWrite)
	if !ok {
		t.Fatalf("write type=%T want fakeBinaryWrite", writes[0])
	}
	var frame wsBatchFrame
	if err := json.Unmarshal(bin.payload, &frame); err != nil {
		t.Fatalf("unmarshal batch frame: %v", err)
	}
	if frame.Type != "batch" {
		t.Fatalf("type=%q want=batch", frame.Type)
	}
	if frame.BaseSeq != 10 {
		t.Fatalf("base_seq=%d want=10", frame.BaseSeq)
	}
	if frame.Count != 3 {
		t.Fatalf("count=%d want=3", frame.Count)
	}
	if len(frame.Events) != 3 {
		t.Fatalf("events len=%d want=3", len(frame.Events))
	}
	for i := 0; i < 3; i++ {
		if got, want := frame.Events[i].SeqDelta, int64(i); got != want {
			t.Fatalf("events[%d].dseq=%d want=%d", i, got, want)
		}
	}
}

func TestCompressionNegotiationHonored(t *testing.T) {
	s, conn := newPerfSession(t, SessionConfig{
		CompressionEnabled: true,
		MaxFrameBytes:      64 * 1024,
	})
	s.handleClientHello(clientCommand{
		Op:                "hello",
		RequestID:         "h1",
		RequestedFeatures: []string{"compress"},
	})
	_ = drainWrites(conn) // hello ack

	beforeApplied := testutil.ToFloat64(metrics.WSCompressAppliedTotal)
	subject := mustParseSubjectForSession(t, "marketdata.trade/binance/BTC-USDT/raw")
	p := s.writeDeliveryEvent(makePerfEvent(subject, "marketdata.trade", 1, 1700000000100, 4096))
	if p != nil {
		t.Fatalf("writeDeliveryEvent: %v", p)
	}

	if !conn.LastCompressionEnabled() {
		t.Fatal("expected compression enabled for large payload")
	}
	afterApplied := testutil.ToFloat64(metrics.WSCompressAppliedTotal)
	if afterApplied < beforeApplied+1 {
		t.Fatalf("ws_compress_applied_total before=%f after=%f", beforeApplied, afterApplied)
	}
}

func TestCompressionThreshold(t *testing.T) {
	s, conn := newPerfSession(t, SessionConfig{
		CompressionEnabled: true,
		MaxFrameBytes:      64 * 1024,
	})
	s.handleClientHello(clientCommand{
		Op:                "hello",
		RequestID:         "h1",
		RequestedFeatures: []string{"compress"},
	})
	_ = drainWrites(conn) // hello ack

	beforeApplied := testutil.ToFloat64(metrics.WSCompressAppliedTotal)
	subject := mustParseSubjectForSession(t, "marketdata.trade/binance/BTC-USDT/raw")
	p := s.writeDeliveryEvent(makePerfEvent(subject, "marketdata.trade", 2, 1700000000200, 64))
	if p != nil {
		t.Fatalf("writeDeliveryEvent: %v", p)
	}

	if conn.LastCompressionEnabled() {
		t.Fatal("compression must stay disabled below threshold")
	}
	afterApplied := testutil.ToFloat64(metrics.WSCompressAppliedTotal)
	if afterApplied != beforeApplied {
		t.Fatalf("ws_compress_applied_total changed before=%f after=%f", beforeApplied, afterApplied)
	}
}

func TestOutboundQueuePrioritizesControlFrames(t *testing.T) {
	s, conn := newPerfSession(t, SessionConfig{
		OutboundQueueSize: 1,
	})
	subject := mustParseSubjectForSession(t, "marketdata.trade/binance/BTC-USDT/raw")
	s.outbound.PushBack(makePerfEvent(subject, "marketdata.trade", 1, 1700000000300, 64))

	s.handlePing(clientCommand{
		Op:        "ping",
		RequestID: "p1",
		TsClient:  123,
	})

	msgs := drainWrites(conn)
	if len(msgs) == 0 {
		t.Fatal("expected pong frame")
	}
	pong, ok := msgs[0].(wsPongFrame)
	if !ok || pong.Type != "pong" {
		t.Fatalf("first message=%#v want wsPongFrame", msgs[0])
	}
}

func TestDropPolicyPrefersBookDeltas(t *testing.T) {
	s, _ := newPerfSession(t, SessionConfig{
		OutboundQueueSize: 2,
	})
	s.policy = domain.BackpressurePriorityDrop

	subjectTrade := mustParseSubjectForSession(t, "marketdata.trade/binance/BTC-USDT/raw")
	subjectBook := mustParseSubjectForSession(t, "marketdata.bookdelta/binance/BTC-USDT/raw")
	subjectCandle := mustParseSubjectForSession(t, "aggregation.candle/binance/BTC-USDT/1m")

	s.outbound.PushBack(makePerfEvent(subjectTrade, "marketdata.trade", 1, 1700000000400, 32))
	s.outbound.PushBack(makePerfEvent(subjectBook, "marketdata.bookdelta", 2, 1700000000401, 32))

	ok := s.priorityDrop(makePerfEvent(subjectCandle, "aggregation.candle", 3, 1700000000402, 32))
	if !ok {
		t.Fatal("expected incoming candle to replace lower-priority queued event")
	}
	if s.outbound.Len() != 2 {
		t.Fatalf("queue len=%d want=2", s.outbound.Len())
	}
	gotTypes := []string{s.outbound.At(0).Env.Type, s.outbound.At(1).Env.Type}
	if containsType(gotTypes, "marketdata.bookdelta") {
		t.Fatalf("bookdelta should be dropped first, got queue=%v", gotTypes)
	}
	if !containsType(gotTypes, "marketdata.trade") || !containsType(gotTypes, "aggregation.candle") {
		t.Fatalf("expected trade+candle in queue, got %v", gotTypes)
	}
}

func newPerfSession(t *testing.T, cfg SessionConfig) (*SessionActor, *fakeConn) {
	t.Helper()
	clk := sharedclock.NewFakeClock(time.Unix(1700000000, 0))
	conn := newFakeConn()
	cfg.Conn = conn
	cfg.Clock = clk
	cfg.ServerInstanceID = "test-instance"
	if cfg.OutboundQueueSize <= 0 {
		cfg.OutboundQueueSize = 32
	}
	s := &SessionActor{cfg: cfg}
	s.ensureDefaults(nil)
	return s, conn
}

func makePerfEvent(subject domain.Subject, eventType string, seq, tsIngest int64, payloadPad int) DeliveryEvent {
	return DeliveryEvent{
		Subject: subject,
		Env: envelope.Envelope{
			Type:       eventType,
			Version:    1,
			Venue:      subject.Venue,
			Instrument: subject.Symbol,
			Seq:        seq,
			TsIngest:   tsIngest,
			Payload:    []byte(fmt.Sprintf(`{"seq":%d,"pad":"%s"}`, seq, strings.Repeat("x", payloadPad))),
		},
	}
}

func drainWrites(conn *fakeConn) []any {
	out := make([]any, 0, len(conn.writeCh))
	for {
		select {
		case msg := <-conn.writeCh:
			out = append(out, msg)
		default:
			return out
		}
	}
}

func encodedWriteLen(t *testing.T, msg any) int {
	t.Helper()
	switch m := msg.(type) {
	case fakeBinaryWrite:
		return len(m.payload)
	default:
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal write payload (%T): %v", msg, err)
		}
		return len(raw)
	}
}

func deliveredEventsInWrite(t *testing.T, msg any) int {
	t.Helper()
	switch m := msg.(type) {
	case wsEventFrame:
		if m.Type == "event" {
			return 1
		}
		return 0
	case wsBatchFrame:
		if m.Type == "batch" {
			return m.Count
		}
		return 0
	case fakeBinaryWrite:
		var env struct {
			Type  string `json:"type"`
			Count int    `json:"count"`
		}
		if err := json.Unmarshal(m.payload, &env); err != nil {
			return 0
		}
		if env.Type == "batch" {
			return env.Count
		}
		if env.Type == "event" {
			return 1
		}
		return 0
	default:
		return 0
	}
}

func containsType(types []string, want string) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}
