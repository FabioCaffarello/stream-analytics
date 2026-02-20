package bybit

import (
	"bytes"
	"compress/gzip"
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

	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/replay"
)

var backfillDownloadGz = downloadTradesGz

// BackfillConfig controls what data to download from Bybit.
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

// DownloadTrades downloads historical trades from public.bybit.com
// and produces a JSONL fixture file ready for replay.
//
//nolint:gocyclo // day-by-day orchestration is intentionally explicit for operability.
func DownloadTrades(ctx context.Context, cfg BackfillConfig) (BackfillResult, *problem.Problem) {
	symbol := naming.CanonicalInstrument(cfg.Symbol)
	if symbol == "" {
		return BackfillResult{}, problem.New(problem.ValidationFailed, "symbol must not be empty")
	}
	marketType := normalizeBackfillMarketType(cfg.MarketType)
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

	venue := naming.CanonicalVenue(VenueBybit)
	seq := int64(1)
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		if err := ctx.Err(); err != nil {
			return BackfillResult{}, problem.Wrap(err, problem.Unavailable, "backfill canceled")
		}
		csvPath := filepath.Join(outputDir, fmt.Sprintf("%s-%s-bybit.csv", symbol, day.Format("2006-01-02")))

		exists, p := fileExists(csvPath)
		if p != nil {
			return BackfillResult{}, p
		}
		if !exists {
			gzPayload, p := backfillDownloadGz(ctx, symbol, day, marketType)
			if p != nil {
				return BackfillResult{}, p
			}
			csvBytes, p := decompressGz(gzPayload)
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

		trades, p := readBybitTradesCSV(csvPath)
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

func normalizeBackfillMarketType(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "spot":
		return "SPOT"
	case "linear", "usd_m_futures", "futures":
		return "LINEAR"
	case "inverse", "coin_m_futures":
		return "INVERSE"
	default:
		return "LINEAR"
	}
}

func utcStartOfDay(ts time.Time) time.Time {
	y, m, d := ts.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func defaultFixturePath(outputDir, symbol string, from, to time.Time) string {
	return filepath.Join(outputDir, fmt.Sprintf("%s-%s-%s-bybit.jsonl", symbol, from.Format("2006-01-02"), to.Format("2006-01-02")))
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

// readBybitTradesCSV parses Bybit's public CSV trade format.
// Columns: timestamp,symbol,side,size,price,tickDirection,trdMatchID,grossValue,homeNotional,foreignNotional
func readBybitTradesCSV(path string) ([]marketdomain.TradeTickV1, *problem.Problem) {
	// #nosec G304 -- path is derived from explicit operator-selected output directory.
	f, err := os.Open(path)
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "open bybit backfill csv failed")
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
			return nil, problem.Wrap(err, problem.ValidationFailed, "read bybit trades csv row failed")
		}
		if len(row) == 0 {
			continue
		}
		// Skip header row.
		first := strings.ToLower(strings.TrimSpace(row[0]))
		if first == "timestamp" || first == "ts" {
			continue
		}
		if len(row) < 7 {
			return nil, problem.Newf(problem.ValidationFailed, "invalid bybit trades csv row columns=%d", len(row))
		}

		// Column 0: timestamp (unix seconds, may have decimal).
		tsRaw := strings.TrimSpace(row[0])
		tsFloat, err := strconv.ParseFloat(tsRaw, 64)
		if err != nil {
			return nil, problem.Wrap(err, problem.ValidationFailed, "bybit backfill: invalid timestamp")
		}
		tsMs := int64(tsFloat * 1000)

		// Column 2: side (Buy/Sell).
		side, p := normalizeBackfillSide(row[2])
		if p != nil {
			return nil, p
		}

		// Column 3: size.
		size, err := strconv.ParseFloat(strings.TrimSpace(row[3]), 64)
		if err != nil {
			return nil, problem.Wrap(err, problem.ValidationFailed, "bybit backfill: invalid size")
		}

		// Column 4: price.
		price, err := strconv.ParseFloat(strings.TrimSpace(row[4]), 64)
		if err != nil {
			return nil, problem.Wrap(err, problem.ValidationFailed, "bybit backfill: invalid price")
		}

		// Column 6: trdMatchID (trade ID).
		tradeID := strings.TrimSpace(row[6])
		if tradeID == "" {
			return nil, problem.New(problem.ValidationFailed, "bybit backfill: trade id is empty")
		}

		out = append(out, marketdomain.TradeTickV1{
			Price:     price,
			Size:      size,
			Side:      side,
			TradeID:   tradeID,
			Timestamp: tsMs,
		})
	}
	return out, nil
}

func normalizeBackfillSide(raw string) (string, *problem.Problem) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "buy":
		return "buy", nil
	case "sell":
		return "sell", nil
	default:
		return "", problem.Newf(problem.ValidationFailed, "bybit backfill: unsupported side %q", raw)
	}
}

func buildTradesURL(symbol string, date time.Time, marketType string) string {
	d := date.UTC().Format("2006-01-02")
	switch marketType {
	case "SPOT":
		return fmt.Sprintf("https://public.bybit.com/spot/%s/%s%s.csv.gz", symbol, symbol, d)
	default:
		return fmt.Sprintf("https://public.bybit.com/trading/%s/%s%s.csv.gz", symbol, symbol, d)
	}
}

func downloadTradesGz(ctx context.Context, symbol string, date time.Time, marketType string) ([]byte, *problem.Problem) {
	url := buildTradesURL(symbol, date, marketType)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "build bybit backfill request failed")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "download bybit trades gz failed")
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, problem.Newf(problem.Unavailable, "bybit trades download failed status=%d url=%s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "read bybit trades gz response failed")
	}
	return body, nil
}

func decompressGz(gzPayload []byte) ([]byte, *problem.Problem) {
	reader, err := gzip.NewReader(bytes.NewReader(gzPayload))
	if err != nil {
		return nil, problem.Wrap(err, problem.ValidationFailed, "open bybit trades gz failed")
	}
	defer func() {
		_ = reader.Close()
	}()
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "decompress bybit trades gz failed")
	}
	return data, nil
}
