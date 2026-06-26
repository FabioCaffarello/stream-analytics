package mdruntime

import (
	"bytes"
	"testing"
	"time"

	ws "github.com/FabioCaffarello/stream-analytics/internal/actors/marketdata/ws"
	"github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/app"
)

// ── MakeRawParseFunc ────────────────────────────────────────────────────────

func TestMakeRawParseFunc_PopulatesAllFields(t *testing.T) {
	parse := MakeRawParseFunc("binance", "BTC-USDT")
	now := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)
	msg := &ws.WsMessage{
		Exchange:   "binance",
		BucketID:   42,
		ConsumerID: "c-1",
		Endpoint:   "wss://stream.binance.com",
		Data:       []byte(`{"e":"aggTrade"}`),
		RecvAt:     now,
	}

	req, skip := parse(msg)

	if skip {
		t.Fatal("expected skip=false for raw parser")
	}
	if req.Venue != "binance" {
		t.Fatalf("Venue = %q, want %q", req.Venue, "binance")
	}
	if req.Instrument != "BTC-USDT" {
		t.Fatalf("Instrument = %q, want %q", req.Instrument, "BTC-USDT")
	}
	if req.EventType != "marketdata.raw" {
		t.Fatalf("EventType = %q, want %q", req.EventType, "marketdata.raw")
	}
	if req.Version != 1 {
		t.Fatalf("Version = %d, want 1", req.Version)
	}
	if req.TsExchange != now.UnixMilli() {
		t.Fatalf("TsExchange = %d, want %d", req.TsExchange, now.UnixMilli())
	}
	raw, ok := req.Payload.(RawMessageV1)
	if !ok {
		t.Fatalf("Payload type = %T, want RawMessageV1", req.Payload)
	}
	if !bytes.Equal(raw.Data, msg.Data) {
		t.Fatalf("Payload.Data = %q, want %q", raw.Data, msg.Data)
	}
}

func TestMakeRawParseFunc_NeverSkips(t *testing.T) {
	parse := MakeRawParseFunc("kraken", "ETH-USD")
	for i := 0; i < 10; i++ {
		msg := &ws.WsMessage{
			Data:   []byte("anything"),
			RecvAt: time.Now(),
		}
		if _, skip := parse(msg); skip {
			t.Fatalf("iteration %d: expected skip=false", i)
		}
	}
}

func TestMakeRawParseFunc_EmptyVenueInstrument(t *testing.T) {
	parse := MakeRawParseFunc("", "")
	msg := &ws.WsMessage{
		Data:   []byte("data"),
		RecvAt: time.Now(),
	}

	req, skip := parse(msg)

	if skip {
		t.Fatal("expected skip=false even with empty venue/instrument")
	}
	if req.Venue != "" {
		t.Fatalf("Venue = %q, want empty", req.Venue)
	}
	if req.Instrument != "" {
		t.Fatalf("Instrument = %q, want empty", req.Instrument)
	}
}

func TestMakeRawParseFunc_NilData(t *testing.T) {
	parse := MakeRawParseFunc("coinbase", "BTC-USD")
	msg := &ws.WsMessage{
		Data:   nil,
		RecvAt: time.Now(),
	}

	req, skip := parse(msg)

	if skip {
		t.Fatal("expected skip=false even with nil data")
	}
	raw, ok := req.Payload.(RawMessageV1)
	if !ok {
		t.Fatalf("Payload type = %T, want RawMessageV1", req.Payload)
	}
	if raw.Data != nil {
		t.Fatalf("Payload.Data = %v, want nil", raw.Data)
	}
}

func TestMakeRawParseFunc_UsesRecvAtNotWallClock(t *testing.T) {
	parse := MakeRawParseFunc("bybit", "SOL-USDT")
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	msg := &ws.WsMessage{
		Data:   []byte("tick"),
		RecvAt: past,
	}

	req, _ := parse(msg)

	if req.TsExchange != past.UnixMilli() {
		t.Fatalf("TsExchange = %d, want %d (should use RecvAt, not wall clock)", req.TsExchange, past.UnixMilli())
	}
}

func TestMakeRawParseFunc_ClosesOverVenueAndInstrument(t *testing.T) {
	tests := []struct {
		venue      string
		instrument string
	}{
		{"binance", "BTC-USDT"},
		{"kraken", "ETH-USD"},
		{"hyperliquid", "SOL-USDT-PERP"},
		{"coinbase", "DOGE-EUR"},
	}

	for _, tt := range tests {
		t.Run(tt.venue+"/"+tt.instrument, func(t *testing.T) {
			parse := MakeRawParseFunc(tt.venue, tt.instrument)
			msg := &ws.WsMessage{
				Data:   []byte("payload"),
				RecvAt: time.Now(),
			}

			req, skip := parse(msg)

			if skip {
				t.Fatal("expected skip=false")
			}
			if req.Venue != tt.venue {
				t.Fatalf("Venue = %q, want %q", req.Venue, tt.venue)
			}
			if req.Instrument != tt.instrument {
				t.Fatalf("Instrument = %q, want %q", req.Instrument, tt.instrument)
			}
		})
	}
}

// ── RawMessageV1 ────────────────────────────────────────────────────────────

func TestRawMessageV1_HoldsData(t *testing.T) {
	data := []byte(`{"price":"42000.50","qty":"1.5"}`)
	raw := RawMessageV1{Data: data}

	if !bytes.Equal(raw.Data, data) {
		t.Fatalf("Data = %q, want %q", raw.Data, data)
	}
}

func TestRawMessageV1_EmptyAndNil(t *testing.T) {
	empty := RawMessageV1{Data: []byte{}}
	if len(empty.Data) != 0 {
		t.Fatalf("expected empty data, got len=%d", len(empty.Data))
	}

	nilMsg := RawMessageV1{Data: nil}
	if nilMsg.Data != nil {
		t.Fatal("expected nil data")
	}
}

// ── ParseFunc contract ──────────────────────────────────────────────────────

func TestParseFuncContract_SkipTrue_MeansDiscard(t *testing.T) {
	// A ParseFunc that always skips (e.g., heartbeat filter).
	alwaysSkip := ParseFunc(func(msg *ws.WsMessage) (app.IngestRequest, bool) {
		return app.IngestRequest{}, true
	})

	msg := &ws.WsMessage{
		Data:   []byte("heartbeat"),
		RecvAt: time.Now(),
	}

	req, skip := alwaysSkip(msg)

	if !skip {
		t.Fatal("expected skip=true for heartbeat filter")
	}
	// When skip=true, the IngestRequest should be treated as meaningless.
	// Verify the zero value is returned (caller must not use it).
	if req.Venue != "" || req.Instrument != "" || req.EventType != "" {
		t.Fatal("expected zero IngestRequest when skip=true")
	}
}

func TestParseFuncContract_SkipFalse_MeansPublish(t *testing.T) {
	// A ParseFunc that always returns a populated request.
	alwaysPublish := ParseFunc(func(msg *ws.WsMessage) (app.IngestRequest, bool) {
		return app.IngestRequest{
			Venue:      "test-venue",
			Instrument: "TEST-PAIR",
			EventType:  "trade",
			Version:    1,
			TsExchange: msg.RecvAt.UnixMilli(),
			Payload:    "some-payload",
		}, false
	})

	msg := &ws.WsMessage{
		Data:   []byte(`{"trade":true}`),
		RecvAt: time.Now(),
	}

	req, skip := alwaysPublish(msg)

	if skip {
		t.Fatal("expected skip=false for publishable trade")
	}
	if req.Venue != "test-venue" {
		t.Fatalf("Venue = %q, want %q", req.Venue, "test-venue")
	}
	if req.EventType != "trade" {
		t.Fatalf("EventType = %q, want %q", req.EventType, "trade")
	}
}

// ── ParseFuncV2 contract ────────────────────────────────────────────────────

func TestParseFuncV2_MetaPopulated(t *testing.T) {
	v2 := ParseFuncV2(func(msg *ws.WsMessage) (app.IngestRequest, bool, ParseMeta) {
		return app.IngestRequest{
				Venue:      "binance",
				Instrument: "BTC-USDT",
				EventType:  "aggTrade",
				Version:    1,
				TsExchange: msg.RecvAt.UnixMilli(),
			}, false, ParseMeta{
				EventType: "aggTrade",
				WSStream:  "btcusdt@aggTrade",
				Ticker:    "BTC-USDT",
			}
	})

	msg := &ws.WsMessage{
		Data:   []byte(`{"e":"aggTrade"}`),
		RecvAt: time.Now(),
	}

	req, skip, meta := v2(msg)

	if skip {
		t.Fatal("expected skip=false")
	}
	if req.EventType != "aggTrade" {
		t.Fatalf("req.EventType = %q, want %q", req.EventType, "aggTrade")
	}
	if meta.EventType != "aggTrade" {
		t.Fatalf("meta.EventType = %q, want %q", meta.EventType, "aggTrade")
	}
	if meta.WSStream != "btcusdt@aggTrade" {
		t.Fatalf("meta.WSStream = %q, want %q", meta.WSStream, "btcusdt@aggTrade")
	}
	if meta.Ticker != "BTC-USDT" {
		t.Fatalf("meta.Ticker = %q, want %q", meta.Ticker, "BTC-USDT")
	}
}

func TestParseFuncV2_SkipWithDiagnostics(t *testing.T) {
	v2 := ParseFuncV2(func(msg *ws.WsMessage) (app.IngestRequest, bool, ParseMeta) {
		return app.IngestRequest{}, true, ParseMeta{
			EventType:      "unknown",
			SkipReason:     "unsupported_event",
			ProblemCode:    "",
			ProblemMessage: "",
			WSStream:       "btcusdt@miniTicker",
			Ticker:         "BTC-USDT",
		}
	})

	msg := &ws.WsMessage{
		Data:   []byte(`{"e":"24hrMiniTicker"}`),
		RecvAt: time.Now(),
	}

	_, skip, meta := v2(msg)

	if !skip {
		t.Fatal("expected skip=true for unsupported event")
	}
	if meta.SkipReason != "unsupported_event" {
		t.Fatalf("meta.SkipReason = %q, want %q", meta.SkipReason, "unsupported_event")
	}
	if meta.EventType != "unknown" {
		t.Fatalf("meta.EventType = %q, want %q", meta.EventType, "unknown")
	}
}

func TestParseFuncV2_SkipWithProblem(t *testing.T) {
	v2 := ParseFuncV2(func(msg *ws.WsMessage) (app.IngestRequest, bool, ParseMeta) {
		return app.IngestRequest{}, true, ParseMeta{
			EventType:      "aggTrade",
			SkipReason:     "parse_error",
			ProblemCode:    "VALIDATION_FAILED",
			ProblemMessage: "price field is negative",
			WSStream:       "btcusdt@aggTrade",
			Ticker:         "BTC-USDT",
		}
	})

	msg := &ws.WsMessage{
		Data:   []byte(`{"e":"aggTrade","p":"-100"}`),
		RecvAt: time.Now(),
	}

	_, skip, meta := v2(msg)

	if !skip {
		t.Fatal("expected skip=true for parse error")
	}
	if meta.ProblemCode != "VALIDATION_FAILED" {
		t.Fatalf("meta.ProblemCode = %q, want %q", meta.ProblemCode, "VALIDATION_FAILED")
	}
	if meta.ProblemMessage != "price field is negative" {
		t.Fatalf("meta.ProblemMessage = %q, want %q", meta.ProblemMessage, "price field is negative")
	}
}

// ── ParseFuncBatch contract ─────────────────────────────────────────────────

func TestParseFuncBatch_NilMeansNotHandled(t *testing.T) {
	// A batch parser that returns nil to indicate "not my message, fall through".
	notHandled := ParseFuncBatch(func(msg *ws.WsMessage) ([]app.IngestRequest, error) {
		return nil, nil
	})

	msg := &ws.WsMessage{
		Data:   []byte(`{"channel":"orderbook"}`),
		RecvAt: time.Now(),
	}

	result, err := notHandled(msg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil (not handled), got slice of len %d", len(result))
	}
}

func TestParseFuncBatch_EmptySliceMeansHandledButEmpty(t *testing.T) {
	// A batch parser that recognizes the message but produces no ingest items
	// (e.g., all instruments filtered out).
	handledEmpty := ParseFuncBatch(func(msg *ws.WsMessage) ([]app.IngestRequest, error) {
		return []app.IngestRequest{}, nil
	})

	msg := &ws.WsMessage{
		Data:   []byte(`{"channel":"allMids","data":[]}`),
		RecvAt: time.Now(),
	}

	result, err := handledEmpty(msg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil empty slice (handled), got nil (not handled)")
	}
	if len(result) != 0 {
		t.Fatalf("expected empty slice, got len=%d", len(result))
	}
}

func TestParseFuncBatch_MultipleRequests(t *testing.T) {
	// Simulates HyperLiquid allMids: one WS message produces N ingest requests.
	batchParser := ParseFuncBatch(func(msg *ws.WsMessage) ([]app.IngestRequest, error) {
		return []app.IngestRequest{
			{Venue: "hyperliquid", Instrument: "BTC-USDT-PERP", EventType: "midPrice", Version: 1, TsExchange: msg.RecvAt.UnixMilli()},
			{Venue: "hyperliquid", Instrument: "ETH-USDT-PERP", EventType: "midPrice", Version: 1, TsExchange: msg.RecvAt.UnixMilli()},
			{Venue: "hyperliquid", Instrument: "SOL-USDT-PERP", EventType: "midPrice", Version: 1, TsExchange: msg.RecvAt.UnixMilli()},
		}, nil
	})

	now := time.Now()
	msg := &ws.WsMessage{
		Data:   []byte(`{"channel":"allMids","data":{"BTC":42000,"ETH":3000,"SOL":100}}`),
		RecvAt: now,
	}

	result, err := batchParser(msg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(result))
	}

	instruments := []string{"BTC-USDT-PERP", "ETH-USDT-PERP", "SOL-USDT-PERP"}
	for i, want := range instruments {
		if result[i].Instrument != want {
			t.Fatalf("result[%d].Instrument = %q, want %q", i, result[i].Instrument, want)
		}
		if result[i].TsExchange != now.UnixMilli() {
			t.Fatalf("result[%d].TsExchange = %d, want %d", i, result[i].TsExchange, now.UnixMilli())
		}
	}
}

func TestParseFuncBatch_ReturnsError(t *testing.T) {
	failing := ParseFuncBatch(func(msg *ws.WsMessage) ([]app.IngestRequest, error) {
		return nil, errTestBatch
	})

	msg := &ws.WsMessage{
		Data:   []byte("malformed"),
		RecvAt: time.Now(),
	}

	result, err := failing(msg)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != errTestBatch {
		t.Fatalf("error = %v, want %v", err, errTestBatch)
	}
	if result != nil {
		t.Fatalf("expected nil result on error, got len=%d", len(result))
	}
}

// ── ParseMeta zero value ────────────────────────────────────────────────────

func TestParseMeta_ZeroValue(t *testing.T) {
	var meta ParseMeta

	if meta.EventType != "" {
		t.Fatalf("zero EventType = %q, want empty", meta.EventType)
	}
	if meta.SkipReason != "" {
		t.Fatalf("zero SkipReason = %q, want empty", meta.SkipReason)
	}
	if meta.ProblemCode != "" {
		t.Fatalf("zero ProblemCode = %q, want empty", meta.ProblemCode)
	}
	if meta.ProblemMessage != "" {
		t.Fatalf("zero ProblemMessage = %q, want empty", meta.ProblemMessage)
	}
	if meta.WSStream != "" {
		t.Fatalf("zero WSStream = %q, want empty", meta.WSStream)
	}
	if meta.Ticker != "" {
		t.Fatalf("zero Ticker = %q, want empty", meta.Ticker)
	}
}

// ── sentinel error for batch test ───────────────────────────────────────────

type testBatchError struct{}

func (testBatchError) Error() string { return "test batch error" }

var errTestBatch = testBatchError{}
