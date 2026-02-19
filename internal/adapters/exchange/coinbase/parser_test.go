package coinbase_test

import (
	"testing"
	"time"

	"github.com/market-raccoon/internal/adapters/exchange/coinbase"
	"github.com/market-raccoon/internal/core/marketdata/domain"
)

func TestParseMessage_CoinbaseTable(t *testing.T) {
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
			name:     "match",
			input:    `{"type":"match","trade_id":12345,"product_id":"BTC-USD","price":"42000.50","size":"0.100","side":"buy","time":"2023-11-14T22:13:20.000000Z"}`,
			wantSkip: false, wantEventType: "marketdata.trade", wantMetaType: "match",
		},
		{
			name:     "snapshot",
			input:    `{"type":"snapshot","product_id":"ETH-USD","bids":[["2500.1","1.2"]],"asks":[["2500.2","2.3"]]}`,
			wantSkip: false, wantEventType: "marketdata.bookdelta", wantMetaType: "snapshot",
		},
		{
			name:     "l2update",
			input:    `{"type":"l2update","product_id":"ETH-USD","time":"2023-11-14T22:13:20.000000Z","changes":[["buy","2500.1","1.2"],["sell","2500.2","2.3"]]}`,
			wantSkip: false, wantEventType: "marketdata.bookdelta", wantMetaType: "l2update",
		},
		{
			name:     "ticker",
			input:    `{"type":"ticker","product_id":"BTC-USD","price":"42000.50","time":"2023-11-14T22:13:20.000000Z"}`,
			wantSkip: false, wantEventType: "marketdata.markprice", wantMetaType: "ticker",
		},
		{
			name:     "heartbeat control",
			input:    `{"type":"heartbeat"}`,
			wantSkip: true, wantMetaType: "heartbeat",
		},
		{
			name:     "subscriptions control",
			input:    `{"type":"subscriptions"}`,
			wantSkip: true, wantMetaType: "subscriptions",
		},
		{
			name:     "error control",
			input:    `{"type":"error","message":"bad"}`,
			wantSkip: true, wantMetaType: "error",
		},
		{
			name:     "invalid json",
			input:    `{"type":`,
			wantSkip: true, wantProblem: true,
		},
		{
			name:     "missing product id",
			input:    `{"type":"ticker","product_id":"","price":"1","time":"2023-11-14T22:13:20.000000Z"}`,
			wantSkip: true, wantProblem: true, wantMetaType: "ticker",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, skip, meta := coinbase.ParseMessageWithMeta([]byte(tc.input), recvAt)
			if skip != tc.wantSkip {
				t.Fatalf("skip=%v want=%v", skip, tc.wantSkip)
			}
			if tc.wantMetaType != "" && meta.EventType != tc.wantMetaType {
				t.Fatalf("meta.EventType=%q want=%q", meta.EventType, tc.wantMetaType)
			}
			if (meta.Problem != nil) != tc.wantProblem {
				t.Fatalf("meta.Problem=%v wantProblem=%v", meta.Problem, tc.wantProblem)
			}
			if !tc.wantSkip {
				if req.EventType != tc.wantEventType {
					t.Fatalf("event_type=%q want=%q", req.EventType, tc.wantEventType)
				}
				if req.Venue != coinbase.VenueCoinbase {
					t.Fatalf("venue=%q", req.Venue)
				}
			}
		})
	}
}

func TestParseMessage_MatchPayload(t *testing.T) {
	req, skip, p := coinbase.ParseMessage([]byte(`{"type":"match","trade_id":12345,"product_id":"BTC-USD","price":"42000.50","size":"0.100","side":"buy","time":"2023-11-14T22:13:20.000000Z"}`), time.UnixMilli(1700000005000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	payload, ok := req.Payload.(domain.TradeTickV1)
	if !ok {
		t.Fatalf("payload type=%T", req.Payload)
	}
	if payload.Price != 42000.5 || payload.Size != 0.1 || payload.Side != "buy" || payload.TradeID != "12345" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestParseMessage_SnapshotPayload(t *testing.T) {
	req, skip, p := coinbase.ParseMessage([]byte(`{"type":"snapshot","product_id":"ETH-USD","bids":[["2500.1","1.2"]],"asks":[["2500.2","2.3"]]}`), time.UnixMilli(1700000005000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	payload, ok := req.Payload.(domain.BookDeltaV1)
	if !ok {
		t.Fatalf("payload type=%T", req.Payload)
	}
	if len(payload.Bids) != 1 || len(payload.Asks) != 1 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestParseMessage_L2UpdateSplitBySide(t *testing.T) {
	req, skip, p := coinbase.ParseMessage([]byte(`{"type":"l2update","product_id":"ETH-USD","time":"2023-11-14T22:13:20.000000Z","changes":[["buy","2500.1","1.2"],["sell","2500.2","2.3"]]}`), time.UnixMilli(1700000005000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	payload, ok := req.Payload.(domain.BookDeltaV1)
	if !ok {
		t.Fatalf("payload type=%T", req.Payload)
	}
	if len(payload.Bids) != 1 || len(payload.Asks) != 1 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestParseMessage_L2UpdateIdempotency_UsesPayloadHashWhenSequenceMissing(t *testing.T) {
	recvAt := time.UnixMilli(1700000005000)
	reqA, skipA, pA := coinbase.ParseMessage([]byte(`{"type":"l2update","product_id":"ETH-USD","time":"2023-11-14T22:13:20.000000Z","changes":[["buy","2500.1","1.2"]]}`), recvAt)
	if pA != nil || skipA {
		t.Fatalf("ParseMessage A failed: skip=%v problem=%v", skipA, pA)
	}
	reqB, skipB, pB := coinbase.ParseMessage([]byte(`{"type":"l2update","product_id":"ETH-USD","time":"2023-11-14T22:13:20.000000Z","changes":[["buy","2500.1","1.3"]]}`), recvAt)
	if pB != nil || skipB {
		t.Fatalf("ParseMessage B failed: skip=%v problem=%v", skipB, pB)
	}
	if reqA.IdempotencyKey == "" || reqB.IdempotencyKey == "" {
		t.Fatal("idempotency key must not be empty")
	}
	if reqA.IdempotencyKey == reqB.IdempotencyKey {
		t.Fatalf("idempotency keys must differ for distinct payloads: %q", reqA.IdempotencyKey)
	}
}

func TestParseMessage_TickerPayload(t *testing.T) {
	req, skip, p := coinbase.ParseMessage([]byte(`{"type":"ticker","product_id":"BTC-USD","price":"42000.50","time":"2023-11-14T22:13:20.000000Z","sequence":4242}`), time.UnixMilli(1700000005000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	payload, ok := req.Payload.(domain.MarkPriceTickV1)
	if !ok {
		t.Fatalf("payload type=%T", req.Payload)
	}
	if payload.MarkPrice != 42000.5 || payload.FundingRate != 0 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if req.IdempotencyKey != "venue=COINBASE|instrument=BTCUSD|sequence=4242" {
		t.Fatalf("idempotency key = %q", req.IdempotencyKey)
	}
}
