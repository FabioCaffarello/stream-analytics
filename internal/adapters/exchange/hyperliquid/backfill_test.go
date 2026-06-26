package hyperliquid

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	marketdomain "github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// --- helpers ----------------------------------------------------------------

// toInt64 extracts an int64 from a map value that may be int64 or float64
// (the latter occurs after JSON round-trip through map[string]any).
func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	default:
		return 0, false
	}
}

// mockHTTPPost returns a backfillHTTPPost replacement that feeds entries into
// the caller-provided *dest.  bodyCapture, if non-nil, receives every request
// body for later inspection.
func mockHTTPPost(
	entries []recentTradeEntry,
	bodyCapture *[]map[string]any,
) func(ctx context.Context, url string, body any, dest any) *problem.Problem {
	return func(_ context.Context, _ string, body any, dest any) *problem.Problem {
		if bodyCapture != nil {
			if m, ok := body.(map[string]any); ok {
				cp := make(map[string]any, len(m))
				for k, v := range m {
					cp[k] = v
				}
				*bodyCapture = append(*bodyCapture, cp)
			}
		}
		raw, err := json.Marshal(entries)
		if err != nil {
			return problem.Wrap(err, problem.Internal, "mock marshal failed")
		}
		if err := json.Unmarshal(raw, dest); err != nil {
			return problem.Wrap(err, problem.Internal, "mock unmarshal into dest failed")
		}
		return nil
	}
}

// swapHTTPPost replaces the package-level backfillHTTPPost and registers
// cleanup to restore the original.
func swapHTTPPost(t *testing.T, fn func(ctx context.Context, url string, body any, dest any) *problem.Problem) {
	t.Helper()
	orig := backfillHTTPPost
	t.Cleanup(func() { backfillHTTPPost = orig })
	backfillHTTPPost = fn
}

// singleDayConfig returns a BackfillConfig for one UTC day with t.TempDir().
func singleDayConfig(t *testing.T, symbol string, day time.Time) BackfillConfig {
	t.Helper()
	return BackfillConfig{
		Symbol:    symbol,
		From:      day,
		To:        day,
		OutputDir: t.TempDir(),
	}
}

// --- 1. DownloadTrades_EmptySymbol -----------------------------------------

func TestDownloadTrades_EmptySymbol(t *testing.T) {
	swapHTTPPost(t, mockHTTPPost(nil, nil))

	cfg := BackfillConfig{
		Symbol:    "",
		From:      time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:        time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		OutputDir: t.TempDir(),
	}
	_, p := DownloadTrades(context.Background(), cfg)
	if p == nil {
		t.Fatal("expected problem for empty symbol, got nil")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed, got %s", p.Code)
	}
}

// --- 2. DownloadTrades_ToBeforeFrom ----------------------------------------

func TestDownloadTrades_ToBeforeFrom(t *testing.T) {
	swapHTTPPost(t, mockHTTPPost(nil, nil))

	cfg := BackfillConfig{
		Symbol:    "BTCUSDT",
		From:      time.Date(2025, 3, 5, 0, 0, 0, 0, time.UTC),
		To:        time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
		OutputDir: t.TempDir(),
	}
	res, p := DownloadTrades(context.Background(), cfg)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if res.DatesDownloaded != 0 {
		t.Fatalf("expected 0 dates downloaded, got %d", res.DatesDownloaded)
	}
	if res.TradesParsed != 0 {
		t.Fatalf("expected 0 trades parsed, got %d", res.TradesParsed)
	}
}

// --- 3. DownloadTrades_SingleDay -------------------------------------------

func TestDownloadTrades_SingleDay(t *testing.T) {
	day := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	dayMS := day.UnixMilli()

	entries := []recentTradeEntry{
		{Coin: "BTC", Side: "B", Px: "65000.5", Sz: "1.2", Hash: "0xabc123", Time: dayMS + 1000, Tid: 1},
		{Coin: "BTC", Side: "A", Px: "65001.0", Sz: "0.5", Hash: "0xdef456", Time: dayMS + 2000, Tid: 2},
	}

	// Return entries only on first call; empty on pagination follow-up.
	var callCount atomic.Int32
	swapHTTPPost(t, func(ctx context.Context, url string, body any, dest any) *problem.Problem {
		n := callCount.Add(1)
		if n == 1 {
			return mockHTTPPost(entries, nil)(ctx, url, body, dest)
		}
		return mockHTTPPost(nil, nil)(ctx, url, body, dest)
	})

	cfg := singleDayConfig(t, "BTCUSDT", day)
	res, p := DownloadTrades(context.Background(), cfg)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if res.DatesDownloaded != 1 {
		t.Fatalf("expected 1 date downloaded, got %d", res.DatesDownloaded)
	}
	if res.TradesParsed != 2 {
		t.Fatalf("expected 2 trades parsed, got %d", res.TradesParsed)
	}

	// Verify output file exists and is non-empty.
	info, err := os.Stat(res.OutputPath)
	if err != nil {
		t.Fatalf("output file stat failed: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output file is empty")
	}
}

// --- 4. DownloadTrades_CachesAndSkips --------------------------------------

func TestDownloadTrades_CachesAndSkips(t *testing.T) {
	day := time.Date(2025, 6, 20, 0, 0, 0, 0, time.UTC)
	dayMS := day.UnixMilli()

	entries := []recentTradeEntry{
		{Coin: "ETH", Side: "B", Px: "3500.1", Sz: "10", Hash: "0x111", Time: dayMS + 500, Tid: 10},
	}

	var callCount atomic.Int32
	swapHTTPPost(t, func(ctx context.Context, url string, body any, dest any) *problem.Problem {
		callCount.Add(1)
		return mockHTTPPost(entries, nil)(ctx, url, body, dest)
	})

	outDir := t.TempDir()
	cfg := BackfillConfig{
		Symbol:    "ETHUSDT",
		From:      day,
		To:        day,
		OutputDir: outDir,
	}

	// First call: should download.
	res1, p := DownloadTrades(context.Background(), cfg)
	if p != nil {
		t.Fatalf("first call failed: %v", p)
	}
	if res1.DatesDownloaded != 1 {
		t.Fatalf("first call: expected 1 date downloaded, got %d", res1.DatesDownloaded)
	}
	firstHTTPCalls := callCount.Load()
	if firstHTTPCalls == 0 {
		t.Fatal("first call: expected at least one HTTP call")
	}

	// Second call: should read from cache, zero new HTTP calls.
	res2, p := DownloadTrades(context.Background(), cfg)
	if p != nil {
		t.Fatalf("second call failed: %v", p)
	}
	if res2.DatesSkipped != 1 {
		t.Fatalf("second call: expected 1 date skipped, got %d", res2.DatesSkipped)
	}
	if res2.DatesDownloaded != 0 {
		t.Fatalf("second call: expected 0 dates downloaded, got %d", res2.DatesDownloaded)
	}
	if res2.TradesParsed != 1 {
		t.Fatalf("second call: expected 1 trade parsed from cache, got %d", res2.TradesParsed)
	}
	secondHTTPCalls := callCount.Load()
	if secondHTTPCalls != firstHTTPCalls {
		t.Fatalf("second call made %d new HTTP calls (total %d, previously %d)",
			secondHTTPCalls-firstHTTPCalls, secondHTTPCalls, firstHTTPCalls)
	}
}

// --- 5. DownloadTrades_PaginationCursor ------------------------------------

func TestDownloadTrades_PaginationCursor(t *testing.T) {
	day := time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)
	dayMS := day.UnixMilli()
	dayEndMS := day.AddDate(0, 0, 1).UnixMilli()

	// Page 1: 100 entries in the day window, oldest at dayMS+50000.
	page1 := make([]recentTradeEntry, 100)
	for i := range page1 {
		page1[i] = recentTradeEntry{
			Coin: "BTC",
			Side: "B",
			Px:   "60000.0",
			Sz:   "0.01",
			Hash: "0x" + time.Now().Format("150405") + string(rune('a'+i%26)),
			Time: dayMS + 50000 + int64(100-i)*100, // decreasing towards dayMS+50000
			Tid:  int64(1000 + i),
		}
	}

	// Page 2: 50 entries (< 100 so pagination stops), earlier timestamps.
	page2 := make([]recentTradeEntry, 50)
	for i := range page2 {
		page2[i] = recentTradeEntry{
			Coin: "BTC",
			Side: "A",
			Px:   "59999.0",
			Sz:   "0.02",
			Hash: "0x" + string(rune('A'+i%26)),
			Time: dayMS + 1000 + int64(50-i)*10,
			Tid:  int64(2000 + i),
		}
	}

	var captures []map[string]any
	var callNum atomic.Int32
	swapHTTPPost(t, func(ctx context.Context, url string, body any, dest any) *problem.Problem {
		n := callNum.Add(1)
		if captures == nil {
			captures = make([]map[string]any, 0)
		}
		switch n {
		case 1:
			return mockHTTPPost(page1, &captures)(ctx, url, body, dest)
		case 2:
			return mockHTTPPost(page2, &captures)(ctx, url, body, dest)
		default:
			return mockHTTPPost(nil, &captures)(ctx, url, body, dest)
		}
	})

	cfg := singleDayConfig(t, "BTCUSDT", day)
	res, p := DownloadTrades(context.Background(), cfg)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}

	// Should have fetched at least 2 pages.
	totalCalls := callNum.Load()
	if totalCalls < 2 {
		t.Fatalf("expected at least 2 HTTP calls for pagination, got %d", totalCalls)
	}

	// First request should use dayEnd as startTime cursor.
	if len(captures) < 2 {
		t.Fatalf("expected at least 2 captured bodies, got %d", len(captures))
	}

	firstStart, ok := toInt64(captures[0]["startTime"])
	if !ok {
		t.Fatalf("first request body missing or non-numeric startTime; body=%+v", captures[0])
	}
	if firstStart != dayEndMS {
		t.Fatalf("first startTime=%d want %d (dayEnd)", firstStart, dayEndMS)
	}

	// Second request startTime should be less than first (cursor advanced backwards).
	secondStart, ok := toInt64(captures[1]["startTime"])
	if !ok {
		t.Fatalf("second request body missing or non-numeric startTime; body=%+v", captures[1])
	}
	if secondStart >= firstStart {
		t.Fatalf("cursor did not advance: second startTime=%d >= first=%d",
			secondStart, firstStart)
	}

	if res.TradesParsed == 0 {
		t.Fatal("expected trades parsed > 0")
	}
}

// --- 6. DownloadTrades_HTTPError -------------------------------------------

func TestDownloadTrades_HTTPError(t *testing.T) {
	day := time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC)

	swapHTTPPost(t, func(_ context.Context, _ string, _ any, _ any) *problem.Problem {
		return problem.New(problem.Unavailable, "simulated HTTP failure")
	})

	cfg := singleDayConfig(t, "BTCUSDT", day)
	_, p := DownloadTrades(context.Background(), cfg)
	if p == nil {
		t.Fatal("expected problem from HTTP error, got nil")
	}
	if p.Code != problem.Unavailable {
		t.Fatalf("expected Unavailable, got %s: %s", p.Code, p.Message)
	}
}

// --- 7. DownloadTrades_ContextCanceled -------------------------------------

func TestDownloadTrades_ContextCanceled(t *testing.T) {
	day := time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)

	// No mock needed; the context check happens before HTTP call.
	swapHTTPPost(t, mockHTTPPost(nil, nil))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	cfg := singleDayConfig(t, "BTCUSDT", day)
	_, p := DownloadTrades(ctx, cfg)
	if p == nil {
		t.Fatal("expected problem from canceled context, got nil")
	}
	if p.Code != problem.Unavailable {
		t.Fatalf("expected Unavailable, got %s: %s", p.Code, p.Message)
	}
}

// --- 8. hlEntryToTick_InvalidPrice -----------------------------------------

func TestHlEntryToTick_InvalidPrice(t *testing.T) {
	entry := recentTradeEntry{
		Coin: "BTC", Side: "B", Px: "not-a-number", Sz: "1.0",
		Hash: "0xabc", Time: 1700000000000, Tid: 1,
	}
	_, p := hlEntryToTick(entry)
	if p == nil {
		t.Fatal("expected problem for invalid price, got nil")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed, got %s", p.Code)
	}
}

// --- 9. hlEntryToTick_InvalidSize ------------------------------------------

func TestHlEntryToTick_InvalidSize(t *testing.T) {
	entry := recentTradeEntry{
		Coin: "BTC", Side: "B", Px: "60000.0", Sz: "bad",
		Hash: "0xabc", Time: 1700000000000, Tid: 1,
	}
	_, p := hlEntryToTick(entry)
	if p == nil {
		t.Fatal("expected problem for invalid size, got nil")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed, got %s", p.Code)
	}
}

// --- 10. hlEntryToTick_ZeroHash_UsesTid ------------------------------------

func TestHlEntryToTick_ZeroHash_UsesTid(t *testing.T) {
	tests := []struct {
		name   string
		hash   string
		tid    int64
		wantID string
	}{
		{name: "0x-prefixed zero hash", hash: "0x0000000000000000000000000000000000000000", tid: 42, wantID: "42"},
		{name: "bare zero hash", hash: "000000", tid: 99, wantID: "99"},
		{name: "empty hash", hash: "", tid: 7, wantID: "7"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry := recentTradeEntry{
				Coin: "BTC", Side: "B", Px: "50000.0", Sz: "0.1",
				Hash: tc.hash, Time: 1700000000000, Tid: tc.tid,
			}
			tick, p := hlEntryToTick(entry)
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
			if tick.TradeID != tc.wantID {
				t.Fatalf("TradeID=%q want %q", tick.TradeID, tc.wantID)
			}
		})
	}
}

// --- 11. coinFromSymbol table test -----------------------------------------

func TestCoinFromSymbol(t *testing.T) {
	tests := []struct {
		symbol string
		want   string
	}{
		{"BTCUSDT", "BTC"},
		{"ETHUSDC", "ETH"},
		{"BTCPERP", "BTC"},
		{"BTCUSD", "BTC"},
		{"SOL", "SOL"},
		{"solusdt", "SOL"},     // lowercase input
		{"  ETHUSDT  ", "ETH"}, // whitespace trimmed
	}
	for _, tc := range tests {
		t.Run(tc.symbol, func(t *testing.T) {
			got := coinFromSymbol(tc.symbol)
			if got != tc.want {
				t.Fatalf("coinFromSymbol(%q)=%q want %q", tc.symbol, got, tc.want)
			}
		})
	}
}

// --- 12. hlSortByTimestamp -------------------------------------------------

func TestHlSortByTimestamp(t *testing.T) {
	trades := []marketdomain.TradeTickV1{
		{Timestamp: 300, TradeID: "c"},
		{Timestamp: 100, TradeID: "a"},
		{Timestamp: 200, TradeID: "b"},
		{Timestamp: 100, TradeID: "d"}, // duplicate timestamp
	}
	sorted := hlSortByTimestamp(trades)
	if len(sorted) != 4 {
		t.Fatalf("expected 4 trades, got %d", len(sorted))
	}
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Timestamp < sorted[i-1].Timestamp {
			t.Fatalf("not sorted at index %d: %d < %d", i, sorted[i].Timestamp, sorted[i-1].Timestamp)
		}
	}
	// First two should have timestamp 100.
	if sorted[0].Timestamp != 100 || sorted[1].Timestamp != 100 {
		t.Fatalf("expected first two timestamps=100, got %d and %d", sorted[0].Timestamp, sorted[1].Timestamp)
	}
}

// --- additional edge cases --------------------------------------------------

func TestHlEntryToTick_ValidEntry(t *testing.T) {
	entry := recentTradeEntry{
		Coin: "BTC", Side: "B", Px: "65432.10", Sz: "0.5",
		Hash: "0xabcdef1234567890", Time: 1700000000000, Tid: 55,
	}
	tick, p := hlEntryToTick(entry)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if tick.Price != 65432.10 {
		t.Fatalf("Price=%f want 65432.10", tick.Price)
	}
	if tick.Size != 0.5 {
		t.Fatalf("Size=%f want 0.5", tick.Size)
	}
	if tick.Side != "buy" {
		t.Fatalf("Side=%q want buy", tick.Side)
	}
	if tick.TradeID != "0xabcdef1234567890" {
		t.Fatalf("TradeID=%q want 0xabcdef1234567890", tick.TradeID)
	}
	if tick.Timestamp != 1700000000000 {
		t.Fatalf("Timestamp=%d want 1700000000000", tick.Timestamp)
	}
}

func TestHlEntryToTick_SellSide(t *testing.T) {
	entry := recentTradeEntry{
		Coin: "ETH", Side: "A", Px: "3500.0", Sz: "2.0",
		Hash: "0xfeed", Time: 1700000001000, Tid: 77,
	}
	tick, p := hlEntryToTick(entry)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if tick.Side != "sell" {
		t.Fatalf("Side=%q want sell", tick.Side)
	}
}

func TestHlEntryToTick_ZeroTimestamp(t *testing.T) {
	entry := recentTradeEntry{
		Coin: "BTC", Side: "B", Px: "60000.0", Sz: "0.1",
		Hash: "0xabc", Time: 0, Tid: 1,
	}
	_, p := hlEntryToTick(entry)
	if p == nil {
		t.Fatal("expected problem for zero timestamp, got nil")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed, got %s", p.Code)
	}
}

func TestHlEntryToTick_ZeroTidAndZeroHash(t *testing.T) {
	entry := recentTradeEntry{
		Coin: "BTC", Side: "B", Px: "60000.0", Sz: "0.1",
		Hash: "0x00000000", Time: 1700000000000, Tid: 0,
	}
	_, p := hlEntryToTick(entry)
	if p == nil {
		t.Fatal("expected problem when both hash and tid are zero/empty")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed, got %s", p.Code)
	}
}

func TestNormalizeBackfillMarketType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "USD_M_FUTURES"},
		{"  ", "USD_M_FUTURES"},
		{"spot", "SPOT"},
		{"USD_M_FUTURES", "USD_M_FUTURES"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := normalizeBackfillMarketType(tc.input)
			if got != tc.want {
				t.Fatalf("normalizeBackfillMarketType(%q)=%q want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestHlSortByTimestamp_Empty(t *testing.T) {
	result := hlSortByTimestamp(nil)
	if result != nil {
		t.Fatalf("expected nil for nil input, got %v", result)
	}
}

func TestHlSortByTimestamp_SingleElement(t *testing.T) {
	trades := []marketdomain.TradeTickV1{{Timestamp: 42, TradeID: "only"}}
	sorted := hlSortByTimestamp(trades)
	if len(sorted) != 1 || sorted[0].Timestamp != 42 {
		t.Fatalf("unexpected result: %+v", sorted)
	}
}

func TestDefaultHLFixturePath(t *testing.T) {
	from := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC)
	got := defaultHLFixturePath("/tmp/backfill", "BTCUSDT", from, to)
	want := filepath.Join("/tmp/backfill", "BTCUSDT-2025-01-15-2025-01-20-hyperliquid.jsonl")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestHlUtcStartOfDay(t *testing.T) {
	input := time.Date(2025, 6, 15, 14, 30, 45, 123, time.FixedZone("EST", -5*3600))
	got := hlUtcStartOfDay(input)
	// 14:30 EST = 19:30 UTC → start of day = 2025-06-15 00:00:00 UTC
	want := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("hlUtcStartOfDay(%v) = %v want %v", input, got, want)
	}
}

func TestHlWriteAndReadDayCache(t *testing.T) {
	trades := []marketdomain.TradeTickV1{
		{Price: 100.5, Size: 1.0, Side: "buy", TradeID: "t1", Timestamp: 1000},
		{Price: 200.5, Size: 2.0, Side: "sell", TradeID: "t2", Timestamp: 2000},
	}
	path := filepath.Join(t.TempDir(), "cache.jsonl")

	if p := hlWriteDayCache(path, trades); p != nil {
		t.Fatalf("write failed: %v", p)
	}

	got, p := hlReadCachedDayTrades(path)
	if p != nil {
		t.Fatalf("read failed: %v", p)
	}
	if len(got) != len(trades) {
		t.Fatalf("expected %d trades, got %d", len(trades), len(got))
	}
	for i, want := range trades {
		if got[i].Price != want.Price || got[i].Size != want.Size ||
			got[i].Side != want.Side || got[i].TradeID != want.TradeID ||
			got[i].Timestamp != want.Timestamp {
			t.Fatalf("trade[%d] mismatch: got=%+v want=%+v", i, got[i], want)
		}
	}
}

func TestHlFileExists(t *testing.T) {
	dir := t.TempDir()

	// Non-existent file.
	exists, p := hlFileExists(filepath.Join(dir, "nope.txt"))
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if exists {
		t.Fatal("expected false for non-existent file")
	}

	// Create a file.
	path := filepath.Join(dir, "exists.txt")
	if err := os.WriteFile(path, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	exists, p = hlFileExists(path)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if !exists {
		t.Fatal("expected true for existing file")
	}
}
