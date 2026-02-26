# Codex Instructions — Market Raccoon

Market Raccoon (MR) is a real-time crypto trading terminal: Go backend (6 exchanges, actor model, NATS/TimescaleDB) + Odin client (native GLFW + WASM). The client renders via a platform-agnostic RCL (Render Command List) — core emits `Cmd_Rect_Filled`, `Cmd_Line`, `Cmd_Text`, `Cmd_Clip_Push/Pop` into a `Command_Buffer`; platform renderers consume it.

## Reference: MarketMonkey (READ-ONLY)
`zip/01-marketmonkey-files/marketmonkey/` contains MarketMonkey, the predecessor trading terminal (also Odin + ImGui). Use it as **architectural reference** for widget design, layout, charting, and data flow patterns. Key files: `client/src/chart_widget.odin`, `layout.odin`, `orderbook_widget.odin`, `trades_widget.odin`, `heatmap_layer.odin`, `vpvr_layer.odin`, `trade_counter_layer.odin`. **Never modify files under `zip/`.**

## Client Structure
- `client/src/core/app/app.odin` — main loop, event drain, widget layout (800x600 viewport)
- `client/src/core/widgets/` — pure RCL widgets: `candle_widget`, `trades_widget`, `orderbook_widget`, `trade_counter`, `heatmap_widget`, `vpvr_widget`
- `client/src/core/services/` — zero-alloc ring-buffer stores (`Trades_Store`, `Candle_Store`, `Orderbook_Store`, etc.)
- `client/src/core/ports/` — platform-abstracted ports (`Marketdata_Port`, `Text_Port`, `Input_State`)
- `client/src/core/util/` — WS protocol structs (`mr_protocol.odin`) + subject builder (`subject.odin`)
- `client/src/core/ui/` — RCL primitives (`commands.odin`), palette (`styles.odin`), layout helpers (`layout.odin`)
- `client/src/platform/native/` — GLFW+OpenGL backend, WS client, ImGui renderer
- `client/src/platform/web/` — WASM backend, JS WS bridge, Canvas2D renderer

## Key Rules
- Go backend: errors are `*problem.Problem`, never plain `error`. Results are `result.Result[T]`.
- Odin client: **`fmt.tprintf` returns temp_allocator strings** — never store in structs. Use `strings.concatenate` for heap strings. `fmt.tprintf` treats `{`/`}` as format verbs — never use with raw JSON.
- Go backend sends **PascalCase** JSON for domain structs (Trade, BookDelta, Stats, Candle). Odin json tags must match exactly.
- Build: `make -C client build-native` (GLFW), `make -C client build-wasm` (WASM). Go: `make test`, `make ci`.
