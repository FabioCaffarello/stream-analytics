package hyperliquid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/replay"
)

const (
	defaultInfoURL = "https://api.hyperliquid.xyz/info"
	// HyperLiquid rate limit guidance: ~3 req/sec for info endpoint.
	hlAPIThrottle = 350 * time.Millisecond
)

var backfillHTTPPost = httpPostJSON

// BackfillConfig controls what data to download from HyperLiquid.
type BackfillConfig struct {
	Symbol     string
	From       time.Time
	To         time.Time
	OutputDir  string
	MarketType string // defaults to USD_M_FUTURES
	BaseURL    string // override for testing
}

// BackfillResult reports download progress.
type BackfillResult struct {
	DatesDownloaded int
	DatesSkipped    int
	TradesParsed    int64
	OutputPath      string
}

type recentTradeEntry struct {
	Coin string `json:"coin"`
	Side string `json:"side"`
	Px   string `json:"px"`
	Sz   string `json:"sz"`
	Hash string `json:"hash"`
	Time int64  `json:"time"`
	Tid  int64  `json:"tid"`
}

// DownloadTrades downloads recent trades from the HyperLiquid REST API
// and produces a JSONL fixture file ready for replay.
//
// HyperLiquid returns up to ~100 recent trades per call. For historical
// backfill beyond recent data, use the startTime cursor to paginate backwards.
//
//nolint:gocyclo // day-by-day orchestration is intentionally explicit for operability.
func DownloadTrades(ctx context.Context, cfg BackfillConfig) (BackfillResult, *problem.Problem) {
	symbol := naming.CanonicalInstrument(cfg.Symbol)
	if symbol == "" {
		return BackfillResult{}, problem.New(problem.ValidationFailed, "symbol must not be empty")
	}
	// HyperLiquid uses the base coin (e.g., "BTC" not "BTCUSD").
	coin := coinFromSymbol(symbol)
	if coin == "" {
		return BackfillResult{}, problem.Newf(problem.ValidationFailed, "cannot derive HyperLiquid coin from %q", cfg.Symbol)
	}
	outputDir := strings.TrimSpace(cfg.OutputDir)
	if outputDir == "" {
		outputDir = "./backfill"
	}
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = defaultInfoURL
	}
	marketType := normalizeBackfillMarketType(cfg.MarketType)

	from := hlUtcStartOfDay(cfg.From)
	to := hlUtcStartOfDay(cfg.To)
	result := BackfillResult{OutputPath: defaultHLFixturePath(outputDir, symbol, from, to)}
	if to.Before(from) {
		return result, nil
	}

	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return BackfillResult{}, problem.Wrap(err, problem.Internal, "create backfill output directory failed")
	}
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		return BackfillResult{}, p
	}

	writer, p := replay.NewWriter(result.OutputPath)
	if p != nil {
		return BackfillResult{}, p
	}
	defer func() {
		_ = writer.Close()
	}()

	venue := naming.CanonicalVenue(VenueHyperLiquid)
	seq := int64(1)
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		if err := ctx.Err(); err != nil {
			return BackfillResult{}, problem.Wrap(err, problem.Unavailable, "backfill canceled")
		}
		cachePath := filepath.Join(outputDir, fmt.Sprintf("%s-%s-hyperliquid.jsonl", symbol, day.Format("2006-01-02")))

		exists, p := hlFileExists(cachePath)
		if p != nil {
			return BackfillResult{}, p
		}
		if exists {
			result.DatesSkipped++
			trades, p := hlReadCachedDayTrades(cachePath)
			if p != nil {
				return BackfillResult{}, p
			}
			for _, trade := range trades {
				env := hlBuildTradeEnvelope(venue, symbol, marketType, trade, seq)
				if p := writer.Append(env); p != nil {
					return BackfillResult{}, p
				}
				seq++
				result.TradesParsed++
			}
			continue
		}

		dayEnd := day.AddDate(0, 0, 1)
		trades, p := fetchHLDayTrades(ctx, baseURL, coin, day, dayEnd)
		if p != nil {
			return BackfillResult{}, p
		}

		if p := hlWriteDayCache(cachePath, trades); p != nil {
			return BackfillResult{}, p
		}
		result.DatesDownloaded++

		for _, trade := range trades {
			env := hlBuildTradeEnvelope(venue, symbol, marketType, trade, seq)
			if p := writer.Append(env); p != nil {
				return BackfillResult{}, p
			}
			seq++
			result.TradesParsed++
		}
	}

	if p := writer.Close(); p != nil {
		return BackfillResult{}, p
	}
	return result, nil
}

func fetchHLDayTrades(ctx context.Context, baseURL, coin string, from, to time.Time) ([]marketdomain.TradeTickV1, *problem.Problem) {
	var allTrades []marketdomain.TradeTickV1
	startTime := to.UnixMilli() // HyperLiquid returns trades before startTime

	for {
		if err := ctx.Err(); err != nil {
			return nil, problem.Wrap(err, problem.Unavailable, "backfill canceled")
		}

		body := map[string]any{
			"type":      "recentTrades",
			"coin":      coin,
			"startTime": startTime,
		}

		var entries []recentTradeEntry
		p := backfillHTTPPost(ctx, baseURL, body, &entries)
		if p != nil {
			return nil, p
		}
		if len(entries) == 0 {
			break
		}

		var addedInPage int
		minTime := startTime
		for _, entry := range entries {
			trade, p := hlEntryToTick(entry)
			if p != nil {
				return nil, p
			}
			tradeTime := time.UnixMilli(trade.Timestamp).UTC()
			if tradeTime.Before(from) {
				continue
			}
			if !tradeTime.Before(to) {
				continue
			}
			allTrades = append(allTrades, trade)
			addedInPage++
			if trade.Timestamp < minTime {
				minTime = trade.Timestamp
			}
		}

		// Check if oldest entry is before our from boundary.
		oldestEntry := entries[len(entries)-1]
		if oldestEntry.Time <= from.UnixMilli() {
			break
		}

		// Avoid infinite loop: if cursor didn't advance, stop.
		if minTime >= startTime {
			break
		}
		startTime = minTime

		if len(entries) < 100 {
			break
		}

		time.Sleep(hlAPIThrottle)
	}

	return hlSortByTimestamp(allTrades), nil
}

func hlEntryToTick(entry recentTradeEntry) (marketdomain.TradeTickV1, *problem.Problem) {
	price, err := strconv.ParseFloat(entry.Px, 64)
	if err != nil {
		return marketdomain.TradeTickV1{}, problem.Wrap(err, problem.ValidationFailed, "hyperliquid backfill: invalid price")
	}
	size, err := strconv.ParseFloat(entry.Sz, 64)
	if err != nil {
		return marketdomain.TradeTickV1{}, problem.Wrap(err, problem.ValidationFailed, "hyperliquid backfill: invalid size")
	}
	side, p := normalizeSide(entry.Side)
	if p != nil {
		return marketdomain.TradeTickV1{}, p
	}
	tradeID := strings.TrimSpace(entry.Hash)
	if tradeID == "" || isZeroHash(tradeID) {
		tradeID = strconv.FormatInt(entry.Tid, 10)
	}
	if tradeID == "" || tradeID == "0" {
		return marketdomain.TradeTickV1{}, problem.New(problem.ValidationFailed, "hyperliquid backfill: trade id is empty")
	}
	ts := entry.Time
	if ts <= 0 {
		return marketdomain.TradeTickV1{}, problem.New(problem.ValidationFailed, "hyperliquid backfill: invalid timestamp")
	}
	return marketdomain.TradeTickV1{
		Price:     price,
		Size:      size,
		Side:      side,
		TradeID:   tradeID,
		Timestamp: ts,
	}, nil
}

func hlBuildTradeEnvelope(venue, symbol, marketType string, trade marketdomain.TradeTickV1, seq int64) envelope.Envelope {
	payload, _ := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, trade)
	return envelope.Envelope{
		Type:           "marketdata.trade",
		Version:        1,
		Venue:          venue,
		Instrument:     symbol,
		ContentType:    envelope.ContentTypeJSON,
		TsExchange:     trade.Timestamp,
		TsIngest:       trade.Timestamp,
		Seq:            seq,
		IdempotencyKey: buildTradeIdempotencyKey(venue, symbol, trade.TradeID),
		Payload:        payload,
		Meta: map[string]string{
			"instrument_market_type": marketType,
			"source":                 "backfill",
		},
	}
}

func hlSortByTimestamp(trades []marketdomain.TradeTickV1) []marketdomain.TradeTickV1 {
	for i := 1; i < len(trades); i++ {
		for j := i; j > 0 && trades[j].Timestamp < trades[j-1].Timestamp; j-- {
			trades[j], trades[j-1] = trades[j-1], trades[j]
		}
	}
	return trades
}

func httpPostJSON(ctx context.Context, url string, body any, dest any) *problem.Problem {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "hyperliquid backfill: marshal request failed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return problem.Wrap(err, problem.Internal, "hyperliquid backfill: build request failed")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "market-raccoon/backfill")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return problem.Wrap(err, problem.Unavailable, "hyperliquid backfill: POST failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return problem.Newf(problem.Unavailable, "hyperliquid backfill: status=%d url=%s body=%s", resp.StatusCode, url, string(respBody))
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "hyperliquid backfill: read response failed")
	}
	if err := json.Unmarshal(respBody, dest); err != nil {
		return problem.Wrap(err, problem.ValidationFailed, "hyperliquid backfill: JSON decode failed")
	}
	return nil
}

func coinFromSymbol(symbol string) string {
	s := strings.ToUpper(strings.TrimSpace(symbol))
	for _, suffix := range []string{"USDT", "USDC", "USD", "PERP"} {
		if strings.HasSuffix(s, suffix) && len(s) > len(suffix) {
			return strings.TrimSuffix(s, suffix)
		}
	}
	// If no known suffix, return as-is (e.g., already "BTC").
	return s
}

func normalizeBackfillMarketType(v string) string {
	mt := strings.ToUpper(strings.TrimSpace(v))
	if mt == "" {
		return "USD_M_FUTURES"
	}
	return mt
}

func hlUtcStartOfDay(ts time.Time) time.Time {
	y, m, d := ts.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func defaultHLFixturePath(outputDir, symbol string, from, to time.Time) string {
	return filepath.Join(outputDir, fmt.Sprintf("%s-%s-%s-hyperliquid.jsonl", symbol, from.Format("2006-01-02"), to.Format("2006-01-02")))
}

func hlFileExists(path string) (bool, *problem.Problem) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, problem.Wrap(err, problem.Internal, "stat backfill file failed")
}

func hlWriteDayCache(path string, trades []marketdomain.TradeTickV1) *problem.Problem {
	// #nosec G304 -- path derived from explicit operator-selected output directory.
	f, err := os.Create(path)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "hyperliquid backfill: create cache file failed")
	}
	defer func() { _ = f.Close() }()

	enc := json.NewEncoder(f)
	for _, t := range trades {
		if err := enc.Encode(t); err != nil {
			return problem.Wrap(err, problem.Internal, "hyperliquid backfill: encode cache trade failed")
		}
	}
	return nil
}

func hlReadCachedDayTrades(path string) ([]marketdomain.TradeTickV1, *problem.Problem) {
	// #nosec G304 -- path derived from explicit operator-selected output directory.
	f, err := os.Open(path)
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "hyperliquid backfill: open cache file failed")
	}
	defer func() { _ = f.Close() }()

	var trades []marketdomain.TradeTickV1
	dec := json.NewDecoder(f)
	for dec.More() {
		var t marketdomain.TradeTickV1
		if err := dec.Decode(&t); err != nil {
			return nil, problem.Wrap(err, problem.ValidationFailed, "hyperliquid backfill: decode cache trade failed")
		}
		trades = append(trades, t)
	}
	return trades, nil
}
