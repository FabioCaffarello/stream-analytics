package krakenf

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

	"github.com/market-raccoon/internal/contracts"
	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/replay"
)

const (
	defaultKrakenFRESTBaseURL = "https://futures.kraken.com"
	// Kraken Futures public rate limit: 1 req/sec.
	krakenFAPIThrottle = 1100 * time.Millisecond
)

var backfillHTTPGet = krakenFHTTPGetJSON

// BackfillConfig controls what data to download from Kraken Futures.
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

type krakenFHistoryResponse struct {
	Result  string           `json:"result"`
	History []krakenFHistory `json:"history"`
}

type krakenFHistory struct {
	UID   string  `json:"uid"`
	Price float64 `json:"price"`
	Qty   float64 `json:"qty"`
	Side  string  `json:"side"`
	Time  string  `json:"time"`
	Type  string  `json:"type"`
}

// DownloadTrades downloads historical trades from the Kraken Futures REST API
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
		return BackfillResult{}, problem.Newf(problem.ValidationFailed, "cannot derive KrakenF product_id from %q", cfg.Symbol)
	}
	outputDir := strings.TrimSpace(cfg.OutputDir)
	if outputDir == "" {
		outputDir = "./backfill"
	}
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = defaultKrakenFRESTBaseURL
	}

	from := utcStartOfDay(cfg.From)
	to := utcStartOfDay(cfg.To)
	result := BackfillResult{OutputPath: defaultKrakenFFixturePath(outputDir, symbol, from, to)}
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

	venue := naming.CanonicalVenue(VenueKrakenF)
	seq := int64(1)
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		if err := ctx.Err(); err != nil {
			return BackfillResult{}, problem.Wrap(err, problem.Unavailable, "backfill canceled")
		}
		jsonlPath := filepath.Join(outputDir, fmt.Sprintf("%s-%s-krakenf.jsonl", symbol, day.Format("2006-01-02")))

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
				env := buildKrakenFTradeEnvelope(venue, symbol, trade, seq)
				if p := writer.Append(env); p != nil {
					return BackfillResult{}, p
				}
				seq++
				result.TradesParsed++
			}
			continue
		}

		dayEnd := day.AddDate(0, 0, 1)
		trades, p := fetchKrakenFDayTrades(ctx, baseURL, productID, day, dayEnd)
		if p != nil {
			return BackfillResult{}, p
		}

		if p := writeDayCache(jsonlPath, trades); p != nil {
			return BackfillResult{}, p
		}
		result.DatesDownloaded++

		for _, trade := range trades {
			env := buildKrakenFTradeEnvelope(venue, symbol, trade, seq)
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

func fetchKrakenFDayTrades(ctx context.Context, baseURL, productID string, from, to time.Time) ([]marketdomain.TradeTickV1, *problem.Problem) {
	var allTrades []marketdomain.TradeTickV1
	lastTime := to.Format(time.RFC3339)

	for {
		if err := ctx.Err(); err != nil {
			return nil, problem.Wrap(err, problem.Unavailable, "backfill canceled")
		}

		url := fmt.Sprintf("%s/derivatives/api/v3/history?symbol=%s&lastTime=%s", baseURL, productID, lastTime)

		var resp krakenFHistoryResponse
		p := backfillHTTPGet(ctx, url, &resp)
		if p != nil {
			return nil, p
		}
		if resp.Result != "success" {
			return nil, problem.Newf(problem.Unavailable, "krakenf API result=%q", resp.Result)
		}
		if len(resp.History) == 0 {
			break
		}

		var addedInPage int
		var earliestTime string
		for _, h := range resp.History {
			trade, p := krakenFHistoryToTick(h)
			if p != nil {
				return nil, p
			}
			tradeTime := time.UnixMilli(trade.Timestamp).UTC()
			if tradeTime.Before(from) {
				return sortByTimestamp(allTrades), nil
			}
			if tradeTime.Before(to) {
				allTrades = append(allTrades, trade)
				addedInPage++
			}
			earliestTime = h.Time
		}

		if earliestTime == "" || earliestTime == lastTime {
			break
		}
		lastTime = earliestTime

		if addedInPage == 0 {
			break
		}

		time.Sleep(krakenFAPIThrottle)
	}

	return sortByTimestamp(allTrades), nil
}

func krakenFHistoryToTick(h krakenFHistory) (marketdomain.TradeTickV1, *problem.Problem) {
	if h.Price <= 0 {
		return marketdomain.TradeTickV1{}, problem.Newf(problem.ValidationFailed, "krakenf backfill: invalid price %v", h.Price)
	}
	if h.Qty <= 0 {
		return marketdomain.TradeTickV1{}, problem.Newf(problem.ValidationFailed, "krakenf backfill: invalid qty %v", h.Qty)
	}

	side := "buy"
	if strings.ToLower(strings.TrimSpace(h.Side)) == "sell" {
		side = "sell"
	}

	ts, err := time.Parse(time.RFC3339Nano, h.Time)
	if err != nil {
		return marketdomain.TradeTickV1{}, problem.Wrap(err, problem.ValidationFailed, "krakenf backfill: invalid timestamp")
	}

	tradeID := h.UID
	if tradeID == "" {
		tradeID = strconv.FormatInt(ts.UnixNano(), 10)
	}

	return marketdomain.TradeTickV1{
		Price:     h.Price,
		Size:      h.Qty,
		Side:      side,
		TradeID:   tradeID,
		Timestamp: ts.UnixMilli(),
	}, nil
}

func buildKrakenFTradeEnvelope(venue, symbol string, trade marketdomain.TradeTickV1, seq int64) envelope.Envelope {
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
			"instrument_market_type": "USDT_FUTURES",
			"source":                 "backfill",
		},
	}
}

func sortByTimestamp(trades []marketdomain.TradeTickV1) []marketdomain.TradeTickV1 {
	for i := 1; i < len(trades); i++ {
		for j := i; j > 0 && trades[j].Timestamp < trades[j-1].Timestamp; j-- {
			trades[j], trades[j-1] = trades[j-1], trades[j]
		}
	}
	return trades
}

func krakenFHTTPGetJSON(ctx context.Context, url string, dest any) *problem.Problem {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "krakenf backfill: build request failed")
	}
	req.Header.Set("User-Agent", "market-raccoon/backfill")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return problem.Wrap(err, problem.Unavailable, "krakenf backfill: GET failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return problem.Newf(problem.Unavailable, "krakenf backfill: status=%d url=%s body=%s", resp.StatusCode, url, string(body))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "krakenf backfill: read response failed")
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return problem.Wrap(err, problem.ValidationFailed, "krakenf backfill: JSON decode failed")
	}
	return nil
}

func writeDayCache(path string, trades []marketdomain.TradeTickV1) *problem.Problem {
	// #nosec G304 -- path derived from explicit operator-selected output directory.
	f, err := os.Create(path)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "krakenf backfill: create cache file failed")
	}
	defer func() { _ = f.Close() }()

	enc := json.NewEncoder(f)
	for _, t := range trades {
		if err := enc.Encode(t); err != nil {
			return problem.Wrap(err, problem.Internal, "krakenf backfill: encode cache trade failed")
		}
	}
	return nil
}

func readCachedDayTrades(path string) ([]marketdomain.TradeTickV1, *problem.Problem) {
	// #nosec G304 -- path derived from explicit operator-selected output directory.
	f, err := os.Open(path)
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "krakenf backfill: open cache file failed")
	}
	defer func() { _ = f.Close() }()

	var trades []marketdomain.TradeTickV1
	dec := json.NewDecoder(f)
	for dec.More() {
		var t marketdomain.TradeTickV1
		if err := dec.Decode(&t); err != nil {
			return nil, problem.Wrap(err, problem.ValidationFailed, "krakenf backfill: decode cache trade failed")
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

func defaultKrakenFFixturePath(outputDir, symbol string, from, to time.Time) string {
	return filepath.Join(outputDir, fmt.Sprintf("%s-%s-%s-krakenf.jsonl", symbol, from.Format("2006-01-02"), to.Format("2006-01-02")))
}
