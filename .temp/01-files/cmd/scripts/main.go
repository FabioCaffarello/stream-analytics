package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
)

type symbol struct {
	Name     string  `json:"name"`
	Ticksize float64 `json:"tickSize"`
}

func main() {
	url := "https://fapi.binance.com/fapi/v1/exchangeInfo"
	resp, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}

	defer resp.Body.Close()

	info := ExchangeInfo{}
	json.NewDecoder(resp.Body).Decode(&info)

	fmt.Println(info)

	symbols := make([]symbol, len(info.Symbols))
	for i, s := range info.Symbols {
		tickSize, err := strconv.ParseFloat(s.Filters[0].TickSize, 64)
		if err != nil {
			log.Fatal(err)
		}
		symbols[i] = symbol{Ticksize: tickSize, Name: strings.ToLower(s.BaseAsset) + "usdt"}
	}
	b, err := json.MarshalIndent(symbols, "  ", "  ")
	if err != nil {
		log.Fatal(err)
	}
	os.WriteFile("binancef.json", b, os.ModePerm)
}

// ExchangeInfo represents the top-level futures exchange information
type ExchangeInfo struct {
	Timezone        string        `json:"timezone"`
	ServerTime      int64         `json:"serverTime"`
	FuturesType     string        `json:"futuresType"`
	RateLimits      []RateLimit   `json:"rateLimits"`
	ExchangeFilters []interface{} `json:"exchangeFilters"`
	Assets          []Asset       `json:"assets"`
	Symbols         []Symbol      `json:"symbols"`
}

// RateLimit contains rate limiting information
type RateLimit struct {
	RateLimitType string `json:"rateLimitType"`
	Interval      string `json:"interval"`
	IntervalNum   int    `json:"intervalNum"`
	Limit         int    `json:"limit"`
}

// Asset represents a trading asset
type Asset struct {
	Asset             string `json:"asset"`
	MarginAvailable   bool   `json:"marginAvailable"`
	AutoAssetExchange string `json:"autoAssetExchange"`
}

// Symbol contains information about a trading pair
type Symbol struct {
	Symbol                string         `json:"symbol"`
	Pair                  string         `json:"pair"`
	ContractType          string         `json:"contractType"`
	DeliveryDate          int64          `json:"deliveryDate"`
	OnboardDate           int64          `json:"onboardDate"`
	Status                string         `json:"status"`
	MaintMarginPercent    string         `json:"maintMarginPercent"`
	RequiredMarginPercent string         `json:"requiredMarginPercent"`
	BaseAsset             string         `json:"baseAsset"`
	QuoteAsset            string         `json:"quoteAsset"`
	MarginAsset           string         `json:"marginAsset"`
	PricePrecision        int            `json:"pricePrecision"`
	QuantityPrecision     int            `json:"quantityPrecision"`
	BaseAssetPrecision    int            `json:"baseAssetPrecision"`
	QuotePrecision        int            `json:"quotePrecision"`
	UnderlyingType        string         `json:"underlyingType"`
	UnderlyingSubType     []string       `json:"underlyingSubType"`
	TriggerProtect        string         `json:"triggerProtect"`
	LiquidationFee        string         `json:"liquidationFee"`
	MarketTakeBound       string         `json:"marketTakeBound"`
	MaxMoveOrderLimit     int            `json:"maxMoveOrderLimit"`
	Filters               []SymbolFilter `json:"filters"`
	OrderTypes            []string       `json:"orderTypes"`
	TimeInForce           []string       `json:"timeInForce"`
}

// SymbolFilter represents various filters applied to symbols
type SymbolFilter struct {
	FilterType        string `json:"filterType"`
	MinPrice          string `json:"minPrice,omitempty"`
	MaxPrice          string `json:"maxPrice,omitempty"`
	TickSize          string `json:"tickSize,omitempty"`
	MaxQty            string `json:"maxQty,omitempty"`
	MinQty            string `json:"minQty,omitempty"`
	StepSize          string `json:"stepSize,omitempty"`
	Limit             int    `json:"limit,omitempty"`
	Notional          string `json:"notional,omitempty"`
	MultiplierUp      string `json:"multiplierUp,omitempty"`
	MultiplierDown    string `json:"multiplierDown,omitempty"`
	MultiplierDecimal string `json:"multiplierDecimal,omitempty"`
}

// Example usage:
// var info ExchangeInfo
// err := json.Unmarshal(data, &info)
