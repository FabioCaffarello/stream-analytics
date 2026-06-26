package krakenf

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

func swapKrakenFBackfillHTTPGet(t *testing.T, fn func(ctx context.Context, url string, dest any) *problem.Problem) {
	t.Helper()
	orig := backfillHTTPGet
	t.Cleanup(func() { backfillHTTPGet = orig })
	backfillHTTPGet = fn
}

func singleDayKrakenFConfig(t *testing.T) BackfillConfig {
	t.Helper()
	return BackfillConfig{
		Symbol:    "BTCUSDT",
		From:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		To:        time.Date(2024, 1, 15, 23, 59, 59, 0, time.UTC),
		OutputDir: t.TempDir(),
		BaseURL:   "https://fake.krakenf.test",
	}
}

func fakeKrakenFHistoryResponse(n int, day time.Time) krakenFHistoryResponse {
	history := make([]krakenFHistory, n)
	for i := 0; i < n; i++ {
		ts := day.Add(time.Duration(n-1-i) * time.Second) // descending order (newest first)
		side := "buy"
		if i%2 == 1 {
			side = "sell"
		}
		history[i] = krakenFHistory{
			UID:   strings.Repeat("a", 10) + string(rune('0'+i)),
			Price: 42000.0 + float64(i),
			Qty:   0.1 + float64(i)*0.01,
			Side:  side,
			Time:  ts.Format(time.RFC3339Nano),
			Type:  "fill",
		}
	}
	return krakenFHistoryResponse{
		Result:  "success",
		History: history,
	}
}

func assignKrakenFJSON(t *testing.T, src any, dest any) {
	t.Helper()
	raw, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("assignKrakenFJSON marshal: %v", err)
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		t.Fatalf("assignKrakenFJSON unmarshal: %v", err)
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
	swapKrakenFBackfillHTTPGet(t, func(_ context.Context, _ string, _ any) *problem.Problem {
		t.Fatal("httpGet should not be called when To < From")
		return nil
	})

	cfg := BackfillConfig{
		Symbol:    "BTCUSDT",
		From:      time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC),
		To:        time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		OutputDir: t.TempDir(),
		BaseURL:   "https://fake.krakenf.test",
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
	cfg := singleDayKrakenFConfig(t)
	day := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	callCount := 0
	swapKrakenFBackfillHTTPGet(t, func(_ context.Context, _ string, dest any) *problem.Problem {
		callCount++
		if callCount == 1 {
			resp := fakeKrakenFHistoryResponse(3, day)
			assignKrakenFJSON(t, resp, dest)
			return nil
		}
		// Second call: return empty history to terminate pagination.
		resp := krakenFHistoryResponse{
			Result:  "success",
			History: nil,
		}
		assignKrakenFJSON(t, resp, dest)
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
	cfg := singleDayKrakenFConfig(t)
	day := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	callCount := 0
	swapKrakenFBackfillHTTPGet(t, func(_ context.Context, _ string, dest any) *problem.Problem {
		callCount++
		resp := fakeKrakenFHistoryResponse(2, day)
		assignKrakenFJSON(t, resp, dest)
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
	cfg := singleDayKrakenFConfig(t)

	swapKrakenFBackfillHTTPGet(t, func(_ context.Context, _ string, _ any) *problem.Problem {
		return problem.New(problem.Unavailable, "simulated HTTP 503")
	})

	_, p := DownloadTrades(context.Background(), cfg)
	if p == nil || p.Code != problem.Unavailable {
		t.Fatalf("expected Unavailable for HTTP error, got=%v", p)
	}
}

func TestDownloadTrades_ContextCanceled(t *testing.T) {
	cfg := singleDayKrakenFConfig(t)
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

func TestKrakenFHistoryToTick_Valid(t *testing.T) {
	t.Parallel()
	ts := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	h := krakenFHistory{
		UID:   "abc123",
		Price: 42000.50,
		Qty:   0.1234,
		Side:  "sell",
		Time:  ts.Format(time.RFC3339Nano),
		Type:  "fill",
	}
	tick, p := krakenFHistoryToTick(h)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if tick.Price != 42000.50 {
		t.Fatalf("Price=%f want=42000.50", tick.Price)
	}
	if tick.Size != 0.1234 {
		t.Fatalf("Size=%f want=0.1234", tick.Size)
	}
	if tick.Side != "sell" {
		t.Fatalf("Side=%q want=sell", tick.Side)
	}
	if tick.TradeID != "abc123" {
		t.Fatalf("TradeID=%q want=abc123", tick.TradeID)
	}
}

func TestKrakenFHistoryToTick_InvalidPrice(t *testing.T) {
	t.Parallel()
	h := krakenFHistory{
		UID:   "test",
		Price: -1.0,
		Qty:   0.1,
		Side:  "buy",
		Time:  time.Now().UTC().Format(time.RFC3339Nano),
		Type:  "fill",
	}
	_, p := krakenFHistoryToTick(h)
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed for invalid price, got=%v", p)
	}
}

func removeIfExists(path string) error {
	if _, err := os.Stat(path); err == nil {
		return os.Remove(path)
	}
	return nil
}
