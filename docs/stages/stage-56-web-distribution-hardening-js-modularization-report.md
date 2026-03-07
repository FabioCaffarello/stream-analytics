# Stage 56 — Web Distribution Hardening + JS Modularization

**Date:** 2026-03-07
**Status:** COMPLETE
**Commits:** 4

## Summary

Replaced the monolithic `odin.js` (1,049 lines, 20+ mutable globals) with a
modular JS architecture of 9 files organized by responsibility. Zero behavior
change — the WASM contract, frame loop, and all foreign procs are preserved
identically.

## Architecture Before

```
client/web/
├── index.html
├── odin.js      (1,049 lines — everything)
└── app.wasm
```

Single file handling: memory helpers, console write buffer, Canvas2D host +
sizing + caches, keyboard/mouse input bridge, WebSocket bridge + auth + runtime
override, perf HUD, WASM imports assembly, 80-line manual probe table, frame
loop with idle throttle, bootstrap + error handling.

## Architecture After

```
client/web/
├── index.html        (script src → main.js)
├── main.js           (18 lines — bootstrap orchestrator)
├── runtime.js        (228 lines — import assembly, frame loop, console writer)
├── modules/
│   ├── memory.js     (18 lines — TEXT_DECODER/ENCODER, loadStringRaw, wasmRef)
│   ├── canvas.js     (195 lines — Canvas2D host, DPR, caches, sizing)
│   ├── input.js      (191 lines — keyboard + mouse state, event listeners)
│   ├── websocket.js  (231 lines — WS lifecycle, msg queue, auth, __mr_* APIs)
│   ├── storage.js    (76 lines — localStorage, clipboard, HTTP GET)
│   ├── perf.js       (136 lines — perf HUD, EMA metrics, idle constants)
│   └── probes.js     (29 lines — dynamic probe table builder)
└── app.wasm
```

**Total: 1,122 lines** (73 more than monolith from module headers/exports)

## Module Dependency Graph

```
main.js
  └── runtime.js
        ├── modules/memory.js     (shared by canvas, websocket, storage)
        ├── modules/canvas.js     ← memory
        ├── modules/input.js      (standalone)
        ├── modules/websocket.js  ← memory
        ├── modules/storage.js    ← memory
        ├── modules/perf.js       (standalone)
        └── modules/probes.js     (standalone)
```

## Key Design Decisions

1. **Factory/init pattern** — Each module exports a factory function (`initCanvas()`,
   `initInput()`, `initWebSocket()`). No module-scope side effects. All mutable
   state is encapsulated in returned objects.

2. **Buffer accessor via closure** — `runtime.js` creates `const buf = () => wasmRef.memory.buffer`
   and passes it to module procs that need WASM memory. This avoids modules
   holding stale buffer references after memory growth.

3. **Dynamic probe builder** — `probes.js` iterates `wasm.exports` keys matching
   `/^probe_/` and builds the probe table automatically. Eliminates 80+ lines of
   manual `typeof` guards. New Odin probe exports need zero JS changes.

4. **Perf HUD receives bridge references** — `onFrame(now, wsBridge, canvasMod)`
   reads WS/canvas state through explicit parameters, not globals.

## Infrastructure Updates

- **Dockerfile:** `COPY modules/` directory alongside main.js and runtime.js
- **nginx:** Comment updated (no config change needed — `*.js` glob still matches)
- **Playwright:** Diagnostic script updated to detect `main.js`/`runtime.js`
- **Odin sources:** Comments updated to reference new file paths

## Problems Resolved

| Problem | Resolution |
|---------|-----------|
| 20+ mutable globals at module scope | Encapsulated per module via factory pattern |
| 80-line manual probe table | Dynamic discovery via `/^probe_/` iteration |
| All concerns in single namespace | 7 modules with clear single responsibilities |
| Canvas/WS/input untestable | Each module independently instantiable |
| Every new probe needs JS edit | Auto-discovered, zero-maintenance |
| Implicit coupling (writeToConsole → perfHud) | Explicit parameter passing |

## Verification

- Odin check: `odin check src/core/app -no-entry-point -collection:mr=src/core` — PASS
- All pre-commit hooks: PASS (4/4 commits)
- Import graph: all paths resolve correctly
- WASM contract: identical odin_env namespace (same function signatures)
- Zero wire changes, zero WASM binary changes

## Long-Term Benefits

1. **Independent module evolution** — Canvas can be swapped to WebGL/WebGPU
2. **Unit testability** — Each module testable with mocks
3. **Zero-maintenance probes** — New Odin exports auto-discovered
4. **Debugging isolation** — One file per concern
5. **Tree-shaking ready** — Perf HUD excludable from prod builds
6. **Foundation for PWA/ServiceWorker/WebRTC** — New features as new modules
