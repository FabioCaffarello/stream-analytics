package kraken

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/market-raccoon/internal/shared/problem"
)

func swapKrakenBackfillHTTPGet(t *testing.T, fn func(ctx context.Context, url string, dest any) *problem.Problem) {
	t.Helper()
	orig := backfillHTTPGet
	t.Cleanup(func() { backfillHTTPGet = orig })
	backfillHTTPGet = fn
}

func singleDayKrakenConfig(t *testing.T) BackfillConfig {
	t.Helper()
	return BackfillConfig{
		Symbol:    "BTC-USD",
		From:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		To:        time.Date(2024, 1, 15, 23, 59, 59, 0, time.UTC),
		OutputDir: t.TempDir(),
		BaseURL:   "https://fake.kraken.test",
	}
}

func fakeKrakenTradesResponse(n int, day time.Time, lastCursor string) krakenTradesResponse {
	trades := make([][]json.RawMessage, n)
	for i := 0; i < n; i++ {
		ts := float64(day.Add(time.Duration(i) * time.Second).Unix())
		price := fmt.Sprintf("%.2f", 42000.0+float64(i))
		vol := fmt.Sprintf("%.4f", 0.1+float64(i)*0.01)
		side := "b"
		if i%2 == 1 {
			side = "s"
		}
		tradeID := fmt.Sprintf("%d", 1000+i)
		trades[i] = []json.RawMessage{
			json.RawMessage(fmt.Sprintf("%q", price)),
			json.RawMessage(fmt.Sprintf("%q", vol)),
			json.RawMessage(fmt.Sprintf("%f", ts)),
			json.RawMessage(fmt.Sprintf("%q", side)),
			json.RawMessage(`"l"`), // type: limit
			json.RawMessage(`""`),  // misc
			json.RawMessage(fmt.Sprintf("%q", tradeID)),
		}
	}

	tradesRaw, _ := json.Marshal(trades)
	lastRaw, _ := json.Marshal(lastCursor)

	return krakenTradesResponse{
		Error: []string{},
		Result: map[string]json.RawMessage{
			"XXBTUSD": tradesRaw,
			"last":    lastRaw,
		},
	}
}

func TestDownloadTrades_EmptySymbol(t *testing.T) {
	t.Parallel()
	cfg := BackfillConfig{
		Symbol:    "",
		From:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		To:        time.Date(2024, 1, 15, 23, 59, 59, 0, time.UTC),
		OutputDir: t.TempDir(),
	}
	_, p := DownloadTrades(context.Background(), cfg)
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed for empty symbol, got=%v", p)
	}
}

func TestDownloadTrades_ToBeforeFrom(t *testing.T) {
	swapKrakenBackfillHTTPGet(t, func(_ context.Context, _ string, _ any) *problem.Problem {
		t.Fatal("httpGet should not be called when To < From")
		return nil
	})

	cfg := BackfillConfig{
		Symbol:    "BTC-USD",
		From:      time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC),
		To:        time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		OutputDir: t.TempDir(),
		BaseURL:   "https://fake.kraken.test",
	}
	result, p := DownloadTrades(context.Background(), cfg)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if result.DatesDownloaded != 0 || result.TradesParsed != 0 {
		t.Fatalf("expected zero downloads, got downloaded=%d trades=%d", result.DatesDownloaded, result.TradesParsed)
	}
}

func TestDownloadTrades_SingleDay(t *testing.T) {
	cfg := singleDayKrakenConfig(t)
	day := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	callCount := 0
	swapKrakenBackfillHTTPGet(t, func(_ context.Context, _ string, dest any) *problem.Problem {
		callCount++
		if callCount == 1 {
			resp := fakeKrakenTradesResponse(3, day, "done")
			assignKrakenJSON(t, resp, dest)
			return nil
		}
		// Second call: return empty response to terminate pagination.
		resp := krakenTradesResponse{
			Error: []string{},
			Result: map[string]json.RawMessage{
				"XXBTUSD": json.RawMessage(`[]`),
				"last":    json.RawMessage(`"done"`),
			},
		}
		assignKrakenJSON(t, resp, dest)
		return nil
	})

	result, p := DownloadTrades(context.Background(), cfg)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if result.DatesDownloaded != 1 {
		t.Fatalf("DatesDownloaded=%d want=1", result.DatesDownloaded)
	}
	if result.TradesParsed != 3 {
		t.Fatalf("TradesParsed=%d want=3", result.TradesParsed)
	}
}

func TestDownloadTrades_CachesAndSkips(t *testing.T) {
	cfg := singleDayKrakenConfig(t)
	day := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	callCount := 0
	swapKrakenBackfillHTTPGet(t, func(_ context.Context, _ string, dest any) *problem.Problem {
		callCount++
		resp := fakeKrakenTradesResponse(2, day, "done")
		assignKrakenJSON(t, resp, dest)
		return nil
	})

	result1, p := DownloadTrades(context.Background(), cfg)
	if p != nil {
		t.Fatalf("first call: %v", p)
	}
	if result1.DatesDownloaded != 1 {
		t.Fatalf("first call: DatesDownloaded=%d want=1", result1.DatesDownloaded)
	}
	firstCallCount := callCount

	// Remove output fixture but keep cache.
	_ = removeIfExists(result1.OutputPath)

	result2, p := DownloadTrades(context.Background(), cfg)
	if p != nil {
		t.Fatalf("second call: %v", p)
	}
	if result2.DatesSkipped != 1 {
		t.Fatalf("second call: DatesSkipped=%d want=1", result2.DatesSkipped)
	}
	if callCount != firstCallCount {
		t.Fatalf("expected no new HTTP calls; first=%d total=%d", firstCallCount, callCount)
	}
}

func TestDownloadTrades_HTTPError(t *testing.T) {
	cfg := singleDayKrakenConfig(t)

	swapKrakenBackfillHTTPGet(t, func(_ context.Context, _ string, _ any) *problem.Problem {
		return problem.New(problem.Unavailable, "simulated HTTP 503")
	})

	_, p := DownloadTrades(context.Background(), cfg)
	if p == nil || p.Code != problem.Unavailable {
		t.Fatalf("expected Unavailable for HTTP error, got=%v", p)
	}
}

func TestDownloadTrades_ContextCanceled(t *testing.T) {
	cfg := singleDayKrakenConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, p := DownloadTrades(ctx, cfg)
	if p == nil || p.Code != problem.Unavailable {
		t.Fatalf("expected Unavailable for canceled context, got=%v", p)
	}
	if !strings.Contains(p.Message, "canceled") {
		t.Fatalf("expected message to contain 'canceled', got: %s", p.Message)
	}
}

func TestKrakenArrayToTick_Valid(t *testing.T) {
	t.Parallel()
	ts := float64(time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC).Unix())
	arr := []json.RawMessage{
		json.RawMessage(`"42000.50"`),
		json.RawMessage(`"0.1234"`),
		json.RawMessage(fmt.Sprintf("%f", ts)),
		json.RawMessage(`"b"`),
		json.RawMessage(`"l"`),
		json.RawMessage(`""`),
		json.RawMessage(`"12345"`),
	}
	tick, p := krakenArrayToTick(arr)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if tick.Price != 42000.50 {
		t.Fatalf("Price=%f want=42000.50", tick.Price)
	}
	if tick.Size != 0.1234 {
		t.Fatalf("Size=%f want=0.1234", tick.Size)
	}
	if tick.Side != "buy" {
		t.Fatalf("Side=%q want=buy", tick.Side)
	}
	if tick.TradeID != "12345" {
		t.Fatalf("TradeID=%q want=12345", tick.TradeID)
	}
}

func TestKrakenArrayToTick_InvalidPrice(t *testing.T) {
	t.Parallel()
	arr := []json.RawMessage{
		json.RawMessage(`"not-a-number"`),
		json.RawMessage(`"0.1"`),
		json.RawMessage(`1700000000.0`),
		json.RawMessage(`"b"`),
		json.RawMessage(`"l"`),
		json.RawMessage(`""`),
	}
	_, p := krakenArrayToTick(arr)
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed for invalid price, got=%v", p)
	}
}

func assignKrakenJSON(t *testing.T, src any, dest any) {
	t.Helper()
	raw, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("assignKrakenJSON marshal: %v", err)
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		t.Fatalf("assignKrakenJSON unmarshal: %v", err)
	}
}

func removeIfExists(path string) error {
	if _, err := os.Stat(path); err == nil {
		return os.Remove(path)
	}
	return nil
}
