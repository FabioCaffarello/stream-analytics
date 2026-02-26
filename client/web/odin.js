// odin.js -- ESM loader for Odin js_wasm32 WebAssembly modules.
//
// Provides the "env" and "odin_env" import namespaces that the Odin
// runtime expects, plus Canvas2D foreign procs for the RCL renderer.
//
// When odin-wasm is added as a dependency, this file can be replaced
// by importing from "odin-wasm/wasm/runtime.js".

// ---------------------------------------------------------------------------
// Memory helpers
// ---------------------------------------------------------------------------

const TEXT_DECODER = new TextDecoder();
const TEXT_ENCODER = new TextEncoder();

function loadStringRaw(buffer, ptr, len) {
    return TEXT_DECODER.decode(new Uint8Array(buffer, ptr, len));
}

// ---------------------------------------------------------------------------
// Console write buffer  (fd 1 = stdout, fd 2 = stderr)
// ---------------------------------------------------------------------------

let writeBuffer = "";
let lastFd = null;

function writeToConsole(fd, str) {
    if (fd !== 1 && fd !== 2) {
        throw new Error(`odin_env.write: invalid fd=${fd}`);
    }
    if (lastFd !== null && lastFd !== fd) {
        if (writeBuffer.length > 0) {
            (lastFd === 2 ? console.error : console.log)(writeBuffer);
            writeBuffer = "";
        }
    }
    lastFd = fd;
    if (str.endsWith("\n")) {
        writeBuffer += str.slice(0, -1);
        perfHudConsumeConsoleLine(writeBuffer);
        (fd === 2 ? console.error : console.log)(writeBuffer);
        writeBuffer = "";
        lastFd = null;
    } else {
        writeBuffer += str;
    }
}

// ---------------------------------------------------------------------------
// Canvas2D  (cached once, used by foreign procs)
// ---------------------------------------------------------------------------

const canvas = document.getElementById("canvas");
const ctx = canvas ? canvas.getContext("2d") : null;
let canvasSizeDirty = true;
const URL_PARAMS = new URLSearchParams(window.location.search);
const PERF_HUD_ENABLED = URL_PARAMS.get("perf_hud") === "1";
const IDLE_STEP_FPS = (() => {
    const raw = Number(URL_PARAMS.get("idle_step_fps") || "15");
    if (!Number.isFinite(raw)) return 15;
    if (raw <= 0) return 0;
    return Math.min(60, Math.max(1, Math.round(raw)));
})();
const IDLE_STEP_INTERVAL_MS = IDLE_STEP_FPS > 0 ? (1000 / IDLE_STEP_FPS) : 0;
const IDLE_QUIET_MS = (() => {
    const raw = Number(URL_PARAMS.get("idle_quiet_ms") || "250");
    if (!Number.isFinite(raw)) return 250;
    return Math.max(0, Math.round(raw));
})();

const TEXT_MEASURE_CACHE_CAP = 2048;
const textMeasureCache = new Map();
let canvasFontKey = "";
let fillStyleKey = "";
let strokeStyleKey = "";
let lineWidthValue = -1;

function setCanvasFont(size) {
    if (!ctx) return;
    const key = `${size}px monospace`;
    if (canvasFontKey === key) return;
    canvasFontKey = key;
    ctx.font = key;
}

function setFillStyle(r, g, b, a) {
    if (!ctx) return;
    const key = `${(r * 255) | 0},${(g * 255) | 0},${(b * 255) | 0},${a}`;
    if (fillStyleKey === key) return;
    fillStyleKey = key;
    ctx.fillStyle = `rgba(${key})`;
}

function setStrokeStyle(r, g, b, a) {
    if (!ctx) return;
    const key = `${(r * 255) | 0},${(g * 255) | 0},${(b * 255) | 0},${a}`;
    if (strokeStyleKey === key) return;
    strokeStyleKey = key;
    ctx.strokeStyle = `rgba(${key})`;
}

function setLineWidth(thickness) {
    if (!ctx) return;
    if (lineWidthValue === thickness) return;
    lineWidthValue = thickness;
    ctx.lineWidth = thickness;
}

function markCanvasSizeDirty() {
    canvasSizeDirty = true;
}

if (canvas) {
    if (typeof ResizeObserver === "function") {
        const ro = new ResizeObserver(() => {
            markCanvasSizeDirty();
        });
        ro.observe(canvas);
    }
    window.addEventListener("resize", markCanvasSizeDirty, { passive: true });
    if (window.visualViewport) {
        window.visualViewport.addEventListener("resize", markCanvasSizeDirty, { passive: true });
    }
}

function syncCanvasSize(force = false) {
    if (!canvas) return;
    if (!force && !canvasSizeDirty) return;
    const rect = canvas.getBoundingClientRect();
    const w = Math.max(1, Math.round(rect.width));
    const h = Math.max(1, Math.round(rect.height));
    if (canvas.width !== w || canvas.height !== h) {
        canvas.width = w;
        canvas.height = h;
    }
    canvasSizeDirty = false;
}

// ---------------------------------------------------------------------------
// WebSocket bridge  (message queue drained by WASM poll loop)
// ---------------------------------------------------------------------------

let ws = null;
let wsState = 0; // 0=closed, 1=connecting, 2=open, 3=closing
const wsMsgQueue = [];
let lastWsMsgTs = 0;
let wsMsgDropCount = 0;
let wsEpoch = 0;
const WS_MSG_QUEUE_CAP = 4096;

// ---------------------------------------------------------------------------
// Perf HUD (optional, debug-only)
// ---------------------------------------------------------------------------

let perfHudEl = null;
const perfHudState = {
    rafFps: 0,
    rafFrames: 0,
    rafWindowStart: 0,
    stepFps: 0,
    stepCalls: 0,
    stepWindowStart: 0,
    lastStepDtMs: 0,
    stepDtAvgMs: 0,
    stepDtSamples: 0,
    lastStepCpuMs: 0,
    stepCpuAvgMs: 0,
    stepCpuMaxMs: 0,
    idleSkips: 0,
    idleSkipsWindow: 0,
    lastHudPaint: 0,
    lastWsPerfLine: "",
    lastWsPerfTs: 0,
};

function perfHudEnsure() {
    if (!PERF_HUD_ENABLED || perfHudEl) return;
    perfHudEl = document.createElement("div");
    perfHudEl.id = "mr-perf-hud";
    Object.assign(perfHudEl.style, {
        position: "fixed",
        top: "8px",
        right: "8px",
        zIndex: "9999",
        minWidth: "240px",
        maxWidth: "min(42vw, 420px)",
        padding: "8px 10px",
        background: "rgba(7, 10, 14, 0.82)",
        border: "1px solid rgba(120, 180, 255, 0.22)",
        borderRadius: "8px",
        color: "#c8f2ff",
        font: "12px/1.35 ui-monospace, SFMono-Regular, Menlo, monospace",
        whiteSpace: "pre-wrap",
        pointerEvents: "none",
        backdropFilter: "blur(6px)",
        boxShadow: "0 6px 24px rgba(0,0,0,0.28)",
    });
    document.body.appendChild(perfHudEl);
}

function perfHudConsumeConsoleLine(line) {
    if (!PERF_HUD_ENABLED || typeof line !== "string") return;
    if (!line.startsWith("[ws-perf] ")) return;
    perfHudState.lastWsPerfLine = line;
    perfHudState.lastWsPerfTs = performance.now();
}

function perfHudOnFrame(now) {
    if (!PERF_HUD_ENABLED) return;
    perfHudEnsure();
    if (!perfHudState.rafWindowStart) perfHudState.rafWindowStart = now;
    perfHudState.rafFrames += 1;
    const rafDt = now - perfHudState.rafWindowStart;
    if (rafDt >= 1000) {
        perfHudState.rafFps = (perfHudState.rafFrames * 1000) / rafDt;
        perfHudState.rafFrames = 0;
        perfHudState.rafWindowStart = now;
    }
    if (now - perfHudState.lastHudPaint < 250) return; // 4 Hz HUD refresh
    perfHudState.lastHudPaint = now;

    const wsStateLabel =
        wsState === 2 ? "open" :
        wsState === 1 ? "connecting" :
        wsState === 3 ? "closing" : "closed";
    const wsPerfAgeMs = perfHudState.lastWsPerfTs > 0 ? Math.round(now - perfHudState.lastWsPerfTs) : -1;
    const lines = [
        "PERF HUD",
        `raf_fps: ${perfHudState.rafFps.toFixed(1)}`,
        `step_fps: ${perfHudState.stepFps.toFixed(1)} dt_ms=${perfHudState.lastStepDtMs.toFixed(1)} avg=${perfHudState.stepDtAvgMs.toFixed(1)} idle_skips=${perfHudState.idleSkips}`,
        `step_cpu_ms: ${perfHudState.lastStepCpuMs.toFixed(2)} avg=${perfHudState.stepCpuAvgMs.toFixed(2)} max=${perfHudState.stepCpuMaxMs.toFixed(2)}`,
        `ws: ${wsStateLabel} queue=${wsMsgQueue.length} drop=${wsMsgDropCount}`,
        `canvas: ${canvas ? `${canvas.width}x${canvas.height}` : "n/a"} dirty=${canvasSizeDirty ? 1 : 0}`,
    ];
    if (perfHudState.lastWsPerfLine) {
        lines.push(`ws-perf age_ms: ${wsPerfAgeMs}`);
        lines.push(perfHudState.lastWsPerfLine);
    } else {
        lines.push("ws-perf: enable ws_perf_debug=1");
    }
    if (perfHudEl) perfHudEl.textContent = lines.join("\n");
}

function perfHudOnStep(now, dtSec, cpuMs) {
    if (!PERF_HUD_ENABLED) return;
    if (!perfHudState.stepWindowStart) perfHudState.stepWindowStart = now;
    perfHudState.stepCalls += 1;
    const dtMs = Math.max(0, dtSec * 1000);
    perfHudState.lastStepDtMs = dtMs;
    const stepCpuMs = Math.max(0, cpuMs || 0);
    perfHudState.lastStepCpuMs = stepCpuMs;
    if (perfHudState.stepDtSamples <= 0) {
        perfHudState.stepDtAvgMs = dtMs;
        perfHudState.stepCpuAvgMs = stepCpuMs;
        perfHudState.stepCpuMaxMs = stepCpuMs;
        perfHudState.stepDtSamples = 1;
    } else {
        // EMA keeps the HUD stable while still reacting to stalls/spikes.
        perfHudState.stepDtAvgMs = perfHudState.stepDtAvgMs * 0.9 + dtMs * 0.1;
        perfHudState.stepCpuAvgMs = perfHudState.stepCpuAvgMs * 0.9 + stepCpuMs * 0.1;
        if (stepCpuMs > perfHudState.stepCpuMaxMs) perfHudState.stepCpuMaxMs = stepCpuMs;
        perfHudState.stepDtSamples += 1;
    }
    const stepWindowDt = now - perfHudState.stepWindowStart;
    if (stepWindowDt >= 1000) {
        perfHudState.stepFps = (perfHudState.stepCalls * 1000) / stepWindowDt;
        perfHudState.stepCalls = 0;
        perfHudState.stepWindowStart = now;
        perfHudState.idleSkips = perfHudState.idleSkipsWindow;
        perfHudState.idleSkipsWindow = 0;
        perfHudState.stepCpuMaxMs = perfHudState.lastStepCpuMs;
    }
}

// ---------------------------------------------------------------------------
// WASM state  (mutable ref — closures read after instantiation)
// ---------------------------------------------------------------------------

const wasm = { exports: null, memory: null };

// ---------------------------------------------------------------------------
// Import object: env + odin_env
// ---------------------------------------------------------------------------

const imports = {
    env: {},

    odin_env: {
        // --- runtime I/O ---
        write(fd, ptr, len) {
            writeToConsole(fd, loadStringRaw(wasm.memory.buffer, ptr, len));
        },
        trap()  { throw new Error("odin: trap"); },
        abort() { throw new Error("odin: abort"); },
        alert(ptr, len) { alert(loadStringRaw(wasm.memory.buffer, ptr, len)); },
        evaluate(ptr, len) { eval.call(null, loadStringRaw(wasm.memory.buffer, ptr, len)); },

        // --- time ---
        time_now:   () => BigInt(Date.now()),
        tick_now:   () => performance.now(),
        time_sleep() {},

        // --- math intrinsics ---
        sqrt:    Math.sqrt,
        sin:     Math.sin,
        cos:     Math.cos,
        pow:     Math.pow,
        fmuladd: (x, y, z) => x * y + z,
        ln:      Math.log,
        exp:     Math.exp,
        ldexp:   (x, exp) => x * 2 ** exp,

        // --- crypto ---
        rand_bytes(addr, len) {
            crypto.getRandomValues(new Uint8Array(wasm.memory.buffer, addr, len));
        },

        // --- Canvas2D foreign procs (RCL renderer) ---
        canvas_clear(r, g, b, a) {
            if (!ctx) return;
            setFillStyle(r, g, b, a);
            ctx.fillRect(0, 0, canvas.width, canvas.height);
        },

        canvas_fill_rect(x, y, w, h, r, g, b, a) {
            if (!ctx) return;
            setFillStyle(r, g, b, a);
            ctx.fillRect(x, y, w, h);
        },

        canvas_fill_text(ptr, len, x, y, size, r, g, b, a) {
            if (!ctx) return;
            const text = loadStringRaw(wasm.memory.buffer, ptr, len);
            setFillStyle(r, g, b, a);
            setCanvasFont(size);
            ctx.fillText(text, x, y);
        },

        canvas_measure_text(ptr, len, size) {
            if (!ctx) return 0;
            const text = loadStringRaw(wasm.memory.buffer, ptr, len);
            const cacheKey = len <= 48 ? `${size}\x1f${text}` : "";
            if (cacheKey) {
                const cached = textMeasureCache.get(cacheKey);
                if (cached !== undefined) return cached;
            }
            setCanvasFont(size);
            const width = ctx.measureText(text).width;
            if (cacheKey) {
                if (textMeasureCache.size >= TEXT_MEASURE_CACHE_CAP) {
                    textMeasureCache.clear();
                }
                textMeasureCache.set(cacheKey, width);
            }
            return width;
        },

        canvas_line(x1, y1, x2, y2, r, g, b, a, thickness) {
            if (!ctx) return;
            setStrokeStyle(r, g, b, a);
            setLineWidth(thickness);
            ctx.beginPath();
            ctx.moveTo(x1, y1);
            ctx.lineTo(x2, y2);
            ctx.stroke();
        },

        canvas_clip_push(x, y, w, h) {
            if (!ctx) return;
            ctx.save();
            ctx.beginPath();
            ctx.rect(x, y, w, h);
            ctx.clip();
        },

        canvas_clip_pop() {
            if (!ctx) return;
            ctx.restore();
        },
        canvas_width() {
            return canvas ? canvas.width : 0;
        },
        canvas_height() {
            return canvas ? canvas.height : 0;
        },

        // --- WebSocket foreign procs ---
        ws_connect(url_ptr, url_len, hdr_ptr, hdr_len) {
            if (ws) {
                try {
                    // Detach handlers first so stale close/error events cannot mutate global wsState.
                    ws.onopen = ws.onmessage = ws.onclose = ws.onerror = null;
                    ws.close();
                } catch(_) {}
            }
            const url = loadStringRaw(wasm.memory.buffer, url_ptr, url_len);
            const hdrs = hdr_len > 0
                ? loadStringRaw(wasm.memory.buffer, hdr_ptr, hdr_len)
                : "";

            wsState = 1; // connecting
            wsMsgQueue.length = 0;

            // Browser WebSocket does not support custom headers.
            // Pass API key as query param so the server can auth.
            let wsUrl = url;
            if (hdrs) {
                const match = hdrs.match(/X-API-Key:\s*(\S+)/i);
                if (match) {
                    const sep = url.includes("?") ? "&" : "?";
                    wsUrl = url + sep + "api_key=" + encodeURIComponent(match[1]);
                }
            }

            try {
                ws = new WebSocket(wsUrl);
            } catch (e) {
                console.error("[ws] connect error:", e);
                wsState = 0;
                return;
            }
            const wsLocal = ws;
            const epoch = ++wsEpoch;
            ws.onopen = () => {
                if (ws !== wsLocal || wsEpoch !== epoch) return;
                wsState = 2; // open
                console.log("[ws] connected to", wsUrl);
            };
            ws.onmessage = (ev) => {
                if (ws !== wsLocal || wsEpoch !== epoch) return;
                if (typeof ev.data === "string") {
                    if (wsMsgQueue.length >= WS_MSG_QUEUE_CAP) {
                        wsMsgQueue.shift(); // Prefer freshest data under pressure.
                        wsMsgDropCount += 1;
                    }
                    wsMsgQueue.push(ev.data);
                    lastWsMsgTs = performance.now();
                }
            };
            ws.onclose = (ev) => {
                if (ws !== wsLocal || wsEpoch !== epoch) return;
                wsState = 0;
                console.log("[ws] closed code=" + ev.code);
            };
            ws.onerror = () => {
                if (ws !== wsLocal || wsEpoch !== epoch) return;
                wsState = 0;
                console.error("[ws] error");
            };
        },

        ws_send(ptr, len) {
            if (!ws || ws.readyState !== WebSocket.OPEN) return;
            const msg = loadStringRaw(wasm.memory.buffer, ptr, len);
            ws.send(msg);
        },

        ws_close() {
            if (ws) {
                wsState = 3; // closing
                try {
                    ws.onopen = ws.onmessage = ws.onclose = ws.onerror = null;
                    ws.close();
                } catch (_) {}
                ws = null;
            }
        },

        ws_state() {
            return wsState;
        },

        ws_poll_msg(buf_ptr, buf_len) {
            if (wsMsgQueue.length === 0) return 0;
            const msg = wsMsgQueue.shift();
            const encoded = TEXT_ENCODER.encode(msg);
            const copyLen = Math.min(encoded.length, buf_len);
            new Uint8Array(wasm.memory.buffer, buf_ptr, copyLen).set(
                encoded.subarray(0, copyLen),
            );
            // Return negative length when message was truncated so Odin can count.
            if (encoded.length > buf_len) return -copyLen;
            return copyLen;
        },

        url_query_param(name_ptr, name_len, out_ptr, out_cap) {
            if (!wasm.memory || out_cap <= 0) return 0;
            const key = loadStringRaw(wasm.memory.buffer, name_ptr, name_len);
            const value = new URLSearchParams(window.location.search).get(key) ?? "";
            if (!value) return 0;
            const encoded = TEXT_ENCODER.encode(value);
            const copyLen = Math.min(encoded.length, out_cap);
            new Uint8Array(wasm.memory.buffer, out_ptr, copyLen).set(
                encoded.subarray(0, copyLen),
            );
            return copyLen;
        },
    },
};

// ---------------------------------------------------------------------------
// Instantiate and run
// ---------------------------------------------------------------------------

const outputEl = document.getElementById("output");

try {
    const wasmUrl = "app.wasm" + window.location.search;
    const source = typeof WebAssembly.instantiateStreaming === "function"
        ? await WebAssembly.instantiateStreaming(fetch(wasmUrl), imports)
        : await fetch(wasmUrl)
              .then(r => r.arrayBuffer())
              .then(bytes => WebAssembly.instantiate(bytes, imports));

    wasm.exports = source.instance.exports;
    wasm.memory  = wasm.exports.memory;

    wasm.exports._start();

    // Optional animation loop (step export).
    if (typeof wasm.exports.step === "function") {
        const odinCtx = wasm.exports.default_context_ptr();
        let prev = performance.now();
        let lastStepNow = 0;
        function frame(now) {
            syncCanvasSize();
            perfHudOnFrame(now);
            const idleThrottleActive =
                IDLE_STEP_INTERVAL_MS > 0 &&
                wsState === 2 &&
                wsMsgQueue.length === 0 &&
                (lastWsMsgTs <= 0 || (now - lastWsMsgTs) >= IDLE_QUIET_MS) &&
                !canvasSizeDirty;
            if (idleThrottleActive && lastStepNow > 0 && (now - lastStepNow) < IDLE_STEP_INTERVAL_MS) {
                if (PERF_HUD_ENABLED) perfHudState.idleSkipsWindow += 1;
                requestAnimationFrame(frame);
                return;
            }
            const dt = (now - prev) * 0.001;
            prev = now;
            lastStepNow = now;
            const stepCpuStart = PERF_HUD_ENABLED ? performance.now() : 0;
            const shouldContinue = wasm.exports.step(dt, odinCtx);
            if (PERF_HUD_ENABLED) {
                perfHudOnStep(now, dt, performance.now() - stepCpuStart);
            }
            if (shouldContinue) {
                requestAnimationFrame(frame);
            } else if (typeof wasm.exports._end === "function") {
                wasm.exports._end();
            }
        }
        requestAnimationFrame(frame);
    } else if (typeof wasm.exports._end === "function") {
        syncCanvasSize(true);
        wasm.exports._end();
    }

    if (outputEl) {
        outputEl.textContent = "WASM loaded. Check canvas and browser console.";
    }
} catch (err) {
    console.error("Failed to load WASM:", err);
    if (outputEl) {
        outputEl.classList.add("error");
        outputEl.textContent = "Failed to load WASM:\n" + err.message + "\n\n" + err.stack;
    }
}
