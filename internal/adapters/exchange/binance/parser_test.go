package binance_test

import (
	"testing"
	"time"

	"github.com/market-raccoon/internal/adapters/exchange/binance"
	"github.com/market-raccoon/internal/core/marketdata/domain"
)

func TestParseMessage_AggTrade(t *testing.T) {
	msg := []byte(`{"stream":"btcusdt@aggTrade","data":{"e":"aggTrade","E":1710000001000,"T":1710000002000,"s":"BTCUSDT","a":12345,"p":"42000.10","q":"0.200","m":true}}`)
	req, skip, p := binance.ParseMessage(msg, time.UnixMilli(1710000003000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	if req.EventType != "marketdata.trade" || req.Venue != "BINANCE" || req.Instrument != "BTCUSDT" {
		t.Fatalf("unexpected request: %#v", req)
	}
	payload, ok := req.Payload.(domain.TradeTickV1)
	if !ok {
		t.Fatalf("unexpected payload type: %T", req.Payload)
	}
	if payload.Side != "sell" || payload.TradeID != "12345" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestParseMessage_DepthUpdate(t *testing.T) {
	msg := []byte(`{"e":"depthUpdate","E":1710000010000,"s":"ETHUSDT","b":[["2500.1","1.2"]],"a":[["2500.2","2.3"]]}`)
	req, skip, p := binance.ParseMessage(msg, time.UnixMilli(1710000011000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	if req.EventType != "marketdata.bookdelta" || req.Instrument != "ETHUSDT" {
		t.Fatalf("unexpected request: %#v", req)
	}
	payload, ok := req.Payload.(domain.BookDeltaV1)
	if !ok {
		t.Fatalf("unexpected payload type: %T", req.Payload)
	}
	if len(payload.Bids) != 1 || len(payload.Asks) != 1 {
		t.Fatalf("unexpected depth payload: %#v", payload)
	}
}

func TestParseMessage_UnknownEventSkips(t *testing.T) {
	req, skip, p := binance.ParseMessage([]byte(`{"e":"ping"}`), time.Now())
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if !skip {
		t.Fatalf("expected skip, got req=%#v", req)
	}
}

func TestParseMessage_InvalidSkipsWithProblem(t *testing.T) {
	_, skip, p := binance.ParseMessage([]byte(`{"e":"aggTrade","p":"abc"}`), time.Now())
	if !skip || p == nil {
		t.Fatalf("expected skip + problem, got skip=%v problem=%v", skip, p)
	}
}
