package kraken_test

import (
	"testing"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/adapters/exchange/kraken"
	"github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/domain"
)

func TestParseMessage_KrakenTable(t *testing.T) {
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
			name:     "trade",
			input:    `{"channel":"trade","type":"update","data":[{"symbol":"BTC/USD","trades":[{"price":"42000.5","qty":"0.2","side":"buy","timestamp":"2023-11-14T22:13:20.000000Z","trade_id":"abc123"}]}]}`,
			wantSkip: false, wantEventType: "marketdata.trade", wantMetaType: "trade",
		},
		{
			name:     "book snapshot",
			input:    `{"channel":"book","type":"snapshot","data":[{"symbol":"ETH/USD","bids":[{"price":"2500.1","qty":"1.2"}],"asks":[{"price":"2500.2","qty":"2.3"}],"sequence":101,"timestamp":"2023-11-14T22:13:20.000000Z"}]}`,
			wantSkip: false, wantEventType: "marketdata.bookdelta", wantMetaType: "book",
		},
		{
			name:     "ticker",
			input:    `{"channel":"ticker","type":"update","data":[{"symbol":"BTC/USD","mark_price":"42000.5","index_price":"41999.9","funding_rate":"0.0001","sequence":42,"timestamp":"2023-11-14T22:13:20.000000Z"}]}`,
			wantSkip: false, wantEventType: "marketdata.markprice", wantMetaType: "ticker",
		},
		{
			name:     "control",
			input:    `{"method":"subscribe","success":true}`,
			wantSkip: true,
		},
		{
			name:        "invalid json",
			input:       `{"channel":`,
			wantSkip:    true,
			wantProblem: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, skip, meta := kraken.ParseMessageWithMeta([]byte(tc.input), recvAt)
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
				if req.Venue != kraken.VenueKraken {
					t.Fatalf("venue=%q", req.Venue)
				}
			}
		})
	}
}

func TestParseMessage_TradePayload(t *testing.T) {
	req, skip, p := kraken.ParseMessage([]byte(`{"channel":"trade","type":"update","data":[{"symbol":"XBT/USD","trades":[{"price":"42000.5","qty":"0.2","side":"sell","timestamp":"2023-11-14T22:13:20.000000Z","trade_id":"abc123"}]}]}`), time.UnixMilli(1700000005000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	if req.Instrument != "BTCUSD" {
		t.Fatalf("instrument=%q want BTCUSD", req.Instrument)
	}
	payload, ok := req.Payload.(domain.TradeTickV1)
	if !ok {
		t.Fatalf("payload type=%T", req.Payload)
	}
	if payload.Side != "sell" || payload.TradeID != "abc123" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestParseMessage_BookPayload(t *testing.T) {
	req, skip, p := kraken.ParseMessage([]byte(`{"channel":"book","type":"snapshot","data":[{"symbol":"ETH/USD","bids":[{"price":"2500.1","qty":"1.2"}],"asks":[{"price":"2500.2","qty":"2.3"}],"sequence":101,"timestamp":"2023-11-14T22:13:20.000000Z"}]}`), time.UnixMilli(1700000005000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	payload, ok := req.Payload.(domain.BookDeltaV1)
	if !ok {
		t.Fatalf("payload type=%T", req.Payload)
	}
	if len(payload.Bids) != 1 || len(payload.Asks) != 1 || !payload.IsSnapshot {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestParseMessage_TickerPayload(t *testing.T) {
	req, skip, p := kraken.ParseMessage([]byte(`{"channel":"ticker","type":"update","data":[{"symbol":"BTC/USD","mark_price":"42000.5","index_price":"41999.9","funding_rate":"0.0001","sequence":42,"timestamp":"2023-11-14T22:13:20.000000Z"}]}`), time.UnixMilli(1700000005000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	payload, ok := req.Payload.(domain.MarkPriceTickV1)
	if !ok {
		t.Fatalf("payload type=%T", req.Payload)
	}
	if payload.MarkPrice != 42000.5 || payload.IndexPrice != 41999.9 || payload.FundingRate != 0.0001 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if req.IdempotencyKey != "venue=KRAKEN|instrument=BTCUSD|sequence=42" {
		t.Fatalf("idempotency key=%q", req.IdempotencyKey)
	}
}
