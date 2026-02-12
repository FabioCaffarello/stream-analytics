# Market Raccoon

[![workspace-ci](https://github.com/market-raccoon/market-raccoon/actions/workflows/ci.yml/badge.svg)](https://github.com/market-raccoon/market-raccoon/actions/workflows/ci.yml)

## Docker Quick Start

Run the full local stack (NATS/JetStream + server + consumer + processor):

```bash
make up
```

Infra-only bootstrap (NATS/JetStream):

```bash
make up-infra
```

Useful operational commands:

```bash
make ps
make logs
make down
```

Compose entrypoint and deploy assets:
- `deploy/compose/docker-compose.yml`
- `deploy/nats/nats-server.conf`
- `deploy/configs/*.jsonc`
- `deploy/docker/*.Dockerfile`

## Binance Ingestion (Current Scope)

Current Binance WS subscriptions are intentionally limited to:
- `aggTrade`
- `depth@100ms`

For each configured ticker, the consumer builds a combined stream endpoint like:
- `wss://stream.binance.com:9443/stream?streams=btcusdt@aggTrade/btcusdt@depth@100ms`

### Supported vs Ignored Events

Supported and published by the current pipeline:
- `aggTrade` -> `marketdata.trade`
- `depthUpdate` -> `marketdata.bookdelta`

Explicitly ignored (for now), with telemetry:
- unknown/unsupported Binance event types (for example `bookTicker`, `kline`, etc.)
- control/empty envelope messages
- invalid JSON / parse failures

### Runtime Telemetry

`mdruntime` emits periodic counters (every 100 messages):
- `total`, `ingested`, `skipped`
- `by_event`
- `skip_by_reason`
- `skip_by_exchange_event_reason`
- `parse_error_by_code`

Parse skips with reason `parse_error` also emit sampled warning logs (rate-limited) with:
- exchange/bucket/consumer/endpoint
- event type and skip reason
- problem code/message
- truncated payload sample

### Consumer JSONC knobs

Main consumer settings in `deploy/configs/consumer.jsonc`:
- `consumer.exchange` (must be `binance`)
- `consumer.tickers`
- `consumer.binance_ws_base_url`
- `consumer.streams_per_ticker`
- `consumer.max_streams_per_websocket`
- `consumer.max_websockets`
- `consumer.max_websocket_lifetime`
- `consumer.respawn_overlap`
