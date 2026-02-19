package hyperliquid_test

import (
	"strings"
	"testing"
	"time"

	"github.com/market-raccoon/internal/adapters/exchange/hyperliquid"
	"github.com/market-raccoon/internal/core/marketdata/domain"
)

func TestParseMessage_HyperLiquidTable(t *testing.T) {
	recvAt := time.UnixMilli(1700000005000)
	tests := []struct {
		name          string
		input         string
		wantSkip      bool
		wantEventType string
		wantMetaType  string
		wantProblem   bool
	}{
		{
			name:     "trades buy hash",
			input:    `{"channel":"trades","data":[{"coin":"BTC","side":"B","px":"42000.5","sz":"0.2","time":1700000000000,"hash":"0xabc","tid":7}]}`,
			wantSkip: false, wantEventType: "marketdata.trade", wantMetaType: "trades",
		},
		{
			name:     "trades sell",
			input:    `{"channel":"trades","data":[{"coin":"BTC","side":"A","px":"42000.5","sz":"0.2","time":1700000000000,"hash":"0xdef","tid":8}]}`,
			wantSkip: false, wantEventType: "marketdata.trade", wantMetaType: "trades",
		},
		{
			name:     "l2Book snapshot",
			input:    `{"channel":"l2Book","data":{"coin":"BTC","time":1700000000001,"levels":[[{"px":"42000.1","sz":"1.2","n":1}],[{"px":"42000.2","sz":"2.3","n":1}]]}}`,
			wantSkip: false, wantEventType: "marketdata.bookdelta", wantMetaType: "l2Book",
		},
		{
			name:     "control message",
			input:    `{"method":"subscribe","subscription":{"type":"trades","coin":"BTC"}}`,
			wantSkip: true,
		},
		{
			name:     "subscription response control",
			input:    `{"channel":"subscriptionResponse","data":{"method":"subscribe","coin":"BTC"}}`,
			wantSkip: true, wantMetaType: "subscriptionResponse",
		},
		{
			name:     "invalid json",
			input:    `{"channel":`,
			wantSkip: true, wantProblem: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, skip, meta := hyperliquid.ParseMessageWithMeta([]byte(tc.input), recvAt)
			if skip != tc.wantSkip {
				t.Fatalf("skip=%v want=%v", skip, tc.wantSkip)
			}
			if tc.wantMetaType != "" && meta.EventType != tc.wantMetaType {
				t.Fatalf("meta.EventType=%q want=%q", meta.EventType, tc.wantMetaType)
			}
			if (meta.Problem != nil) != tc.wantProblem {
				t.Fatalf("meta.Problem=%v wantProblem=%v", meta.Problem, tc.wantProblem)
			}
			if !tc.wantSkip && req.EventType != tc.wantEventType {
				t.Fatalf("event_type=%q want=%q", req.EventType, tc.wantEventType)
			}
		})
	}
}

func TestParseMessage_TradeUsesHashTradeID(t *testing.T) {
	req, skip, p := hyperliquid.ParseMessage([]byte(`{"channel":"trades","data":[{"coin":"BTC","side":"B","px":"42000.5","sz":"0.2","time":1700000000000,"hash":"0xabc","tid":7}]}`), time.UnixMilli(1700000005000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	payload, ok := req.Payload.(domain.TradeTickV1)
	if !ok {
		t.Fatalf("payload type=%T", req.Payload)
	}
	if payload.TradeID != "0xabc" || payload.Side != "buy" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestParseMessage_TradeZeroHashFallsBackToTid(t *testing.T) {
	zeroHash := "0x0000000000000000000000000000000000000000000000000000000000000000"
	input := `{"channel":"trades","data":[{"coin":"BTC","side":"B","px":"42000.5","sz":"0.2","time":1700000000000,"hash":"` + zeroHash + `","tid":12345}]}`
	req, skip, p := hyperliquid.ParseMessage([]byte(input), time.UnixMilli(1700000005000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	payload, ok := req.Payload.(domain.TradeTickV1)
	if !ok {
		t.Fatalf("payload type=%T", req.Payload)
	}
	if payload.TradeID != "12345" {
		t.Fatalf("expected tid fallback, got TradeID=%q", payload.TradeID)
	}
	if !strings.Contains(req.IdempotencyKey, "trade_id=12345") {
		t.Fatalf("idempotency key should contain tid, got %q", req.IdempotencyKey)
	}
}

func TestParseMessage_TradeShortZeroHashFallsBackToTid(t *testing.T) {
	input := `{"channel":"trades","data":[{"coin":"ETH","side":"A","px":"3000.0","sz":"1.5","time":1700000000000,"hash":"0x0000","tid":9999}]}`
	req, skip, p := hyperliquid.ParseMessage([]byte(input), time.UnixMilli(1700000005000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	payload, ok := req.Payload.(domain.TradeTickV1)
	if !ok {
		t.Fatalf("payload type=%T", req.Payload)
	}
	if payload.TradeID != "9999" {
		t.Fatalf("expected tid fallback for short zero hash, got TradeID=%q", payload.TradeID)
	}
}

func TestParseMessage_L2BookFullSnapshot(t *testing.T) {
	req, skip, p := hyperliquid.ParseMessage([]byte(`{"channel":"l2Book","data":{"coin":"BTC","time":1700000000001,"levels":[[{"px":"42000.1","sz":"1.2","n":1}],[{"px":"42000.2","sz":"2.3","n":1}]]}}`), time.UnixMilli(1700000005000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	payload, ok := req.Payload.(domain.BookDeltaV1)
	if !ok {
		t.Fatalf("payload type=%T", req.Payload)
	}
	if len(payload.Bids) != 1 || len(payload.Asks) != 1 || payload.FirstID != payload.FinalID {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestParseMessage_L2BookSwapsReversedSidesWhenNeeded(t *testing.T) {
	req, skip, p := hyperliquid.ParseMessage([]byte(`{"channel":"l2Book","data":{"coin":"BTC","time":1700000000100,"levels":[[{"px":"42000.2","sz":"2.3","n":1}],[{"px":"42000.1","sz":"1.2","n":1}]]}}`), time.UnixMilli(1700000005000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	payload, ok := req.Payload.(domain.BookDeltaV1)
	if !ok {
		t.Fatalf("payload type=%T", req.Payload)
	}
	if len(payload.Bids) != 1 || len(payload.Asks) != 1 {
		t.Fatalf("unexpected depth levels: %#v", payload)
	}
	if payload.Bids[0].Price >= payload.Asks[0].Price {
		t.Fatalf("expected normalized sides with bid < ask, got bid=%f ask=%f", payload.Bids[0].Price, payload.Asks[0].Price)
	}
}

func TestParseMessage_L2BookRejectsCrossedSnapshot(t *testing.T) {
	// Even after potential side normalization, this remains crossed and must be rejected.
	req, skip, p := hyperliquid.ParseMessage([]byte(`{"channel":"l2Book","data":{"coin":"BTC","time":1700000000100,"levels":[[{"px":"42000.2","sz":"2.3","n":1},{"px":"42000.0","sz":"1.1","n":1}],[{"px":"42000.1","sz":"1.2","n":1}]]}}`), time.UnixMilli(1700000005000))
	if !skip {
		t.Fatalf("skip=%v want=true", skip)
	}
	if p == nil {
		t.Fatal("expected validation problem for crossed snapshot")
	}
	if req.EventType != "" {
		t.Fatalf("expected empty request on skip, got event_type=%q", req.EventType)
	}
}

func TestParseAllMids_ParsesBroadcast(t *testing.T) {
	subscribedCoins := map[string]bool{"BTC": true, "ETH": true}
	parse := hyperliquid.ParseAllMids(subscribedCoins, "USDM_FUTURES")
	input := `{"channel":"allMids","data":{"mids":{"BTC":"96000.5","ETH":"2700.3","SOL":"150.2"}}}`
	recvAt := time.UnixMilli(1700000005000)

	reqs, err := parse([]byte(input), recvAt)
	if err != nil {
		t.Fatalf("ParseAllMids error: %v", err)
	}
	if len(reqs) != 2 {
		t.Fatalf("reqs len=%d want=2 (BTC+ETH, SOL filtered out)", len(reqs))
	}

	byInstrument := make(map[string]domain.MarkPriceTickV1)
	for _, req := range reqs {
		if req.EventType != "marketdata.markprice" {
			t.Fatalf("event_type=%q want=marketdata.markprice", req.EventType)
		}
		if req.Venue != "HYPERLIQUID" {
			t.Fatalf("venue=%q want=HYPERLIQUID", req.Venue)
		}
		payload, ok := req.Payload.(domain.MarkPriceTickV1)
		if !ok {
			t.Fatalf("payload type=%T", req.Payload)
		}
		byInstrument[req.Instrument] = payload
	}

	btc, ok := byInstrument["BTCUSD"]
	if !ok {
		t.Fatal("missing BTC request")
	}
	if btc.MarkPrice != 96000.5 {
		t.Fatalf("BTC markprice=%f want=96000.5", btc.MarkPrice)
	}

	eth, ok := byInstrument["ETHUSD"]
	if !ok {
		t.Fatal("missing ETH request")
	}
	if eth.MarkPrice != 2700.3 {
		t.Fatalf("ETH markprice=%f want=2700.3", eth.MarkPrice)
	}
}

func TestParseAllMids_ReturnsNilForNonAllMids(t *testing.T) {
	subscribedCoins := map[string]bool{"BTC": true}
	parse := hyperliquid.ParseAllMids(subscribedCoins, "USDM_FUTURES")
	input := `{"channel":"trades","data":[{"coin":"BTC","side":"B","px":"42000.5","sz":"0.2","time":1700000000000,"hash":"0xabc","tid":7}]}`
	recvAt := time.UnixMilli(1700000005000)

	reqs, err := parse([]byte(input), recvAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs != nil {
		t.Fatalf("expected nil for non-allMids message, got len=%d", len(reqs))
	}
}

func TestParseAllMids_EmptyWhenNoSubscribedCoins(t *testing.T) {
	subscribedCoins := map[string]bool{"DOGE": true}
	parse := hyperliquid.ParseAllMids(subscribedCoins, "USDM_FUTURES")
	input := `{"channel":"allMids","data":{"mids":{"BTC":"96000.5","ETH":"2700.3"}}}`
	recvAt := time.UnixMilli(1700000005000)

	reqs, err := parse([]byte(input), recvAt)
	if err != nil {
		t.Fatalf("ParseAllMids error: %v", err)
	}
	if reqs == nil {
		t.Fatal("expected non-nil (handled) result, got nil")
	}
	if len(reqs) != 0 {
		t.Fatalf("reqs len=%d want=0 (no subscribed coins matched)", len(reqs))
	}
}
