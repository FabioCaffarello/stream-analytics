package binance

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/replay"
)

//nolint:gocyclo // end-to-end parsing assertions are intentionally explicit.
func TestDownloadAggTrades_ParsesCSVCorrectly(t *testing.T) {
	tmp := t.TempDir()
	csvData := "agg_trade_id,price,quantity,first_trade_id,last_trade_id,transact_time,is_buyer_maker\n" +
		"1001,50000.1,0.25,1001,1001,1735689600000,false\n" +
		"1002,50001.2,0.10,1002,1002,1735689601000,true\n"

	restore := withMockBackfillDownloader(t, func(_ context.Context, _ string, _ time.Time, _ string) ([]byte, *problem.Problem) {
		return zipCSV(t, csvData), nil
	})
	defer restore()

	res, p := DownloadAggTrades(context.Background(), BackfillConfig{
		Symbol:     "BTCUSDT",
		From:       time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		To:         time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		OutputDir:  tmp,
		MarketType: marketTypeUSDMFutures,
	})
	if p != nil {
		t.Fatalf("DownloadAggTrades: %v", p)
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

	first, ok, p := r.Next()
	if p != nil || !ok {
		t.Fatalf("Next #1: ok=%v p=%v", ok, p)
	}
	decoded, p := codec.DecodePayload(first.Envelope.Type, first.Envelope.Version, first.Envelope.ContentType, first.Envelope.Payload)
	if p != nil {
		t.Fatalf("DecodePayload #1: %v", p)
	}
	trade, ok := decoded.(marketdomain.TradeTickV1)
	if !ok {
		t.Fatalf("decoded type=%T", decoded)
	}
	if trade.Price != 50000.1 || trade.Size != 0.25 || trade.Side != "buy" || trade.TradeID != "1001" || trade.Timestamp != 1735689600000 {
		t.Fatalf("trade=%+v", trade)
	}

	second, ok, p := r.Next()
	if p != nil || !ok {
		t.Fatalf("Next #2: ok=%v p=%v", ok, p)
	}
	decoded, p = codec.DecodePayload(second.Envelope.Type, second.Envelope.Version, second.Envelope.ContentType, second.Envelope.Payload)
	if p != nil {
		t.Fatalf("DecodePayload #2: %v", p)
	}
	trade, ok = decoded.(marketdomain.TradeTickV1)
	if !ok {
		t.Fatalf("decoded type=%T", decoded)
	}
	if trade.Side != "sell" {
		t.Fatalf("side=%q want=sell", trade.Side)
	}
}

func TestDownloadAggTrades_SkipsExistingCSV(t *testing.T) {
	tmp := t.TempDir()
	day := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	csvPath := filepath.Join(tmp, "BTCUSDT-2025-01-02.csv")
	if err := os.WriteFile(csvPath, []byte("1001,50000,1,1001,1001,1735776000000,false\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	downloadCalls := 0
	restore := withMockBackfillDownloader(t, func(_ context.Context, _ string, _ time.Time, _ string) ([]byte, *problem.Problem) {
		downloadCalls++
		return zipCSV(t, "1002,50001,1,1002,1002,1735776001000,false\n"), nil
	})
	defer restore()

	res, p := DownloadAggTrades(context.Background(), BackfillConfig{
		Symbol:     "BTCUSDT",
		From:       day,
		To:         day,
		OutputDir:  tmp,
		MarketType: marketTypeSpot,
	})
	if p != nil {
		t.Fatalf("DownloadAggTrades: %v", p)
	}
	if downloadCalls != 0 {
		t.Fatalf("downloadCalls=%d want=0", downloadCalls)
	}
	if res.DatesSkipped != 1 || res.DatesDownloaded != 0 {
		t.Fatalf("result=%+v", res)
	}
}

func TestDownloadAggTrades_ProducesValidFixture(t *testing.T) {
	tmp := t.TempDir()
	csvData := "2001,51000,0.5,2001,2001,1735862400000,false\n" +
		"2002,51001,0.8,2002,2002,1735862401000,true\n"

	restore := withMockBackfillDownloader(t, func(_ context.Context, _ string, _ time.Time, _ string) ([]byte, *problem.Problem) {
		return zipCSV(t, csvData), nil
	})
	defer restore()

	res, p := DownloadAggTrades(context.Background(), BackfillConfig{
		Symbol:     "ETHUSDT",
		From:       time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC),
		To:         time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC),
		OutputDir:  tmp,
		MarketType: marketTypeUSDMFutures,
	})
	if p != nil {
		t.Fatalf("DownloadAggTrades: %v", p)
	}

	player, p := replay.NewPlayer(res.OutputPath, nil)
	if p != nil {
		t.Fatalf("NewPlayer: %v", p)
	}
	count := 0
	summary, p := player.Replay(context.Background(), func(_ context.Context, env envelope.Envelope) *problem.Problem {
		count++
		if env.Seq != int64(count) {
			t.Fatalf("seq=%d want=%d", env.Seq, count)
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

func TestBackfill_ProducesValidFixture(t *testing.T) {
	TestDownloadAggTrades_ProducesValidFixture(t)
}

func TestDownloadAggTrades_EmptyDateRange(t *testing.T) {
	tmp := t.TempDir()
	restore := withMockBackfillDownloader(t, func(_ context.Context, _ string, _ time.Time, _ string) ([]byte, *problem.Problem) {
		t.Fatal("downloader should not be called")
		return nil, nil
	})
	defer restore()

	res, p := DownloadAggTrades(context.Background(), BackfillConfig{
		Symbol:     "BTCUSDT",
		From:       time.Date(2025, 1, 4, 0, 0, 0, 0, time.UTC),
		To:         time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC),
		OutputDir:  tmp,
		MarketType: marketTypeUSDMFutures,
	})
	if p != nil {
		t.Fatalf("DownloadAggTrades: %v", p)
	}
	if res.DatesDownloaded != 0 || res.DatesSkipped != 0 || res.TradesParsed != 0 {
		t.Fatalf("result=%+v", res)
	}
}

func TestSideFromIsBuyerMaker(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{name: "true means sell", input: "true", want: "sell", ok: true},
		{name: "false means buy", input: "false", want: "buy", ok: true},
		{name: "invalid", input: "x", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, p := sideFromIsBuyerMaker(tt.input)
			if tt.ok && p != nil {
				t.Fatalf("sideFromIsBuyerMaker: %v", p)
			}
			if !tt.ok && p == nil {
				t.Fatal("expected error")
			}
			if tt.ok && got != tt.want {
				t.Fatalf("got=%q want=%q", got, tt.want)
			}
		})
	}
}

func withMockBackfillDownloader(
	t *testing.T,
	fn func(ctx context.Context, symbol string, date time.Time, marketType string) ([]byte, *problem.Problem),
) func() {
	t.Helper()
	prev := backfillDownloadZip
	backfillDownloadZip = fn
	return func() {
		backfillDownloadZip = prev
	}
}

func zipCSV(t *testing.T, csvBody string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("sample.csv")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := w.Write([]byte(csvBody)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf.Bytes()
}
