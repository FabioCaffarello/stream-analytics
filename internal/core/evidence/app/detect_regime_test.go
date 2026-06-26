package app

import (
	"math"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/evidence/domain"
)

func TestTrendRegimeDetector_Trending(t *testing.T) {
	detector := NewTrendRegimeDetector(DefaultTrendPolicy())
	candles := make([]domain.RegimeCandleSample, 0, 20)
	for i := 0; i < 20; i++ {
		closePrice := 100.0 + float64(i)
		candles = append(candles, makeCandle(i, closePrice, 200.0))
	}

	signal, ok := detector.Detect(testRegimeKey(), candles)
	if !ok {
		t.Fatal("expected trending signal")
	}
	if signal.Kind != domain.RegimeTrending {
		t.Fatalf("kind=%s want=%s", signal.Kind, domain.RegimeTrending)
	}
	if signal.Strength < 0.8 {
		t.Fatalf("strength=%0.4f want>=0.8", signal.Strength)
	}
}

func TestTrendRegimeDetector_Ranging(t *testing.T) {
	detector := NewTrendRegimeDetector(DefaultTrendPolicy())
	candles := make([]domain.RegimeCandleSample, 0, 20)
	for i := 0; i < 20; i++ {
		closePrice := 100.0
		if i%2 == 0 {
			closePrice = 100.05
		}
		candles = append(candles, makeCandle(i, closePrice, 150.0))
	}

	signal, ok := detector.Detect(testRegimeKey(), candles)
	if !ok {
		t.Fatal("expected ranging signal")
	}
	if signal.Kind != domain.RegimeRanging {
		t.Fatalf("kind=%s want=%s", signal.Kind, domain.RegimeRanging)
	}
}

func TestVolatilityRegimeDetector_HighAndLow(t *testing.T) {
	detector := NewVolatilityRegimeDetector(DefaultVolatilityPolicy())

	highCandles := make([]domain.RegimeCandleSample, 0, 16)
	for i := 0; i < 16; i++ {
		highCandles = append(highCandles, domain.RegimeCandleSample{
			TsServer:    int64(i+1) * 60_000,
			WindowStart: int64(i) * 60_000,
			WindowEnd:   int64(i+1) * 60_000,
			Open:        100,
			High:        110,
			Low:         90,
			Close:       100,
			Volume:      120,
		})
	}
	highSignal, ok := detector.Detect(testRegimeKey(), highCandles)
	if !ok {
		t.Fatal("expected high volatility signal")
	}
	if highSignal.Kind != domain.RegimeHighVolatility {
		t.Fatalf("kind=%s want=%s", highSignal.Kind, domain.RegimeHighVolatility)
	}

	lowCandles := make([]domain.RegimeCandleSample, 0, 16)
	for i := 0; i < 16; i++ {
		lowCandles = append(lowCandles, domain.RegimeCandleSample{
			TsServer:    int64(i+1) * 60_000,
			WindowStart: int64(i) * 60_000,
			WindowEnd:   int64(i+1) * 60_000,
			Open:        100,
			High:        100.05,
			Low:         99.95,
			Close:       100,
			Volume:      120,
		})
	}
	lowSignal, ok := detector.Detect(testRegimeKey(), lowCandles)
	if !ok {
		t.Fatal("expected low volatility signal")
	}
	if lowSignal.Kind != domain.RegimeLowVolatility {
		t.Fatalf("kind=%s want=%s", lowSignal.Kind, domain.RegimeLowVolatility)
	}
}

func TestBreakoutRegimeDetector_Breakout(t *testing.T) {
	detector := NewBreakoutRegimeDetector(DefaultBreakoutPolicy())
	candles := make([]domain.RegimeCandleSample, 0, 21)
	for i := 0; i < 20; i++ {
		closePrice := 100.0 + float64(i%2)*0.05
		candles = append(candles, makeCandle(i, closePrice, 100.0))
	}
	candles = append(candles, makeCandle(20, 101.8, 320.0))

	signal, ok := detector.Detect(testRegimeKey(), candles)
	if !ok {
		t.Fatal("expected breakout signal")
	}
	if signal.Kind != domain.RegimeBreakout {
		t.Fatalf("kind=%s want=%s", signal.Kind, domain.RegimeBreakout)
	}
	if signal.Strength < 0.6 {
		t.Fatalf("strength=%0.4f want>=0.6", signal.Strength)
	}

	signal2, ok := detector.Detect(testRegimeKey(), candles)
	if !ok {
		t.Fatal("expected deterministic breakout signal on second run")
	}
	if signal2.Kind != signal.Kind {
		t.Fatalf("second kind=%s want=%s", signal2.Kind, signal.Kind)
	}
	if math.Abs(signal2.Strength-signal.Strength) > 1e-12 {
		t.Fatalf("strength mismatch second=%0.12f first=%0.12f", signal2.Strength, signal.Strength)
	}
}

func makeCandle(i int, closePrice, volume float64) domain.RegimeCandleSample {
	start := int64(i+1) * 60_000
	end := int64(i+2) * 60_000
	return domain.RegimeCandleSample{
		TsServer:    end,
		WindowStart: start,
		WindowEnd:   end,
		Open:        closePrice,
		High:        closePrice + 0.1,
		Low:         closePrice - 0.1,
		Close:       closePrice,
		Volume:      volume,
	}
}

func testRegimeKey() domain.RegimeStoreKey {
	return domain.RegimeStoreKey{Venue: "binance", Instrument: "BTCUSDT", Timeframe: "1m"}
}
