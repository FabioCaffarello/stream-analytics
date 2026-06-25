package bybit

import (
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/market-raccoon/internal/contracts"
	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/replay"
)

func TestDownloadTrades_ParsesCSVCorrectly(t *testing.T) {
	tmp := t.TempDir()
	csvData := "timestamp,symbol,side,size,price,tickDirection,trdMatchID,grossValue,homeNotional,foreignNotional\n" +
		"1735689600.123,BTCUSDT,Buy,0.25,50000.1,ZeroPlusTick,abc001,12500.025,0.25,12500.025\n" +
		"1735689601.456,BTCUSDT,Sell,0.10,50001.2,MinusTick,abc002,5000.12,0.10,5000.12\n"

	restore := withMockBybitDownloader(t, func(_ context.Context, _ string, _ time.Time, _ string) ([]byte, *problem.Problem) {
		return gzipCSV(t, csvData), nil
	})
	defer restore()

	res, p := DownloadTrades(context.Background(), BackfillConfig{
		Symbol:     "BTCUSDT",
		From:       time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		To:         time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		OutputDir:  tmp,
		MarketType: "LINEAR",
	})
	if p != nil {
		t.Fatalf("DownloadTrades: %v", p)
	}
	if res.TradesParsed != 2 {
		t.Fatalf("TradesParsed=%d want=2", res.TradesParsed)
	}

	r, p := replay.NewReader(res.OutputPath)
	if p != nil {
		t.Fatalf("NewReader: %v", p)
	}
	defer func() {
		_ = r.Close()
	}()

	trade1 := readNextTrade(t, r)
	if trade1.Price != 50000.1 || trade1.Size != 0.25 || trade1.Side != "buy" || trade1.TradeID != "abc001" {
		t.Fatalf("trade=%+v", trade1)
	}
	if trade1.Timestamp != 1735689600123 {
		t.Fatalf("timestamp=%d want=1735689600123", trade1.Timestamp)
	}

	trade2 := readNextTrade(t, r)
	if trade2.Side != "sell" {
		t.Fatalf("side=%q want=sell", trade2.Side)
	}
}

func readNextTrade(t *testing.T, r *replay.Reader) marketdomain.TradeTickV1 {
	t.Helper()
	rec, ok, p := r.Next()
	if p != nil || !ok {
		t.Fatalf("Next: ok=%v p=%v", ok, p)
	}
	decoded, p := codec.DecodePayload(rec.Envelope.Type, rec.Envelope.Version, rec.Envelope.ContentType, rec.Envelope.Payload)
	if p != nil {
		t.Fatalf("DecodePayload: %v", p)
	}
	trade, ok := decoded.(marketdomain.TradeTickV1)
	if !ok {
		t.Fatalf("decoded type=%T", decoded)
	}
	return trade
}

func TestDownloadTrades_SkipsExistingCSV(t *testing.T) {
	tmp := t.TempDir()
	day := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	csvPath := filepath.Join(tmp, "BTCUSDT-2025-01-02-bybit.csv")
	if err := os.WriteFile(csvPath, []byte("1735776000.000,BTCUSDT,Buy,1,50000,ZeroPlusTick,t1,50000,1,50000\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	downloadCalls := 0
	restore := withMockBybitDownloader(t, func(_ context.Context, _ string, _ time.Time, _ string) ([]byte, *problem.Problem) {
		downloadCalls++
		return gzipCSV(t, "1735776001.000,BTCUSDT,Sell,1,50001,MinusTick,t2,50001,1,50001\n"), nil
	})
	defer restore()

	res, p := DownloadTrades(context.Background(), BackfillConfig{
		Symbol:     "BTCUSDT",
		From:       day,
		To:         day,
		OutputDir:  tmp,
		MarketType: "SPOT",
	})
	if p != nil {
		t.Fatalf("DownloadTrades: %v", p)
	}
	if downloadCalls != 0 {
		t.Fatalf("downloadCalls=%d want=0", downloadCalls)
	}
	if res.DatesSkipped != 1 || res.DatesDownloaded != 0 {
		t.Fatalf("result=%+v", res)
	}
}

func TestDownloadTrades_EmptyDateRange(t *testing.T) {
	tmp := t.TempDir()
	restore := withMockBybitDownloader(t, func(_ context.Context, _ string, _ time.Time, _ string) ([]byte, *problem.Problem) {
		t.Fatal("downloader should not be called")
		return nil, nil
	})
	defer restore()

	res, p := DownloadTrades(context.Background(), BackfillConfig{
		Symbol:     "BTCUSDT",
		From:       time.Date(2025, 1, 4, 0, 0, 0, 0, time.UTC),
		To:         time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC),
		OutputDir:  tmp,
		MarketType: "LINEAR",
	})
	if p != nil {
		t.Fatalf("DownloadTrades: %v", p)
	}
	if res.DatesDownloaded != 0 || res.DatesSkipped != 0 || res.TradesParsed != 0 {
		t.Fatalf("result=%+v", res)
	}
}

func TestDownloadTrades_ProducesReplayableFixture(t *testing.T) {
	tmp := t.TempDir()
	csvData := "1735862400.000,ETHUSDT,Buy,0.5,3200,PlusTick,t100,1600,0.5,1600\n" +
		"1735862401.500,ETHUSDT,Sell,0.8,3201,MinusTick,t101,2560.8,0.8,2560.8\n"

	restore := withMockBybitDownloader(t, func(_ context.Context, _ string, _ time.Time, _ string) ([]byte, *problem.Problem) {
		return gzipCSV(t, csvData), nil
	})
	defer restore()

	res, p := DownloadTrades(context.Background(), BackfillConfig{
		Symbol:     "ETHUSDT",
		From:       time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC),
		To:         time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC),
		OutputDir:  tmp,
		MarketType: "LINEAR",
	})
	if p != nil {
		t.Fatalf("DownloadTrades: %v", p)
	}

	player, p := replay.NewPlayer(res.OutputPath, nil, contracts.BootstrapPayloadCodecRegistry)
	if p != nil {
		t.Fatalf("NewPlayer: %v", p)
	}
	count := 0
	summary, p := player.Replay(context.Background(), func(_ context.Context, env envelope.Envelope) *problem.Problem {
		count++
		if env.Seq != int64(count) {
			t.Fatalf("seq=%d want=%d", env.Seq, count)
		}
		if env.Venue != "BYBIT" {
			t.Fatalf("venue=%q want=BYBIT", env.Venue)
		}
		return nil
	})
	if p != nil {
		t.Fatalf("Replay: %v", p)
	}
	if summary.InputCount != 2 || count != 2 {
		t.Fatalf("summary=%+v count=%d", summary, count)
	}
}

func TestBuildTradesURL(t *testing.T) {
	tests := []struct {
		name       string
		symbol     string
		date       time.Time
		marketType string
		want       string
	}{
		{
			name:       "linear",
			symbol:     "BTCUSDT",
			date:       time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
			marketType: "LINEAR",
			want:       "https://public.bybit.com/trading/BTCUSDT/BTCUSDT2025-01-15.csv.gz",
		},
		{
			name:       "spot",
			symbol:     "BTCUSDT",
			date:       time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
			marketType: "SPOT",
			want:       "https://public.bybit.com/spot/BTCUSDT/BTCUSDT2025-03-01.csv.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildTradesURL(tt.symbol, tt.date, tt.marketType)
			if got != tt.want {
				t.Fatalf("got=%q want=%q", got, tt.want)
			}
		})
	}
}

func withMockBybitDownloader(
	t *testing.T,
	fn func(ctx context.Context, symbol string, date time.Time, marketType string) ([]byte, *problem.Problem),
) func() {
	t.Helper()
	prev := backfillDownloadGz
	backfillDownloadGz = fn
	return func() {
		backfillDownloadGz = prev
	}
}

func gzipCSV(t *testing.T, csvBody string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write([]byte(csvBody)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}
