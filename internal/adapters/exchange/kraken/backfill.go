package kraken

import (
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
	defaultKrakenRESTBaseURL = "https://api.kraken.com"
	// Kraken public rate limit: 1 req/sec.
	krakenAPIThrottle = 1100 * time.Millisecond
)

var backfillHTTPGet = krakenHTTPGetJSON

// BackfillConfig controls what data to download from Kraken.
type BackfillConfig struct {
	Symbol     string
	From       time.Time
	To         time.Time
	OutputDir  string
	MarketType string
	BaseURL    string
}

// BackfillResult reports download progress.
type BackfillResult struct {
	DatesDownloaded int
	DatesSkipped    int
	TradesParsed    int64
	OutputPath      string
}

type krakenTradesResponse struct {
	Error  []string                   `json:"error"`
	Result map[string]json.RawMessage `json:"result"`
}

// DownloadTrades downloads historical trades from the Kraken REST API
// and produces a JSONL fixture file ready for replay.
//
//nolint:gocyclo // day-by-day orchestration is intentionally explicit for operability.
func DownloadTrades(ctx context.Context, cfg BackfillConfig) (BackfillResult, *problem.Problem) {
	symbol := naming.CanonicalInstrument(cfg.Symbol)
	if symbol == "" {
		return BackfillResult{}, problem.New(problem.ValidationFailed, "symbol must not be empty")
	}
	pair := krakenRESTpair(symbol)
	if pair == "" {
		return BackfillResult{}, problem.Newf(problem.ValidationFailed, "cannot derive Kraken pair from %q", cfg.Symbol)
	}
	outputDir := strings.TrimSpace(cfg.OutputDir)
	if outputDir == "" {
		outputDir = "./backfill"
	}
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = defaultKrakenRESTBaseURL
	}

	from := utcStartOfDay(cfg.From)
	to := utcStartOfDay(cfg.To)
	result := BackfillResult{OutputPath: defaultKrakenFixturePath(outputDir, symbol, from, to)}
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

	venue := naming.CanonicalVenue(VenueKraken)
	seq := int64(1)
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		if err := ctx.Err(); err != nil {
			return BackfillResult{}, problem.Wrap(err, problem.Unavailable, "backfill canceled")
		}
		jsonlPath := filepath.Join(outputDir, fmt.Sprintf("%s-%s-kraken.jsonl", symbol, day.Format("2006-01-02")))

		exists, p := fileExists(jsonlPath)
		if p != nil {
			return BackfillResult{}, p
		}
		if exists {
			result.DatesSkipped++
			trades, p := readCachedDayTrades(jsonlPath)
			if p != nil {
				return BackfillResult{}, p
			}
			for _, trade := range trades {
				env := buildKrakenTradeEnvelope(venue, symbol, trade, seq)
				if p := writer.Append(env); p != nil {
					return BackfillResult{}, p
				}
				seq++
				result.TradesParsed++
			}
			continue
		}

		dayEnd := day.AddDate(0, 0, 1)
		trades, p := fetchKrakenDayTrades(ctx, baseURL, pair, day, dayEnd)
		if p != nil {
			return BackfillResult{}, p
		}

		if p := writeDayCache(jsonlPath, trades); p != nil {
			return BackfillResult{}, p
		}
		result.DatesDownloaded++

		for _, trade := range trades {
			env := buildKrakenTradeEnvelope(venue, symbol, trade, seq)
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

//nolint:gocyclo // pagination loop with cursor management is intentionally explicit.
func fetchKrakenDayTrades(ctx context.Context, baseURL, pair string, from, to time.Time) ([]marketdomain.TradeTickV1, *problem.Problem) {
	var allTrades []marketdomain.TradeTickV1
	since := strconv.FormatInt(from.UnixNano(), 10)

	for {
		if err := ctx.Err(); err != nil {
			return nil, problem.Wrap(err, problem.Unavailable, "backfill canceled")
		}

		url := fmt.Sprintf("%s/0/public/Trades?pair=%s&since=%s", baseURL, pair, since)

		var resp krakenTradesResponse
		p := backfillHTTPGet(ctx, url, &resp)
		if p != nil {
			return nil, p
		}
		if len(resp.Error) > 0 {
			return nil, problem.Newf(problem.Unavailable, "kraken API error: %s", strings.Join(resp.Error, "; "))
		}

		// Extract the "last" cursor and the trade array.
		lastRaw, hasLast := resp.Result["last"]
		if !hasLast {
			break
		}
		var lastCursor string
		if err := json.Unmarshal(lastRaw, &lastCursor); err != nil {
			return nil, problem.Wrap(err, problem.ValidationFailed, "kraken backfill: invalid last cursor")
		}

		// Find the pair key (may differ from our input pair).
		var tradesRaw json.RawMessage
		for k, v := range resp.Result {
			if k != "last" {
				tradesRaw = v
				break
			}
		}
		if tradesRaw == nil {
			break
		}

		var arrays [][]json.RawMessage
		if err := json.Unmarshal(tradesRaw, &arrays); err != nil {
			return nil, problem.Wrap(err, problem.ValidationFailed, "kraken backfill: invalid trades array")
		}
		if len(arrays) == 0 {
			break
		}

		var addedInPage int
		for _, arr := range arrays {
			trade, p := krakenArrayToTick(arr)
			if p != nil {
				return nil, p
			}
			tradeTime := time.UnixMilli(trade.Timestamp).UTC()
			if !tradeTime.Before(to) {
				return sortByTimestamp(allTrades), nil
			}
			if !tradeTime.Before(from) {
				allTrades = append(allTrades, trade)
				addedInPage++
			}
		}

		if lastCursor == since {
			break
		}
		since = lastCursor

		if addedInPage == 0 {
			break
		}

		time.Sleep(krakenAPIThrottle)
	}

	return sortByTimestamp(allTrades), nil
}

// krakenArrayToTick parses a Kraken trade array:
// [price, volume, time, side, type, misc, tradeID]
func krakenArrayToTick(arr []json.RawMessage) (marketdomain.TradeTickV1, *problem.Problem) {
	if len(arr) < 6 {
		return marketdomain.TradeTickV1{}, problem.Newf(problem.ValidationFailed, "kraken trade array needs >=6 elements, got %d", len(arr))
	}

	var priceStr, volStr, sideStr, tradeIDStr string
	var ts float64

	if err := json.Unmarshal(arr[0], &priceStr); err != nil {
		return marketdomain.TradeTickV1{}, problem.Wrap(err, problem.ValidationFailed, "kraken backfill: invalid price")
	}
	if err := json.Unmarshal(arr[1], &volStr); err != nil {
		return marketdomain.TradeTickV1{}, problem.Wrap(err, problem.ValidationFailed, "kraken backfill: invalid volume")
	}
	if err := json.Unmarshal(arr[2], &ts); err != nil {
		return marketdomain.TradeTickV1{}, problem.Wrap(err, problem.ValidationFailed, "kraken backfill: invalid timestamp")
	}
	if err := json.Unmarshal(arr[3], &sideStr); err != nil {
		return marketdomain.TradeTickV1{}, problem.Wrap(err, problem.ValidationFailed, "kraken backfill: invalid side")
	}

	// tradeID is at index 6 if present, otherwise use timestamp.
	if len(arr) >= 7 {
		// tradeID may be a string or an int.
		rawID := arr[6]
		var idStr string
		var idInt int64
		if err := json.Unmarshal(rawID, &idStr); err == nil {
			tradeIDStr = idStr
		} else if err := json.Unmarshal(rawID, &idInt); err == nil {
			tradeIDStr = strconv.FormatInt(idInt, 10)
		}
	}
	if tradeIDStr == "" {
		tradeIDStr = fmt.Sprintf("%.6f", ts)
	}

	price, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		return marketdomain.TradeTickV1{}, problem.Wrap(err, problem.ValidationFailed, "kraken backfill: invalid price float")
	}
	vol, err := strconv.ParseFloat(volStr, 64)
	if err != nil {
		return marketdomain.TradeTickV1{}, problem.Wrap(err, problem.ValidationFailed, "kraken backfill: invalid volume float")
	}

	side := "buy"
	if strings.ToLower(strings.TrimSpace(sideStr)) == "s" {
		side = "sell"
	}

	tsMs := int64(ts * 1000)

	return marketdomain.TradeTickV1{
		Price:     price,
		Size:      vol,
		Side:      side,
		TradeID:   tradeIDStr,
		Timestamp: tsMs,
	}, nil
}

func buildKrakenTradeEnvelope(venue, symbol string, trade marketdomain.TradeTickV1, seq int64) envelope.Envelope {
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
		IdempotencyKey: "venue=" + venue + "|instrument=" + symbol + "|trade_id=" + trade.TradeID,
		Payload:        payload,
		Meta: map[string]string{
			"instrument_market_type": "SPOT",
			"source":                 "backfill",
		},
	}
}

// krakenRESTpair converts a canonical instrument to a Kraken REST pair.
// Kraken REST uses concatenated pairs like "XBTUSDT" (XBT for BTC).
func krakenRESTpair(canonical string) string {
	if canonical == "" {
		return ""
	}
	s := strings.ToUpper(strings.TrimSpace(canonical))
	// Replace BTC with XBT for Kraken.
	if strings.HasPrefix(s, "BTC") {
		s = "XBT" + s[3:]
	}
	return s
}

func sortByTimestamp(trades []marketdomain.TradeTickV1) []marketdomain.TradeTickV1 {
	for i := 1; i < len(trades); i++ {
		for j := i; j > 0 && trades[j].Timestamp < trades[j-1].Timestamp; j-- {
			trades[j], trades[j-1] = trades[j-1], trades[j]
		}
	}
	return trades
}

func krakenHTTPGetJSON(ctx context.Context, url string, dest any) *problem.Problem {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "kraken backfill: build request failed")
	}
	req.Header.Set("User-Agent", "market-raccoon/backfill")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return problem.Wrap(err, problem.Unavailable, "kraken backfill: GET failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return problem.Newf(problem.Unavailable, "kraken backfill: status=%d url=%s body=%s", resp.StatusCode, url, string(body))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "kraken backfill: read response failed")
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return problem.Wrap(err, problem.ValidationFailed, "kraken backfill: JSON decode failed")
	}
	return nil
}

func writeDayCache(path string, trades []marketdomain.TradeTickV1) *problem.Problem {
	// #nosec G304 -- path derived from explicit operator-selected output directory.
	f, err := os.Create(path)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "kraken backfill: create cache file failed")
	}
	defer func() { _ = f.Close() }()

	enc := json.NewEncoder(f)
	for _, t := range trades {
		if err := enc.Encode(t); err != nil {
			return problem.Wrap(err, problem.Internal, "kraken backfill: encode cache trade failed")
		}
	}
	return nil
}

func readCachedDayTrades(path string) ([]marketdomain.TradeTickV1, *problem.Problem) {
	// #nosec G304 -- path derived from explicit operator-selected output directory.
	f, err := os.Open(path)
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "kraken backfill: open cache file failed")
	}
	defer func() { _ = f.Close() }()

	var trades []marketdomain.TradeTickV1
	dec := json.NewDecoder(f)
	for dec.More() {
		var t marketdomain.TradeTickV1
		if err := dec.Decode(&t); err != nil {
			return nil, problem.Wrap(err, problem.ValidationFailed, "kraken backfill: decode cache trade failed")
		}
		trades = append(trades, t)
	}
	return trades, nil
}

func fileExists(path string) (bool, *problem.Problem) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, problem.Wrap(err, problem.Internal, "stat backfill file failed")
}

func utcStartOfDay(ts time.Time) time.Time {
	y, m, d := ts.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func defaultKrakenFixturePath(outputDir, symbol string, from, to time.Time) string {
	return filepath.Join(outputDir, fmt.Sprintf("%s-%s-%s-kraken.jsonl", symbol, from.Format("2006-01-02"), to.Format("2006-01-02")))
}
