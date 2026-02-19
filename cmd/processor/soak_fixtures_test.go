package main

import (
	"fmt"
	"strings"
	"time"
)

func buildBinanceTradePayload(symbol string, price float64, qty float64, ts int64, tradeID int64) []byte {
	return []byte(fmt.Sprintf(
		`{"stream":"%s@aggTrade","data":{"e":"aggTrade","E":%d,"T":%d,"s":"%s","a":%d,"p":"%.8f","q":"%.8f","m":%t}}`,
		strings.ToLower(symbol), ts, ts, symbol, tradeID, price, qty, tradeID%2 == 0,
	))
}

func buildBinanceDepthPayload(symbol string, bids, asks [][]string, ts int64, finalID int64) []byte {
	firstID := finalID - 2
	if firstID < 1 {
		firstID = 1
	}
	prevFinal := firstID - 1
	if prevFinal < 0 {
		prevFinal = 0
	}
	return []byte(fmt.Sprintf(
		`{"e":"depthUpdate","E":%d,"s":"%s","U":%d,"u":%d,"pu":%d,"b":%s,"a":%s}`,
		ts,
		symbol,
		firstID,
		finalID,
		prevFinal,
		encode2DStrings(bids),
		encode2DStrings(asks),
	))
}

func buildBinanceMarkPricePayload(symbol string, markPrice, fundingRate float64, ts int64) []byte {
	indexPrice := markPrice - 2.0
	if indexPrice <= 0 {
		indexPrice = markPrice
	}
	return []byte(fmt.Sprintf(
		`{"stream":"%s@markPrice","data":{"e":"markPriceUpdate","E":%d,"s":"%s","p":"%.8f","i":"%.8f","r":"%.8f"}}`,
		strings.ToLower(symbol), ts, symbol, markPrice, indexPrice, fundingRate,
	))
}

func buildBinanceLiquidationPayload(symbol string, side string, price, qty float64, ts int64) []byte {
	normalizedSide := "SELL"
	if strings.EqualFold(side, "buy") {
		normalizedSide = "BUY"
	}
	return []byte(fmt.Sprintf(
		`{"e":"forceOrder","E":%d,"o":{"s":"%s","S":"%s","p":"%.8f","q":"%.8f","T":%d}}`,
		ts, symbol, normalizedSide, price, qty, ts,
	))
}

func buildBybitTradePayload(symbol string, price float64, qty float64, ts int64, tradeID string) []byte {
	side := "Buy"
	if strings.HasSuffix(tradeID, "1") || strings.HasSuffix(tradeID, "3") || strings.HasSuffix(tradeID, "5") {
		side = "Sell"
	}
	return []byte(fmt.Sprintf(
		`{"topic":"publicTrade.%s","type":"snapshot","ts":%d,"data":[{"T":%d,"s":"%s","S":"%s","v":"%.8f","p":"%.8f","i":"%s"}]}`,
		symbol, ts, ts, symbol, side, qty, price, tradeID,
	))
}

func buildBybitDepthPayload(symbol string, bids, asks [][]string, ts int64, finalID int64) []byte {
	prevFinal := finalID - 1
	if prevFinal < 0 {
		prevFinal = 0
	}
	return []byte(fmt.Sprintf(
		`{"topic":"orderbook.50.%s","type":"delta","ts":%d,"data":{"s":"%s","b":%s,"a":%s,"u":%d,"seq":%d,"pu":%d,"cts":%d}}`,
		symbol,
		ts,
		symbol,
		encode2DStrings(bids),
		encode2DStrings(asks),
		finalID,
		finalID,
		prevFinal,
		ts,
	))
}

func buildBybitTickerPayload(symbol string, markPrice, fundingRate float64, ts int64) []byte {
	indexPrice := markPrice - 1.5
	if indexPrice <= 0 {
		indexPrice = markPrice
	}
	return []byte(fmt.Sprintf(
		`{"topic":"tickers.%s","type":"snapshot","ts":%d,"data":{"symbol":"%s","markPrice":"%.8f","indexPrice":"%.8f","fundingRate":"%.8f"}}`,
		symbol, ts, symbol, markPrice, indexPrice, fundingRate,
	))
}

func buildBybitLiquidationPayload(symbol string, side string, price, qty float64, ts int64) []byte {
	normalizedSide := "Sell"
	if strings.EqualFold(side, "buy") {
		normalizedSide = "Buy"
	}
	return []byte(fmt.Sprintf(
		`{"topic":"liquidation.%s","type":"snapshot","ts":%d,"data":[{"s":"%s","S":"%s","v":"%.8f","p":"%.8f","T":%d}]}`,
		symbol, ts, symbol, normalizedSide, qty, price, ts,
	))
}

func buildCoinbaseMatchPayload(productID string, price, size float64, side string, ts time.Time, tradeID int64) []byte {
	return []byte(fmt.Sprintf(
		`{"type":"match","trade_id":%d,"product_id":"%s","price":"%.8f","size":"%.8f","side":"%s","time":"%s"}`,
		tradeID,
		productID,
		price,
		size,
		strings.ToLower(side),
		ts.UTC().Format(time.RFC3339Nano),
	))
}

func buildCoinbaseL2UpdatePayload(productID string, bids, asks [][]string, ts time.Time) []byte {
	changes := make([]string, 0, len(bids)+len(asks))
	for _, bid := range bids {
		if len(bid) != 2 {
			continue
		}
		changes = append(changes, fmt.Sprintf(`["buy","%s","%s"]`, bid[0], bid[1]))
	}
	for _, ask := range asks {
		if len(ask) != 2 {
			continue
		}
		changes = append(changes, fmt.Sprintf(`["sell","%s","%s"]`, ask[0], ask[1]))
	}
	if len(changes) == 0 {
		changes = append(changes, `["buy","1","1"]`)
	}
	return []byte(fmt.Sprintf(
		`{"type":"l2update","product_id":"%s","time":"%s","changes":[%s]}`,
		productID,
		ts.UTC().Format(time.RFC3339Nano),
		strings.Join(changes, ","),
	))
}

func buildCoinbaseTickerPayload(productID string, price float64, ts time.Time) []byte {
	return []byte(fmt.Sprintf(
		`{"type":"ticker","product_id":"%s","price":"%.8f","time":"%s"}`,
		productID,
		price,
		ts.UTC().Format(time.RFC3339Nano),
	))
}

func buildHyperLiquidTradePayload(coin string, side string, price, size float64, ts int64, hash string) []byte {
	hlSide := "B"
	if strings.EqualFold(side, "sell") {
		hlSide = "A"
	}
	return []byte(fmt.Sprintf(
		`{"channel":"trades","data":[{"coin":"%s","side":"%s","px":"%.8f","sz":"%.8f","time":%d,"hash":"%s","tid":%d}]}`,
		coin,
		hlSide,
		price,
		size,
		ts,
		hash,
		ts,
	))
}

func buildHyperLiquidL2BookPayload(coin string, bids, asks [][]string, ts int64) []byte {
	return []byte(fmt.Sprintf(
		`{"channel":"l2Book","data":{"coin":"%s","time":%d,"levels":[%s,%s]}}`,
		coin,
		ts,
		encodeHyperLevels(bids),
		encodeHyperLevels(asks),
	))
}

func encode2DStrings(levels [][]string) string {
	if len(levels) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(levels))
	for _, level := range levels {
		if len(level) != 2 {
			continue
		}
		parts = append(parts, fmt.Sprintf(`["%s","%s"]`, level[0], level[1]))
	}
	if len(parts) == 0 {
		return "[]"
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func encodeHyperLevels(levels [][]string) string {
	if len(levels) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(levels))
	for _, level := range levels {
		if len(level) != 2 {
			continue
		}
		parts = append(parts, fmt.Sprintf(`{"px":"%s","sz":"%s","n":1}`, level[0], level[1]))
	}
	if len(parts) == 0 {
		return "[]"
	}
	return "[" + strings.Join(parts, ",") + "]"
}
