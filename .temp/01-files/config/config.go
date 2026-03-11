package config

import (
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	Binancef    = "binancef"
	Binance     = "binance"
	Bybit       = "bybit"
	Hyperliquid = "hyperliquid"
	Coinbase    = "coinbase"
)

var cfg conf

type conf struct {
	Version   string   `yaml:"version" json:"version"`
	Auth      bool     `yaml:"auth" json:"auth"`
	Intervals []int64  `yaml:"intervals" json:"intervals"`
	Markets   []Market `yaml:"markets" json:"markets"`
}

func Get() conf {
	return cfg
}

func Intervals() []int64 {
	return cfg.Intervals
}

func GetMarket(market string) (Market, bool) {
	for _, m := range cfg.Markets {
		if m.Name == market {
			return m, true
		}
	}
	return Market{}, false
}

type Market struct {
	Name     string   `yaml:"name" json:"name"`
	Type     string   `yaml:"type" json:"type"`
	Category string   `yaml:"category" json:"category"`
	Stats    bool     `yaml:"stats" json:"stats"`
	Symbols  []Symbol `yaml:"symbols" json:"symbols"`
}

func (m Market) GetSymbol(ticker string) (Symbol, bool) {
	for _, sym := range m.Symbols {
		if sym.Ticker == ticker {
			return sym, true
		}
	}
	return Symbol{}, false
}

type Symbol struct {
	Ticker   string  `yaml:"ticker" json:"ticker"`
	Base     string  `yaml:"base" json:"base"`
	Quote    string  `yaml:"quote" json:"quote"`
	TickSize float64 `yaml:"tickSize" json:"tickSize"`
}

func parseFromFile() {
	b, err := os.ReadFile("config.yml")
	if err != nil {
		log.Fatal("failed to read config from file", err)
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		log.Fatal("failed to parse config", err)
	}
}

func init() {
	parseFromFile()
}
