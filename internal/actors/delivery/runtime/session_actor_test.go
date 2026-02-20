package deliveryruntime

import (
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/market-raccoon/internal/core/delivery/ports"
)

func TestSessionActor_GetRangeRequest_WritesRangeResponse(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture")
	defer e.Poison(routerPID)

	sub := mustParseSubjectForSession(t, "insights.volume_profile_snapshot.v1/binance/BTCUSDT/1m")
	store := &stubRangeStore{
		bySubject: map[string][]ports.RangeItem{
			sub.String(): {
				{Seq: 1, TsIngest: 1700000000001, Payload: []byte(`{"seq":1}`)},
				{Seq: 2, TsIngest: 1700000000002, Payload: []byte(`{"seq":2}`)},
			},
		},
	}

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn, RangeStore: store}), "ws-session")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)
	e.Send(sessionPID, GetRangeRequest{
		RequestID: "req-1",
		Subject:   "insights.volume_profile_snapshot.v1/binance/BTC-USDT/1m",
		FromMs:    0,
		ToMs:      0,
		Limit:     2,
		Page:      1,
	})

	resp := <-conn.writeCh
	msg, ok := resp.(wsRangeFrame)
	if !ok {
		t.Fatalf("response type = %T, want wsRangeFrame", resp)
	}
	if got, want := msg.Type, "range"; got != want {
		t.Fatalf("type=%v want=%v", got, want)
	}
	if got, want := msg.RequestID, "req-1"; got != want {
		t.Fatalf("request_id=%v want=%v", got, want)
	}
	items, ok := msg.Items.([]ports.RangeItem)
	if !ok {
		t.Fatalf("items type = %T, want []ports.RangeItem", msg.Items)
	}
	if got, want := len(items), 2; got != want {
		t.Fatalf("items len=%d want=%d", got, want)
	}
	if got, want := items[0].Seq, int64(1); got != want {
		t.Fatalf("first seq=%d want=%d", got, want)
	}
	if got, want := items[1].Seq, int64(2); got != want {
		t.Fatalf("second seq=%d want=%d", got, want)
	}
}
