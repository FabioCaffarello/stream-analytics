package main
import (
  "fmt"
  js "github.com/market-raccoon/internal/adapters/jetstream"
)
func main(){
  subs := []string{
    "marketdata.trade.v1.binance.BTCUSDT",
    "marketdata.trade.v1.binance.ETHUSDT",
    "marketdata.trade.v1.binance.SOLUSDT",
    "marketdata.trade.v1.binance.BNBUSDT",
    "marketdata.trade.v1.bybit.BTCUSDT",
    "marketdata.trade.v1.bybit.ETHUSDT",
    "marketdata.trade.v1.bybit.SOLUSDT",
    "marketdata.trade.v1.coinbase.BTCUSD",
    "marketdata.trade.v1.coinbase.ETHUSD",
    "marketdata.trade.v1.coinbase.SOLUSD",
    "marketdata.trade.v1.hyperliquid.BTCUSD",
    "marketdata.trade.v1.hyperliquid.ETHUSD",
    "marketdata.trade.v1.hyperliquid.SOLUSD",
  }
  for _, s := range subs {
    k := js.ShardKey(s)
    fmt.Printf("%s\tgroup2=%d\n", s, js.ShardGroup(k, 2))
  }
}
