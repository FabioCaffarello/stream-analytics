# Client Multi-Exchange M2 All Widgets Functional (2026-02-27)

## Objective
Garantir funcionamento de todas as widgets no client (candle, stats, trade counter, trades, orderbook, heatmap, vpvr) com dados reais em native e web.

## Changes Applied
- `client/src/core/app/app.odin`
  - layout atualizado para incluir linha dedicada de analytics com:
    - `heatmap_widget`
    - `vpvr_widget`
  - responsividade mantida para desktop e mobile.
- `client/src/platform/native/main.odin`
  - subscriptions padrão/multi incluem `Heatmaps` e `VPVR`.
- `client/src/platform/web/main.odin`
  - subscriptions web incluem `Heatmaps` e `VPVR`.

## Validation Commands
```bash
make -C client check-core
make -C client build-native
make -C client build-wasm
make smoke
make -C client run-native-compose NATIVE_FLAGS='--soak-seconds=20 --soak-log-ms=1000 --soak-multi'
make -C client serve SERVE_PORT=8091
```

## Results
- `check-core`: `PASS`
- `build-native`: `PASS`
- `build-wasm`: `PASS`
- `smoke`: `PASS`
- native run-compose (20s): `PASS`
  - `subscriptions=18` (6 canais x 3 venues no modo `--soak-multi`)
  - ACKs recebidos para:
    - `marketdata.trade`
    - `marketdata.bookdelta`
    - `aggregation.stats`
    - `insights.heatmap_snapshot`
    - `insights.volume_profile_snapshot`
    - `aggregation.candle`

## Playwright MCP (web 8091, build atual)
- URL: `http://127.0.0.1:8091/?venue=binance&symbol=BTCUSDT&market_type=SPOT`
- ACKs confirmados:
  - `marketdata.trade/binance/BTCUSDT:SPOT/raw`
  - `marketdata.bookdelta/binance/BTCUSDT:SPOT/raw`
  - `aggregation.stats/binance/BTCUSDT:SPOT/raw`
  - `insights.heatmap_snapshot/binance/BTCUSDT:SPOT/1m`
  - `insights.volume_profile_snapshot/binance/BTCUSDT:SPOT/1m`
  - `aggregation.candle/binance/BTCUSDT:SPOT/raw`

## Notes
- Erro de `favicon.ico` (404) observado no console web; sem impacto funcional no client.
- Próximo passo: adicionar assert automatizado Playwright para presença/atualização visual de cada painel (M4 gate recorrente).
