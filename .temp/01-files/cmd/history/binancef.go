package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"marketmonkey/common"
	"marketmonkey/config"
	"marketmonkey/event"
	"marketmonkey/pkg/db"
	"math"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/valyala/fastjson"
)

type BinanceFuturesApi struct {
	apiEndpoint string
	store       db.Client
}

func NewBinanceFuturesApi(store db.Client) *BinanceFuturesApi {
	return &BinanceFuturesApi{
		apiEndpoint: "https://fapi.binance.com/fapi",
		store:       store,
	}
}

// FetchHistoricalCandles fetches historical 1-minute candles from Binance Futures API
// and inserts them into the database. It handles rate limits and pagination properly.
// The function fetches candles in batches of 1000 (API limit) and respects the rate limit
// of 1,200 weight units per minute.
func (api *BinanceFuturesApi) FetchHistoricalCandles(symbol string, store db.Client, days int) error {
	pair := event.NewPair(config.Binancef, strings.ToLower(symbol))

	firstCandle, err := store.GetFirstCandle(context.Background(), pair)
	if err != nil {
		return fmt.Errorf("failed to check for existing candles: %w", err)
	}
	if firstCandle == nil {
		return fmt.Errorf("no existing candles found")
	}

	const (
		requestWeight     = 1
		weightLimitPerMin = 1200
		maxRequestsPerMin = weightLimitPerMin / requestWeight
		requestInterval   = time.Minute / time.Duration(maxRequestsPerMin)
	)

	// Use a rate limiter to control request frequency
	rateLimiter := time.NewTicker(requestInterval)
	defer rateLimiter.Stop()

	// Track total candles fetched
	totalCandles := 0

	endUnix := firstCandle.Unix
	startUnix := endUnix - (1000 * 60)
	const candleLimit = 1000
	for totalCandles < days*1440 { // Wait for rate limiter
		<-rateLimiter.C

		candles, volumes, err := api.getKlinesInRange(store, pair, "1m", startUnix*1000, endUnix*1000, candleLimit)
		if err != nil {
			return fmt.Errorf("failed to fetch candles: %w", err)
		}

		if len(candles) == 0 || len(volumes) == 0 {
			fmt.Println("failed to fetch candles from binance fapi", err)
			break
		}

		if err := store.InsertCandles(context.Background(), pair, candles); err != nil {
			return fmt.Errorf("failed to insert candles: %w", err)
		}
		if err := store.InsertVolumes(context.Background(), volumes); err != nil {
			return fmt.Errorf("failed to insert volumes: %w", err)
		}

		candle, err := store.GetFirstCandle(context.Background(), pair)
		if err != nil {
			return fmt.Errorf("failed to get first candle: %w", err)
		}
		slog.Info("fetched", "from", candles[0].Unix, "to", candles[len(candles)-1].Unix, "first", candle.Unix)
		totalCandles += len(candles)

		endUnix = startUnix
		startUnix = endUnix - (1000 * 60)
	}

	slog.Info("completed fetching historical candles",
		"symbol", symbol,
		"days", days,
		"total_candles", totalCandles)
	return nil
}

// https://binance-docs.github.io/apidocs/futures/en/#kline-candlestick-data
// symbol: BTCUSDT
// interval: 1m
// limit: 1000
func (api *BinanceFuturesApi) getKlinesInRange(store db.Client, pair *event.Pair, interval string, startTime int64, endTime int64, limit int) ([]*event.Candle, []*event.Volume, error) {
	if limit == 0 {
		limit = 1000
	}

	// Build URL with startTime and endTime parameters
	url := fmt.Sprintf("%s/v1/klines?symbol=%s&interval=%s&limit=%d&startTime=%d&endTime=%d",
		api.apiEndpoint, pair.Symbol, interval, limit, startTime, endTime)

	resp, err := http.Get(url)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	// Check for API errors
	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	parser := fastjson.Parser{}
	v, err := parser.ParseBytes(respBytes)
	if err != nil {
		slog.Error("failed to parse kline history", "err", err.Error())
		return nil, nil, err
	}

	firstVolume, err := store.GetFirstVolume(context.Background(), pair)
	if err != nil {
		return nil, nil, err
	}
	priceGroup := firstVolume.PriceGroup

	candles := []*event.Candle{}
	volumes := []*event.Volume{}
	for _, k := range v.GetArray() {
		arr := k.GetArray()
		if len(arr) < 11 {
			slog.Error("invalid kline format", "element", k.String())
			continue
		}
		// Parse required fields from the kline array
		candle := &event.Candle{
			Unix:  arr[0].GetInt64() / 1000, // Convert milliseconds to seconds
			Open:  parseFloat(arr[1].GetStringBytes()),
			High:  parseFloat(arr[2].GetStringBytes()),
			Low:   parseFloat(arr[3].GetStringBytes()),
			Close: parseFloat(arr[4].GetStringBytes()),
			Vbuy:  parseFloat(arr[9].GetStringBytes()),
			Vsell: parseFloat(arr[5].GetStringBytes()) - parseFloat(arr[9].GetStringBytes()),
			Tbuy:  float64(arr[8].GetInt64()),
			Tsell: 0,
			Final: true,
		}
		candles = append(candles, candle)

		volume := &event.Volume{
			Pair:       pair,
			Unix:       candle.Unix,
			Timeframe:  60,
			Prices:     []float64{},
			Buys:       []float64{},
			Sells:      []float64{},
			PriceGroup: priceGroup,
			Final:      true,
		}

		if candle.High > candle.Low {
			minPrice := common.RoundDown(candle.Low, priceGroup)
			maxPrice := common.RoundDown(candle.High, priceGroup) + priceGroup

			// Create a more realistic volume distribution based on price levels
			// Volumes tend to be higher near open/close prices and at significant levels

			// Initialize price levels with weights
			priceWeights := make(map[float64]float64)
			totalWeight := 0.0

			// Add weight to each price level
			for price := minPrice; price <= maxPrice; price += priceGroup {
				roundedPrice := common.RoundDown(price, priceGroup)

				// Base weight for each level
				weight := 1.0

				// Add more weight to levels near open and close prices
				openPrice := common.RoundDown(candle.Open, priceGroup)
				closePrice := common.RoundDown(candle.Close, priceGroup)

				// Higher weight near open price
				if math.Abs(roundedPrice-openPrice) < priceGroup*2 {
					weight += 2.0 - (math.Abs(roundedPrice-openPrice)/priceGroup)*0.5
				}

				// Higher weight near close price
				if math.Abs(roundedPrice-closePrice) < priceGroup*2 {
					weight += 2.0 - (math.Abs(roundedPrice-closePrice)/priceGroup)*0.5
				}

				// Use trade count to influence distribution
				// More trades usually means more activity at certain price levels
				if candle.Tbuy > 0 {
					// Scale weight by trade count - more trades = more weight variation
					tradeScale := math.Min(1.0, math.Log10(candle.Tbuy)/3.0)
					weight = 1.0 + (weight-1.0)*tradeScale
				}

				priceWeights[roundedPrice] = weight
				totalWeight += weight
			}

			// Distribute volumes according to weights
			for price, weight := range priceWeights {
				// Calculate volume proportion based on weight
				buyVolume := candle.Vbuy * (weight / totalWeight)
				sellVolume := candle.Vsell * (weight / totalWeight)

				// Add to existing price level or create new one
				if idx := common.IndexOfFloats(volume.Prices, price); idx != -1 {
					volume.Buys[idx] += buyVolume
					volume.Sells[idx] += sellVolume
				} else {
					volume.Prices = append(volume.Prices, price)
					volume.Buys = append(volume.Buys, buyVolume)
					volume.Sells = append(volume.Sells, sellVolume)
				}
			}

			// Sort prices and corresponding volume data
			if len(volume.Prices) > 1 {
				// Create temporary slices to hold sorted data
				type priceVolume struct {
					price float64
					buy   float64
					sell  float64
				}

				pvs := make([]priceVolume, len(volume.Prices))
				for i, price := range volume.Prices {
					pvs[i] = priceVolume{
						price: price,
						buy:   volume.Buys[i],
						sell:  volume.Sells[i],
					}
				}

				// Sort by price
				sort.Slice(pvs, func(i, j int) bool {
					return pvs[i].price < pvs[j].price
				})

				// Rebuild the sorted slices
				for i, pv := range pvs {
					volume.Prices[i] = pv.price
					volume.Buys[i] = pv.buy
					volume.Sells[i] = pv.sell
				}
			}
		} else {
			price := common.RoundDown(candle.Close, priceGroup)
			volume.Prices = append(volume.Prices, price)
			volume.Buys = append(volume.Buys, candle.Vbuy)
			volume.Sells = append(volume.Sells, candle.Vsell)
		}

		for i := range volume.Buys {
			volume.Buys[i] = common.Round(volume.Buys[i])
			volume.Sells[i] = common.Round(volume.Sells[i])
		}

		volumes = append(volumes, volume)
	}

	return candles, volumes, nil
}

// FetchHistoricalTrades fetches historical trades from Binance Futures API
// The function fetches trades in batches and respects the rate limits
func (api *BinanceFuturesApi) FetchHistoricalTrades(symbol string, store db.Client, days int) error {
	const (
		requestWeight     = 20   // Weight for aggTrades API
		weightLimitPerMin = 1200 // Weight limit per minute
		maxRequestsPerMin = weightLimitPerMin / requestWeight
		requestInterval   = time.Minute / time.Duration(maxRequestsPerMin)
		tradesLimit       = 1000 // Maximum allowed by API
	)

	rateLimiter := time.NewTicker(requestInterval)
	defer rateLimiter.Stop()

	pair := event.NewPair(config.Binancef, strings.ToLower(symbol))

	// terri: fetch from db
	endTime := time.Now().UnixMilli()
	startTime := endTime - (int64(days) * 24 * 60 * 60 * 1000)

	var fromId int64 = 0
	firstBatch := true

	// Keep fetching until we've processed all trades in the time window
	for {
		<-rateLimiter.C

		// Create parameters
		params := &aggTradeParams{
			Symbol: strings.ToUpper(pair.Symbol),
			Limit:  tradesLimit,
		}

		if firstBatch {
			params.StartTime = startTime
			slog.Info("fetching initial trades batch", "startTime", time.UnixMilli(startTime).Format(time.RFC3339))
		} else {
			params.FromID = fromId
		}

		// Get URL and fetch trades
		url := params.String(api.apiEndpoint)
		fmt.Println("url", url)
		trades, lastFromID, err := api.getAggTrades(url, pair)

		if err != nil {
			return fmt.Errorf("failed to fetch trades: %w", err)
		}

		fmt.Println("received trades", len(trades))

		// No more trades to fetch
		if len(trades) == 0 {
			if firstBatch {
				slog.Info("no trades found in the specified time range")
			}
			break
		}

		reachedEndTime := false
		for _, trade := range trades {
			tradeTimeMs := trade.Unix * 1000

			// If this trade's timestamp is greater than or equal to endTime,
			// we've reached the end of our time window
			if tradeTimeMs >= endTime {
				reachedEndTime = true
				slog.Info("reached end time, stopping fetch",
					"trade_time", time.UnixMilli(tradeTimeMs).Format(time.RFC3339),
					"end_time", time.UnixMilli(endTime).Format(time.RFC3339))
				break
			}

		}
		firstBatch = false

		// If we've reached a trade at or beyond our end time, or if we got fewer
		// trades than requested, we're done
		if reachedEndTime {
			break
		}

		// Prepare for next batch
		fromId = lastFromID + 1

		slog.Info("fetched trades",
			"symbol", symbol,
			"trades", len(trades),
			"last_id", lastFromID)
	}

	slog.Info("completed fetching historical trades",
		"symbol", symbol,
		"days", days,
	)
	return nil
}

type aggTradeParams struct {
	Symbol    string
	Limit     int
	FromID    int64
	StartTime int64
	EndTime   int64
}

func (p *aggTradeParams) String(baseURL string) string {
	url := fmt.Sprintf("%s/v1/aggTrades?symbol=%s&limit=%d", baseURL, p.Symbol, p.Limit)

	// We either use time-based parameters OR fromId, never both (Binance API constraint)
	if p.FromID > 0 {
		// FromId-based pagination
		url += fmt.Sprintf("&fromId=%d", p.FromID)
	} else if p.StartTime > 0 {
		// Time-based window
		url += fmt.Sprintf("&startTime=%d", p.StartTime)

		if p.EndTime > 0 {
			url += fmt.Sprintf("&endTime=%d", p.EndTime)
		}
	}

	return url
}

func (api *BinanceFuturesApi) getAggTrades(url string, pair *event.Pair) ([]*event.Trade, int64, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	// Check for API errors
	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return nil, 0, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}

	parser := fastjson.Parser{}
	v, err := parser.ParseBytes(respBytes)
	if err != nil {
		slog.Error("failed to parse aggTrade history", "err", err.Error())
		return nil, 0, err
	}

	trades := make([]*event.Trade, 0, len(v.GetArray()))
	var lastAggTradeId int64 = 0
	var lastAggUnix int64 = 0

	for _, t := range v.GetArray() {
		price, _ := strconv.ParseFloat(string(t.GetStringBytes("p")), 64)
		qty, _ := strconv.ParseFloat(string(t.GetStringBytes("q")), 64)
		timestamp := t.GetInt64("T")
		isBuyer := t.GetBool("m")
		aggTradeId := t.GetInt64("a")

		// Keep track of the highest trade ID
		if aggTradeId > lastAggTradeId {
			lastAggTradeId = aggTradeId
			lastAggUnix = timestamp / 1000
		}

		trade := &event.Trade{
			Pair:  pair,
			Price: price,
			Qty:   qty,
			IsBuy: !isBuyer,
			Unix:  timestamp / 1000,
		}
		trades = append(trades, trade)
	}
	fmt.Println("lastAggUnix", time.Unix(lastAggUnix, 0).Format(time.RFC3339))

	return trades, lastAggTradeId, nil
}

/*
*

	{
	  "lastUpdateId": 1027024,
	  "E": 1589436922972,   // Message output time
	  "T": 1589436922959,   // Transaction time
	  "bids": [
	    [
	      "4.00000000",     // PRICE
	      "431.00000000"    // QTY
	    ]
	  ],
	  "asks": [
	    [
	      "4.00000200",
	      "12.00000000"
	    ]
	  ]
	}
*/
func (api *BinanceFuturesApi) GetOrderbook(symbol string, limit int) error {
	limits := []int{5, 10, 20, 50, 100, 500, 1000}
	if !slices.Contains(limits, limit) {
		return fmt.Errorf("invalid limit: %d", limit)
	}
	url := fmt.Sprintf("%s/v1/depth?symbol=%s&limit=%d", api.apiEndpoint, symbol, limit)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch orderbook: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBytes))
	}
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	parser := fastjson.Parser{}
	v, err := parser.ParseBytes(respBytes)
	if err != nil {
		return fmt.Errorf("failed to parse orderbook data: %w", err)
	}

	lastUpdateID := v.GetInt64("lastUpdateId")
	messageTime := v.GetInt64("E")
	transactionTime := v.GetInt64("T")

	fmt.Printf("Orderbook for %s:\n", symbol)
	fmt.Printf("Last Update ID: %d\n", lastUpdateID)
	fmt.Printf("Message Time: %s\n", time.UnixMilli(messageTime).Format(time.RFC3339))
	fmt.Printf("Transaction Time: %s\n\n", time.UnixMilli(transactionTime).Format(time.RFC3339))

	bids := v.GetArray("bids")
	fmt.Printf("Top %d Bids:\n", len(bids))
	if len(bids) > 0 {
		fmt.Printf("%-15s %-15s\n", "Price", "Quantity")
		fmt.Println("------------------------------")
		for i, bid := range bids {
			if i >= 10 {
				fmt.Printf("... and %d more bids\n", len(bids)-10)
				break
			}
			bidArr := bid.GetArray()
			if len(bidArr) >= 2 {
				price := string(bidArr[0].GetStringBytes())
				qty := string(bidArr[1].GetStringBytes())
				fmt.Printf("%-15s %-15s\n", price, qty)
			}
		}
	} else {
		fmt.Println("No bids found.")
	}

	asks := v.GetArray("asks")
	fmt.Printf("\nTop %d Asks:\n", len(asks))
	if len(asks) > 0 {
		fmt.Printf("%-15s %-15s\n", "Price", "Quantity")
		fmt.Println("------------------------------")
		for i, ask := range asks {
			if i >= 10 {
				fmt.Printf("... and %d more asks\n", len(asks)-10)
				break
			}
			askArr := ask.GetArray()
			if len(askArr) >= 2 {
				price := string(askArr[0].GetStringBytes())
				qty := string(askArr[1].GetStringBytes())
				fmt.Printf("%-15s %-15s\n", price, qty)
			}
		}
	} else {
		fmt.Println("No asks found.")
	}

	return nil
}
