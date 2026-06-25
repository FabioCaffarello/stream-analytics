package wsserver

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	"github.com/market-raccoon/internal/contracts"
	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/shared/envelope"
)

type benchWSConn struct {
	readDone chan struct{}
	writes   chan int
}

func newBenchWSConn() *benchWSConn {
	return &benchWSConn{
		readDone: make(chan struct{}),
		writes:   make(chan int, 2048),
	}
}

func (c *benchWSConn) ReadMessage() (messageType int, p []byte, err error) {
	<-c.readDone
	return 0, nil, errors.New("closed")
}

func (c *benchWSConn) WriteJSON(v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.writes <- len(payload)
	return nil
}

func (c *benchWSConn) WriteMessage(_ int, data []byte) error {
	cp := append([]byte(nil), data...)
	c.writes <- len(cp)
	return nil
}

func (c *benchWSConn) SetReadLimit(_ int64) {}

func (c *benchWSConn) SetReadDeadline(_ time.Time) error { return nil }

func (c *benchWSConn) SetPongHandler(_ func(string) error) {}

func (c *benchWSConn) Close() error {
	select {
	case <-c.readDone:
	default:
		close(c.readDone)
	}
	return nil
}

var benchSessionWriteSink int

func BenchmarkSessionWriteJSON(b *testing.B) {
	benchmarkSessionWrite(b, false)
}

func BenchmarkSessionWriteProto(b *testing.B) {
	previous := os.Getenv(contracts.EnvProtoMarketDataTrade)
	if err := os.Setenv(contracts.EnvProtoMarketDataTrade, "1"); err != nil {
		b.Fatalf("setenv %s: %v", contracts.EnvProtoMarketDataTrade, err)
	}
	b.Cleanup(func() {
		if previous == "" {
			_ = os.Unsetenv(contracts.EnvProtoMarketDataTrade)
			return
		}
		_ = os.Setenv(contracts.EnvProtoMarketDataTrade, previous)
	})
	benchmarkSessionWrite(b, true)
}

func benchmarkSessionWrite(b *testing.B, preferProto bool) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		b.Fatalf("new engine: %v", err)
	}

	conn := newBenchWSConn()
	sessionPID := e.Spawn(deliveryruntime.NewSessionActor(deliveryruntime.SessionConfig{
		Conn:        conn,
		PreferProto: preferProto,
	}), "bench-session-writer")
	b.Cleanup(func() {
		if sessionPID != nil {
			<-e.Poison(sessionPID).Done()
		}
	})

	subject, p := domain.ParseSubject("marketdata.trade/binance/BTC-USDT/raw")
	if p != nil {
		b.Fatalf("parse subject: %v", p)
	}

	evt := deliveryruntime.DeliveryEvent{
		Subject: subject,
		Env: envelope.Envelope{
			Type:       "marketdata.trade",
			Version:    1,
			Venue:      "binance",
			Instrument: "BTC-USDT",
			TsIngest:   1_735_689_600_000,
			Payload:    []byte(`{"price":123.45,"size":0.10,"side":"buy","trade_id":"bench-1","timestamp":1735689600000}`),
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		evt.Env.Seq = int64(i + 1)
		e.Send(sessionPID, evt)
		benchSessionWriteSink += <-conn.writes
	}
}
