package coinbase

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

// --- helpers ----------------------------------------------------------------

// swapBackfillHTTPGet replaces the package-level backfillHTTPGet and restores
// it when the test finishes.
//
// IMPORTANT: tests that call this function MUST NOT use t.Parallel() because
// they mutate a shared package-level variable.
func swapBackfillHTTPGet(t *testing.T, fn func(ctx context.Context, url string, dest any) *problem.Problem) {
	t.Helper()
	orig := backfillHTTPGet
	t.Cleanup(func() { backfillHTTPGet = orig })
	backfillHTTPGet = fn
}

// singleDayConfig returns a BackfillConfig for a single UTC day (2024-01-15)
// pointed at t.TempDir().
func singleDayConfig(t *testing.T) BackfillConfig {
	t.Helper()
	return BackfillConfig{
		Symbol:    "BTC-USD",
		From:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		To:        time.Date(2024, 1, 15, 23, 59, 59, 0, time.UTC),
		OutputDir: t.TempDir(),
		BaseURL:   "https://fake.coinbase.test",
	}
}

// fakeRestTrades builds n restTrade records starting at tradeID=startID,
// within the given day, 1 second apart, alternating buy/sell.
// Trades are returned newest-first, matching Coinbase REST API ordering.
func fakeRestTrades(n int, startID int64, day time.Time) []restTrade {
	trades := make([]restTrade, n)
	for i := 0; i < n; i++ {
		ts := day.Add(time.Duration(n-1-i) * time.Second) // newest first
		side := "buy"
		if i%2 == 1 {
			side = "sell"
		}
		trades[i] = restTrade{
			TradeID: startID - int64(i),
			Price:   fmt.Sprintf("%.2f", 42000.0+float64(i)),
			Size:    fmt.Sprintf("%.4f", 0.1+float64(i)*0.01),
			Time:    ts.Format(time.RFC3339Nano),
			Side:    side,
		}
	}
	return trades
}

// assignJSON marshals src and unmarshals into dest (simulating httpGetJSON
// writing into the caller's pointer).
func assignJSON(t *testing.T, src any, dest any) {
	t.Helper()
	raw, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("assignJSON marshal: %v", err)
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		t.Fatalf("assignJSON unmarshal: %v", err)
	}
}

// countJSONLLines returns the number of non-empty lines in a JSONL file.
func countJSONLLines(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path) // #nosec G304 -- test helper, path from test fixtures
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	count := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) != "" {
			count++
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return count
}

// ============================================================================
// Tests for validation (no HTTP mock needed, safe for t.Parallel)
// ============================================================================

func TestDownloadTrades_EmptySymbol(t *testing.T) {
	t.Parallel()
	cfg := BackfillConfig{
		Symbol:    "",
		From:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		To:        time.Date(2024, 1, 15, 23, 59, 59, 0, time.UTC),
		OutputDir: t.TempDir(),
	}
	_, p := DownloadTrades(context.Background(), cfg)
	if p == nil {
		t.Fatal("expected *problem.Problem for empty symbol, got nil")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("expected code=%s got=%s", problem.ValidationFailed, p.Code)
	}
	if !strings.Contains(p.Message, "symbol") {
		t.Fatalf("expected problem message to mention 'symbol', got: %s", p.Message)
	}
}

func TestDownloadTrades_InvalidSymbol(t *testing.T) {
	t.Parallel()
	cfg := BackfillConfig{
		Symbol:    "   ",
		From:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		To:        time.Date(2024, 1, 15, 23, 59, 59, 0, time.UTC),
		OutputDir: t.TempDir(),
	}
	_, p := DownloadTrades(context.Background(), cfg)
	if p == nil {
		t.Fatal("expected *problem.Problem for whitespace-only symbol, got nil")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("expected code=%s got=%s", problem.ValidationFailed, p.Code)
	}
}

// ============================================================================
// Tests that mutate backfillHTTPGet (sequential, no t.Parallel)
// ============================================================================

func TestDownloadTrades_ToBeforeFrom(t *testing.T) {
	swapBackfillHTTPGet(t, func(_ context.Context, _ string, _ any) *problem.Problem {
		t.Fatal("httpGetJSON should not be called when To < From")
		return nil
	})

	cfg := BackfillConfig{
		Symbol:    "BTC-USD",
		From:      time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC),
		To:        time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		OutputDir: t.TempDir(),
		BaseURL:   "https://fake.coinbase.test",
	}
	result, p := DownloadTrades(context.Background(), cfg)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if result.DatesDownloaded != 0 {
		t.Fatalf("DatesDownloaded=%d want 0", result.DatesDownloaded)
	}
	if result.TradesParsed != 0 {
		t.Fatalf("TradesParsed=%d want 0", result.TradesParsed)
	}
}

func TestDownloadTrades_SingleDay(t *testing.T) {
	cfg := singleDayConfig(t)
	day := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	fakeTrades := fakeRestTrades(3, 1003, day)

	swapBackfillHTTPGet(t, func(_ context.Context, rawURL string, dest any) *problem.Problem {
		if strings.Contains(rawURL, "before=") {
			assignJSON(t, []restTrade{}, dest)
			return nil
		}
		assignJSON(t, fakeTrades, dest)
		return nil
	})

	result, p := DownloadTrades(context.Background(), cfg)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if result.DatesDownloaded != 1 {
		t.Fatalf("DatesDownloaded=%d want 1", result.DatesDownloaded)
	}
	if result.TradesParsed != 3 {
		t.Fatalf("TradesParsed=%d want 3", result.TradesParsed)
	}

	// Verify the output fixture file exists.
	if _, err := os.Stat(result.OutputPath); err != nil {
		t.Fatalf("output file missing: %v", err)
	}

	// Verify the per-day cache JSONL file was written.
	symbol := "BTCUSD" // CanonicalInstrument("BTC-USD")
	cachePath := filepath.Join(cfg.OutputDir, fmt.Sprintf("%s-%s-coinbase.jsonl", symbol, day.Format("2006-01-02")))
	lines := countJSONLLines(t, cachePath)
	if lines != 3 {
		t.Fatalf("cache JSONL lines=%d want 3", lines)
	}

	// Verify each cached trade can be decoded back to TradeTickV1.
	f, err := os.Open(cachePath) // #nosec G304 -- test helper, path from t.TempDir()
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	defer func() { _ = f.Close() }()
	dec := json.NewDecoder(f)
	var decoded int
	for dec.More() {
		var tick marketdomain.TradeTickV1
		if err := dec.Decode(&tick); err != nil {
			t.Fatalf("decode cached trade: %v", err)
		}
		if tick.Price == 0 {
			t.Fatal("decoded trade has zero price")
		}
		decoded++
	}
	if decoded != 3 {
		t.Fatalf("decoded=%d want 3", decoded)
	}
}

func TestDownloadTrades_CachesAndSkips(t *testing.T) {
	cfg := singleDayConfig(t)
	day := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	fakeTrades := fakeRestTrades(2, 1002, day)

	callCount := 0
	swapBackfillHTTPGet(t, func(_ context.Context, rawURL string, dest any) *problem.Problem {
		callCount++
		if strings.Contains(rawURL, "before=") {
			assignJSON(t, []restTrade{}, dest)
			return nil
		}
		assignJSON(t, fakeTrades, dest)
		return nil
	})

	// First call: downloads from HTTP.
	result1, p := DownloadTrades(context.Background(), cfg)
	if p != nil {
		t.Fatalf("first call: unexpected problem: %v", p)
	}
	if result1.DatesDownloaded != 1 {
		t.Fatalf("first call: DatesDownloaded=%d want 1", result1.DatesDownloaded)
	}
	firstCallCount := callCount

	// Second call: should read from cache, no new HTTP calls.
	// Remove the replay fixture file so the writer can start clean, but
	// keep the per-day cache file.
	_ = os.Remove(result1.OutputPath)

	result2, p := DownloadTrades(context.Background(), cfg)
	if p != nil {
		t.Fatalf("second call: unexpected problem: %v", p)
	}
	if result2.DatesSkipped != 1 {
		t.Fatalf("second call: DatesSkipped=%d want 1", result2.DatesSkipped)
	}
	if result2.DatesDownloaded != 0 {
		t.Fatalf("second call: DatesDownloaded=%d want 0", result2.DatesDownloaded)
	}
	if result2.TradesParsed != 2 {
		t.Fatalf("second call: TradesParsed=%d want 2", result2.TradesParsed)
	}

	// Verify no additional HTTP calls were made.
	if callCount != firstCallCount {
		t.Fatalf("expected no new HTTP calls; first=%d total=%d", firstCallCount, callCount)
	}
}

func TestDownloadTrades_PaginationCursor(t *testing.T) {
	cfg := singleDayConfig(t)
	day := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	// Page 1: maxPerPage trades with cursor. Timestamps within the day.
	page1 := make([]restTrade, maxPerPage)
	for i := 0; i < maxPerPage; i++ {
		ts := day.Add(23*time.Hour - time.Duration(i)*time.Second) // newest first
		side := "buy"
		if i%2 == 1 {
			side = "sell"
		}
		page1[i] = restTrade{
			TradeID: int64(5000 - i),
			Price:   fmt.Sprintf("%.2f", 42000.0+float64(i)),
			Size:    "0.1000",
			Time:    ts.Format(time.RFC3339Nano),
			Side:    side,
		}
	}

	// Page 2: fewer than maxPerPage trades (signals end of data).
	page2 := make([]restTrade, 3)
	for i := 0; i < 3; i++ {
		ts := day.Add(time.Duration(3-i) * time.Second) // earlier in the day
		side := "sell"
		if i%2 == 0 {
			side = "buy"
		}
		page2[i] = restTrade{
			TradeID: int64(4900 - maxPerPage - i),
			Price:   fmt.Sprintf("%.2f", 41000.0+float64(i)),
			Size:    "0.0500",
			Time:    ts.Format(time.RFC3339Nano),
			Side:    side,
		}
	}

	pageCallCount := 0
	returnedPage2 := false
	swapBackfillHTTPGet(t, func(_ context.Context, rawURL string, dest any) *problem.Problem {
		pageCallCount++
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return problem.Wrap(err, problem.Internal, "test: parse URL")
		}
		beforeParam := parsed.Query().Get("before")
		if beforeParam == "" {
			// First page: full batch.
			assignJSON(t, page1, dest)
		} else if !returnedPage2 {
			// Second page: partial batch (signals near-end).
			returnedPage2 = true
			assignJSON(t, page2, dest)
		} else {
			// Third+ calls: empty, terminates pagination.
			assignJSON(t, []restTrade{}, dest)
		}
		return nil
	})

	result, p := DownloadTrades(context.Background(), cfg)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}

	expectedTrades := int64(maxPerPage + 3)
	if result.TradesParsed != expectedTrades {
		t.Fatalf("TradesParsed=%d want %d", result.TradesParsed, expectedTrades)
	}
	if pageCallCount < 2 {
		t.Fatalf("expected at least 2 HTTP calls for pagination, got %d", pageCallCount)
	}
}

func TestDownloadTrades_HTTPError(t *testing.T) {
	cfg := singleDayConfig(t)

	swapBackfillHTTPGet(t, func(_ context.Context, _ string, _ any) *problem.Problem {
		return problem.New(problem.Unavailable, "simulated HTTP 503")
	})

	_, p := DownloadTrades(context.Background(), cfg)
	if p == nil {
		t.Fatal("expected *problem.Problem for HTTP error, got nil")
	}
	if p.Code != problem.Unavailable {
		t.Fatalf("expected code=%s got=%s", problem.Unavailable, p.Code)
	}
}

func TestDownloadTrades_ContextCanceled(t *testing.T) {
	cfg := singleDayConfig(t)

	// No mock swap needed: context is already canceled before the function runs,
	// so it returns before hitting the HTTP call.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, p := DownloadTrades(ctx, cfg)
	if p == nil {
		t.Fatal("expected *problem.Problem for canceled context, got nil")
	}
	if p.Code != problem.Unavailable {
		t.Fatalf("expected code=%s got=%s", problem.Unavailable, p.Code)
	}
	if !strings.Contains(p.Message, "canceled") {
		t.Fatalf("expected message to contain 'canceled', got: %s", p.Message)
	}
}

func TestDownloadTrades_MultiDayRange(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := BackfillConfig{
		Symbol:    "ETH-USD",
		From:      time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
		To:        time.Date(2024, 2, 3, 0, 0, 0, 0, time.UTC),
		OutputDir: tmpDir,
		BaseURL:   "https://fake.coinbase.test",
	}

	// The mock needs to return trades whose timestamps fall within the day
	// being fetched. fetchDayTrades calls with from=dayStart, to=dayStart+1day
	// and filters: tradeTime >= from && tradeTime < to.
	// Since we cannot easily detect which day is being fetched from the URL
	// alone (Coinbase REST does not accept time params), we return trades at
	// a fixed midday time. fetchDayTrades walks backward from "now" and stops
	// when it hits trades before `from`. For our mock we place trades at
	// 12:00 of every possible day; they will be accepted for whichever day
	// the function is iterating.
	swapBackfillHTTPGet(t, func(_ context.Context, rawURL string, dest any) *problem.Problem {
		if strings.Contains(rawURL, "before=") {
			assignJSON(t, []restTrade{}, dest)
			return nil
		}
		// Return 2 trades. We generate them with timestamps that will match
		// ANY single day window because fetchDayTrades stops paginating when
		// it encounters trades before `from`. We use a timestamp far in the
		// future (relative to the query range) so they always land "inside"
		// the day window initially, and the function terminates because the
		// second call returns empty (via the before= branch above).
		//
		// Actually, fetchDayTrades does not send time filters to the API.
		// It fetches pages of newest-first trades and filters locally. So
		// the mock just needs to return trades that have timestamps within
		// the current day window. Since we do not know the day here, we
		// use a trick: return trades at day 2024-02-02 12:00. For day 1 and
		// day 3, those trades will be outside window and thus 0 added, but
		// addedInPage==0 && len(batch)<maxPerPage causes break. That means
		// only 1 day will have trades.
		//
		// A cleaner approach: return trades with timestamps matching ALL days.
		// We return 6 trades spanning all 3 days.
		trades := []restTrade{
			// Newest first, one per day.
			{TradeID: 2006, Price: "3000.00", Size: "1.0", Side: "buy",
				Time: time.Date(2024, 2, 3, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)},
			{TradeID: 2005, Price: "3001.00", Size: "1.0", Side: "sell",
				Time: time.Date(2024, 2, 3, 11, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)},
			{TradeID: 2004, Price: "2900.00", Size: "1.0", Side: "buy",
				Time: time.Date(2024, 2, 2, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)},
			{TradeID: 2003, Price: "2901.00", Size: "1.0", Side: "sell",
				Time: time.Date(2024, 2, 2, 11, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)},
			{TradeID: 2002, Price: "2800.00", Size: "1.0", Side: "buy",
				Time: time.Date(2024, 2, 1, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)},
			{TradeID: 2001, Price: "2801.00", Size: "1.0", Side: "sell",
				Time: time.Date(2024, 2, 1, 11, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)},
		}
		assignJSON(t, trades, dest)
		return nil
	})

	result, p := DownloadTrades(context.Background(), cfg)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}

	// From Feb 1 to Feb 3 inclusive = 3 days.
	if result.DatesDownloaded != 3 {
		t.Fatalf("DatesDownloaded=%d want 3", result.DatesDownloaded)
	}
	// 2 trades x 3 days = 6 trades total.
	if result.TradesParsed != 6 {
		t.Fatalf("TradesParsed=%d want 6", result.TradesParsed)
	}
}

// ============================================================================
// restTradeToTick unit tests (parallel-safe, no shared state)
// ============================================================================

func TestRestTradeToTick_InvalidPrice(t *testing.T) {
	t.Parallel()
	rt := restTrade{
		TradeID: 1,
		Price:   "not-a-number",
		Size:    "1.0",
		Time:    time.Now().UTC().Format(time.RFC3339Nano),
		Side:    "buy",
	}
	_, p := restTradeToTick(rt)
	if p == nil {
		t.Fatal("expected *problem.Problem for invalid price, got nil")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("expected code=%s got=%s", problem.ValidationFailed, p.Code)
	}
	if !strings.Contains(p.Message, "price") {
		t.Fatalf("expected message to mention 'price', got: %s", p.Message)
	}
}

func TestRestTradeToTick_InvalidSize(t *testing.T) {
	t.Parallel()
	rt := restTrade{
		TradeID: 2,
		Price:   "42000.50",
		Size:    "XXX",
		Time:    time.Now().UTC().Format(time.RFC3339Nano),
		Side:    "sell",
	}
	_, p := restTradeToTick(rt)
	if p == nil {
		t.Fatal("expected *problem.Problem for invalid size, got nil")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("expected code=%s got=%s", problem.ValidationFailed, p.Code)
	}
	if !strings.Contains(p.Message, "size") {
		t.Fatalf("expected message to mention 'size', got: %s", p.Message)
	}
}

func TestRestTradeToTick_ValidTrade(t *testing.T) {
	t.Parallel()
	now := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	rt := restTrade{
		TradeID: 99,
		Price:   "42000.50",
		Size:    "0.1234",
		Time:    now.Format(time.RFC3339Nano),
		Side:    "buy",
	}
	tick, p := restTradeToTick(rt)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if tick.Price != 42000.50 {
		t.Fatalf("Price=%f want 42000.50", tick.Price)
	}
	if tick.Size != 0.1234 {
		t.Fatalf("Size=%f want 0.1234", tick.Size)
	}
	if tick.Side != "buy" {
		t.Fatalf("Side=%q want 'buy'", tick.Side)
	}
	if tick.TradeID != "99" {
		t.Fatalf("TradeID=%q want '99'", tick.TradeID)
	}
	if tick.Timestamp != now.UnixMilli() {
		t.Fatalf("Timestamp=%d want %d", tick.Timestamp, now.UnixMilli())
	}
}

func TestRestTradeToTick_InvalidSide(t *testing.T) {
	t.Parallel()
	rt := restTrade{
		TradeID: 3,
		Price:   "100.0",
		Size:    "1.0",
		Time:    time.Now().UTC().Format(time.RFC3339Nano),
		Side:    "unknown_side",
	}
	_, p := restTradeToTick(rt)
	if p == nil {
		t.Fatal("expected *problem.Problem for invalid side, got nil")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("expected code=%s got=%s", problem.ValidationFailed, p.Code)
	}
}

func TestRestTradeToTick_InvalidTimestamp(t *testing.T) {
	t.Parallel()
	rt := restTrade{
		TradeID: 4,
		Price:   "100.0",
		Size:    "1.0",
		Time:    "not-a-timestamp",
		Side:    "buy",
	}
	_, p := restTradeToTick(rt)
	if p == nil {
		t.Fatal("expected *problem.Problem for invalid timestamp, got nil")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("expected code=%s got=%s", problem.ValidationFailed, p.Code)
	}
}

// ============================================================================
// sortByTimestamp tests (parallel-safe, pure function)
// ============================================================================

func TestSortByTimestamp(t *testing.T) {
	t.Parallel()
	trades := []marketdomain.TradeTickV1{
		{Timestamp: 300, TradeID: "3"},
		{Timestamp: 100, TradeID: "1"},
		{Timestamp: 200, TradeID: "2"},
		{Timestamp: 50, TradeID: "0"},
		{Timestamp: 500, TradeID: "5"},
		{Timestamp: 400, TradeID: "4"},
	}
	sorted := sortByTimestamp(trades)
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Timestamp < sorted[i-1].Timestamp {
			t.Fatalf("not sorted at index %d: %d < %d", i, sorted[i].Timestamp, sorted[i-1].Timestamp)
		}
	}
	expectedOrder := []string{"0", "1", "2", "3", "4", "5"}
	for i, want := range expectedOrder {
		if sorted[i].TradeID != want {
			t.Fatalf("sorted[%d].TradeID=%q want %q", i, sorted[i].TradeID, want)
		}
	}
}

func TestSortByTimestamp_AlreadySorted(t *testing.T) {
	t.Parallel()
	trades := []marketdomain.TradeTickV1{
		{Timestamp: 10},
		{Timestamp: 20},
		{Timestamp: 30},
	}
	sorted := sortByTimestamp(trades)
	if sorted[0].Timestamp != 10 || sorted[1].Timestamp != 20 || sorted[2].Timestamp != 30 {
		t.Fatal("already-sorted input was reordered")
	}
}

func TestSortByTimestamp_Empty(t *testing.T) {
	t.Parallel()
	sorted := sortByTimestamp(nil)
	if len(sorted) != 0 {
		t.Fatalf("expected empty slice, got len=%d", len(sorted))
	}
}

func TestSortByTimestamp_SingleElement(t *testing.T) {
	t.Parallel()
	trades := []marketdomain.TradeTickV1{{Timestamp: 42, TradeID: "only"}}
	sorted := sortByTimestamp(trades)
	if len(sorted) != 1 || sorted[0].TradeID != "only" {
		t.Fatal("single element sort failed")
	}
}

// ============================================================================
// Helper function unit tests (utcStartOfDay, defaultCBFixturePath)
// ============================================================================

func TestUtcStartOfDay(t *testing.T) {
	t.Parallel()
	input := time.Date(2024, 3, 15, 14, 30, 45, 999, time.UTC)
	got := utcStartOfDay(input)
	want := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("utcStartOfDay=%v want %v", got, want)
	}
}

func TestUtcStartOfDay_NonUTCInput(t *testing.T) {
	t.Parallel()
	loc := time.FixedZone("EST", -5*3600)
	// 2024-03-15 21:00 EST = 2024-03-16 02:00 UTC
	input := time.Date(2024, 3, 15, 21, 0, 0, 0, loc)
	got := utcStartOfDay(input)
	want := time.Date(2024, 3, 16, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("utcStartOfDay(non-UTC)=%v want %v", got, want)
	}
}

func TestDefaultCBFixturePath(t *testing.T) {
	t.Parallel()
	from := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	got := defaultCBFixturePath("/tmp/out", "BTCUSD", from, to)
	want := filepath.Join("/tmp/out", "BTCUSD-2024-01-10-2024-01-15-coinbase.jsonl")
	if got != want {
		t.Fatalf("defaultCBFixturePath=%q want %q", got, want)
	}
}

// ============================================================================
// writeDayCache / readCachedDayTrades round-trip
// ============================================================================

func TestWriteAndReadDayCache_RoundTrip(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "test-cache.jsonl")

	trades := []marketdomain.TradeTickV1{
		{Price: 42000.50, Size: 0.1, Side: "buy", TradeID: "1001", Timestamp: 1700000001000},
		{Price: 42001.75, Size: 0.2, Side: "sell", TradeID: "1002", Timestamp: 1700000002000},
		{Price: 42002.00, Size: 0.3, Side: "buy", TradeID: "1003", Timestamp: 1700000003000},
	}

	if p := writeDayCache(cachePath, trades); p != nil {
		t.Fatalf("writeDayCache: %v", p)
	}

	got, p := readCachedDayTrades(cachePath)
	if p != nil {
		t.Fatalf("readCachedDayTrades: %v", p)
	}
	if len(got) != len(trades) {
		t.Fatalf("len=%d want %d", len(got), len(trades))
	}
	for i, want := range trades {
		if got[i].Price != want.Price || got[i].Size != want.Size ||
			got[i].Side != want.Side || got[i].TradeID != want.TradeID ||
			got[i].Timestamp != want.Timestamp {
			t.Fatalf("trade[%d] mismatch: got=%+v want=%+v", i, got[i], want)
		}
	}
}

func TestReadCachedDayTrades_EmptyFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "empty.jsonl")
	if err := os.WriteFile(cachePath, []byte{}, 0o600); err != nil {
		t.Fatalf("create empty file: %v", err)
	}
	got, p := readCachedDayTrades(cachePath)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 trades from empty file, got %d", len(got))
	}
}

// ============================================================================
// fileExistsCB
// ============================================================================

func TestFileExistsCB_Exists(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "present.txt")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	exists, p := fileExistsCB(path)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if !exists {
		t.Fatal("expected file to exist")
	}
}

func TestFileExistsCB_NotExists(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "absent.txt")
	exists, p := fileExistsCB(path)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if exists {
		t.Fatal("expected file to not exist")
	}
}
