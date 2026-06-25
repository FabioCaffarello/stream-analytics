//go:build soak

package wsserver

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	"github.com/market-raccoon/internal/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

//nolint:gocyclo // soak scenario intentionally exercises mixed client behavior and delivery pressure.
func TestSoak_WSBackpressure_MixedClients60(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak test in short mode")
	}
	if os.Getenv(wsSoakEnableEnv) != "1" {
		t.Skipf("set %s=1 to run soak tests", wsSoakEnableEnv)
	}
	t.Setenv(contracts.EnvProtoMarketDataTrade, "1")
	requireLoopbackListener(t)

	const (
		totalClients  = 60
		fastJSON      = 8
		fastProto     = 8
		totalMessages = 120_000
	)

	runtime.GC()
	beforeG := runtime.NumGoroutine()

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(deliveryruntime.NewRouterActor(deliveryruntime.RouterConfig{
		Timeframe:     "raw",
		EnvelopeStore: soakEnvelopeStore{},
	}), "delivery-router-backpressure")
	defer e.Poison(routerPID)

	ws := NewServer(e, routerPID, nil, &staticRangeStore{}, 32)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", ws.HandleWS)
	srv := httptest.NewUnstartedServer(mux)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listener unavailable in this environment: %v", err)
		return
	}
	srv.Listener = ln
	srv.Start()
	defer srv.Close()

	conns := make([]*websocket.Conn, 0, totalClients)
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()

	for i := 0; i < totalClients; i++ {
		url := wsURLFromHTTP(srv.URL) + "/ws"
		if i >= fastJSON && i < fastJSON+fastProto {
			url += "?format=proto"
		}
		conn, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			t.Fatalf("dial ws[%d]: %v", i, err)
		}
		conns = append(conns, conn)

		if err := conn.WriteJSON(map[string]any{
			"op":         "subscribe",
			"subject":    "marketdata.trade/binance/BTC-USDT/raw",
			"request_id": "sub",
		}); err != nil {
			t.Fatalf("subscribe write[%d]: %v", i, err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var ack map[string]any
		if err := conn.ReadJSON(&ack); err != nil {
			t.Fatalf("subscribe read[%d]: %v", i, err)
		}
		_ = conn.SetReadDeadline(time.Time{})
	}

	beforeDrop := testutil.ToFloat64(metrics.WSDropsTotal.WithLabelValues("queue_full"))
	done := make(chan struct{})
	var jsonRecv [fastJSON]atomic.Int64
	var protoRecv [fastProto]atomic.Int64

	for i := 0; i < fastJSON; i++ {
		conn := conns[i]
		go func(idx int, c *websocket.Conn) {
			for {
				select {
				case <-done:
					return
				default:
				}
				_ = c.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
				var msg map[string]any
				if err := c.ReadJSON(&msg); err != nil {
					continue
				}
				if msg["type"] == "event" {
					jsonRecv[idx].Add(1)
				}
			}
		}(i, conn)
	}

	for i := 0; i < fastProto; i++ {
		conn := conns[fastJSON+i]
		go func(idx int, c *websocket.Conn) {
			for {
				select {
				case <-done:
					return
				default:
				}
				_ = c.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
				messageType, _, err := c.ReadMessage()
				if err != nil {
					continue
				}
				if messageType == websocket.BinaryMessage {
					protoRecv[idx].Add(1)
				}
			}
		}(i, conn)
	}

	payload := []byte(`{"v":1}`)
	for i := 1; i <= totalMessages; i++ {
		e.Send(routerPID, deliveryruntime.DeliverEnvelope{Envelope: envelope.Envelope{
			Type:       "marketdata.trade",
			Version:    1,
			Venue:      "binance",
			Instrument: "BTC-USDT",
			Seq:        int64(i),
			TsIngest:   1_735_689_600_000 + int64(i),
			Payload:    payload,
		}})
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		allReady := true
		for i := 0; i < fastJSON; i++ {
			if jsonRecv[i].Load() < int64(totalMessages/3) {
				allReady = false
				break
			}
		}
		if allReady {
			for i := 0; i < fastProto; i++ {
				if protoRecv[i].Load() < int64(totalMessages/3) {
					allReady = false
					break
				}
			}
		}
		if allReady {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	close(done)
	time.Sleep(500 * time.Millisecond)

	for i := 0; i < fastJSON; i++ {
		if got := jsonRecv[i].Load(); got < int64(totalMessages/3) {
			t.Fatalf("json fast client[%d] received too few events: got=%d want>=%d", i, got, totalMessages/3)
		}
	}
	for i := 0; i < fastProto; i++ {
		if got := protoRecv[i].Load(); got < int64(totalMessages/3) {
			t.Fatalf("proto fast client[%d] received too few events: got=%d want>=%d", i, got, totalMessages/3)
		}
	}

	afterDrop := testutil.ToFloat64(metrics.WSDropsTotal.WithLabelValues("queue_full"))
	if afterDrop < beforeDrop+1 {
		t.Fatalf("expected drop metric increment, before=%f after=%f", beforeDrop, afterDrop)
	}

	runtime.GC()
	afterG := runtime.NumGoroutine()
	if afterG-beforeG > 128 {
		t.Fatalf("goroutine drift too high: before=%d after=%d", beforeG, afterG)
	}
}
