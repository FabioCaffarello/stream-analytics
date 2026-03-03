package deliveryruntime

import (
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/core/delivery/ports"
	"github.com/market-raccoon/internal/shared/envelope"
)

type stubSnapshotProvider struct {
	bySubject map[string][]byte
}

func (s stubSnapshotProvider) GetLatest(subject domain.Subject) ([]byte, bool) {
	v, ok := s.bySubject[subject.String()]
	return v, ok
}

func TestSession_SubscribeEmitsHotSnapshot(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-snapshot")
	defer e.Poison(routerPID)

	subject := mustParseSubjectForSession(t, "marketdata.trade/binance/BTC-USDT/raw")
	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:              routerPID,
		Conn:                   conn,
		HotSnapshotProvider:    stubSnapshotProvider{bySubject: map[string][]byte{subject.String(): []byte(`{"seq":101}`)}},
		OutboundQueueSize:      8,
		BackpressurePolicy:     domain.BackpressureDropNewest,
		BackpressurePriorities: nil,
	}), "ws-session-snapshot")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s1"}`)}

	first := <-conn.writeCh
	snap, ok := first.(wsSnapshotFrame)
	if !ok || snap.Type != "snapshot" {
		t.Fatalf("expected snapshot first, got %#v", first)
	}
	if got, want := snap.SnapshotSource, "hot_snapshot_fallback"; got != want {
		t.Fatalf("snapshot_source=%q want=%q", got, want)
	}
	second := <-conn.writeCh
	ack, ok := second.(wsAckFrame)
	if !ok || ack.Type != "ack" {
		t.Fatalf("expected ack second, got %#v", second)
	}
}

func TestSession_SubscribeNoSnapshot_WhenEmpty(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-no-snapshot")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:           routerPID,
		Conn:                conn,
		HotSnapshotProvider: stubSnapshotProvider{},
	}), "ws-session-no-snapshot")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s2"}`)}

	msg := <-conn.writeCh
	ack, ok := msg.(wsAckFrame)
	if !ok || ack.Type != "ack" {
		t.Fatalf("expected ack when no snapshot, got %#v", msg)
	}
}

func TestSession_GetRange_ReturnsItems(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-range")
	defer e.Poison(routerPID)

	sub := mustParseSubjectForSession(t, "marketdata.trade/binance/BTCUSDT/raw")
	store := &stubRangeStore{
		bySubject: map[string][]ports.RangeItem{
			sub.String(): {
				{Seq: 1, TsIngest: 1, Payload: []byte(`{"seq":1}`)},
				{Seq: 2, TsIngest: 2, Payload: []byte(`{"seq":2}`)},
			},
		},
	}

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn, RangeStore: store}), "ws-session-range")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"getrange","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"r1","params":{"from_ms":0,"to_ms":0,"limit":2}}`)}
	msg := <-conn.writeCh
	resp, ok := msg.(wsRangeFrame)
	if !ok || resp.Type != "range" {
		t.Fatalf("expected range response, got %#v", msg)
	}
}

func TestSession_GetRange_EmptyRange(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-empty-range")
	defer e.Poison(routerPID)

	sub := mustParseSubjectForSession(t, "marketdata.trade/binance/BTCUSDT/raw")
	store := &stubRangeStore{bySubject: map[string][]ports.RangeItem{sub.String(): {}}}

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn, RangeStore: store}), "ws-session-empty-range")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"getrange","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"r2","params":{"from_ms":0,"to_ms":0,"limit":2}}`)}
	msg := <-conn.writeCh
	resp, ok := msg.(wsRangeFrame)
	if !ok || resp.Type != "range" {
		t.Fatalf("expected range response, got %#v", msg)
	}
	items, ok := resp.Items.([]ports.RangeItem)
	if !ok {
		t.Fatalf("items type=%T", resp.Items)
	}
	if len(items) != 0 {
		t.Fatalf("expected empty items, got %d", len(items))
	}
}

func TestSession_GetLast_FallsBackToHotSnapshot_WhenRangeEmpty(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-last-fallback")
	defer e.Poison(routerPID)

	sub := mustParseSubjectForSession(t, "aggregation.candle/binance/BTCUSDT:SPOT/raw")
	store := &stubRangeStore{bySubject: map[string][]ports.RangeItem{sub.String(): {}}}
	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:           routerPID,
		Conn:                conn,
		RangeStore:          store,
		HotSnapshotProvider: stubSnapshotProvider{bySubject: map[string][]byte{sub.String(): []byte(`{"bootstrap":true}`)}},
	}), "ws-session-last-fallback")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"getlast","subject":"aggregation.candle/binance/BTC-USDT:SPOT/raw","request_id":"r-last-fallback"}`)}
	msg := <-conn.writeCh
	resp, ok := msg.(wsLastFrame)
	if !ok || resp.Type != "last" {
		t.Fatalf("expected last response, got %#v", msg)
	}
	item, ok := resp.Item.(ports.RangeItem)
	if !ok {
		t.Fatalf("item type=%T want ports.RangeItem", resp.Item)
	}
	if got, want := resp.SnapshotSource, "hot_snapshot_fallback"; got != want {
		t.Fatalf("snapshot_source=%q want=%q", got, want)
	}
	if got, want := string(item.Payload), `{"bootstrap":true}`; got != want {
		t.Fatalf("payload=%s want=%s", got, want)
	}
}

func TestSession_GetRange_FallsBackToHotSnapshot_WhenUnboundedFirstPageAndRangeEmpty(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-range-fallback")
	defer e.Poison(routerPID)

	sub := mustParseSubjectForSession(t, "aggregation.candle/binance/BTCUSDT:SPOT/raw")
	store := &stubRangeStore{bySubject: map[string][]ports.RangeItem{sub.String(): {}}}
	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:           routerPID,
		Conn:                conn,
		RangeStore:          store,
		HotSnapshotProvider: stubSnapshotProvider{bySubject: map[string][]byte{sub.String(): []byte(`{"bootstrap":true}`)}},
	}), "ws-session-range-fallback")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"getrange","subject":"aggregation.candle/binance/BTC-USDT:SPOT/raw","request_id":"r-range-fallback","params":{"limit":2}}`)}
	msg := <-conn.writeCh
	resp, ok := msg.(wsRangeFrame)
	if !ok || resp.Type != "range" {
		t.Fatalf("expected range response, got %#v", msg)
	}
	items, ok := resp.Items.([]ports.RangeItem)
	if !ok {
		t.Fatalf("items type=%T", resp.Items)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item fallback, got %d", len(items))
	}
	if got, want := resp.SnapshotSource, "hot_snapshot_fallback"; got != want {
		t.Fatalf("snapshot_source=%q want=%q", got, want)
	}
	if got, want := string(items[0].Payload), `{"bootstrap":true}`; got != want {
		t.Fatalf("payload=%s want=%s", got, want)
	}
}

func TestSession_GetRange_FallbackSnapshot_PopulatesSyntheticMetadataFromAggregatePayload(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-range-fallback-meta")
	defer e.Poison(routerPID)

	sub := mustParseSubjectForSession(t, "aggregation.stats/binance/SOLUSDT:USDMFUTURES/raw")
	store := &stubRangeStore{bySubject: map[string][]ports.RangeItem{sub.String(): {}}}
	payload := []byte(`{"WindowEndTs":1234567890000,"SeqLast":98765,"IsClosed":true}`)
	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:           routerPID,
		Conn:                conn,
		RangeStore:          store,
		HotSnapshotProvider: stubSnapshotProvider{bySubject: map[string][]byte{sub.String(): payload}},
	}), "ws-session-range-fallback-meta")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"getrange","subject":"aggregation.stats/binance/SOLUSDT:USDMFUTURES/raw","request_id":"r-range-fallback-meta","params":{"limit":1}}`)}
	msg := <-conn.writeCh
	resp, ok := msg.(wsRangeFrame)
	if !ok || resp.Type != "range" {
		t.Fatalf("expected range response, got %#v", msg)
	}
	items, ok := resp.Items.([]ports.RangeItem)
	if !ok || len(items) != 1 {
		t.Fatalf("items=%#v", resp.Items)
	}
	if got, want := resp.SnapshotSource, "hot_snapshot_fallback"; got != want {
		t.Fatalf("snapshot_source=%q want=%q", got, want)
	}
	if items[0].Seq != 98765 {
		t.Fatalf("seq=%d want=98765", items[0].Seq)
	}
	if items[0].TsIngest != 1234567890000 {
		t.Fatalf("ts_ingest=%d want=1234567890000", items[0].TsIngest)
	}
}

func TestSession_GetRange_LimitEnforced(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-limit")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn, RangeStore: &stubRangeStore{}}), "ws-session-limit")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"getrange","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"r3","params":{"from_ms":0,"to_ms":0,"limit":1001}}`)}
	msg := <-conn.writeCh
	resp, ok := msg.(wsErrorFrame)
	if !ok || resp.Type != "error" {
		t.Fatalf("expected error response, got %#v", msg)
	}
}

func TestSession_ResyncFallsBackToLastDeliveredEventSnapshot(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-resync-fallback")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:           routerPID,
		Conn:                conn,
		HotSnapshotProvider: stubSnapshotProvider{},
	}), "ws-session-resync-fallback")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s1"}`)}
	subAck := <-conn.writeCh
	if ack, ok := subAck.(wsAckFrame); !ok || ack.Op != "subscribe" {
		t.Fatalf("expected subscribe ack, got %#v", subAck)
	}

	subject := mustParseSubjectForSession(t, "marketdata.trade/binance/BTC-USDT/raw")
	e.Send(sessionPID, DeliveryEvent{
		Subject: subject,
		Env: envelope.Envelope{
			Type:     "marketdata.trade",
			Version:  1,
			Seq:      10,
			TsIngest: time.Now().UnixMilli(),
			Payload:  []byte(`{"Price":50000,"Size":1.0,"Side":"buy","TradeID":"t1","Timestamp":1700000000000}`),
		},
	})
	if evt, ok := (<-conn.writeCh).(wsEventFrame); !ok || evt.Type != "event" {
		t.Fatalf("expected event frame before resync, got %#v", evt)
	}

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"resync","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"r1","last_seq":10}`)}
	snapMsg := <-conn.writeCh
	snap, ok := snapMsg.(wsSnapshotFrame)
	if !ok || snap.Type != "snapshot" {
		t.Fatalf("expected snapshot first on resync, got %#v", snapMsg)
	}
	if got, want := snap.SnapshotSource, "session_last_event"; got != want {
		t.Fatalf("snapshot_source=%q want=%q", got, want)
	}
	if snap.Seq <= 0 {
		t.Fatalf("snapshot seq=%d want > 0", snap.Seq)
	}
	ackMsg := <-conn.writeCh
	ack, ok := ackMsg.(wsAckFrame)
	if !ok || ack.Op != "resync" {
		t.Fatalf("expected resync ack second, got %#v", ackMsg)
	}
}
