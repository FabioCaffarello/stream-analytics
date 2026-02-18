package hyperliquid_test

import (
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
