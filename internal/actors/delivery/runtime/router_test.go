package deliveryruntime

import (
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/market-raccoon/internal/core/delivery/domain"
	sharedclock "github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/ids"
	sharedmetrics "github.com/market-raccoon/internal/shared/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type captureActor struct{ ch chan any }

func (c *captureActor) Receive(ctx *actor.Context) {
	switch m := ctx.Message().(type) {
	case actor.Initialized, actor.Started, actor.Stopped:
	default:
		select {
		case c.ch <- m:
		default:
		}
	}
}

func mustParseSubject(t *testing.T, raw string) domain.Subject {
	t.Helper()
	s, p := domain.ParseSubject(raw)
	if p != nil {
		t.Fatalf("ParseSubject(%q): %v", raw, p)
	}
	return s
}

func waitForMessage[T any](t *testing.T, ch <-chan any, timeout time.Duration) T {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case raw := <-ch:
			if msg, ok := raw.(T); ok {
				return msg
			}
		case <-deadline:
			var zero T
			t.Fatalf("timeout waiting for %T", zero)
		}
	}
}

func waitForMetricEqual(t *testing.T, name string, read func() float64, want float64, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		got := read()
		if got == want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("%s=%f want=%f", name, got, want)
		case <-tick.C:
		}
	}
}

func waitForMetricAtLeast(t *testing.T, name string, read func() float64, want float64, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		got := read()
		if got >= want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("%s=%f want>=%f", name, got, want)
		case <-tick.C:
		}
	}
}

func TestRouter_subscribeUnsubscribeAndBroadcast(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(NewRouterActor(RouterConfig{Timeframe: "raw"}), "router")
	defer e.Poison(routerPID)

	ch1 := make(chan any, 16)
	ch2 := make(chan any, 16)
	s1 := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch1} }, "session-capture")
	s2 := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch2} }, "session-capture")
	defer e.Poison(s1)
	defer e.Poison(s2)

	id1 := ids.NewSessionID()
	id2 := ids.NewSessionID()
	subject := mustParseSubject(t, "marketdata.trade/binance/BTC-USDT/raw")

	e.Send(routerPID, RegisterSession{SessionID: id1, PID: s1})
	e.Send(routerPID, RegisterSession{SessionID: id2, PID: s2})
	e.Send(routerPID, SubscribeSession{SessionID: id1, Subject: subject})
	e.Send(routerPID, SubscribeSession{SessionID: id2, Subject: subject})

	e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{Type: "marketdata.trade", Version: 1, Venue: "binance", Instrument: "BTC-USDT", Seq: 10, TsIngest: time.Now().UnixMilli(), Payload: []byte("x")}})

	msg1 := waitForMessage[DeliveryEvent](t, ch1, time.Second)
	msg2 := waitForMessage[DeliveryEvent](t, ch2, time.Second)
	if msg1.Subject != subject || msg2.Subject != subject {
		t.Fatalf("unexpected subject: msg1=%s msg2=%s", msg1.Subject.String(), msg2.Subject.String())
	}

	e.Send(routerPID, UnsubscribeSession{SessionID: id2, Subject: subject})
	e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{Type: "marketdata.trade", Version: 1, Venue: "binance", Instrument: "BTC-USDT", Seq: 11, TsIngest: time.Now().UnixMilli(), Payload: []byte("y")}})

	_ = waitForMessage[DeliveryEvent](t, ch1, time.Second)
	select {
	case raw := <-ch2:
		if _, ok := raw.(DeliveryEvent); ok {
			t.Fatal("session 2 should not receive after unsubscribe")
		}
	case <-time.After(150 * time.Millisecond):
	}

	e.Send(routerPID, UnregisterSession{SessionID: id1})
	e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{Type: "marketdata.trade", Version: 1, Venue: "binance", Instrument: "BTC-USDT", Seq: 12, TsIngest: time.Now().UnixMilli(), Payload: []byte("z")}})
	select {
	case raw := <-ch1:
		if _, ok := raw.(DeliveryEvent); ok {
			t.Fatal("session 1 should not receive after unregister")
		}
	case <-time.After(150 * time.Millisecond):
	}
}

func TestRouter_MaxSessionsEnforced(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	rejectedBefore := testutil.ToFloat64(sharedmetrics.DeliveryRouterSessionsRejectedTotal)
	routerPID := e.Spawn(NewRouterActor(RouterConfig{
		Timeframe:         "raw",
		MaxActiveSessions: 1,
	}), "router-max-sessions")
	defer e.Poison(routerPID)

	ch1 := make(chan any, 16)
	ch2 := make(chan any, 16)
	s1 := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch1} }, "session-max-sessions-1")
	s2 := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch2} }, "session-max-sessions-2")
	defer e.Poison(s1)
	defer e.Poison(s2)

	id1 := ids.NewSessionID()
	id2 := ids.NewSessionID()
	subject := mustParseSubject(t, "marketdata.trade/binance/BTC-USDT/raw")

	e.Send(routerPID, RegisterSession{SessionID: id1, PID: s1})
	e.Send(routerPID, RegisterSession{SessionID: id2, PID: s2})
	e.Send(routerPID, SubscribeSession{SessionID: id1, Subject: subject})
	e.Send(routerPID, SubscribeSession{SessionID: id2, Subject: subject})

	e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       "marketdata.trade",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        1,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{}`),
	}})

	_ = waitForMessage[DeliveryEvent](t, ch1, time.Second)
	select {
	case raw := <-ch2:
		if _, ok := raw.(DeliveryEvent); ok {
			t.Fatal("rejected session should not receive delivery events")
		}
	case <-time.After(150 * time.Millisecond):
	}

	waitForMetricEqual(t, "delivery_router_sessions_active", func() float64 {
		return testutil.ToFloat64(sharedmetrics.DeliveryRouterSessionsActive)
	}, 1, time.Second)
	waitForMetricAtLeast(t, "delivery_router_sessions_rejected_total", func() float64 {
		return testutil.ToFloat64(sharedmetrics.DeliveryRouterSessionsRejectedTotal)
	}, rejectedBefore+1, time.Second)
}

func TestRouter_RoutesSignalSubjects(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(NewRouterActor(RouterConfig{Timeframe: "raw"}), "router-signal")
	defer e.Poison(routerPID)

	ch := make(chan any, 16)
	sessionPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-signal-capture")
	defer e.Poison(sessionPID)

	id := ids.NewSessionID()
	subject := mustParseSubject(t, "signal/absorption/binance/BTC-USDT/1m")

	e.Send(routerPID, RegisterSession{SessionID: id, PID: sessionPID})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})
	e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       "signal.composite",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        1,
		TsIngest:   time.Now().UnixMilli(),
		Meta: map[string]string{
			"timeframe": "1m",
			"kind":      "absorption",
		},
		Payload: []byte(`{"kind":"absorption"}`),
	}})

	msg := waitForMessage[DeliveryEvent](t, ch, time.Second)
	if got, want := msg.Subject.String(), "signal/absorption/binance/BTCUSDT/1m"; got != want {
		t.Fatalf("subject=%q want=%q", got, want)
	}
}

func TestRouter_RoutesSignalWildcardKindSubjects(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(NewRouterActor(RouterConfig{Timeframe: "raw"}), "router-signal-wild")
	defer e.Poison(routerPID)

	wildCh := make(chan any, 16)
	exactCh := make(chan any, 16)
	wildPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: wildCh} }, "session-signal-wild")
	exactPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: exactCh} }, "session-signal-exact")
	defer e.Poison(wildPID)
	defer e.Poison(exactPID)

	wildID := ids.NewSessionID()
	exactID := ids.NewSessionID()
	wildSubject := mustParseSubject(t, "signal/*/binance/BTC-USDT/1m")
	exactSubject := mustParseSubject(t, "signal/liquidity_collapse/binance/BTC-USDT/1m")

	e.Send(routerPID, RegisterSession{SessionID: wildID, PID: wildPID})
	e.Send(routerPID, RegisterSession{SessionID: exactID, PID: exactPID})
	e.Send(routerPID, SubscribeSession{SessionID: wildID, Subject: wildSubject})
	e.Send(routerPID, SubscribeSession{SessionID: exactID, Subject: exactSubject})
	e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       "signal.event",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        10,
		TsIngest:   time.Now().UnixMilli(),
		Meta: map[string]string{
			"timeframe": "1m",
			"kind":      "liquidity_collapse",
		},
		Payload: []byte(`{"type":"liquidity_collapse"}`),
	}})

	wild := waitForMessage[DeliveryEvent](t, wildCh, time.Second)
	exact := waitForMessage[DeliveryEvent](t, exactCh, time.Second)
	if got, want := wild.Subject.String(), "signal/*/binance/BTCUSDT/1m"; got != want {
		t.Fatalf("wild subject=%q want=%q", got, want)
	}
	if got, want := exact.Subject.String(), "signal/liquidity_collapse/binance/BTCUSDT/1m"; got != want {
		t.Fatalf("exact subject=%q want=%q", got, want)
	}
}

func TestRouter_AssignsContinuousDeliverySeqPerStream(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(NewRouterActor(RouterConfig{Timeframe: "raw"}), "router-contiguous-seq")
	defer e.Poison(routerPID)

	ch := make(chan any, 32)
	sessionPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-seq-capture")
	defer e.Poison(sessionPID)

	id := ids.NewSessionID()
	subject := mustParseSubject(t, "marketdata.trade/binance/BTC-USDT/raw")
	e.Send(routerPID, RegisterSession{SessionID: id, PID: sessionPID})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})

	emit := func(srcSeq int64, payload string) {
		e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
			Type:       "marketdata.trade",
			Version:    1,
			Venue:      "binance",
			Instrument: "BTC-USDT",
			Seq:        srcSeq,
			TsIngest:   time.Now().UnixMilli(),
			Payload:    []byte(payload),
		}})
	}

	emit(100, `{"n":1}`)
	emit(105, `{"n":2}`)
	emit(220, `{"n":3}`)

	ev1 := waitForMessage[DeliveryEvent](t, ch, time.Second)
	ev2 := waitForMessage[DeliveryEvent](t, ch, time.Second)
	ev3 := waitForMessage[DeliveryEvent](t, ch, time.Second)
	if ev1.Env.Seq != 1 || ev2.Env.Seq != 2 || ev3.Env.Seq != 3 {
		t.Fatalf("expected contiguous delivery seq [1,2,3], got [%d,%d,%d]", ev1.Env.Seq, ev2.Env.Seq, ev3.Env.Seq)
	}
}

func TestRouter_RejectsInvalidOrNonMonotonicSourceSeq(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(NewRouterActor(RouterConfig{Timeframe: "raw"}), "router-seq-guards")
	defer e.Poison(routerPID)

	ch := make(chan any, 16)
	sessionPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-seq-guards")
	defer e.Poison(sessionPID)

	id := ids.NewSessionID()
	subject := mustParseSubject(t, "marketdata.trade/binance/BTC-USDT/raw")
	e.Send(routerPID, RegisterSession{SessionID: id, PID: sessionPID})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})

	emit := func(srcSeq int64) {
		e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
			Type:       "marketdata.trade",
			Version:    1,
			Venue:      "binance",
			Instrument: "BTC-USDT",
			Seq:        srcSeq,
			TsIngest:   time.Now().UnixMilli(),
			Payload:    []byte(`{}`),
		}})
	}

	emit(10)
	emit(0)  // invalid
	emit(9)  // non-monotonic
	emit(11) // valid

	ev1 := waitForMessage[DeliveryEvent](t, ch, time.Second)
	ev2 := waitForMessage[DeliveryEvent](t, ch, time.Second)
	if got, want := ev1.Env.Seq, int64(1); got != want {
		t.Fatalf("first delivered seq=%d want=%d", got, want)
	}
	if got, want := ev2.Env.Seq, int64(2); got != want {
		t.Fatalf("second delivered seq=%d want=%d", got, want)
	}
	select {
	case raw := <-ch:
		if _, ok := raw.(DeliveryEvent); ok {
			t.Fatal("unexpected extra delivery event")
		}
	case <-time.After(120 * time.Millisecond):
	}
}

func TestRouter_StreamMonotonicityIsReplicaScopedDeterministic(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	spawnReplica := func(name string) (*actor.PID, chan any) {
		routerPID := e.Spawn(NewRouterActor(RouterConfig{
			Timeframe:           "raw",
			StreamCoherenceMode: "sticky_session",
		}), "router-"+name)
		ch := make(chan any, 16)
		sessionPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-"+name)
		id := ids.NewSessionID()
		subject := mustParseSubject(t, "marketdata.trade/binance/BTC-USDT/raw")
		e.Send(routerPID, RegisterSession{SessionID: id, PID: sessionPID})
		e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})
		t.Cleanup(func() { e.Poison(sessionPID) })
		t.Cleanup(func() { e.Poison(routerPID) })
		return routerPID, ch
	}

	replicaA, chA := spawnReplica("replica-a")
	replicaB, chB := spawnReplica("replica-b")

	emitBoth := func(srcSeq int64) {
		msg := DeliverEnvelope{Envelope: envelope.Envelope{
			Type:       "marketdata.trade",
			Version:    1,
			Venue:      "binance",
			Instrument: "BTC-USDT",
			Seq:        srcSeq,
			TsIngest:   time.Now().UnixMilli(),
			Payload:    []byte(`{}`),
		}}
		e.Send(replicaA, msg)
		e.Send(replicaB, msg)
	}

	emitBoth(100)
	emitBoth(101)

	a1 := waitForMessage[DeliveryEvent](t, chA, time.Second)
	a2 := waitForMessage[DeliveryEvent](t, chA, time.Second)
	b1 := waitForMessage[DeliveryEvent](t, chB, time.Second)
	b2 := waitForMessage[DeliveryEvent](t, chB, time.Second)
	if a1.Env.Seq != 1 || a2.Env.Seq != 2 {
		t.Fatalf("replica A seqs=%d,%d want 1,2", a1.Env.Seq, a2.Env.Seq)
	}
	if b1.Env.Seq != 1 || b2.Env.Seq != 2 {
		t.Fatalf("replica B seqs=%d,%d want 1,2", b1.Env.Seq, b2.Env.Seq)
	}
}

func TestNormalizeStreamCoherenceMode(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: "sticky_session"},
		{in: "sticky_session", want: "sticky_session"},
		{in: "upstream_sequencer", want: "upstream_sequencer"},
		{in: "invalid", want: "unknown"},
	}
	for _, tc := range tests {
		if got := normalizeStreamCoherenceMode(tc.in); got != tc.want {
			t.Fatalf("mode=%q want=%q", got, tc.want)
		}
	}
}

func TestRouter_routesCandleByEnvelopeTimeframeMeta(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(NewRouterActor(RouterConfig{Timeframe: "raw"}), "router")
	defer e.Poison(routerPID)

	ch := make(chan any, 16)
	s := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-capture")
	defer e.Poison(s)

	id := ids.NewSessionID()
	// Client subscribes with specific timeframe ("/1m").
	subject := mustParseSubject(t, "aggregation.candle/binance/BTC-USDT/1m")

	e.Send(routerPID, RegisterSession{SessionID: id, PID: s})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})

	e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       "aggregation.candle",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        42,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{}`),
		Meta:       map[string]string{"timeframe": "1m"},
	}})

	msg := waitForMessage[DeliveryEvent](t, ch, time.Second)
	if got, want := msg.Subject.String(), "aggregation.candle/binance/BTCUSDT/1m"; got != want {
		t.Fatalf("subject=%q want=%q", got, want)
	}
}

func TestRouter_candleFallsBackToRawWithoutTimeframeMeta(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(NewRouterActor(RouterConfig{Timeframe: "raw"}), "router")
	defer e.Poison(routerPID)

	ch := make(chan any, 16)
	s := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-capture")
	defer e.Poison(s)

	id := ids.NewSessionID()
	// Fallback: subscribe to /raw for candles that carry no timeframe meta.
	subject := mustParseSubject(t, "aggregation.candle/binance/BTC-USDT/raw")

	e.Send(routerPID, RegisterSession{SessionID: id, PID: s})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})

	e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       "aggregation.candle",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        42,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{}`),
	}})

	msg := waitForMessage[DeliveryEvent](t, ch, time.Second)
	if got, want := msg.Subject.String(), "aggregation.candle/binance/BTCUSDT/raw"; got != want {
		t.Fatalf("subject=%q want=%q", got, want)
	}
}

func TestRouter_routesStatsByEnvelopeTimeframeMeta(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(NewRouterActor(RouterConfig{Timeframe: "raw"}), "router")
	defer e.Poison(routerPID)

	ch := make(chan any, 16)
	s := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-capture")
	defer e.Poison(s)

	id := ids.NewSessionID()
	subject := mustParseSubject(t, "aggregation.stats/binance/BTC-USDT/1m")

	e.Send(routerPID, RegisterSession{SessionID: id, PID: s})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})

	e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       "aggregation.stats",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        10,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{}`),
		Meta:       map[string]string{"timeframe": "1m"},
	}})

	msg := waitForMessage[DeliveryEvent](t, ch, time.Second)
	if got, want := msg.Subject.String(), "aggregation.stats/binance/BTCUSDT/1m"; got != want {
		t.Fatalf("subject=%q want=%q", got, want)
	}
}

func TestRouter_routesMarketTypeAliasWhenEnvelopeMetaPresent(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(NewRouterActor(RouterConfig{Timeframe: "raw"}), "router")
	defer e.Poison(routerPID)

	ch := make(chan any, 16)
	s := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-capture")
	defer e.Poison(s)

	id := ids.NewSessionID()
	// Candle envelopes now honour Meta["timeframe"], so subscribe to /1m.
	subject := mustParseSubject(t, "aggregation.candle/binance/SOLUSDT:SPOT/1m")

	e.Send(routerPID, RegisterSession{SessionID: id, PID: s})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})

	e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       "aggregation.candle",
		Version:    1,
		Venue:      "binance",
		Instrument: "SOLUSDT",
		Seq:        77,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{}`),
		Meta: map[string]string{
			"timeframe":              "1m",
			"instrument_market_type": "SPOT",
		},
	}})

	msg := waitForMessage[DeliveryEvent](t, ch, time.Second)
	if got, want := msg.Subject.String(), "aggregation.candle/binance/SOLUSDT:SPOT/1m"; got != want {
		t.Fatalf("subject=%q want=%q", got, want)
	}
}

func TestRouter_routesLegacyAndMarketTypeSubscriptions(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(NewRouterActor(RouterConfig{Timeframe: "raw"}), "router")
	defer e.Poison(routerPID)

	legacyCh := make(chan any, 16)
	aliasCh := make(chan any, 16)
	legacyPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: legacyCh} }, "legacy-session")
	aliasPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: aliasCh} }, "alias-session")
	defer e.Poison(legacyPID)
	defer e.Poison(aliasPID)

	legacyID := ids.NewSessionID()
	aliasID := ids.NewSessionID()
	legacySub := mustParseSubject(t, "marketdata.trade/binance/SOLUSDT/raw")
	aliasSub := mustParseSubject(t, "marketdata.trade/binance/SOLUSDT:SPOT/raw")

	e.Send(routerPID, RegisterSession{SessionID: legacyID, PID: legacyPID})
	e.Send(routerPID, RegisterSession{SessionID: aliasID, PID: aliasPID})
	e.Send(routerPID, SubscribeSession{SessionID: legacyID, Subject: legacySub})
	e.Send(routerPID, SubscribeSession{SessionID: aliasID, Subject: aliasSub})

	e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       "marketdata.trade",
		Version:    1,
		Venue:      "binance",
		Instrument: "SOLUSDT",
		Seq:        1,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{}`),
		Meta:       map[string]string{"instrument_market_type": "SPOT"},
	}})

	if got, want := waitForMessage[DeliveryEvent](t, legacyCh, time.Second).Subject.String(), "marketdata.trade/binance/SOLUSDT/raw"; got != want {
		t.Fatalf("legacy subject=%q want=%q", got, want)
	}
	if got, want := waitForMessage[DeliveryEvent](t, aliasCh, time.Second).Subject.String(), "marketdata.trade/binance/SOLUSDT:SPOT/raw"; got != want {
		t.Fatalf("alias subject=%q want=%q", got, want)
	}
}

func TestRouter_doesNotDuplicateWhenSameSessionSubscribesLegacyAndAlias(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(NewRouterActor(RouterConfig{Timeframe: "raw"}), "router")
	defer e.Poison(routerPID)

	ch := make(chan any, 16)
	sessionPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-capture")
	defer e.Poison(sessionPID)

	id := ids.NewSessionID()
	// Candle envelopes honour Meta["timeframe"], so both subs use /1m.
	legacySub := mustParseSubject(t, "aggregation.candle/binance/SOLUSDT/1m")
	aliasSub := mustParseSubject(t, "aggregation.candle/binance/SOLUSDT:SPOT/1m")

	e.Send(routerPID, RegisterSession{SessionID: id, PID: sessionPID})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: legacySub})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: aliasSub})

	e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       "aggregation.candle",
		Version:    1,
		Venue:      "binance",
		Instrument: "SOLUSDT",
		Seq:        9,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{}`),
		Meta: map[string]string{
			"timeframe":              "1m",
			"instrument_market_type": "SPOT",
		},
	}})

	msg := waitForMessage[DeliveryEvent](t, ch, time.Second)
	if got, want := msg.Subject.String(), "aggregation.candle/binance/SOLUSDT/1m"; got != want {
		t.Fatalf("subject=%q want=%q", got, want)
	}
	select {
	case raw := <-ch:
		if _, ok := raw.(DeliveryEvent); ok {
			t.Fatal("expected single delivery event when same session subscribes to legacy+alias")
		}
	case <-time.After(150 * time.Millisecond):
	}
}

func TestRouter_usesTimeframeMetaForInsightsStreams(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(NewRouterActor(RouterConfig{Timeframe: "raw"}), "router")
	defer e.Poison(routerPID)

	ch := make(chan any, 16)
	s := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-capture")
	defer e.Poison(s)

	id := ids.NewSessionID()
	subject := mustParseSubject(t, "insights.heatmap_snapshot/binance/BTC-USDT/5m")

	e.Send(routerPID, RegisterSession{SessionID: id, PID: s})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})

	e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       "insights.heatmap_snapshot",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        7,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{}`),
		Meta:       map[string]string{"timeframe": "5m"},
	}})

	msg := waitForMessage[DeliveryEvent](t, ch, time.Second)
	if got, want := msg.Subject.String(), "insights.heatmap_snapshot/binance/BTCUSDT/5m"; got != want {
		t.Fatalf("subject=%q want=%q", got, want)
	}
}

func TestRouter_routesAggregationSnapshot(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(NewRouterActor(RouterConfig{Timeframe: "raw"}), "router")
	defer e.Poison(routerPID)

	ch := make(chan any, 16)
	s := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-capture")
	defer e.Poison(s)

	id := ids.NewSessionID()
	subject := mustParseSubject(t, "aggregation.snapshot/binance/BTC-USDT/raw")

	e.Send(routerPID, RegisterSession{SessionID: id, PID: s})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})

	e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       "aggregation.snapshot",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        100,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{"ok":true}`),
	}})

	msg := waitForMessage[DeliveryEvent](t, ch, time.Second)
	if got, want := msg.Subject.String(), "aggregation.snapshot/binance/BTCUSDT/raw"; got != want {
		t.Fatalf("subject=%q want=%q", got, want)
	}
}

func TestRouter_rejectsUngovernedEnvelopeType(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(NewRouterActor(RouterConfig{Timeframe: "raw"}), "router")
	defer e.Poison(routerPID)

	ch := make(chan any, 16)
	s := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-capture")
	defer e.Poison(s)

	id := ids.NewSessionID()
	subject := mustParseSubject(t, "insights.unknown/binance/BTCUSDT/raw")

	e.Send(routerPID, RegisterSession{SessionID: id, PID: s})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})
	e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       "insights.unknown",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Seq:        1,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{}`),
	}})

	select {
	case raw := <-ch:
		if _, ok := raw.(DeliveryEvent); ok {
			t.Fatal("unexpected delivery event for ungoverned envelope type")
		}
	case <-time.After(200 * time.Millisecond):
	}
}

func TestRouter_WatermarkMonotonicPerStream_5Events(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(NewRouterActor(RouterConfig{Timeframe: "raw"}), "router")
	defer e.Poison(routerPID)

	ch := make(chan any, 32)
	sessionPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-capture")
	defer e.Poison(sessionPID)

	id := ids.NewSessionID()
	subject := mustParseSubject(t, "marketdata.trade/binance/BTC-USDT/raw")
	e.Send(routerPID, RegisterSession{SessionID: id, PID: sessionPID})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})

	srcSeqs := []int64{10, 20, 30, 40, 50}
	for _, seq := range srcSeqs {
		e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
			Type: "marketdata.trade", Version: 1, Venue: "binance",
			Instrument: "BTC-USDT", Seq: seq,
			TsIngest: time.Now().UnixMilli(), Payload: []byte(`{}`),
		}})
	}

	for i := int64(1); i <= 5; i++ {
		ev := waitForMessage[DeliveryEvent](t, ch, time.Second)
		if ev.Env.Seq != i {
			t.Fatalf("event %d: delivery seq=%d want=%d", i, ev.Env.Seq, i)
		}
	}
}

func TestRouter_CrossStreamIndependence(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(NewRouterActor(RouterConfig{Timeframe: "raw"}), "router")
	defer e.Poison(routerPID)

	ch := make(chan any, 32)
	sessionPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-capture")
	defer e.Poison(sessionPID)

	id := ids.NewSessionID()
	subA := mustParseSubject(t, "marketdata.trade/binance/BTC-USDT/raw")
	subB := mustParseSubject(t, "marketdata.trade/binance/ETH-USDT/raw")
	e.Send(routerPID, RegisterSession{SessionID: id, PID: sessionPID})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subA})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subB})

	// Interleave events from two streams.
	for i := int64(1); i <= 3; i++ {
		e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
			Type: "marketdata.trade", Version: 1, Venue: "binance",
			Instrument: "BTC-USDT", Seq: i * 100,
			TsIngest: time.Now().UnixMilli(), Payload: []byte(`{}`),
		}})
		e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
			Type: "marketdata.trade", Version: 1, Venue: "binance",
			Instrument: "ETH-USDT", Seq: i * 200,
			TsIngest: time.Now().UnixMilli(), Payload: []byte(`{}`),
		}})
	}

	seqBySubject := map[string][]int64{}
	for range 6 {
		ev := waitForMessage[DeliveryEvent](t, ch, time.Second)
		seqBySubject[ev.Subject.String()] = append(seqBySubject[ev.Subject.String()], ev.Env.Seq)
	}
	for sub, seqs := range seqBySubject {
		for i, seq := range seqs {
			if seq != int64(i+1) {
				t.Fatalf("stream %s: delivery seq[%d]=%d want=%d", sub, i, seq, i+1)
			}
		}
	}
}

func TestRouter_DeliverySeqContiguityInvariant_20Events(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(NewRouterActor(RouterConfig{Timeframe: "raw"}), "router")
	defer e.Poison(routerPID)

	ch := make(chan any, 64)
	sessionPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-capture")
	defer e.Poison(sessionPID)

	id := ids.NewSessionID()
	subject := mustParseSubject(t, "marketdata.trade/binance/BTC-USDT/raw")
	e.Send(routerPID, RegisterSession{SessionID: id, PID: sessionPID})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})

	// Scattered source seqs — all strictly increasing but with large gaps.
	srcSeqs := []int64{3, 7, 12, 55, 100, 111, 222, 333, 444, 500,
		601, 700, 815, 900, 1001, 1200, 1500, 2000, 5000, 9999}
	for _, seq := range srcSeqs {
		e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
			Type: "marketdata.trade", Version: 1, Venue: "binance",
			Instrument: "BTC-USDT", Seq: seq,
			TsIngest: time.Now().UnixMilli(), Payload: []byte(`{}`),
		}})
	}

	for i := int64(1); i <= 20; i++ {
		ev := waitForMessage[DeliveryEvent](t, ch, time.Second)
		if ev.Env.Seq != i {
			t.Fatalf("event %d: delivery seq=%d want=%d", i, ev.Env.Seq, i)
		}
	}
}

func TestRouter_DeliverySeqContiguityAfterReject(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(NewRouterActor(RouterConfig{Timeframe: "raw"}), "router")
	defer e.Poison(routerPID)

	ch := make(chan any, 16)
	sessionPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-capture")
	defer e.Poison(sessionPID)

	id := ids.NewSessionID()
	subject := mustParseSubject(t, "marketdata.trade/binance/BTC-USDT/raw")
	e.Send(routerPID, RegisterSession{SessionID: id, PID: sessionPID})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})

	emit := func(seq int64) {
		e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
			Type: "marketdata.trade", Version: 1, Venue: "binance",
			Instrument: "BTC-USDT", Seq: seq,
			TsIngest: time.Now().UnixMilli(), Payload: []byte(`{}`),
		}})
	}

	emit(10) // accepted → delivery seq 1
	emit(5)  // rejected (non-monotonic)
	emit(0)  // rejected (invalid)
	emit(-1) // rejected (invalid)
	emit(10) // rejected (non-monotonic, equal)
	emit(15) // accepted → delivery seq 2

	ev1 := waitForMessage[DeliveryEvent](t, ch, time.Second)
	ev2 := waitForMessage[DeliveryEvent](t, ch, time.Second)
	if ev1.Env.Seq != 1 {
		t.Fatalf("first delivery seq=%d want=1", ev1.Env.Seq)
	}
	if ev2.Env.Seq != 2 {
		t.Fatalf("second delivery seq=%d want=2", ev2.Env.Seq)
	}

	// No third event should arrive.
	select {
	case raw := <-ch:
		if _, ok := raw.(DeliveryEvent); ok {
			t.Fatal("unexpected third delivery event")
		}
	case <-time.After(120 * time.Millisecond):
	}
}

func TestRouterStreamStateTTLExpiresInactive(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	fakeClock := sharedclock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	routerPID := e.Spawn(NewRouterActor(RouterConfig{
		Timeframe:             "raw",
		StreamStateTTL:        30 * time.Minute,
		StreamStateSweepEvery: 24 * time.Hour,
		Now:                   fakeClock.Now,
	}), "router-stream-state-ttl-expires")
	defer e.Poison(routerPID)

	ch := make(chan any, 16)
	sessionPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-stream-state-ttl-expires")
	defer e.Poison(sessionPID)

	id := ids.NewSessionID()
	subject := mustParseSubject(t, "marketdata.trade/binance/BTC-USDT/raw")
	e.Send(routerPID, RegisterSession{SessionID: id, PID: sessionPID})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})

	emit := func(srcSeq int64) {
		e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
			Type:       "marketdata.trade",
			Version:    1,
			Venue:      "binance",
			Instrument: "BTC-USDT",
			Seq:        srcSeq,
			TsIngest:   fakeClock.NowUnixMilli(),
			Payload:    []byte(`{}`),
		}})
	}

	emit(10)
	if got := waitForMessage[DeliveryEvent](t, ch, time.Second).Env.Seq; got != 1 {
		t.Fatalf("first delivery seq=%d want=1", got)
	}

	fakeClock.Advance(31 * time.Minute)
	e.Send(routerPID, routerSweepStreamState{})

	emit(9)
	if got := waitForMessage[DeliveryEvent](t, ch, time.Second).Env.Seq; got != 1 {
		t.Fatalf("delivery seq after ttl eviction=%d want=1", got)
	}
}

func TestRouterStreamStateDoesNotExpireActive(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	fakeClock := sharedclock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	routerPID := e.Spawn(NewRouterActor(RouterConfig{
		Timeframe:             "raw",
		StreamStateTTL:        30 * time.Minute,
		StreamStateSweepEvery: 24 * time.Hour,
		Now:                   fakeClock.Now,
	}), "router-stream-state-ttl-active")
	defer e.Poison(routerPID)

	ch := make(chan any, 16)
	sessionPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-stream-state-ttl-active")
	defer e.Poison(sessionPID)

	id := ids.NewSessionID()
	subject := mustParseSubject(t, "marketdata.trade/binance/BTC-USDT/raw")
	e.Send(routerPID, RegisterSession{SessionID: id, PID: sessionPID})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})

	emit := func(srcSeq int64) {
		e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
			Type:       "marketdata.trade",
			Version:    1,
			Venue:      "binance",
			Instrument: "BTC-USDT",
			Seq:        srcSeq,
			TsIngest:   fakeClock.NowUnixMilli(),
			Payload:    []byte(`{}`),
		}})
	}

	emit(10)
	if got := waitForMessage[DeliveryEvent](t, ch, time.Second).Env.Seq; got != 1 {
		t.Fatalf("first delivery seq=%d want=1", got)
	}

	fakeClock.Advance(20 * time.Minute)
	e.Send(routerPID, routerSweepStreamState{})
	emit(11)
	if got := waitForMessage[DeliveryEvent](t, ch, time.Second).Env.Seq; got != 2 {
		t.Fatalf("second delivery seq=%d want=2", got)
	}

	fakeClock.Advance(20 * time.Minute)
	e.Send(routerPID, routerSweepStreamState{})
	emit(12)
	if got := waitForMessage[DeliveryEvent](t, ch, time.Second).Env.Seq; got != 3 {
		t.Fatalf("third delivery seq=%d want=3", got)
	}
}

func TestRouterStreamStateGaugeUpdates(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	fakeClock := sharedclock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	evictedBefore := testutil.ToFloat64(sharedmetrics.DeliveryRouterStreamStateEvictedTotal)

	routerPID := e.Spawn(NewRouterActor(RouterConfig{
		Timeframe:             "raw",
		StreamStateTTL:        30 * time.Minute,
		StreamStateSweepEvery: 24 * time.Hour,
		Now:                   fakeClock.Now,
	}), "router-stream-state-gauge")
	defer e.Poison(routerPID)

	ch := make(chan any, 16)
	sessionPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-stream-state-gauge")
	defer e.Poison(sessionPID)

	id := ids.NewSessionID()
	subject := mustParseSubject(t, "marketdata.trade/binance/BTC-USDT/raw")
	e.Send(routerPID, RegisterSession{SessionID: id, PID: sessionPID})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})
	e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       "marketdata.trade",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        10,
		TsIngest:   fakeClock.NowUnixMilli(),
		Payload:    []byte(`{}`),
	}})
	_ = waitForMessage[DeliveryEvent](t, ch, time.Second)

	e.Send(routerPID, routerSweepStreamState{})
	waitForMetricEqual(t, "router_stream_state_entries", func() float64 {
		return testutil.ToFloat64(sharedmetrics.DeliveryRouterStreamStateEntries)
	}, 1, time.Second)
	waitForMetricEqual(t, "router_stream_state_active_total", func() float64 {
		return testutil.ToFloat64(sharedmetrics.DeliveryRouterStreamStateActiveTotal)
	}, 1, time.Second)

	fakeClock.Advance(31 * time.Minute)
	e.Send(routerPID, routerSweepStreamState{})
	waitForMetricEqual(t, "router_stream_state_entries", func() float64 {
		return testutil.ToFloat64(sharedmetrics.DeliveryRouterStreamStateEntries)
	}, 0, time.Second)
	waitForMetricEqual(t, "router_stream_state_active_total", func() float64 {
		return testutil.ToFloat64(sharedmetrics.DeliveryRouterStreamStateActiveTotal)
	}, 0, time.Second)
	waitForMetricAtLeast(t, "router_stream_state_evicted_total", func() float64 {
		return testutil.ToFloat64(sharedmetrics.DeliveryRouterStreamStateEvictedTotal)
	}, evictedBefore+1, time.Second)
}

func TestRouterSeqContinuityStillHoldsAfterEviction(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	fakeClock := sharedclock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	routerPID := e.Spawn(NewRouterActor(RouterConfig{
		Timeframe:             "raw",
		StreamStateTTL:        30 * time.Minute,
		StreamStateSweepEvery: 24 * time.Hour,
		Now:                   fakeClock.Now,
	}), "router-stream-state-seq-after-eviction")
	defer e.Poison(routerPID)

	ch := make(chan any, 16)
	sessionPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-stream-state-seq-after-eviction")
	defer e.Poison(sessionPID)

	id := ids.NewSessionID()
	subject := mustParseSubject(t, "marketdata.trade/binance/BTC-USDT/raw")
	e.Send(routerPID, RegisterSession{SessionID: id, PID: sessionPID})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})

	emit := func(srcSeq int64) {
		e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
			Type:       "marketdata.trade",
			Version:    1,
			Venue:      "binance",
			Instrument: "BTC-USDT",
			Seq:        srcSeq,
			TsIngest:   fakeClock.NowUnixMilli(),
			Payload:    []byte(`{}`),
		}})
	}

	emit(100)
	if got := waitForMessage[DeliveryEvent](t, ch, time.Second).Env.Seq; got != 1 {
		t.Fatalf("first delivery seq=%d want=1", got)
	}

	fakeClock.Advance(31 * time.Minute)
	e.Send(routerPID, routerSweepStreamState{})

	emit(50)
	emit(51)

	ev2 := waitForMessage[DeliveryEvent](t, ch, time.Second)
	ev3 := waitForMessage[DeliveryEvent](t, ch, time.Second)
	if ev2.Env.Seq != 1 || ev3.Env.Seq != 2 {
		t.Fatalf("expected contiguous delivery seq after eviction [1,2], got [%d,%d]", ev2.Env.Seq, ev3.Env.Seq)
	}
}

func TestRouter_cleansUpStoppedSession(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(NewRouterActor(RouterConfig{Timeframe: "raw"}), "router")
	defer e.Poison(routerPID)

	ch := make(chan any, 16)
	sessionPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-capture")
	id := ids.NewSessionID()
	subject := mustParseSubject(t, "marketdata.trade/binance/BTC-USDT/raw")

	e.Send(routerPID, RegisterSession{SessionID: id, PID: sessionPID})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})
	<-e.Poison(sessionPID).Done()
	waitForMetricEqual(t, "delivery_router_sessions_active", func() float64 {
		return testutil.ToFloat64(sharedmetrics.DeliveryRouterSessionsActive)
	}, 0, time.Second)

	e.Send(routerPID, DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       "marketdata.trade",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        999,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{}`),
	}})

	select {
	case raw := <-ch:
		if _, ok := raw.(DeliveryEvent); ok {
			t.Fatal("stopped session should not receive delivery events")
		}
	case <-time.After(150 * time.Millisecond):
	}
}
