package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"marketmonkey/config"
	"marketmonkey/event"
	"marketmonkey/pkg/db"
	"marketmonkey/pkg/db/clickhouse"
	"marketmonkey/pkg/db/timescale"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
	"github.com/valyala/fastjson"
)

var (
	candleLimit = 1000
)

func main() {
	exchange := flag.String("exchange", "", "Exchange name (binance)")
	symbol := flag.String("symbol", "", "Trading symbol (e.g. BTCUSDT)")
	data := flag.String("data", "", "Data to fetch (candles, historical_candles, check_gaps, orderbook)")
	days := flag.Int("days", 30, "Number of days to fetch for historical_candles (default: 30)")
	flag.Parse()

	if err := godotenv.Load(); err != nil {
		log.Fatal(err)
	}

	if *exchange == "" || *symbol == "" || *data == "" {
		log.Fatal("missing params: exchange, symbol, and data must be specified\n" +
			"data options: candles (latest 1000 candles), historical_candles (historical 1-minute candles), check_gaps (check for gaps in candle data)")
	}

	store, err := createDbClient()
	if err != nil {
		log.Fatal(err)
	}
	// connStr := "postgres://postgres:marketmonkey@127.0.0.1:5432/postgres?sslmode=disable"
	// conn, err := pgx.Connect(context.Background(), connStr)
	// if err != nil {
	// 	log.Fatal(err)
	// }

	switch *exchange {
	case config.Binancef:
		handleBinance(*symbol, *data, store, *days)
	default:
		log.Fatalf("Unsupported exchange: %s", *exchange)
	}
}

func handleBinance(symbol string, data string, store db.Client, days int) {
	api := NewBinanceFuturesApi(store)
	switch data {
	case "weekly_candles", "historical_candles":
		if err := api.FetchHistoricalCandles(symbol, store, days); err != nil {
			log.Fatal(err)
		}
	case "check_gaps":
		if err := CheckCandleGaps(symbol, store); err != nil {
			log.Fatal(err)
		}
	case "orderbook":
		if err := api.GetOrderbook(symbol, 500); err != nil {
			log.Fatal(err)
		}
	case "trades":
		if err := api.FetchHistoricalTrades(symbol, store, days); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("Unsupported data for Binance: %s", data)
	}
}

func parseFloat(b []byte) float64 {
	v, _ := fastjson.ParseBytes(b)
	return v.GetFloat64()
}

// GetKlinesInRange fetches candles for a specific time range

// CheckCandleGaps queries all candles for a pair and checks for gaps in 1-minute intervals
func CheckCandleGaps(symbol string, store db.Client) error {
	pair := event.NewPair(config.Binancef, strings.ToLower(symbol))

	// Get the first and last candle to determine the range
	firstCandle, err := store.GetFirstCandle(context.Background(), pair)
	if err != nil {
		return fmt.Errorf("failed to get first candle: %w", err)
	}
	if firstCandle == nil {
		return fmt.Errorf("no candles found for %s", symbol)
	}

	lastCandle, err := store.GetLastCandle(context.Background(), pair)
	if err != nil {
		return fmt.Errorf("failed to get last candle: %w", err)
	}

	slog.Info("checking for gaps",
		"symbol", symbol,
		"first_candle", firstCandle.Unix,
		"last_candle", lastCandle.Unix,
		"time_range", fmt.Sprintf("%s to %s",
			time.Unix(firstCandle.Unix, 0).Format(time.RFC3339),
			time.Unix(lastCandle.Unix, 0).Format(time.RFC3339)),
	)

	allCandles, err := store.GetAllCandles(pair)
	if err != nil {
		return fmt.Errorf("failed to get all candles: %w", err)
	}
	slog.Info("retrieved candles", "count", len(allCandles))

	timestamps := make([]int64, len(allCandles))
	for i, candle := range allCandles {
		timestamps[i] = candle.Unix
	}

	// Check for gaps
	var gaps []struct {
		start int64
		end   int64
		count int
	}

	// Expected interval is 60 seconds (1 minute)
	const expectedInterval = 60

	for i := 1; i < len(timestamps); i++ {
		current := timestamps[i]
		previous := timestamps[i-1]
		diff := current - previous

		if diff > expectedInterval {
			// Found a gap
			gapStart := previous + expectedInterval
			gapEnd := current - expectedInterval
			missingCount := int((current-previous)/expectedInterval) - 1

			gaps = append(gaps, struct {
				start int64
				end   int64
				count int
			}{
				start: gapStart,
				end:   gapEnd,
				count: missingCount,
			})
		}
	}

	// Report gaps
	if len(gaps) == 0 {
		slog.Info("no gaps found in candle data")
	} else {
		slog.Info("found gaps in candle data", "gap_count", len(gaps), "total_missing_candles", sumMissingCandles(gaps))

		// Print details for the first 10 gaps
		maxGapsToShow := 10
		if len(gaps) < maxGapsToShow {
			maxGapsToShow = len(gaps)
		}

		for i := 0; i < maxGapsToShow; i++ {
			gap := gaps[i]
			slog.Info("gap details",
				"index", i+1,
				"start_time", time.Unix(gap.start, 0).Format(time.RFC3339),
				"end_time", time.Unix(gap.end, 0).Format(time.RFC3339),
				"missing_candles", gap.count,
			)
		}

		if len(gaps) > maxGapsToShow {
			slog.Info("more gaps exist", "remaining_gaps", len(gaps)-maxGapsToShow)
		}
	}

	return nil
}

// Helper function to sum up the total number of missing candles
func sumMissingCandles(gaps []struct {
	start int64
	end   int64
	count int
}) int {
	total := 0
	for _, gap := range gaps {
		total += gap.count
	}
	return total
}

// by default we use timescaledb
// you can switch to clickhouse using env key
// DATABASE_ENGINE=clickhouse
func createDbClient() (db.Client, error) {
	engine := os.Getenv("DATABASE_ENGINE")
	if len(engine) == 0 {
		engine = "timescaledb"
	}

	if engine == "timescaledb" {
		addr := os.Getenv("TIMESCALE_ADDR")
		dbName := os.Getenv("TIMESCALE_DB")
		username := os.Getenv("TIMESCALE_USER")
		password := os.Getenv("TIMESCALE_PASSWORD")
		if len(addr) == 0 || len(dbName) == 0 || len(username) == 0 || len(password) == 0 {
			return nil, fmt.Errorf("you FORGOT to set the TIMESCALE_ADDR, TIMESCALE_DB, TIMESCALE_USER, TIMESCALE_PASSWORD in the environment")
		}
		conn, err := pgx.Connect(context.Background(), fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable", username, password, addr, dbName))
		if err != nil {
			return nil, err
		}
		return timescale.NewClient(conn)
	}

	if engine == "clickhouse" {
		addr := os.Getenv("CLICKHOUSE_ADDR")
		dbName := os.Getenv("CLICKHOUSE_DB")
		username := os.Getenv("CLICKHOUSE_USER")
		password := os.Getenv("CLICKHOUSE_PASSWORD")
		if len(addr) == 0 || len(dbName) == 0 || len(username) == 0 || len(password) == 0 {
			return nil, fmt.Errorf("you FORGOT to set the CLICKHOUSE_ADDR, CLICKHOUSE_DB, CLICKHOUSE_USER, CLICKHOUSE_PASSWORD in the environment")
		}
		return clickhouse.NewClient(addr, dbName, username, password)
	}
	return nil, fmt.Errorf("invalid database engine: %s", engine)
}
