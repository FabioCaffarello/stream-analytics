package binance

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/contracts"
	marketdomain "github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/codec"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/naming"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/replay"
)

const (
	marketTypeSpot        = "SPOT"
	marketTypeUSDMFutures = "USD_M_FUTURES"
)

var backfillDownloadZip = downloadAggTradesZip

// BackfillConfig controls what data to download.
type BackfillConfig struct {
	Symbol     string
	From       time.Time
	To         time.Time
	OutputDir  string
	MarketType string
}

// BackfillResult reports download progress.
type BackfillResult struct {
	DatesDownloaded int
	DatesSkipped    int
	TradesParsed    int64
	OutputPath      string
}

// DownloadAggTrades downloads historical aggregate trades from data.binance.vision
// and produces a JSONL fixture file ready for replay.
//
//nolint:gocyclo // day-by-day orchestration is intentionally explicit for operability.
func DownloadAggTrades(ctx context.Context, cfg BackfillConfig) (BackfillResult, *problem.Problem) {
	symbol := naming.CanonicalInstrument(cfg.Symbol)
	if symbol == "" {
		return BackfillResult{}, problem.New(problem.ValidationFailed, "symbol must not be empty")
	}
	marketType, p := normalizeBackfillMarketType(cfg.MarketType)
	if p != nil {
		return BackfillResult{}, p
	}
	outputDir := strings.TrimSpace(cfg.OutputDir)
	if outputDir == "" {
		outputDir = "./backfill"
	}

	from := utcStartOfDay(cfg.From)
	to := utcStartOfDay(cfg.To)
	result := BackfillResult{OutputPath: defaultFixturePath(outputDir, symbol, from, to)}
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

	venue := naming.CanonicalVenue(VenueBinance)
	seq := int64(1)
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		if err := ctx.Err(); err != nil {
			return BackfillResult{}, problem.Wrap(err, problem.Unavailable, "backfill canceled")
		}
		csvPath := filepath.Join(outputDir, fmt.Sprintf("%s-%s.csv", symbol, day.Format("2006-01-02")))

		exists, p := fileExists(csvPath)
		if p != nil {
			return BackfillResult{}, p
		}
		if !exists {
			zipPayload, p := backfillDownloadZip(ctx, symbol, day, marketType)
			if p != nil {
				return BackfillResult{}, p
			}
			csvBytes, p := extractCSVFromZip(zipPayload)
			if p != nil {
				return BackfillResult{}, p
			}
			if err := os.WriteFile(csvPath, csvBytes, 0o600); err != nil {
				return BackfillResult{}, problem.Wrap(err, problem.Internal, "write backfill csv failed")
			}
			result.DatesDownloaded++
		} else {
			result.DatesSkipped++
		}

		trades, p := readAggTradesCSV(csvPath)
		if p != nil {
			return BackfillResult{}, p
		}
		for _, trade := range trades {
			payload, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, trade)
			if p != nil {
				return BackfillResult{}, p
			}
			env := envelope.Envelope{
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

func normalizeBackfillMarketType(v string) (string, *problem.Problem) {
	normalized := strings.ToUpper(strings.TrimSpace(v))
	if normalized == "" {
		normalized = marketTypeUSDMFutures
	}
	switch normalized {
	case marketTypeSpot, marketTypeUSDMFutures:
		return normalized, nil
	default:
		return "", problem.Newf(problem.ValidationFailed, "unsupported market_type %q", v)
	}
}

func utcStartOfDay(ts time.Time) time.Time {
	y, m, d := ts.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func defaultFixturePath(outputDir, symbol string, from, to time.Time) string {
	return filepath.Join(outputDir, fmt.Sprintf("%s-%s-%s.jsonl", symbol, from.Format("2006-01-02"), to.Format("2006-01-02")))
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

func readAggTradesCSV(path string) ([]marketdomain.TradeTickV1, *problem.Problem) {
	// #nosec G304 -- path is derived from explicit operator-selected output directory.
	f, err := os.Open(path)
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "open backfill csv failed")
	}
	defer func() {
		_ = f.Close()
	}()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1

	out := make([]marketdomain.TradeTickV1, 0, 4096)
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, problem.Wrap(err, problem.ValidationFailed, "read aggTrades csv row failed")
		}
		if len(row) == 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(row[0]), "agg_trade_id") {
			continue
		}
		if len(row) < 7 {
			return nil, problem.Newf(problem.ValidationFailed, "invalid aggTrades csv row columns=%d", len(row))
		}

		price, p := parseFloatField(row[1], "price")
		if p != nil {
			return nil, p
		}
		size, p := parseFloatField(row[2], "quantity")
		if p != nil {
			return nil, p
		}
		side, p := sideFromIsBuyerMaker(row[6])
		if p != nil {
			return nil, p
		}
		ts, p := parseIntField(row[5], "transact_time")
		if p != nil {
			return nil, p
		}

		out = append(out, marketdomain.TradeTickV1{
			Price:     price,
			Size:      size,
			Side:      side,
			TradeID:   strings.TrimSpace(row[0]),
			Timestamp: ts,
		})
	}
	return out, nil
}

func parseFloatField(raw, field string) (float64, *problem.Problem) {
	_ = field
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, problem.Wrap(err, problem.ValidationFailed, "parse aggTrades float field failed")
	}
	return v, nil
}

func parseIntField(raw, field string) (int64, *problem.Problem) {
	v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, problem.Wrap(err, problem.ValidationFailed, "parse aggTrades int field failed")
	}
	_ = field
	return v, nil
}

func sideFromIsBuyerMaker(raw string) (string, *problem.Problem) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true":
		return "sell", nil
	case "false":
		return "buy", nil
	default:
		return "", problem.Newf(problem.ValidationFailed, "invalid is_buyer_maker value %q", raw)
	}
}

func buildAggTradesURL(symbol string, date time.Time, marketType string) string {
	d := date.UTC().Format("2006-01-02")
	switch marketType {
	case marketTypeSpot:
		return fmt.Sprintf("https://data.binance.vision/data/spot/daily/aggTrades/%s/%s-aggTrades-%s.zip", symbol, symbol, d)
	default:
		return fmt.Sprintf("https://data.binance.vision/data/futures/um/daily/aggTrades/%s/%s-aggTrades-%s.zip", symbol, symbol, d)
	}
}

func downloadAggTradesZip(ctx context.Context, symbol string, date time.Time, marketType string) ([]byte, *problem.Problem) {
	url := buildAggTradesURL(symbol, date, marketType)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "build backfill request failed")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "download aggTrades zip failed")
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, problem.Newf(problem.Unavailable, "aggTrades download failed status=%d url=%s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "read aggTrades zip response failed")
	}
	return body, nil
}

func extractCSVFromZip(zipPayload []byte) ([]byte, *problem.Problem) {
	zr, err := zip.NewReader(bytes.NewReader(zipPayload), int64(len(zipPayload)))
	if err != nil {
		return nil, problem.Wrap(err, problem.ValidationFailed, "open aggTrades zip failed")
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(f.Name), ".csv") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, problem.Wrap(err, problem.Internal, "open aggTrades csv in zip failed")
		}
		defer func() {
			_ = rc.Close()
		}()
		csvBytes, err := io.ReadAll(rc)
		if err != nil {
			return nil, problem.Wrap(err, problem.Internal, "read aggTrades csv in zip failed")
		}
		return csvBytes, nil
	}
	return nil, problem.New(problem.ValidationFailed, "aggTrades zip missing csv file")
}
