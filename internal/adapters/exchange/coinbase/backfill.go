package coinbase

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
	defaultRESTBaseURL = "https://api.exchange.coinbase.com"
	// Coinbase rate limit: 10 req/sec for public endpoints.
	apiThrottle = 120 * time.Millisecond
	maxPerPage  = 100
)

var backfillHTTPGet = httpGetJSON

// BackfillConfig controls what data to download from Coinbase.
type BackfillConfig struct {
	Symbol     string
	From       time.Time
	To         time.Time
	OutputDir  string
	MarketType string // always SPOT for Coinbase
	BaseURL    string // override for testing
}

// BackfillResult reports download progress.
type BackfillResult struct {
	DatesDownloaded int
	DatesSkipped    int
	TradesParsed    int64
	OutputPath      string
}

type restTrade struct {
	TradeID int64  `json:"trade_id"`
	Price   string `json:"price"`
	Size    string `json:"size"`
	Time    string `json:"time"`
	Side    string `json:"side"`
}

// DownloadTrades downloads historical trades from the Coinbase REST API
// and produces a JSONL fixture file ready for replay.
//
//nolint:gocyclo // day-by-day orchestration is intentionally explicit for operability.
func DownloadTrades(ctx context.Context, cfg BackfillConfig) (BackfillResult, *problem.Problem) {
	symbol := naming.CanonicalInstrument(cfg.Symbol)
	if symbol == "" {
		return BackfillResult{}, problem.New(problem.ValidationFailed, "symbol must not be empty")
	}
	productID := normalizeProductID(symbol)
	if productID == "" {
		return BackfillResult{}, problem.Newf(problem.ValidationFailed, "cannot derive Coinbase product_id from %q", cfg.Symbol)
	}
	outputDir := strings.TrimSpace(cfg.OutputDir)
	if outputDir == "" {
		outputDir = "./backfill"
	}
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = defaultRESTBaseURL
	}

	from := utcStartOfDay(cfg.From)
	to := utcStartOfDay(cfg.To)
	result := BackfillResult{OutputPath: defaultCBFixturePath(outputDir, symbol, from, to)}
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

	venue := naming.CanonicalVenue(VenueCoinbase)
	seq := int64(1)
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		if err := ctx.Err(); err != nil {
			return BackfillResult{}, problem.Wrap(err, problem.Unavailable, "backfill canceled")
		}
		jsonlPath := filepath.Join(outputDir, fmt.Sprintf("%s-%s-coinbase.jsonl", symbol, day.Format("2006-01-02")))

		exists, p := fileExistsCB(jsonlPath)
		if p != nil {
			return BackfillResult{}, p
		}
		if exists {
			result.DatesSkipped++
			// Re-read cached day to maintain sequence numbering.
			trades, p := readCachedDayTrades(jsonlPath)
			if p != nil {
				return BackfillResult{}, p
			}
			for _, trade := range trades {
				env := buildTradeEnvelope(venue, symbol, trade, seq)
				if p := writer.Append(env); p != nil {
					return BackfillResult{}, p
				}
				seq++
				result.TradesParsed++
			}
			continue
		}

		dayEnd := day.AddDate(0, 0, 1)
		trades, p := fetchDayTrades(ctx, baseURL, productID, day, dayEnd)
		if p != nil {
			return BackfillResult{}, p
		}

		// Cache raw trades as JSONL for idempotent reruns.
		if p := writeDayCache(jsonlPath, trades); p != nil {
			return BackfillResult{}, p
		}
		result.DatesDownloaded++

		for _, trade := range trades {
			env := buildTradeEnvelope(venue, symbol, trade, seq)
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

func fetchDayTrades(ctx context.Context, baseURL, productID string, from, to time.Time) ([]marketdomain.TradeTickV1, *problem.Problem) {
	var allTrades []marketdomain.TradeTickV1
	var beforeID int64

	for {
		if err := ctx.Err(); err != nil {
			return nil, problem.Wrap(err, problem.Unavailable, "backfill canceled")
		}

		url := fmt.Sprintf("%s/products/%s/trades?limit=%d", baseURL, productID, maxPerPage)
		if beforeID > 0 {
			url += fmt.Sprintf("&before=%d", beforeID)
		}

		var batch []restTrade
		p := backfillHTTPGet(ctx, url, &batch)
		if p != nil {
			return nil, p
		}
		if len(batch) == 0 {
			break
		}

		var addedInPage int
		for _, rt := range batch {
			trade, p := restTradeToTick(rt)
			if p != nil {
				return nil, p
			}
			tradeTime := time.UnixMilli(trade.Timestamp).UTC()
			if tradeTime.Before(from) {
				// Passed the start boundary; trades are returned newest-first.
				return sortByTimestamp(allTrades), nil
			}
			if tradeTime.Before(to) {
				allTrades = append(allTrades, trade)
				addedInPage++
			}
		}

		// Coinbase returns trades newest-first; use smallest trade_id as cursor.
		lastID := batch[len(batch)-1].TradeID
		if lastID <= 0 || lastID == beforeID {
			break
		}
		beforeID = lastID

		if addedInPage == 0 && len(batch) < maxPerPage {
			break
		}

		time.Sleep(apiThrottle)
	}

	return sortByTimestamp(allTrades), nil
}

func restTradeToTick(rt restTrade) (marketdomain.TradeTickV1, *problem.Problem) {
	price, err := strconv.ParseFloat(rt.Price, 64)
	if err != nil {
		return marketdomain.TradeTickV1{}, problem.Wrap(err, problem.ValidationFailed, "coinbase backfill: invalid price")
	}
	size, err := strconv.ParseFloat(rt.Size, 64)
	if err != nil {
		return marketdomain.TradeTickV1{}, problem.Wrap(err, problem.ValidationFailed, "coinbase backfill: invalid size")
	}
	side, p := normalizeSide(rt.Side)
	if p != nil {
		return marketdomain.TradeTickV1{}, p
	}
	ts, p := parseExchangeTimeMillis(rt.Time, time.Now())
	if p != nil {
		return marketdomain.TradeTickV1{}, p
	}
	return marketdomain.TradeTickV1{
		Price:     price,
		Size:      size,
		Side:      side,
		TradeID:   strconv.FormatInt(rt.TradeID, 10),
		Timestamp: ts,
	}, nil
}

func buildTradeEnvelope(venue, symbol string, trade marketdomain.TradeTickV1, seq int64) envelope.Envelope {
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
			"instrument_market_type": "SPOT",
			"source":                 "backfill",
		},
	}
}

func sortByTimestamp(trades []marketdomain.TradeTickV1) []marketdomain.TradeTickV1 {
	// Simple insertion sort — backfill pages are small.
	for i := 1; i < len(trades); i++ {
		for j := i; j > 0 && trades[j].Timestamp < trades[j-1].Timestamp; j-- {
			trades[j], trades[j-1] = trades[j-1], trades[j]
		}
	}
	return trades
}

func httpGetJSON(ctx context.Context, url string, dest any) *problem.Problem {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "coinbase backfill: build request failed")
	}
	req.Header.Set("User-Agent", "market-raccoon/backfill")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return problem.Wrap(err, problem.Unavailable, "coinbase backfill: GET failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return problem.Newf(problem.Unavailable, "coinbase backfill: status=%d url=%s body=%s", resp.StatusCode, url, string(body))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "coinbase backfill: read response failed")
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return problem.Wrap(err, problem.ValidationFailed, "coinbase backfill: JSON decode failed")
	}
	return nil
}

func writeDayCache(path string, trades []marketdomain.TradeTickV1) *problem.Problem {
	// #nosec G304 -- path derived from explicit operator-selected output directory.
	f, err := os.Create(path)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "coinbase backfill: create cache file failed")
	}
	defer func() { _ = f.Close() }()

	enc := json.NewEncoder(f)
	for _, t := range trades {
		if err := enc.Encode(t); err != nil {
			return problem.Wrap(err, problem.Internal, "coinbase backfill: encode cache trade failed")
		}
	}
	return nil
}

func readCachedDayTrades(path string) ([]marketdomain.TradeTickV1, *problem.Problem) {
	// #nosec G304 -- path derived from explicit operator-selected output directory.
	f, err := os.Open(path)
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "coinbase backfill: open cache file failed")
	}
	defer func() { _ = f.Close() }()

	var trades []marketdomain.TradeTickV1
	dec := json.NewDecoder(f)
	for dec.More() {
		var t marketdomain.TradeTickV1
		if err := dec.Decode(&t); err != nil {
			return nil, problem.Wrap(err, problem.ValidationFailed, "coinbase backfill: decode cache trade failed")
		}
		trades = append(trades, t)
	}
	return trades, nil
}

func fileExistsCB(path string) (bool, *problem.Problem) {
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

func defaultCBFixturePath(outputDir, symbol string, from, to time.Time) string {
	return filepath.Join(outputDir, fmt.Sprintf("%s-%s-%s-coinbase.jsonl", symbol, from.Format("2006-01-02"), to.Format("2006-01-02")))
}
