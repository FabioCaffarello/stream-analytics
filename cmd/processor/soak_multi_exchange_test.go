//go:build soak
// +build soak

package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/market-raccoon/internal/adapters/exchange/binance"
	"github.com/market-raccoon/internal/adapters/exchange/bybit"
	"github.com/market-raccoon/internal/adapters/exchange/coinbase"
	"github.com/market-raccoon/internal/adapters/exchange/hyperliquid"
	aggapp "github.com/market-raccoon/internal/core/aggregation/app"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	mdapp "github.com/market-raccoon/internal/core/marketdata/app"
	mddomain "github.com/market-raccoon/internal/core/marketdata/domain"
)

//nolint:gocyclo // soak scenario intentionally exercises mixed parser and aggregation paths.
func TestSoak_MultiExchange_10M_Messages(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak test in short mode")
	}
	if os.Getenv(soakEnableEnv) != "1" {
		t.Skipf("set %s=1 to run soak tests", soakEnableEnv)
	}

	const (
		totalMessages          = 10_000_000
		maxBooks               = 4_096
		maxCandles             = 50_000
		maxWindows             = 50_000
		heapBudgetBytes uint64 = 1024 * 1024 * 1024
		runtimeBudget          = 120 * time.Second
	)

	tStart := time.Now()
	ctx := context.Background()
	pub := &soakArtifactPublisher{}
	svc := aggapp.NewAggregationService(aggapp.AggregationServiceConfig{
		Update: aggapp.UpdateConfig{MaxBooks: maxBooks},
		Candle: aggapp.BuildCandleConfig{MaxCandles: maxCandles},
		Stats:  aggapp.BuildStatsConfig{MaxWindows: maxWindows},

		Publisher:   pub,
		Store:       soakHotStore{},
		CandleStore: soakCandleStore{},
		StatsStore:  soakStatsStore{},
	})

	binanceSpotSymbols := buildLinearSymbols("SPOT", "USDT", 50)
	binanceFuturesSymbols := buildLinearSymbols("FUT", "USDT", 50)
	bybitSymbols := buildLinearSymbols("BYB", "USDT", 50)
	coinbaseProducts := buildCoinbaseProducts(50)
	hyperCoins := buildHyperCoins(50)

	seqByStream := make(map[string]int64, 256)
	latencies := make([]time.Duration, 0, 8192)
	var expectedCandleClosed int

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	beforeG := runtime.NumGoroutine()

	const baseTs = int64(1_735_689_600_000) // 2025-01-01T00:00:00Z

	for i := 0; i < totalMessages; i++ {
		evtStarted := time.Now()
		ts := baseTs + int64(i)*5
		exchangeBucket := i % 100

		switch {
		case exchangeBucket < 30:
			symbol := binanceSpotSymbols[(i/2)%len(binanceSpotSymbols)]
			seq := nextSeq(seqByStream, "BINANCE|SPOT|"+symbol)
			if i%2 == 0 {
				raw := buildBinanceTradePayload(symbol, 100.0+float64(i%100), 0.1+float64(i%10)/100.0, ts, seq)
				req, skip, p := binance.ParseMessageForMarketType(raw, time.UnixMilli(ts), mddomain.MarketTypeSpot.String())
				if p != nil || skip {
					t.Fatalf("binance spot trade parse failed at i=%d skip=%v p=%v", i, skip, p)
				}
				expectedCandleClosed += applyParsedIngestRequest(t, ctx, svc, req, seq)
			} else {
				raw := buildBinanceDepthPayload(symbol, [][]string{{"100.10", "1.5"}}, [][]string{{"100.20", "1.2"}}, ts, seq)
				req, skip, p := binance.ParseMessageForMarketType(raw, time.UnixMilli(ts), mddomain.MarketTypeSpot.String())
				if p != nil || skip {
					t.Fatalf("binance spot depth parse failed at i=%d skip=%v p=%v", i, skip, p)
				}
				expectedCandleClosed += applyParsedIngestRequest(t, ctx, svc, req, seq)
			}
		case exchangeBucket < 60:
			symbol := binanceFuturesSymbols[(i/2)%len(binanceFuturesSymbols)]
			seq := nextSeq(seqByStream, "BINANCE|FUT|"+symbol)
			selector := i % 20
			switch {
			case selector < 8:
				raw := buildBinanceTradePayload(symbol, 200.0+float64(i%200), 0.2+float64(i%10)/100.0, ts, seq)
				req, skip, p := binance.ParseMessageForMarketType(raw, time.UnixMilli(ts), mddomain.MarketTypeUSDMFutures.String())
				if p != nil || skip {
					t.Fatalf("binance fut trade parse failed at i=%d skip=%v p=%v", i, skip, p)
				}
				expectedCandleClosed += applyParsedIngestRequest(t, ctx, svc, req, seq)
			case selector < 14:
				raw := buildBinanceDepthPayload(symbol, [][]string{{"200.10", "2.5"}}, [][]string{{"200.20", "2.2"}}, ts, seq)
				req, skip, p := binance.ParseMessageForMarketType(raw, time.UnixMilli(ts), mddomain.MarketTypeUSDMFutures.String())
				if p != nil || skip {
					t.Fatalf("binance fut depth parse failed at i=%d skip=%v p=%v", i, skip, p)
				}
				expectedCandleClosed += applyParsedIngestRequest(t, ctx, svc, req, seq)
			case selector < 18:
				raw := buildBinanceMarkPricePayload(symbol, 200.5+float64(i%50), 0.0001, ts)
				req, skip, p := binance.ParseMessageForMarketType(raw, time.UnixMilli(ts), mddomain.MarketTypeUSDMFutures.String())
				if p != nil || skip {
					t.Fatalf("binance fut mark parse failed at i=%d skip=%v p=%v", i, skip, p)
				}
				expectedCandleClosed += applyParsedIngestRequest(t, ctx, svc, req, seq)
			default:
				side := "sell"
				if i%2 == 0 {
					side = "buy"
				}
				raw := buildBinanceLiquidationPayload(symbol, side, 199.8+float64(i%50), 0.3+float64(i%5)/10.0, ts)
				req, skip, p := binance.ParseMessageForMarketType(raw, time.UnixMilli(ts), mddomain.MarketTypeUSDMFutures.String())
				if p != nil || skip {
					t.Fatalf("binance fut liq parse failed at i=%d skip=%v p=%v", i, skip, p)
				}
				expectedCandleClosed += applyParsedIngestRequest(t, ctx, svc, req, seq)
			}
		case exchangeBucket < 80:
			symbol := bybitSymbols[(i/2)%len(bybitSymbols)]
			seq := nextSeq(seqByStream, "BYBIT|FUT|"+symbol)
			selector := i % 20
			switch {
			case selector < 8:
				raw := buildBybitTradePayload(symbol, 300.0+float64(i%120), 0.1+float64(i%10)/100.0, ts, fmt.Sprintf("%d", seq))
				req, skip, p := bybit.ParseMessageForMarketType(raw, time.UnixMilli(ts), mddomain.MarketTypeUSDMFutures.String())
				if p != nil || skip {
					t.Fatalf("bybit trade parse failed at i=%d skip=%v p=%v", i, skip, p)
				}
				expectedCandleClosed += applyParsedIngestRequest(t, ctx, svc, req, seq)
			case selector < 14:
				raw := buildBybitDepthPayload(symbol, [][]string{{"300.10", "1.5"}}, [][]string{{"300.20", "1.3"}}, ts, seq)
				req, skip, p := bybit.ParseMessageForMarketType(raw, time.UnixMilli(ts), mddomain.MarketTypeUSDMFutures.String())
				if p != nil || skip {
					t.Fatalf("bybit depth parse failed at i=%d skip=%v p=%v", i, skip, p)
				}
				expectedCandleClosed += applyParsedIngestRequest(t, ctx, svc, req, seq)
			case selector < 18:
				raw := buildBybitTickerPayload(symbol, 300.9+float64(i%70), 0.0002, ts)
				req, skip, p := bybit.ParseMessageForMarketType(raw, time.UnixMilli(ts), mddomain.MarketTypeUSDMFutures.String())
				if p != nil || skip {
					t.Fatalf("bybit ticker parse failed at i=%d skip=%v p=%v", i, skip, p)
				}
				expectedCandleClosed += applyParsedIngestRequest(t, ctx, svc, req, seq)
			default:
				side := "sell"
				if i%2 == 0 {
					side = "buy"
				}
				raw := buildBybitLiquidationPayload(symbol, side, 299.9+float64(i%70), 0.4+float64(i%5)/10.0, ts)
				req, skip, p := bybit.ParseMessageForMarketType(raw, time.UnixMilli(ts), mddomain.MarketTypeUSDMFutures.String())
				if p != nil || skip {
					t.Fatalf("bybit liquidation parse failed at i=%d skip=%v p=%v", i, skip, p)
				}
				expectedCandleClosed += applyParsedIngestRequest(t, ctx, svc, req, seq)
			}
		case exchangeBucket < 90:
			productID := coinbaseProducts[(i/2)%len(coinbaseProducts)]
			seq := nextSeq(seqByStream, "COINBASE|SPOT|"+productID)
			selector := i % 10
			switch {
			case selector < 5:
				raw := buildCoinbaseMatchPayload(productID, 400.0+float64(i%90), 0.05+float64(i%5)/100.0, "buy", time.UnixMilli(ts), seq)
				req, skip, p := coinbase.ParseMessageForMarketType(raw, time.UnixMilli(ts), mddomain.MarketTypeSpot.String())
				if p != nil || skip {
					t.Fatalf("coinbase match parse failed at i=%d skip=%v p=%v", i, skip, p)
				}
				expectedCandleClosed += applyParsedIngestRequest(t, ctx, svc, req, seq)
			case selector < 8:
				raw := buildCoinbaseL2UpdatePayload(productID, [][]string{{"400.10", "1.1"}}, [][]string{{"400.20", "1.2"}}, time.UnixMilli(ts))
				req, skip, p := coinbase.ParseMessageForMarketType(raw, time.UnixMilli(ts), mddomain.MarketTypeSpot.String())
				if p != nil || skip {
					t.Fatalf("coinbase l2 parse failed at i=%d skip=%v p=%v", i, skip, p)
				}
				expectedCandleClosed += applyParsedIngestRequest(t, ctx, svc, req, seq)
			default:
				raw := buildCoinbaseTickerPayload(productID, 401.0+float64(i%90), time.UnixMilli(ts))
				req, skip, p := coinbase.ParseMessageForMarketType(raw, time.UnixMilli(ts), mddomain.MarketTypeSpot.String())
				if p != nil || skip {
					t.Fatalf("coinbase ticker parse failed at i=%d skip=%v p=%v", i, skip, p)
				}
				expectedCandleClosed += applyParsedIngestRequest(t, ctx, svc, req, seq)
			}
		default:
			coin := hyperCoins[(i/2)%len(hyperCoins)]
			seq := nextSeq(seqByStream, "HYPERLIQUID|FUT|"+coin)
			if i%2 == 0 {
				raw := buildHyperLiquidTradePayload(coin, "buy", 500.0+float64(i%110), 0.07+float64(i%7)/100.0, ts, fmt.Sprintf("0x%x", seq))
				req, skip, p := hyperliquid.ParseMessageForMarketType(raw, time.UnixMilli(ts), mddomain.MarketTypeUSDMFutures.String())
				if p != nil || skip {
					t.Fatalf("hyperliquid trade parse failed at i=%d skip=%v p=%v", i, skip, p)
				}
				expectedCandleClosed += applyParsedIngestRequest(t, ctx, svc, req, seq)
			} else {
				raw := buildHyperLiquidL2BookPayload(coin, [][]string{{"500.10", "1.7"}}, [][]string{{"500.20", "1.4"}}, ts)
				req, skip, p := hyperliquid.ParseMessageForMarketType(raw, time.UnixMilli(ts), mddomain.MarketTypeUSDMFutures.String())
				if p != nil || skip {
					t.Fatalf("hyperliquid l2 parse failed at i=%d skip=%v p=%v", i, skip, p)
				}
				expectedCandleClosed += applyParsedIngestRequest(t, ctx, svc, req, seq)
			}
		}

		if i%2048 == 0 {
			latencies = append(latencies, time.Since(evtStarted))
		}
	}

	if got := svc.UpdateBook.ActiveBooks(); got > maxBooks {
		t.Fatalf("active books=%d exceeded max=%d", got, maxBooks)
	}
	if got := svc.Candle.ActiveCandles(); got > maxCandles {
		t.Fatalf("active candles=%d exceeded max=%d", got, maxCandles)
	}
	if got := svc.Stats.ActiveWindows(); got > maxWindows {
		t.Fatalf("active stats windows=%d exceeded max=%d", got, maxWindows)
	}
	if pub.candles != expectedCandleClosed {
		t.Fatalf("published candle closed=%d expected=%d", pub.candles, expectedCandleClosed)
	}
	if pub.stats <= 0 {
		t.Fatalf("expected stats publishes > 0, got=%d", pub.stats)
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	afterG := runtime.NumGoroutine()

	goroutineDrift := afterG - beforeG
	if goroutineDrift > 32 {
		t.Fatalf("goroutine drift too high: before=%d after=%d", beforeG, afterG)
	}

	var heapDelta uint64
	if after.HeapAlloc > before.HeapAlloc {
		heapDelta = after.HeapAlloc - before.HeapAlloc
	}
	if heapDelta > heapBudgetBytes {
		t.Fatalf("heap growth too high delta=%d bytes budget=%d", heapDelta, heapBudgetBytes)
	}

	duration := time.Since(tStart)
	if duration > runtimeBudget {
		t.Fatalf("runtime exceeded budget: duration=%s budget=%s", duration, runtimeBudget)
	}
	throughput := float64(totalMessages) / duration.Seconds()
	if throughput < 83_000 {
		t.Fatalf("throughput too low: %.2f events/sec", throughput)
	}

	p50, p95, p99 := latencyQuantiles(latencies)
	t.Logf("multi-exchange soak: runtime=%s throughput=%.2f/s p50=%s p95=%s p99=%s candles=%d stats=%d",
		duration,
		throughput,
		p50,
		p95,
		p99,
		pub.candles,
		pub.stats,
	)
}

func applyParsedIngestRequest(t *testing.T, ctx context.Context, svc *aggapp.AggregationService, req mdapp.IngestRequest, seq int64) int {
	t.Helper()

	switch payload := req.Payload.(type) {
	case mddomain.BookDeltaV1:
		result := svc.UpdateBook.Execute(ctx, aggapp.UpdateRequest{
			Venue:      strings.ToLower(req.Venue),
			Instrument: req.Instrument,
			Seq:        seq,
			Bids:       asAggLevels(payload.Bids),
			Asks:       asAggLevels(payload.Asks),
		})
		if result.IsFail() {
			t.Fatalf("update book failed: %v", result.Problem())
		}
		return 0
	case mddomain.TradeTickV1:
		candleResp, p := svc.Candle.Execute(ctx, aggapp.BuildCandleRequest{
			Venue:      strings.ToLower(req.Venue),
			Instrument: req.Instrument,
			Price:      payload.Price,
			Quantity:   payload.Size,
			IsBuy:      strings.EqualFold(payload.Side, "buy"),
			Seq:        seq,
			TsIngest:   payload.Timestamp,
		})
		if p != nil {
			t.Fatalf("build candle failed: %v", p)
		}
		return len(candleResp.Closed)
	case mddomain.MarkPriceTickV1:
		if _, p := svc.Stats.Execute(ctx, aggapp.BuildStatsRequest{
			Venue:      strings.ToLower(req.Venue),
			Instrument: req.Instrument,
			Kind:       aggapp.StatsInputMarkPrice,
			MarkPrice:  payload.MarkPrice,
			Seq:        seq,
			TsIngest:   payload.Timestamp,
		}); p != nil {
			t.Fatalf("build stats markprice failed: %v", p)
		}
		if payload.FundingRate != 0 {
			if _, p := svc.Stats.Execute(ctx, aggapp.BuildStatsRequest{
				Venue:       strings.ToLower(req.Venue),
				Instrument:  req.Instrument,
				Kind:        aggapp.StatsInputFundingRate,
				FundingRate: payload.FundingRate,
				Seq:         seq,
				TsIngest:    payload.Timestamp,
			}); p != nil {
				t.Fatalf("build stats funding failed: %v", p)
			}
		}
		return 0
	case mddomain.LiquidationTickV1:
		if _, p := svc.Stats.Execute(ctx, aggapp.BuildStatsRequest{
			Venue:           strings.ToLower(req.Venue),
			Instrument:      req.Instrument,
			Kind:            aggapp.StatsInputLiquidation,
			LiquidationSide: strings.ToLower(payload.Side),
			LiquidationQty:  payload.Size,
			Seq:             seq,
			TsIngest:        payload.Timestamp,
		}); p != nil {
			t.Fatalf("build stats liquidation failed: %v", p)
		}
		return 0
	default:
		t.Fatalf("unsupported parsed payload type: %T", payload)
		return 0
	}
}

func asAggLevels(levels []mddomain.PriceLevel) []aggdomain.Level {
	out := make([]aggdomain.Level, 0, len(levels))
	for _, level := range levels {
		out = append(out, aggdomain.Level{Price: aggdomain.Price(level.Price), Quantity: aggdomain.Quantity(level.Size)})
	}
	return out
}

func nextSeq(m map[string]int64, key string) int64 {
	m[key]++
	return m[key]
}

func buildLinearSymbols(prefix, quote string, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("%s%02d%s", prefix, i+1, quote))
	}
	return out
}

func buildCoinbaseProducts(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("CB%02d-USD", i+1))
	}
	return out
}

func buildHyperCoins(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("HL%02d", i+1))
	}
	return out
}

func latencyQuantiles(samples []time.Duration) (time.Duration, time.Duration, time.Duration) {
	if len(samples) == 0 {
		return 0, 0, 0
	}
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	p50 := sorted[len(sorted)/2]
	p95 := sorted[(len(sorted)-1)*95/100]
	p99 := sorted[(len(sorted)-1)*99/100]
	return p50, p95, p99
}
