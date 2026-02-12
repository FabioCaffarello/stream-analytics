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
	msg := []byte(`{"e":"depthUpdate","E":1710000010000,"s":"ETHUSDT","U":101,"u":105,"pu":100,"b":[["2500.1","1.2"]],"a":[["2500.2","2.3"]]}`)
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
	if payload.FirstID != 101 || payload.FinalID != 105 || payload.PrevFinal != 100 {
		t.Fatalf("unexpected depth update ids: %#v", payload)
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

func TestParseMessage_AggTrade_FallsBackToEventTime(t *testing.T) {
	msg := []byte(`{"e":"aggTrade","E":1710000001111,"T":0,"s":"BTCUSDT","a":1,"p":"1.0","q":"2.0","m":false}`)
	req, skip, p := binance.ParseMessage(msg, time.UnixMilli(1710000009999))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	if req.TsExchange != 1710000001111 {
		t.Fatalf("ts_exchange = %d, want 1710000001111", req.TsExchange)
	}
}

func TestParseMessage_AggTrade_FallsBackToRecvAt(t *testing.T) {
	recvAt := time.UnixMilli(1710000009999)
	msg := []byte(`{"e":"aggTrade","E":0,"T":0,"s":"BTCUSDT","a":1,"p":"1.0","q":"2.0","m":false}`)
	req, skip, p := binance.ParseMessage(msg, recvAt)
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	if req.TsExchange != recvAt.UnixMilli() {
		t.Fatalf("ts_exchange = %d, want %d", req.TsExchange, recvAt.UnixMilli())
	}
}

func TestParseMessage_CombinedEnvelopeWithoutData_Skips(t *testing.T) {
	msg := []byte(`{"stream":"btcusdt@aggTrade","data":null}`)
	req, skip, p := binance.ParseMessage(msg, time.Now())
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if !skip {
		t.Fatalf("expected skip, got req=%#v", req)
	}
}

func TestParseMessage_DepthUpdateInvalidLevel_SkipsWithProblem(t *testing.T) {
	msg := []byte(`{"e":"depthUpdate","E":1710000010000,"s":"ETHUSDT","b":[["2500.1"]],"a":[["2500.2","2.3"]]}`)
	_, skip, p := binance.ParseMessage(msg, time.UnixMilli(1710000011000))
	if !skip || p == nil {
		t.Fatalf("expected skip + problem, got skip=%v problem=%v", skip, p)
	}
}

func TestParseMessageWithMeta_UnsupportedEvent(t *testing.T) {
	_, skip, meta := binance.ParseMessageWithMeta([]byte(`{"e":"bookTicker"}`), time.Now())
	if !skip {
		t.Fatal("expected skip for unsupported event")
	}
	if meta.SkipReason != "unsupported_event" {
		t.Fatalf("skip reason = %q, want unsupported_event", meta.SkipReason)
	}
	if meta.EventType != "bookTicker" {
		t.Fatalf("event type = %q, want bookTicker", meta.EventType)
	}
}

func TestParseMessageWithMeta_InvalidJSON(t *testing.T) {
	_, skip, meta := binance.ParseMessageWithMeta([]byte(`{invalid`), time.Now())
	if !skip {
		t.Fatal("expected skip for invalid JSON")
	}
	if meta.SkipReason != "parse_error" {
		t.Fatalf("skip reason = %q, want parse_error", meta.SkipReason)
	}
	if meta.Problem == nil {
		t.Fatal("expected problem for invalid JSON")
	}
}

func TestParseMessageWithMeta_WrappedStreamCarriesWSStream(t *testing.T) {
	msg := []byte(`{"stream":"btcusdt@depth@100ms","data":{"e":"depthUpdate","E":1710000010000,"s":"BTCUSDT","U":11,"u":12,"b":[["2500.1","1.2"]],"a":[["2500.2","2.3"]]}}`)
	req, skip, meta := binance.ParseMessageWithMeta(msg, time.UnixMilli(1710000011000))
	if skip || meta.Problem != nil {
		t.Fatalf("expected parse success, skip=%v problem=%v", skip, meta.Problem)
	}
	if req.Metadata["instrument_base"] != "BTC" || req.Metadata["instrument_quote"] != "USDT" {
		t.Fatalf("expected instrument base/quote metadata, got=%v", req.Metadata)
	}
	if req.Metadata["ws_stream"] != "btcusdt@depth@100ms" {
		t.Fatalf("req metadata ws_stream = %q, want btcusdt@depth@100ms", req.Metadata["ws_stream"])
	}
	if meta.WSStream != "btcusdt@depth@100ms" {
		t.Fatalf("WSStream = %q, want btcusdt@depth@100ms", meta.WSStream)
	}
}
