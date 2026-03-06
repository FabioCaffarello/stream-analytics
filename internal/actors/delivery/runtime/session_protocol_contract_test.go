package deliveryruntime

import (
	"fmt"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/shared/envelope"
)

// ── WS-10: prev_seq chain across events ─────────────────────────────────────

func TestProtocol_PrevSeqChain_MonotonicAcrossEvents(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-prevseq-chain")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:           routerPID,
		Conn:                conn,
		HotSnapshotProvider: stubSnapshotProvider{},
		OutboundQueueSize:   16,
		BackpressurePolicy:  domain.BackpressureDropNewest,
	}), "ws-session-prevseq-chain")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s1"}`)}
	ack := <-conn.writeCh
	if a, ok := ack.(wsAckFrame); !ok || a.Op != "subscribe" {
		t.Fatalf("expected subscribe ack, got %#v", ack)
	}

	subject := mustParseSubjectForSession(t, "marketdata.trade/binance/BTC-USDT/raw")
	seqs := []int64{10, 11, 12}
	for _, seq := range seqs {
		e.Send(sessionPID, DeliveryEvent{
			Subject: subject,
			Env: envelope.Envelope{
				Type:     "marketdata.trade",
				Version:  1,
				Seq:      seq,
				TsIngest: time.Now().UnixMilli(),
				Payload:  []byte(`{"Price":50000,"Size":1.0,"Side":"buy","TradeID":"t1","Timestamp":1700000000000}`),
			},
		})
	}

	// First event: prev_seq must be 0 (fresh chain after subscribe)
	evt1 := readEventFrame(t, conn.writeCh)
	if evt1.Seq != 10 {
		t.Fatalf("event1.seq=%d want=10", evt1.Seq)
	}
	if evt1.PrevSeq != 0 {
		t.Fatalf("event1.prev_seq=%d want=0 (first after subscribe)", evt1.PrevSeq)
	}

	// Second event: prev_seq must equal first event's seq
	evt2 := readEventFrame(t, conn.writeCh)
	if evt2.Seq != 11 {
		t.Fatalf("event2.seq=%d want=11", evt2.Seq)
	}
	if evt2.PrevSeq != 10 {
		t.Fatalf("event2.prev_seq=%d want=10", evt2.PrevSeq)
	}

	// Third event: prev_seq must equal second event's seq
	evt3 := readEventFrame(t, conn.writeCh)
	if evt3.Seq != 12 {
		t.Fatalf("event3.seq=%d want=12", evt3.Seq)
	}
	if evt3.PrevSeq != 11 {
		t.Fatalf("event3.prev_seq=%d want=11", evt3.PrevSeq)
	}
}

// ── WS-10: prev_seq resets to 0 after resync ────────────────────────────────

func TestProtocol_PrevSeqZero_AfterResync(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-prevseq-resync")
	defer e.Poison(routerPID)

	subject := mustParseSubjectForSession(t, "marketdata.trade/binance/BTC-USDT/raw")
	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:           routerPID,
		Conn:                conn,
		HotSnapshotProvider: stubSnapshotProvider{bySubject: map[string][]byte{subject.String(): []byte(`{"seq":100}`)}},
		OutboundQueueSize:   16,
		BackpressurePolicy:  domain.BackpressureDropNewest,
	}), "ws-session-prevseq-resync")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	// Subscribe — snapshot + ack
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s1"}`)}
	<-conn.writeCh // snapshot
	<-conn.writeCh // ack

	// Deliver two events to establish a chain
	for _, seq := range []int64{10, 11} {
		e.Send(sessionPID, DeliveryEvent{
			Subject: subject,
			Env: envelope.Envelope{
				Type: "marketdata.trade", Version: 1, Seq: seq,
				TsIngest: time.Now().UnixMilli(),
				Payload:  []byte(`{"Price":50000,"Size":1.0,"Side":"buy","TradeID":"t1","Timestamp":1700000000000}`),
			},
		})
	}
	evt1 := readEventFrame(t, conn.writeCh)
	evt2 := readEventFrame(t, conn.writeCh)
	if evt2.PrevSeq != evt1.Seq {
		t.Fatalf("pre-resync chain broken: event2.prev_seq=%d want=%d", evt2.PrevSeq, evt1.Seq)
	}

	// Resync — snapshot + ack
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"resync","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"r1","last_seq":11}`)}
	snap := <-conn.writeCh
	if s, ok := snap.(wsSnapshotFrame); !ok || s.Type != "snapshot" {
		t.Fatalf("expected snapshot on resync, got %#v", snap)
	}
	ackMsg := <-conn.writeCh
	if a, ok := ackMsg.(wsAckFrame); !ok || a.Op != "resync" {
		t.Fatalf("expected resync ack, got %#v", ackMsg)
	}

	// Deliver event after resync — prev_seq must be 0
	e.Send(sessionPID, DeliveryEvent{
		Subject: subject,
		Env: envelope.Envelope{
			Type: "marketdata.trade", Version: 1, Seq: 20,
			TsIngest: time.Now().UnixMilli(),
			Payload:  []byte(`{"Price":50000,"Size":1.0,"Side":"buy","TradeID":"t1","Timestamp":1700000000000}`),
		},
	})
	postResync := readEventFrame(t, conn.writeCh)
	if postResync.Seq != 20 {
		t.Fatalf("post-resync event.seq=%d want=20", postResync.Seq)
	}
	if postResync.PrevSeq != 0 {
		t.Fatalf("post-resync event.prev_seq=%d want=0 (chain reset after resync)", postResync.PrevSeq)
	}
}

// ── WS-9: snapshot_seq monotonically increases ──────────────────────────────

func TestProtocol_SnapshotSeq_IncrementsOnResync(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-snapshotseq")
	defer e.Poison(routerPID)

	subject := mustParseSubjectForSession(t, "aggregation.candle/binance/BTC-USDT/1m")
	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:           routerPID,
		Conn:                conn,
		HotSnapshotProvider: stubSnapshotProvider{bySubject: map[string][]byte{subject.String(): []byte(`{"Open":100,"Close":101}`)}},
		OutboundQueueSize:   16,
		BackpressurePolicy:  domain.BackpressureDropNewest,
	}), "ws-session-snapshotseq")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	// Subscribe — should get snapshot with snapshot_seq = 1
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"aggregation.candle/binance/BTC-USDT/1m","request_id":"s1"}`)}
	snap1 := readSnapshotFrame(t, conn.writeCh)
	<-conn.writeCh // ack
	if snap1.SnapshotSeq != 1 {
		t.Fatalf("subscribe snapshot_seq=%d want=1", snap1.SnapshotSeq)
	}

	// Resync — should get snapshot with snapshot_seq = 2
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"resync","subject":"aggregation.candle/binance/BTC-USDT/1m","request_id":"r1"}`)}
	snap2 := readSnapshotFrame(t, conn.writeCh)
	<-conn.writeCh // ack
	if snap2.SnapshotSeq != 2 {
		t.Fatalf("resync snapshot_seq=%d want=2", snap2.SnapshotSeq)
	}

	// Second resync — snapshot_seq = 3
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"resync","subject":"aggregation.candle/binance/BTC-USDT/1m","request_id":"r2"}`)}
	snap3 := readSnapshotFrame(t, conn.writeCh)
	<-conn.writeCh // ack
	if snap3.SnapshotSeq != 3 {
		t.Fatalf("second resync snapshot_seq=%d want=3", snap3.SnapshotSeq)
	}

	// Verify monotonicity: WS-9
	if snap3.SnapshotSeq <= snap2.SnapshotSeq || snap2.SnapshotSeq <= snap1.SnapshotSeq {
		t.Fatalf("WS-9 violated: snapshot_seq not monotonic: %d, %d, %d", snap1.SnapshotSeq, snap2.SnapshotSeq, snap3.SnapshotSeq)
	}
}

// ── WS-11: snapshot before ack on subscribe (explicit contract) ─────────────

func TestProtocol_WS11_SnapshotBeforeAck_OnSubscribe(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-ws11")
	defer e.Poison(routerPID)

	subject := mustParseSubjectForSession(t, "marketdata.trade/binance/BTC-USDT/raw")
	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:           routerPID,
		Conn:                conn,
		HotSnapshotProvider: stubSnapshotProvider{bySubject: map[string][]byte{subject.String(): []byte(`{"snapshot":true}`)}},
		OutboundQueueSize:   16,
		BackpressurePolicy:  domain.BackpressureDropNewest,
	}), "ws-session-ws11")
	defer e.Poison(sessionPID)
	_ = sessionPID

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s1"}`)}

	// WS-11: first frame MUST be snapshot (when available)
	first := <-conn.writeCh
	snap, ok := first.(wsSnapshotFrame)
	if !ok || snap.Type != "snapshot" {
		t.Fatalf("WS-11 violated: first frame after subscribe must be snapshot when available, got %T %#v", first, first)
	}

	// Second frame MUST be ack
	second := <-conn.writeCh
	ack, ok := second.(wsAckFrame)
	if !ok || ack.Type != "ack" || ack.Op != "subscribe" {
		t.Fatalf("WS-11 violated: second frame after subscribe must be ack, got %T %#v", second, second)
	}
}

// ── WS-11: no snapshot → ack is first frame ─────────────────────────────────

func TestProtocol_WS11_NoSnapshot_AckFirst(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-ws11-nosnapshot")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	_ = e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:           routerPID,
		Conn:                conn,
		HotSnapshotProvider: stubSnapshotProvider{}, // empty — no snapshots
		OutboundQueueSize:   16,
		BackpressurePolicy:  domain.BackpressureDropNewest,
	}), "ws-session-ws11-nosnapshot")

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s1"}`)}

	// When no snapshot available, first frame is ack directly
	first := <-conn.writeCh
	ack, ok := first.(wsAckFrame)
	if !ok || ack.Type != "ack" || ack.Op != "subscribe" {
		t.Fatalf("expected ack as first frame when no snapshot, got %T %#v", first, first)
	}
}

// ── S18-Slice2: Resync ack carries watermark_seq and snapshot_seq ─────────

func TestProtocol_ResyncAck_CarriesWatermark(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-resync-watermark")
	defer e.Poison(routerPID)

	subject := mustParseSubjectForSession(t, "aggregation.candle/binance/BTC-USDT/1m")
	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:           routerPID,
		Conn:                conn,
		HotSnapshotProvider: stubSnapshotProvider{bySubject: map[string][]byte{subject.String(): []byte(`{"Open":100,"Close":101}`)}},
		OutboundQueueSize:   16,
		BackpressurePolicy:  domain.BackpressureDropNewest,
	}), "ws-session-resync-watermark")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	// Subscribe — snapshot + ack (subscribe ack has no watermark)
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"aggregation.candle/binance/BTC-USDT/1m","request_id":"s1"}`)}
	<-conn.writeCh // snapshot
	subAck := readAckFrame(t, conn.writeCh)
	if subAck.Op != "subscribe" {
		t.Fatalf("expected subscribe ack, got op=%s", subAck.Op)
	}

	// Deliver an event so lastSnapshot gets populated with a known seq
	e.Send(sessionPID, DeliveryEvent{
		Subject: subject,
		Env: envelope.Envelope{
			Type: "aggregation.candle", Version: 1, Seq: 42,
			TsIngest: time.Now().UnixMilli(),
			Payload:  []byte(`{"Open":100,"Close":102}`),
		},
	})
	_ = readEventFrame(t, conn.writeCh)

	// Resync — snapshot + ack with watermark
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"resync","subject":"aggregation.candle/binance/BTC-USDT/1m","request_id":"r1"}`)}
	<-conn.writeCh // snapshot
	resyncAck := readAckFrame(t, conn.writeCh)
	if resyncAck.Op != "resync" {
		t.Fatalf("expected resync ack, got op=%s", resyncAck.Op)
	}
	if resyncAck.SnapshotSeq < 1 {
		t.Fatalf("resync ack snapshot_seq=%d want>=1", resyncAck.SnapshotSeq)
	}
	// The watermark should reflect the last delivered event's seq (42)
	// because emitSnapshot uses lastSnapshot which was populated by the event.
	if resyncAck.WatermarkSeq != 42 {
		t.Fatalf("resync ack watermark_seq=%d want=42", resyncAck.WatermarkSeq)
	}
}

// ── S18-Slice2: Resync watermark monotonicity across multiple resyncs ────

func TestProtocol_ResyncWatermark_MonotonicAcrossResyncs(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-resync-mono")
	defer e.Poison(routerPID)

	subject := mustParseSubjectForSession(t, "marketdata.trade/binance/BTC-USDT/raw")
	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:           routerPID,
		Conn:                conn,
		HotSnapshotProvider: stubSnapshotProvider{bySubject: map[string][]byte{subject.String(): []byte(`{"Price":50000}`)}},
		OutboundQueueSize:   16,
		BackpressurePolicy:  domain.BackpressureDropNewest,
	}), "ws-session-resync-mono")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	// Subscribe
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s1"}`)}
	<-conn.writeCh // snapshot
	<-conn.writeCh // ack

	// Deliver events with increasing seq
	for _, seq := range []int64{10, 20, 30} {
		e.Send(sessionPID, DeliveryEvent{
			Subject: subject,
			Env: envelope.Envelope{
				Type: "marketdata.trade", Version: 1, Seq: seq,
				TsIngest: time.Now().UnixMilli(),
				Payload:  []byte(`{"Price":50000,"Size":1.0,"Side":"buy","TradeID":"t1","Timestamp":1700000000000}`),
			},
		})
		_ = readEventFrame(t, conn.writeCh)
	}

	// First resync
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"resync","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"r1"}`)}
	<-conn.writeCh // snapshot
	ack1 := readAckFrame(t, conn.writeCh)
	if ack1.WatermarkSeq != 30 {
		t.Fatalf("resync1 watermark_seq=%d want=30", ack1.WatermarkSeq)
	}
	if ack1.SnapshotSeq != 2 {
		t.Fatalf("resync1 snapshot_seq=%d want=2", ack1.SnapshotSeq)
	}

	// Deliver more events
	for _, seq := range []int64{40, 50} {
		e.Send(sessionPID, DeliveryEvent{
			Subject: subject,
			Env: envelope.Envelope{
				Type: "marketdata.trade", Version: 1, Seq: seq,
				TsIngest: time.Now().UnixMilli(),
				Payload:  []byte(`{"Price":50000,"Size":1.0,"Side":"buy","TradeID":"t1","Timestamp":1700000000000}`),
			},
		})
		_ = readEventFrame(t, conn.writeCh)
	}

	// Second resync
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"resync","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"r2"}`)}
	<-conn.writeCh // snapshot
	ack2 := readAckFrame(t, conn.writeCh)
	if ack2.WatermarkSeq != 50 {
		t.Fatalf("resync2 watermark_seq=%d want=50", ack2.WatermarkSeq)
	}
	if ack2.SnapshotSeq != 3 {
		t.Fatalf("resync2 snapshot_seq=%d want=3", ack2.SnapshotSeq)
	}

	// Verify monotonicity
	if ack2.WatermarkSeq <= ack1.WatermarkSeq {
		t.Fatalf("watermark_seq not monotonic: %d <= %d", ack2.WatermarkSeq, ack1.WatermarkSeq)
	}
	if ack2.SnapshotSeq <= ack1.SnapshotSeq {
		t.Fatalf("snapshot_seq not monotonic: %d <= %d", ack2.SnapshotSeq, ack1.SnapshotSeq)
	}
}

// ── S18-Slice2: Per-session resync counter in metrics frame ─────────────

func TestProtocol_MetricsFrame_ResyncCount(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-resync-count")
	defer e.Poison(routerPID)

	subject := mustParseSubjectForSession(t, "aggregation.candle/binance/BTC-USDT/1m")
	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:           routerPID,
		Conn:                conn,
		HotSnapshotProvider: stubSnapshotProvider{bySubject: map[string][]byte{subject.String(): []byte(`{"Open":100}`)}},
		OutboundQueueSize:   16,
		BackpressurePolicy:  domain.BackpressureDropNewest,
	}), "ws-session-resync-count")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	// Subscribe
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"aggregation.candle/binance/BTC-USDT/1m","request_id":"s1"}`)}
	<-conn.writeCh // snapshot
	<-conn.writeCh // ack

	// Trigger metrics tick — resync_count should be 0
	e.Send(sessionPID, sessionMetricsTick{})
	m1 := readMetricsFrame(t, conn.writeCh)
	if m1.Payload.ResyncCount != 0 {
		t.Fatalf("pre-resync resync_count=%d want=0", m1.Payload.ResyncCount)
	}

	// Do 2 resyncs
	for i := 0; i < 2; i++ {
		conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"resync","subject":"aggregation.candle/binance/BTC-USDT/1m","request_id":"rx"}`)}
		<-conn.writeCh // snapshot
		<-conn.writeCh // ack
	}

	// Trigger metrics tick — resync_count should be 2
	e.Send(sessionPID, sessionMetricsTick{})
	m2 := readMetricsFrame(t, conn.writeCh)
	if m2.Payload.ResyncCount != 2 {
		t.Fatalf("post-resync resync_count=%d want=2", m2.Payload.ResyncCount)
	}
}

// ── S18-Slice3: RequireClientHello gate rejects subscribe without hello ──

func TestProtocol_HelloGate_RejectsSubscribeWithoutHello(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-hellogate")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	_ = e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:          routerPID,
		Conn:               conn,
		OutboundQueueSize:  16,
		BackpressurePolicy: domain.BackpressureDropNewest,
		RequireClientHello: true, // gate enabled
	}), "ws-session-hellogate")

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	// Subscribe without hello — should be rejected
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s1"}`)}
	errFrame := readErrorFrame(t, conn.writeCh)
	if errFrame.Op != "subscribe" {
		t.Fatalf("expected subscribe error, got op=%s", errFrame.Op)
	}
	if errFrame.Problem.Code != "VAL_VALIDATION_FAILED" {
		t.Fatalf("expected validation error, got code=%s", errFrame.Problem.Code)
	}
}

// ── S18-Slice3: HelloGate allows subscribe after hello ──────────────────

func TestProtocol_HelloGate_AllowsAfterHello(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-hellogate-ok")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	_ = e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:          routerPID,
		Conn:               conn,
		OutboundQueueSize:  16,
		BackpressurePolicy: domain.BackpressureDropNewest,
		RequireClientHello: true,
	}), "ws-session-hellogate-ok")

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	// Send hello first
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"hello","request_id":"h1"}`)}
	helloAck := readHelloAckFrame(t, conn.writeCh)
	if helloAck.Op != "hello" {
		t.Fatalf("expected hello ack, got op=%s", helloAck.Op)
	}

	// Now subscribe — should succeed
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s1"}`)}
	subAck := readAckFrame(t, conn.writeCh)
	if subAck.Op != "subscribe" {
		t.Fatalf("expected subscribe ack, got op=%s", subAck.Op)
	}
}

// ── S18-Slice3: Clock skew diagnostic in hello ack ──────────────────────

func TestProtocol_HelloAck_ClockSkew(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-clockskew")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	_ = e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:          routerPID,
		Conn:               conn,
		OutboundQueueSize:  16,
		BackpressurePolicy: domain.BackpressureDropNewest,
	}), "ws-session-clockskew")

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	// Send hello with ts_client
	nowMs := time.Now().UnixMilli()
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"hello","request_id":"h1","ts_client":` + itoa64(nowMs) + `}`)}
	ack := readHelloAckFrame(t, conn.writeCh)
	if ack.TsServer <= 0 {
		t.Fatalf("hello ack ts_server=%d want>0", ack.TsServer)
	}
	// Clock skew should be very small (same machine)
	if ack.ClockSkewMs < -5000 || ack.ClockSkewMs > 5000 {
		t.Fatalf("hello ack clock_skew_ms=%d out of expected range", ack.ClockSkewMs)
	}
}

// ── S18-Slice3: Hello ack without ts_client has no clock_skew ───────────

func TestProtocol_HelloAck_NoClockSkewWithoutTsClient(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-noskew")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	_ = e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:          routerPID,
		Conn:               conn,
		OutboundQueueSize:  16,
		BackpressurePolicy: domain.BackpressureDropNewest,
	}), "ws-session-noskew")

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	// Send hello without ts_client
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"hello","request_id":"h1"}`)}
	ack := readHelloAckFrame(t, conn.writeCh)
	if ack.TsServer <= 0 {
		t.Fatalf("hello ack ts_server=%d want>0", ack.TsServer)
	}
	if ack.ClockSkewMs != 0 {
		t.Fatalf("hello ack clock_skew_ms=%d want=0 (no ts_client)", ack.ClockSkewMs)
	}
}

// ── S18-Slice4: Metrics frame includes dropped_count and subject_count ──

func TestProtocol_MetricsFrame_DiagnosticCounters(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-diag-counters")
	defer e.Poison(routerPID)

	subject := mustParseSubjectForSession(t, "marketdata.trade/binance/BTC-USDT/raw")
	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:           routerPID,
		Conn:                conn,
		HotSnapshotProvider: stubSnapshotProvider{},
		OutboundQueueSize:   16,
		BackpressurePolicy:  domain.BackpressureDropNewest,
	}), "ws-session-diag-counters")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	// Subscribe
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s1"}`)}
	<-conn.writeCh // ack (no snapshot since empty provider)

	// Deliver an event to create a subject entry in lastDeliveredSeq
	e.Send(sessionPID, DeliveryEvent{
		Subject: subject,
		Env: envelope.Envelope{
			Type: "marketdata.trade", Version: 1, Seq: 1,
			TsIngest: time.Now().UnixMilli(),
			Payload:  []byte(`{"Price":50000,"Size":1.0,"Side":"buy","TradeID":"t1","Timestamp":1700000000000}`),
		},
	})
	_ = readEventFrame(t, conn.writeCh)

	// Trigger metrics tick
	e.Send(sessionPID, sessionMetricsTick{})
	m := readMetricsFrame(t, conn.writeCh)
	if m.Payload.SubjectCount < 1 {
		t.Fatalf("subject_count=%d want>=1", m.Payload.SubjectCount)
	}
	if m.Payload.DroppedCount != 0 {
		t.Fatalf("dropped_count=%d want=0", m.Payload.DroppedCount)
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func readErrorFrame(t *testing.T, ch <-chan any) wsErrorFrame {
	t.Helper()
	select {
	case msg := <-ch:
		ef, ok := msg.(wsErrorFrame)
		if !ok {
			t.Fatalf("expected wsErrorFrame, got %T %#v", msg, msg)
		}
		return ef
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for error frame")
		return wsErrorFrame{}
	}
}

func readHelloAckFrame(t *testing.T, ch <-chan any) wsHelloAckFrame {
	t.Helper()
	select {
	case msg := <-ch:
		ack, ok := msg.(wsHelloAckFrame)
		if !ok {
			t.Fatalf("expected wsHelloAckFrame, got %T %#v", msg, msg)
		}
		return ack
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for hello ack frame")
		return wsHelloAckFrame{}
	}
}

func itoa64(v int64) string {
	return fmt.Sprintf("%d", v)
}

func readAckFrame(t *testing.T, ch <-chan any) wsAckFrame {
	t.Helper()
	select {
	case msg := <-ch:
		ack, ok := msg.(wsAckFrame)
		if !ok {
			t.Fatalf("expected wsAckFrame, got %T %#v", msg, msg)
		}
		return ack
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ack frame")
		return wsAckFrame{}
	}
}

func readMetricsFrame(t *testing.T, ch <-chan any) wsMetricsFrame {
	t.Helper()
	select {
	case msg := <-ch:
		m, ok := msg.(wsMetricsFrame)
		if !ok {
			t.Fatalf("expected wsMetricsFrame, got %T %#v", msg, msg)
		}
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for metrics frame")
		return wsMetricsFrame{}
	}
}

func readEventFrame(t *testing.T, ch <-chan any) wsEventFrame {
	t.Helper()
	select {
	case msg := <-ch:
		evt, ok := msg.(wsEventFrame)
		if !ok {
			t.Fatalf("expected wsEventFrame, got %T %#v", msg, msg)
		}
		return evt
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event frame")
		return wsEventFrame{}
	}
}

func readSnapshotFrame(t *testing.T, ch <-chan any) wsSnapshotFrame {
	t.Helper()
	select {
	case msg := <-ch:
		snap, ok := msg.(wsSnapshotFrame)
		if !ok {
			t.Fatalf("expected wsSnapshotFrame, got %T %#v", msg, msg)
		}
		return snap
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for snapshot frame")
		return wsSnapshotFrame{}
	}
}
