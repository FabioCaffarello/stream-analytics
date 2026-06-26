# Client Memory & Ownership Rules (Odin)

## Ownership model

### FrameArena (`ui.Frame_Arena`)
- Owner: `ui.Command_Buffer.frame_arena`.
- Scope: one UI frame.
- Reset: `ui.reset(&cmd_buf)` every frame.
- Allowed data:
  - interned UI text emitted by `ui.push_text`.
  - transient render-command payload bytes.
- Forbidden:
  - storing pointers/strings from `frame_arena.bytes` in app state, stores, stream handles, or platform adapters.

### ParseArena (`services.Parse_Arena`)
- Owner: marketdata adapters (`platform/native/marketdata_native.odin`, `platform/web/marketdata_web.odin`).
- Scope: one inbound WS message.
- Lifecycle:
  1. parse via `services.parse_mr_message_with_arena(...)`
  2. consume parse result into fixed staging/rings
  3. reset with `services.parse_arena_reset_message(...)`
- Allowed data:
  - JSON temporary structs/strings from `json.unmarshal`.
- Forbidden:
  - storing pointers/slices/strings allocated by parse path into long-lived state.
  - using parse-owned memory after `parse_arena_reset_message`.

### Stores (domain state)
- Owners: `services/*_store.odin` + stream view slots.
- Scope: persistent runtime domain state.
- Rules:
  - fixed-capacity/ring-buffer semantics only.
  - overwrite/evict when full; never unbounded append.
  - no ownership transfer from FrameArena/ParseArena.

## Hard prohibitions
- Do not keep pointers/references from FrameArena or ParseArena in:
  - `App_State`
  - StreamRegistry handles
  - store snapshots/rings
  - widget persistent state
- Do not reintroduce per-frame heap allocations in hot widgets for label formatting.
- Do not add unbounded maps/slices in live data paths (trade/orderbook/candle/stats/heatmap/vpvr).

## Runtime budgets
- Parse reset invariant:
  - `parse_arena_resets` should track `parsed_msgs_total` in soak output.
- Memory contract baseline (native soak):
  - default total RSS growth budget: `+64 MB / 10 min`
  - default sustained-window growth budget: `+24 MB / 180 s`

## Soak commands
- Standard memory contract (10 min):
```bash
client/scripts/soak-mem-native.sh \
  --duration-sec 600 \
  --sample-sec 5 \
  --log-ms 2000 \
  --rss-budget-mb 64 \
  --sustained-budget-mb 24 \
  --sustained-window-sec 180
```

- Quick smoke:
```bash
client/scripts/soak-mem-native.sh \
  --duration-sec 30 \
  --sample-sec 2 \
  --log-ms 500 \
  --rss-budget-mb 256 \
  --sustained-budget-mb 128 \
  --sustained-window-sec 10
```

## Verification checklist
- `make -C client build-native`
- `make -C client build-wasm`
- `client/scripts/check-widgets-functional.sh`
- `client/scripts/soak-mem-native.sh ...` (contract profile)
