package bybit_test

import (
	"testing"
	"time"

	"github.com/market-raccoon/internal/adapters/exchange/bybit"
	"github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

func TestParseMessage_Trade(t *testing.T) {
	msg := []byte(`{"topic":"publicTrade.BTCUSDT","type":"snapshot","ts":1710000001000,"data":[{"T":1710000001001,"s":"BTCUSDT","S":"Buy","v":"0.010","p":"65000.50","i":"123456"}]}`)
	req, skip, p := bybit.ParseMessage(msg, time.UnixMilli(1710000003000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	if req.EventType != "marketdata.trade" || req.Venue != "BYBIT" || req.Instrument != "BTCUSDT" {
		t.Fatalf("unexpected request: %#v", req)
	}
	if req.IdempotencyKey != "venue=BYBIT|instrument=BTCUSDT|trade_id=123456" {
		t.Fatalf("idempotency key = %q", req.IdempotencyKey)
	}
	payload, ok := req.Payload.(domain.TradeTickV1)
	if !ok {
		t.Fatalf("unexpected payload type: %T", req.Payload)
	}
	if payload.Side != "buy" || payload.TradeID != "123456" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestParseMessage_BookDelta(t *testing.T) {
	msg := []byte(`{"topic":"orderbook.50.ETHUSDT","type":"delta","ts":1710000010000,"data":{"s":"ETHUSDT","b":[["2500.1","1.2"]],"a":[["2500.2","2.3"]],"u":105,"seq":101,"cts":1710000010001}}`)
	req, skip, p := bybit.ParseMessage(msg, time.UnixMilli(1710000011000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	if req.EventType != "marketdata.bookdelta" || req.Instrument != "ETHUSDT" {
		t.Fatalf("unexpected request: %#v", req)
	}
	if req.IdempotencyKey != "venue=BYBIT|instrument=ETHUSDT|final_update_id=105" {
		t.Fatalf("idempotency key = %q", req.IdempotencyKey)
	}
	payload, ok := req.Payload.(domain.BookDeltaV1)
	if !ok {
		t.Fatalf("unexpected payload type: %T", req.Payload)
	}
	if len(payload.Bids) != 1 || len(payload.Asks) != 1 {
		t.Fatalf("unexpected depth payload: %#v", payload)
	}
	if payload.FirstID != 105 || payload.FinalID != 105 || payload.PrevFinal != 104 {
		t.Fatalf("unexpected depth update ids: %#v", payload)
	}
	if payload.IsSnapshot {
		t.Fatalf("expected delta payload, got snapshot=true: %#v", payload)
	}
}

func TestParseMessage_BookSnapshot_UsesDeterministicWindow(t *testing.T) {
	msg := []byte(`{"topic":"orderbook.50.ETHUSDT","type":"snapshot","ts":1710000010000,"data":{"s":"ETHUSDT","b":[["2500.1","1.2"]],"a":[["2500.2","2.3"]],"u":105,"seq":910105,"cts":1710000010001}}`)
	req, skip, p := bybit.ParseMessage(msg, time.UnixMilli(1710000011000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	payload, ok := req.Payload.(domain.BookDeltaV1)
	if !ok {
		t.Fatalf("unexpected payload type: %T", req.Payload)
	}
	if !payload.IsSnapshot {
		t.Fatalf("expected snapshot payload, got %#v", payload)
	}
	if payload.FirstID != 105 || payload.FinalID != 105 {
		t.Fatalf("unexpected snapshot ids: %#v", payload)
	}
}

func TestParseMessage_MarkPrice(t *testing.T) {
	msg := []byte(`{"topic":"tickers.BTCUSDT","type":"snapshot","ts":1710000020000,"data":{"symbol":"BTCUSDT","markPrice":"65010.5","indexPrice":"65005.1","fundingRate":"0.0001"}}`)
	req, skip, p := bybit.ParseMessage(msg, time.UnixMilli(1710000021000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	if req.EventType != "marketdata.markprice" || req.Instrument != "BTCUSDT" {
		t.Fatalf("unexpected request: %#v", req)
	}
	payload, ok := req.Payload.(domain.MarkPriceTickV1)
	if !ok {
		t.Fatalf("unexpected payload type: %T", req.Payload)
	}
	if payload.MarkPrice != 65010.5 || payload.IndexPrice != 65005.1 || payload.FundingRate != 0.0001 {
		t.Fatalf("unexpected markprice payload: %#v", payload)
	}
}

func TestParseMessage_MarkPriceMissingFallsBackToIndexPrice(t *testing.T) {
	msg := []byte(`{"topic":"tickers.BTCUSDT","type":"delta","ts":1710000020000,"data":{"symbol":"BTCUSDT","markPrice":"","indexPrice":"65005.1","fundingRate":"0.0001"}}`)
	req, skip, p := bybit.ParseMessage(msg, time.UnixMilli(1710000021000))
	if skip {
		t.Fatal("expected fallback publish, got skip=true")
	}
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	payload, ok := req.Payload.(domain.MarkPriceTickV1)
	if !ok {
		t.Fatalf("unexpected payload type: %T", req.Payload)
	}
	if payload.MarkPrice != 65005.1 {
		t.Fatalf("mark price = %v, want 65005.1", payload.MarkPrice)
	}
}

func TestParseMessageWithMeta_MarkPriceMissingUsesExplicitSkipReason(t *testing.T) {
	msg := []byte(`{"topic":"tickers.BTCUSDT","type":"delta","ts":1710000020000,"data":{"symbol":"BTCUSDT","markPrice":"","indexPrice":"","fundingRate":"0.0001"}}`)
	_, skip, meta := bybit.ParseMessageWithMeta(msg, time.UnixMilli(1710000021000))
	if !skip {
		t.Fatal("expected skip")
	}
	if meta.Problem != nil {
		t.Fatalf("unexpected problem: %v", meta.Problem)
	}
	if meta.SkipReason != "markprice_unavailable" {
		t.Fatalf("skip reason = %q, want markprice_unavailable", meta.SkipReason)
	}
}

func TestParseMarkPrice_WithFundingRate(t *testing.T) {
	msg := []byte(`{"topic":"tickers.BTCUSDT","type":"snapshot","ts":1700000000000,"data":{"symbol":"BTCUSDT","markPrice":"42000.50","indexPrice":"42001.00","fundingRate":"0.00010000"}}`)
	req, skip, p := bybit.ParseMessage(msg, time.UnixMilli(1700000001000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	if req.EventType != "marketdata.markprice" || req.Venue != "BYBIT" || req.Instrument != "BTCUSDT" {
		t.Fatalf("unexpected request: %#v", req)
	}
	payload, ok := req.Payload.(domain.MarkPriceTickV1)
	if !ok {
		t.Fatalf("unexpected payload type: %T", req.Payload)
	}
	if payload.MarkPrice != 42000.50 || payload.IndexPrice != 42001.0 || payload.FundingRate != 0.0001 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestParseMessage_Liquidation(t *testing.T) {
	msg := []byte(`{"topic":"allLiquidation.BTCUSDT","type":"snapshot","ts":1710000030000,"data":[{"s":"BTCUSDT","S":"Sell","v":"5.25","p":"64900.5","T":1710000030001}]}`)
	req, skip, p := bybit.ParseMessage(msg, time.UnixMilli(1710000031000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	if req.EventType != "marketdata.liquidation" || req.Instrument != "BTCUSDT" {
		t.Fatalf("unexpected request: %#v", req)
	}
	payload, ok := req.Payload.(domain.LiquidationTickV1)
	if !ok {
		t.Fatalf("unexpected payload type: %T", req.Payload)
	}
	if payload.Side != "sell" || payload.Price != 64900.5 || payload.Size != 5.25 || payload.Timestamp != 1710000030001 {
		t.Fatalf("unexpected liquidation payload: %#v", payload)
	}
}

func TestParseMessageForMarketType_PropagatesMarketType(t *testing.T) {
	msg := []byte(`{"topic":"publicTrade.BTCUSDT","type":"snapshot","ts":1710000001000,"data":[{"T":1710000001001,"s":"BTCUSDT","S":"Sell","v":"0.010","p":"65000.50","i":"123456"}]}`)
	req, skip, p := bybit.ParseMessageForMarketType(msg, time.UnixMilli(1710000003000), "USD_M_FUTURES")
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	if req.MarketType != domain.MarketTypeUSDMFutures.String() {
		t.Fatalf("market type = %q, want %q", req.MarketType, domain.MarketTypeUSDMFutures.String())
	}
	if req.Metadata["instrument_market_type"] != domain.MarketTypeUSDMFutures.String() {
		t.Fatalf("metadata market type = %q, want %q", req.Metadata["instrument_market_type"], domain.MarketTypeUSDMFutures.String())
	}
}

func TestParseMessage_UnknownEventRejected(t *testing.T) {
	_, skip, p := bybit.ParseMessage([]byte(`{"topic":"option.BTCUSDT","type":"delta","data":{}}`), time.Now())
	if !skip || p == nil {
		t.Fatalf("expected skip + problem, got skip=%v problem=%v", skip, p)
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("problem code = %q, want %q", p.Code, problem.ValidationFailed)
	}
	if p.Details["reason"] != "unsupported_event_type" {
		t.Fatalf("problem reason = %v, want unsupported_event_type", p.Details["reason"])
	}
}

func TestParseMessage_ControlEventSkipsWithoutProblem(t *testing.T) {
	_, skip, p := bybit.ParseMessage([]byte(`{"op":"ping"}`), time.Now())
	if !skip {
		t.Fatal("expected skip")
	}
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
}

func TestParseMessage_SubscribeRejectedReturnsProblem(t *testing.T) {
	_, skip, p := bybit.ParseMessage([]byte(`{"op":"subscribe","success":false,"ret_msg":"handler not found","ret_code":10404}`), time.Now())
	if !skip {
		t.Fatal("expected skip")
	}
	if p == nil {
		t.Fatal("expected problem")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("problem code = %q, want %q", p.Code, problem.ValidationFailed)
	}
	if got := p.Details["reason"]; got != "subscribe_rejected" {
		t.Fatalf("reason = %v, want subscribe_rejected", got)
	}
}
