package wsserver

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type soakEnvelopeStore struct{}

func (soakEnvelopeStore) StoreEnvelope(envelope.Envelope) {}

const wsSoakEnableEnv = "MR_ENABLE_SOAK"

//nolint:gocyclo // end-to-end soak flow intentionally validates many lifecycle branches.
func TestSoak_WSDelivery_SlowClients(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak test in short mode")
	}
	if os.Getenv(wsSoakEnableEnv) != "1" {
		t.Skipf("set %s=1 to run soak tests", wsSoakEnableEnv)
	}
	requireLoopbackListener(t)

	const (
		totalClients  = 50
		fastClients   = 4
		totalMessages = 100_000
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
	}), "delivery-router-soak")
	defer e.Poison(routerPID)

	ws := NewServer(e, routerPID, nil, &staticRangeStore{}, 64)
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
	var closeConnsOnce sync.Once
	closeConns := func() {
		closeConnsOnce.Do(func() {
			for _, c := range conns {
				_ = c.Close()
			}
		})
	}
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()

	for i := 0; i < totalClients; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(wsURLFromHTTP(srv.URL)+"/ws", nil)
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
	var fastRecv [fastClients]atomic.Int64

	for i := 0; i < fastClients; i++ {
		conn := conns[i]
		go func(idx int, c *websocket.Conn) {
			for {
				select {
				case <-done:
					return
				default:
				}
				_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
				var msg map[string]any
				if err := c.ReadJSON(&msg); err != nil {
					continue
				}
				if msg["type"] == "event" {
					fastRecv[idx].Add(1)
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
			TsIngest:   1_710_000_000_000 + int64(i),
			Payload:    payload,
		}})
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		allReady := true
		for i := 0; i < fastClients; i++ {
			if fastRecv[i].Load() < int64(totalMessages/2) {
				allReady = false
				break
			}
		}
		if allReady {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	close(done)
	closeConns()

	drainDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(drainDeadline) {
		runtime.GC()
		if runtime.NumGoroutine()-beforeG <= 96 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	for i := 0; i < fastClients; i++ {
		if got := fastRecv[i].Load(); got < int64(totalMessages/2) {
			t.Fatalf("fast client[%d] received too few events: got=%d want>=%d", i, got, totalMessages/2)
		}
	}
	afterDrop := testutil.ToFloat64(metrics.WSDropsTotal.WithLabelValues("queue_full"))
	if afterDrop < beforeDrop+1 {
		t.Fatalf("expected drop metric increment, before=%f after=%f", beforeDrop, afterDrop)
	}

	runtime.GC()
	afterG := runtime.NumGoroutine()
	if afterG-beforeG > 96 {
		t.Fatalf("goroutine drift too high: before=%d after=%d", beforeG, afterG)
	}
}
