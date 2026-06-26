package krakenf_test

import (
	"testing"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/adapters/exchange/krakenf"
	"github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/domain"
)

func TestParseMessage_KrakenFTable(t *testing.T) {
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
			input:    `{"feed":"trade","product_id":"PI_XBTUSD","trades":[{"price":"65000.5","qty":"0.01","side":"buy","time":"2023-11-14T22:13:20.000000Z","uid":"t-1"}]}`,
			wantSkip: false, wantEventType: "marketdata.trade", wantMetaType: "trade",
		},
		{
			name:     "book snapshot",
			input:    `{"feed":"book_snapshot","product_id":"PI_XBTUSD","bids":[["64999.9","1.2"]],"asks":[["65000.1","2.3"]],"seq":105,"time":"2023-11-14T22:13:20.000000Z"}`,
			wantSkip: false, wantEventType: "marketdata.bookdelta", wantMetaType: "book_snapshot",
		},
		{
			name:     "ticker",
			input:    `{"feed":"ticker","product_id":"PI_XBTUSD","mark_price":"65010.5","index_price":"65005.1","funding_rate":"0.0001","seq":9,"time":"2023-11-14T22:13:20.000000Z"}`,
			wantSkip: false, wantEventType: "marketdata.markprice", wantMetaType: "ticker",
		},
		{
			name:     "control",
			input:    `{"event":"subscribed","product_ids":["PI_XBTUSD"]}`,
			wantSkip: true,
		},
		{
			name:        "invalid json",
			input:       `{"feed":`,
			wantSkip:    true,
			wantProblem: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, skip, meta := krakenf.ParseMessageWithMeta([]byte(tc.input), recvAt)
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
				if req.Venue != krakenf.VenueKrakenF {
					t.Fatalf("venue=%q", req.Venue)
				}
			}
		})
	}
}

func TestParseMessage_TradePayload(t *testing.T) {
	req, skip, p := krakenf.ParseMessage([]byte(`{"feed":"trade","product_id":"PI_XBTUSD","trades":[{"price":"65000.5","qty":"0.01","side":"sell","time":"2023-11-14T22:13:20.000000Z","uid":"t-1"}]}`), time.UnixMilli(1700000005000))
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
	if payload.Side != "sell" || payload.TradeID != "t-1" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestParseMessage_BookPayload(t *testing.T) {
	req, skip, p := krakenf.ParseMessage([]byte(`{"feed":"book_snapshot","product_id":"PI_XBTUSD","bids":[["64999.9","1.2"]],"asks":[["65000.1","2.3"]],"seq":105,"time":"2023-11-14T22:13:20.000000Z"}`), time.UnixMilli(1700000005000))
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
	req, skip, p := krakenf.ParseMessage([]byte(`{"feed":"ticker","product_id":"PI_XBTUSD","mark_price":"65010.5","index_price":"65005.1","funding_rate":"0.0001","seq":9,"time":"2023-11-14T22:13:20.000000Z"}`), time.UnixMilli(1700000005000))
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	payload, ok := req.Payload.(domain.MarkPriceTickV1)
	if !ok {
		t.Fatalf("payload type=%T", req.Payload)
	}
	if payload.MarkPrice != 65010.5 || payload.IndexPrice != 65005.1 || payload.FundingRate != 0.0001 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if req.IdempotencyKey != "venue=KRAKENF|instrument=BTCUSD|sequence=9" {
		t.Fatalf("idempotency key=%q", req.IdempotencyKey)
	}
}

func TestParseMessageForMarketType_PropagatesMarketType(t *testing.T) {
	req, skip, p := krakenf.ParseMessageForMarketType([]byte(`{"feed":"trade","product_id":"PI_XBTUSD","trades":[{"price":"65000.5","qty":"0.01","side":"buy","time":"2023-11-14T22:13:20.000000Z","uid":"t-1"}]}`), time.UnixMilli(1700000005000), "USD_M_FUTURES")
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}
	if req.MarketType != domain.MarketTypeUSDMFutures.String() {
		t.Fatalf("market type=%q want=%q", req.MarketType, domain.MarketTypeUSDMFutures.String())
	}
}
