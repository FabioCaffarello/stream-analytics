# Client Architecture (Market-Raccoon)

Status: active
Owner: client/core
Last updated: 2026-03-02

## Goal

Freeze a drift-proof, multi-exchange client architecture with strict anti-coupling boundaries:

- `core/*` owns state, stream orchestration, protocol parsing, normalization, and widget data contracts.
- `platform/*` owns transport + IO only (native/web specifics).
- widgets render from stores/stream handles only; widgets do not parse raw payloads and do not perform IO.

## Module Map

- `client/src/core/app`
  - app lifecycle, subscription reconcile, stream view state, UI actions.
- `client/src/core/streams`
  - stream registry/controller/status, stream id model, endpoint capabilities.
- `client/src/core/services`
  - parser + parse arena, stores (trade/orderbook/stats/candle/heatmap/vpvr), settings/profile services.
- `client/src/core/widgets`
  - pure render logic from stores/handles.
- `client/src/platform/native`
  - websocket/native runtime integration; pushes parsed events to core port contract.
- `client/src/platform/web`
  - wasm/js bridge integration; pushes parsed events to core port contract.

## Cross-Layer Ports

Only `core/ports/*` crosses core/platform boundary.

- `Marketdata_Port`
  - subscribe/unsubscribe/poll/metrics/describe/reconnect/getrange.
- `Settings_Port`
  - load/save/flush persisted settings.
- `Text_Port` / `Font_Port` / `Input_Port`
  - rendering/input abstractions.

Rules:

1. `core` must not import platform packages.
2. `platform` must not mutate core stores directly.
3. parser is shared in `core/services`; platform adapters only feed raw bytes and consume parsed result.

## Data Flow

`ws (platform)` -> `message_parser` -> `normalize (ports.MD_Event)` -> `stores` -> `widgets`

- transport IO: `platform/native|web/marketdata_*`
- parsing/validation: `core/services/message_parser.odin`
- stream control + health: `core/streams/*` + `core/app/health.odin`
- rendering: `core/widgets/*`

## stream_id and Topics Contract

### stream_id

Canonical id format:

- `stream://{venue}/{symbol[:market_type]}`

Examples:

- `stream://binance/BTCUSDT:SPOT`
- `stream://bybit/BTCUSDT:USDMFUTURES`

`stream_id` identity is venue+symbol(+market_type), independent of topic.

### topics

Normalized topic set (`MD_Channel`):

- `Trades` -> `marketdata.trade/{venue}/{symbol}/raw`
- `Orderbook` -> `marketdata.bookdelta/{venue}/{symbol}/raw`
- `Stats` -> `aggregation.stats/{venue}/{symbol}/raw`
- `Candles` -> `aggregation.candle/{venue}/{symbol}/{tf}`
- `Heatmaps` -> `insights.heatmap_snapshot/{venue}/{symbol}/{tf}`
- `VPVR` -> `insights.volume_profile_snapshot/{venue}/{symbol}/{tf}`

## Ownership and Memory Rules

- hot path data uses bounded rings/latest-wins buffers only.
- no unbounded maps/slices in live stream path.
- `ParseArena` resets per message.
- `FrameArena` resets per frame.
- pointers from parse/frame arena must not be stored in persistent stores.
- any string formatting in hot path must be throttled (250ms+) or cached.

## Multi-Exchange Endpoint Model

Exchange support is an endpoint concern in `core/streams`.

Endpoint responsibilities:

- canonical venue id
- supported channels/topics
- subject/timeframe mapping helpers
- stream id normalization helpers

Endpoint non-responsibilities:

- no widget/store mutation
- no transport management
- no platform import

Adding new exchange:

1. add endpoint definition in `core/streams`.
2. define channel capabilities and subject mapping.
3. wire endpoint selection via profile venue.
4. validate reconcile subscribes through normalized channels.
5. verify no widget/platform coupling introduced.

Adding new topic:

1. add `MD_Channel` + normalized subject mapping.
2. add parser path in `message_parser`.
3. add store update path in `core/app/marketdata.odin`.
4. add endpoint capability flags.
5. update protocol `HELLO.capabilities.topics` docs and tests.

## Anti-Drift Rules

1. Protocol gate is mandatory: first control frame must be `hello` with `proto_ver` and `capabilities`.
2. Unsupported protocol version must fail loudly (`DESYNC(reason)`), no silent fallback.
3. Required fields validation is strict on parser and control frames.
4. Snapshot-before-delta invariants are enforced for orderbook.
5. StreamController/StreamRegistry is single source of truth for connect/subscribe/resync/health.
6. URL query params are not an operational config source.
7. Native runtime flags are not an operational config source (`--ws-url/--api-key/--venue/--symbol` removed).

## Operational Defaults

- connection profile is persisted via settings/profile store.
- API key defaults to session-only handling.
- no runtime operational toggle via URL params.
- no runtime operational toggle via native CLI connection flags.
- Connection Manager is the only UI control surface for profile connect/disconnect and "Add Stream" fan-out.

## Verification Hooks

- `make -C client build-wasm`
- `make -C client build-native`
- `cd client && odin test src/core/services -collection:mr=src/core`
- `cd client && odin test src/core/streams -collection:mr=src/core`
- `go test ./internal/actors/delivery/runtime -count=1`
- soak scripts:
  - `client/scripts/soak-native.sh`
  - `client/scripts/soak-mem-native.sh`
