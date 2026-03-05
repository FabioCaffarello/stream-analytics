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
let dpr = window.devicePixelRatio || 1;
const PERF_HUD_ENABLED = false;
const IDLE_STEP_FPS = 15;
const IDLE_STEP_INTERVAL_MS = IDLE_STEP_FPS > 0 ? (1000 / IDLE_STEP_FPS) : 0;
const IDLE_QUIET_MS = 250;

function defaultWsUrlForCurrentOrigin() {
    const proto = window.location.protocol === "https:" ? "wss" : "ws";
    const host = window.location.host || "127.0.0.1:8090";
    return `${proto}://${host}/ws`;
}

const SETTINGS_STORAGE_PREFIX = "mr.settings.";

function settingsStorageKey(key) {
    return `${SETTINGS_STORAGE_PREFIX}${key}`;
}

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
    // Detect DPR changes (e.g., window moved between monitors).
    const newDpr = window.devicePixelRatio || 1;
    if (newDpr !== dpr) {
        dpr = newDpr;
        canvasSizeDirty = true;
    }
    if (!force && !canvasSizeDirty) return;
    const rect = canvas.getBoundingClientRect();
    const w = Math.max(1, Math.round(rect.width * dpr));
    const h = Math.max(1, Math.round(rect.height * dpr));
    if (canvas.width !== w || canvas.height !== h) {
        canvas.width = w;
        canvas.height = h;
        // Apply DPR scale so all drawing uses CSS-pixel coordinates.
        if (ctx) {
            ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
        }
        // Resize resets Canvas2D state — invalidate cached keys.
        canvasFontKey = "";
        fillStyleKey = "";
        strokeStyleKey = "";
        lineWidthValue = -1;
        textMeasureCache.clear();
    }
    canvasSizeDirty = false;
}

// ---------------------------------------------------------------------------
// WebSocket bridge  (message queue drained by WASM poll loop)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Input bridge (keyboard + mouse) polled by WASM
// ---------------------------------------------------------------------------

// Key enum ordinals: Up=0 Down=1 Left=2 Right=3 Enter=4 Escape=5 Tab=6 Space=7
//                    Num_1=8 Num_2=9 Num_3=10 Num_4=11 Num_5=12 Num_6=13
//                    Num_7=14 Num_8=15 Num_9=16 S=17 Slash=18 C=19 G=20 F=21
//                    M=22 B=23 V=24 R=25 I=26 H=27 J=28 K=29 Z=30 Delete=31
let keyBits = 0;
let keyPressedBits = 0;
let keyReleasedBits = 0;
let modifierBits = 0; // shift=1 ctrl=2 alt=4 super/meta=8
let mouseX = 0;
let mouseY = 0;
let mouseButtons = 0; // left=bit0 right=bit1 middle=bit2
let mousePressedBits = 0;
let mouseReleasedBits = 0;
let mouseScrollX = 0;
let mouseScrollY = 0;

const KEY_MAP = {
    "ArrowUp": 0, "ArrowDown": 1, "ArrowLeft": 2, "ArrowRight": 3,
    "Enter": 4, "Escape": 5, "Tab": 6, " ": 7,
    "1": 8, "2": 9, "3": 10, "4": 11, "5": 12, "6": 13, "7": 14, "8": 15, "9": 16,
    "s": 17, "S": 17, "/": 18, "?": 18, "c": 19, "C": 19, "g": 20, "G": 20,
    "f": 21, "F": 21, "m": 22, "M": 22, "b": 23, "B": 23, "v": 24, "V": 24,
    "r": 25, "R": 25, "i": 26, "I": 26,
    "h": 27, "H": 27, "j": 28, "J": 28,
    "k": 29, "K": 29,
    "z": 30, "Z": 30,
    "Delete": 31, "Backspace": 31,
};

function updateModifierBits(ev) {
    if (!ev) return;
    let bits = 0;
    if (ev.shiftKey) bits |= (1 << 0);
    if (ev.ctrlKey) bits |= (1 << 1);
    if (ev.altKey) bits |= (1 << 2);
    if (ev.metaKey) bits |= (1 << 3);
    modifierBits = bits;
}

function updateMousePosition(ev) {
    if (!ev) return;
    if (!canvas) {
        mouseX = ev.clientX || 0;
        mouseY = ev.clientY || 0;
        return;
    }
    const rect = canvas.getBoundingClientRect();
    mouseX = (ev.clientX || 0) - rect.left;
    mouseY = (ev.clientY || 0) - rect.top;
}

document.addEventListener("keydown", (ev) => {
    updateModifierBits(ev);
    const bit = KEY_MAP[ev.key];
    if (bit !== undefined) {
        const mask = (1 << bit);
        if ((keyBits & mask) === 0) keyPressedBits |= mask;
        keyBits |= mask;
        // Prevent Tab from changing focus.
        if (ev.key === "Tab") ev.preventDefault();
    }
}, { passive: false });

document.addEventListener("keyup", (ev) => {
    updateModifierBits(ev);
    const bit = KEY_MAP[ev.key];
    if (bit !== undefined) {
        const mask = (1 << bit);
        if ((keyBits & mask) !== 0) keyReleasedBits |= mask;
        keyBits &= ~mask;
    }
});

const MOUSE_WHEEL_SCALE = 1 / 100;
const mouseEventTarget = canvas || document;

mouseEventTarget.addEventListener("mousemove", (ev) => {
    updateMousePosition(ev);
    updateModifierBits(ev);
}, { passive: true });

mouseEventTarget.addEventListener("mousedown", (ev) => {
    updateMousePosition(ev);
    updateModifierBits(ev);
    if (ev.button === 0) {
        if ((mouseButtons & (1 << 0)) === 0) mousePressedBits |= (1 << 0);
        mouseButtons |= (1 << 0);
    } else if (ev.button === 2) {
        if ((mouseButtons & (1 << 1)) === 0) mousePressedBits |= (1 << 1);
        mouseButtons |= (1 << 1);
    } else if (ev.button === 1) {
        if ((mouseButtons & (1 << 2)) === 0) mousePressedBits |= (1 << 2);
        mouseButtons |= (1 << 2);
    }
}, { passive: true });

document.addEventListener("mouseup", (ev) => {
    updateMousePosition(ev);
    updateModifierBits(ev);
    if (ev.button === 0) {
        if ((mouseButtons & (1 << 0)) !== 0) mouseReleasedBits |= (1 << 0);
        mouseButtons &= ~(1 << 0);
    } else if (ev.button === 2) {
        if ((mouseButtons & (1 << 1)) !== 0) mouseReleasedBits |= (1 << 1);
        mouseButtons &= ~(1 << 1);
    } else if (ev.button === 1) {
        if ((mouseButtons & (1 << 2)) !== 0) mouseReleasedBits |= (1 << 2);
        mouseButtons &= ~(1 << 2);
    }
}, { passive: true });

mouseEventTarget.addEventListener("wheel", (ev) => {
    updateMousePosition(ev);
    updateModifierBits(ev);
    // Match native semantics: positive Y = wheel up.
    mouseScrollX += -ev.deltaX * MOUSE_WHEEL_SCALE;
    mouseScrollY += -ev.deltaY * MOUSE_WHEEL_SCALE;
    ev.preventDefault();
}, { passive: false });

window.addEventListener("blur", () => {
    keyBits = 0;
    keyPressedBits = 0;
    keyReleasedBits = 0;
    modifierBits = 0;
    mouseButtons = 0;
    mousePressedBits = 0;
    mouseReleasedBits = 0;
    mouseScrollX = 0;
    mouseScrollY = 0;
}, { passive: true });

let ws = null;
let wsState = 0; // 0=closed, 1=connecting, 2=open, 3=closing
const wsMsgQueue = [];
let lastWsMsgTs = 0;
let wsMsgDropCount = 0;
let wsEpoch = 0;
const WS_MSG_QUEUE_CAP = 4096;
const wsRuntimeOverride = {
    ws_url: "",
    api_key: "",
};

function parseAuthFromHeaderString(hdrs) {
    if (!hdrs || typeof hdrs !== "string") return { api_key: "", jwt_token: "" };
    const jwtMatch = hdrs.match(/Authorization:\s*Bearer\s+(\S+)/i);
    if (jwtMatch && typeof jwtMatch[1] === "string") {
        return { api_key: "", jwt_token: jwtMatch[1].trim() };
    }
    const apiMatch = hdrs.match(/X-API-Key:\s*(\S+)/i);
    return {
        api_key: apiMatch && typeof apiMatch[1] === "string" ? apiMatch[1].trim() : "",
        jwt_token: "",
    };
}

function buildWsUrlWithAuth(baseUrl, apiKey, jwtToken) {
    if (!baseUrl) return "";
    // JWT auth: browser WebSocket doesn't support custom headers, pass as query param.
    if (jwtToken) {
        const sep = baseUrl.includes("?") ? "&" : "?";
        return `${baseUrl}${sep}token=${encodeURIComponent(jwtToken)}`;
    }
    if (!apiKey) return baseUrl;
    const sep = baseUrl.includes("?") ? "&" : "?";
    return `${baseUrl}${sep}api_key=${encodeURIComponent(apiKey)}`;
}

function sanitizeWsUrlForLog(wsUrl) {
    if (!wsUrl || typeof wsUrl !== "string") return wsUrl || "";
    try {
        const u = new URL(wsUrl, window.location.href);
        if (u.searchParams.has("api_key")) u.searchParams.set("api_key", "***");
        if (u.searchParams.has("token")) u.searchParams.set("token", "***");
        if (u.searchParams.has("jwt")) u.searchParams.set("jwt", "***");
        return u.toString();
    } catch (_) {
        return wsUrl
            .replace(/([?&]api_key=)[^&]*/gi, "$1***")
            .replace(/([?&]token=)[^&]*/gi, "$1***")
            .replace(/([?&]jwt=)[^&]*/gi, "$1***");
    }
}

function resolveWsConfigFromBridge(url, hdrs) {
    const baseUrl = (wsRuntimeOverride.ws_url || defaultWsUrlForCurrentOrigin() || url || "").trim();
    const parsed = parseAuthFromHeaderString(hdrs);
    const apiKey = (wsRuntimeOverride.api_key || parsed.api_key).trim();
    const jwtToken = parsed.jwt_token;
    return {
        base_url: baseUrl,
        api_key: apiKey,
        jwt_token: jwtToken,
        ws_url: buildWsUrlWithAuth(baseUrl, apiKey, jwtToken),
        override_active: wsRuntimeOverride.ws_url.length > 0 || wsRuntimeOverride.api_key.length > 0,
    };
}

function closeActiveSocket(markClosing = false) {
    if (!ws) return;
    if (markClosing) wsState = 3; // closing
    try {
        ws.onopen = ws.onmessage = ws.onclose = ws.onerror = null;
        ws.close();
    } catch (_) {}
    ws = null;
}

function connectSocketUrl(wsUrl) {
    if (!wsUrl || typeof wsUrl !== "string") {
        wsState = 0;
        console.error("[ws] invalid url");
        return false;
    }

    closeActiveSocket(false);
    wsState = 1; // connecting
    wsMsgQueue.length = 0;

    try {
        ws = new WebSocket(wsUrl);
    } catch (e) {
        console.error("[ws] connect error:", e);
        wsState = 0;
        return false;
    }

    const wsLocal = ws;
    const epoch = ++wsEpoch;
    ws.onopen = () => {
        if (ws !== wsLocal || wsEpoch !== epoch) return;
        wsState = 2; // open
        console.log("[ws] connected to", sanitizeWsUrlForLog(wsUrl));
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

    return true;
}

function switchWsRuntime(wsUrl, apiKey, options = {}) {
    if (typeof wsUrl === "string") wsRuntimeOverride.ws_url = wsUrl.trim();
    if (typeof apiKey === "string") wsRuntimeOverride.api_key = apiKey.trim();
    const live = !options || options.live !== false;
    if (!live) {
        return runtimeConfigSnapshot();
    }

    // Force Odin reconnection flow. Adapter will reconnect and re-subscribe with the overridden endpoint.
    closeActiveSocket(false);
    wsState = 0;
    wsMsgQueue.length = 0;
    lastWsMsgTs = 0;

    return runtimeConfigSnapshot();
}

function runtimeConfigSnapshot() {
    return {
        mode: wsRuntimeOverride.ws_url || wsRuntimeOverride.api_key ? "runtime-override" : "default",
        ws_url: wsRuntimeOverride.ws_url,
        api_key: wsRuntimeOverride.api_key,
        default_ws_url: defaultWsUrlForCurrentOrigin(),
    };
}

window.__mr_get_runtime_config = () => runtimeConfigSnapshot();
window.__mr_set_runtime_config = (config = {}) => switchWsRuntime(config.ws_url, config.api_key, { live: true });
window.__mr_set_runtime_config_live = (config = {}) => switchWsRuntime(config.ws_url, config.api_key, { live: true });
window.__mr_set_ws_endpoint = (wsUrl, apiKey, options = {}) => switchWsRuntime(wsUrl, apiKey, options);
window.__mr_switch_ws_runtime = switchWsRuntime;
window.__mr_clear_ws_runtime_override = (options = {}) => {
    wsRuntimeOverride.ws_url = "";
    wsRuntimeOverride.api_key = "";
    const live = !options || options.live !== false;
    if (live) {
        closeActiveSocket(false);
        wsState = 0;
        wsMsgQueue.length = 0;
        lastWsMsgTs = 0;
    }
    return runtimeConfigSnapshot();
};

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
        lines.push("ws-perf: waiting telemetry");
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
            // Clear at physical resolution, then restore DPR transform.
            ctx.save();
            ctx.setTransform(1, 0, 0, 1, 0, 0);
            setFillStyle(r, g, b, a);
            ctx.fillRect(0, 0, canvas.width, canvas.height);
            ctx.restore();
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
            // Return CSS pixels (Odin UI works in CSS coordinate space).
            return canvas ? Math.round(canvas.width / dpr) : 0;
        },
        canvas_height() {
            return canvas ? Math.round(canvas.height / dpr) : 0;
        },

        // --- WebSocket foreign procs ---
        ws_connect(url_ptr, url_len, hdr_ptr, hdr_len) {
            const url = loadStringRaw(wasm.memory.buffer, url_ptr, url_len);
            const hdrs = hdr_len > 0
                ? loadStringRaw(wasm.memory.buffer, hdr_ptr, hdr_len)
                : "";
            const resolved = resolveWsConfigFromBridge(url, hdrs);
            connectSocketUrl(resolved.ws_url);
        },

        ws_send(ptr, len) {
            if (!ws || ws.readyState !== WebSocket.OPEN) return;
            const msg = loadStringRaw(wasm.memory.buffer, ptr, len);
            ws.send(msg);
        },

        ws_close() {
            closeActiveSocket(true);
        },

        ws_state() {
            return wsState;
        },

        ws_drop_count() {
            return wsMsgDropCount >>> 0;
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

        key_state() {
            return keyBits;
        },

        key_pressed_state() {
            const v = keyPressedBits >>> 0;
            keyPressedBits = 0;
            return v;
        },

        key_released_state() {
            const v = keyReleasedBits >>> 0;
            keyReleasedBits = 0;
            return v;
        },

        mouse_x() {
            return mouseX;
        },

        mouse_y() {
            return mouseY;
        },

        mouse_buttons() {
            return mouseButtons >>> 0;
        },

        mouse_pressed_buttons() {
            const v = mousePressedBits >>> 0;
            mousePressedBits = 0;
            return v;
        },

        mouse_released_buttons() {
            const v = mouseReleasedBits >>> 0;
            mouseReleasedBits = 0;
            return v;
        },

        mouse_scroll_x() {
            const v = mouseScrollX;
            mouseScrollX = 0;
            return v;
        },

        mouse_scroll_y() {
            const v = mouseScrollY;
            mouseScrollY = 0;
            return v;
        },

        modifier_state() {
            return modifierBits >>> 0;
        },

        web_settings_load(key_ptr, key_len, out_ptr, out_cap) {
            if (!wasm.memory || out_cap <= 0) return 0;
            const key = loadStringRaw(wasm.memory.buffer, key_ptr, key_len);
            if (!key) return 0;
            try {
                const value = window.localStorage.getItem(settingsStorageKey(key));
                if (typeof value !== "string" || value.length === 0) return 0;
                const encoded = TEXT_ENCODER.encode(value);
                const copyLen = Math.min(encoded.length, out_cap);
                new Uint8Array(wasm.memory.buffer, out_ptr, copyLen).set(
                    encoded.subarray(0, copyLen),
                );
                return copyLen;
            } catch (_) {
                return 0;
            }
        },

        web_settings_save(key_ptr, key_len, value_ptr, value_len) {
            if (!wasm.memory) return 0;
            const key = loadStringRaw(wasm.memory.buffer, key_ptr, key_len);
            if (!key) return 0;
            const value = value_len > 0
                ? loadStringRaw(wasm.memory.buffer, value_ptr, value_len)
                : "";
            try {
                window.localStorage.setItem(settingsStorageKey(key), value);
                return 1;
            } catch (_) {
                return 0;
            }
        },

        web_clipboard_write(text_ptr, text_len) {
            if (!wasm.memory || text_len <= 0) return 0;
            const text = loadStringRaw(wasm.memory.buffer, text_ptr, text_len);
            if (!text) return 0;
            try {
                navigator.clipboard.writeText(text).catch(() => {});
                return 1;
            } catch (_) {
                return 0;
            }
        },

        http_get_sync(url_ptr, url_len, out_ptr, out_cap) {
            if (!wasm.memory || out_cap <= 0) return 0;
            const url = loadStringRaw(wasm.memory.buffer, url_ptr, url_len);
            try {
                const xhr = new XMLHttpRequest();
                xhr.open("GET", url, false); // synchronous
                xhr.setRequestHeader("Accept", "application/json");
                xhr.send(null);
                if (xhr.status !== 200) return 0;
                const body = xhr.responseText || "";
                if (!body) return 0;
                const encoded = TEXT_ENCODER.encode(body);
                const copyLen = Math.min(encoded.length, out_cap);
                new Uint8Array(wasm.memory.buffer, out_ptr, copyLen).set(
                    encoded.subarray(0, copyLen),
                );
                return copyLen;
            } catch (_) {
                return 0;
            }
        },
    },
};

// ---------------------------------------------------------------------------
// Instantiate and run
// ---------------------------------------------------------------------------

const outputEl = document.getElementById("output");

try {
    const wasmUrl = "app.wasm";
    const source = typeof WebAssembly.instantiateStreaming === "function"
        ? await WebAssembly.instantiateStreaming(fetch(wasmUrl), imports)
        : await fetch(wasmUrl)
              .then(r => r.arrayBuffer())
              .then(bytes => WebAssembly.instantiate(bytes, imports));

    wasm.exports = source.instance.exports;
    wasm.memory  = wasm.exports.memory;
    // Expose lightweight runtime probe hooks for Playwright/web gates.
    window.__mr_wasm_exports = wasm.exports;
    window.__mr_widget_probe = () => ({
        t:  typeof wasm.exports.probe_widget_trades_count === "function" ? wasm.exports.probe_widget_trades_count() : -1,
        obA: typeof wasm.exports.probe_widget_orderbook_asks === "function" ? wasm.exports.probe_widget_orderbook_asks() : -1,
        obB: typeof wasm.exports.probe_widget_orderbook_bids === "function" ? wasm.exports.probe_widget_orderbook_bids() : -1,
        st: typeof wasm.exports.probe_widget_stats_count === "function" ? wasm.exports.probe_widget_stats_count() : -1,
        hm: typeof wasm.exports.probe_widget_heatmap_snaps === "function" ? wasm.exports.probe_widget_heatmap_snaps() : -1,
        vp: typeof wasm.exports.probe_widget_vpvr_levels === "function" ? wasm.exports.probe_widget_vpvr_levels() : -1,
        c:  typeof wasm.exports.probe_widget_candle_count === "function" ? wasm.exports.probe_widget_candle_count() : -1,
        cClose: typeof wasm.exports.probe_widget_candle_latest_close === "function" ? wasm.exports.probe_widget_candle_latest_close() : -1,
        cEndTs: typeof wasm.exports.probe_widget_candle_latest_end_ts === "function" ? wasm.exports.probe_widget_candle_latest_end_ts() : -1,
        tf: typeof wasm.exports.probe_active_tf_index === "function" ? wasm.exports.probe_active_tf_index() : -1,
        uaq: typeof wasm.exports.probe_ui_actions_enqueued_total === "function" ? wasm.exports.probe_ui_actions_enqueued_total() : -1,
        sc: typeof wasm.exports.probe_stream_count === "function" ? wasm.exports.probe_stream_count() : -1,
        hasStream: typeof wasm.exports.probe_has_active_stream === "function" ? wasm.exports.probe_has_active_stream() : -1,
        sid32: typeof wasm.exports.probe_active_subject_lo32 === "function" ? wasm.exports.probe_active_subject_lo32() : -1,
        ssw: typeof wasm.exports.probe_stream_switches_total === "function" ? wasm.exports.probe_stream_switches_total() : -1,
        tfw: typeof wasm.exports.probe_timeframe_switches_total === "function" ? wasm.exports.probe_timeframe_switches_total() : -1,
        lSt: typeof wasm.exports.probe_active_live_stats === "function" ? wasm.exports.probe_active_live_stats() : -1,
        lHm: typeof wasm.exports.probe_active_live_heatmap === "function" ? wasm.exports.probe_active_live_heatmap() : -1,
        lVp: typeof wasm.exports.probe_active_live_vpvr === "function" ? wasm.exports.probe_active_live_vpvr() : -1,
        sHm: typeof wasm.exports.probe_active_synth_heatmap === "function" ? wasm.exports.probe_active_synth_heatmap() : -1,
        sVp: typeof wasm.exports.probe_active_synth_vpvr === "function" ? wasm.exports.probe_active_synth_vpvr() : -1,
        lC: typeof wasm.exports.probe_active_live_candle === "function" ? wasm.exports.probe_active_live_candle() : -1,
        cm: typeof wasm.exports.probe_compare_mode === "function" ? wasm.exports.probe_compare_mode() : -1,
        cwi: typeof wasm.exports.probe_compare_widget_idx === "function" ? wasm.exports.probe_compare_widget_idx() : -1,
        cc: typeof wasm.exports.probe_compare_count === "function" ? wasm.exports.probe_compare_count() : -1,
        rsiEn: typeof wasm.exports.probe_indicator_rsi_enabled === "function" ? wasm.exports.probe_indicator_rsi_enabled() : -1,
        macdEn: typeof wasm.exports.probe_indicator_macd_enabled === "function" ? wasm.exports.probe_indicator_macd_enabled() : -1,
        fundingEn: typeof wasm.exports.probe_indicator_funding_enabled === "function" ? wasm.exports.probe_indicator_funding_enabled() : -1,
        liqEn: typeof wasm.exports.probe_indicator_liq_enabled === "function" ? wasm.exports.probe_indicator_liq_enabled() : -1,
        tcEn: typeof wasm.exports.probe_indicator_trade_counter_enabled === "function" ? wasm.exports.probe_indicator_trade_counter_enabled() : -1,
        rsiOk: typeof wasm.exports.probe_indicator_rsi_rendered === "function" ? wasm.exports.probe_indicator_rsi_rendered() : -1,
        macdOk: typeof wasm.exports.probe_indicator_macd_rendered === "function" ? wasm.exports.probe_indicator_macd_rendered() : -1,
        fundingOk: typeof wasm.exports.probe_indicator_funding_rendered === "function" ? wasm.exports.probe_indicator_funding_rendered() : -1,
        liqOk: typeof wasm.exports.probe_indicator_liq_rendered === "function" ? wasm.exports.probe_indicator_liq_rendered() : -1,
        tcOk: typeof wasm.exports.probe_indicator_trade_counter_rendered === "function" ? wasm.exports.probe_indicator_trade_counter_rendered() : -1,
        mdAlloc: typeof wasm.exports.probe_md_alloc_estimate_total === "function" ? wasm.exports.probe_md_alloc_estimate_total() : -1,
        mdAllocFrame: typeof wasm.exports.probe_md_alloc_estimate_frame === "function" ? wasm.exports.probe_md_alloc_estimate_frame() : -1,
        mdEvidenceCanon: typeof wasm.exports.probe_md_canonical_evidence_frames === "function" ? wasm.exports.probe_md_canonical_evidence_frames() : -1,
        mdEvidenceLegacy: typeof wasm.exports.probe_md_legacy_evidence_frames === "function" ? wasm.exports.probe_md_legacy_evidence_frames() : -1,
        mdEvidenceFallback: typeof wasm.exports.probe_md_evidence_fallback_frames === "function" ? wasm.exports.probe_md_evidence_fallback_frames() : -1,
        mdSignalCanon: typeof wasm.exports.probe_md_canonical_signal_frames === "function" ? wasm.exports.probe_md_canonical_signal_frames() : -1,
        mdSignalLegacy: typeof wasm.exports.probe_md_legacy_signal_frames === "function" ? wasm.exports.probe_md_legacy_signal_frames() : -1,
        mdSignalFallback: typeof wasm.exports.probe_md_signal_fallback_frames === "function" ? wasm.exports.probe_md_signal_fallback_frames() : -1,
        mdEvidenceRejected: typeof wasm.exports.probe_md_legacy_evidence_rejected === "function" ? wasm.exports.probe_md_legacy_evidence_rejected() : -1,
        mdSignalRejected: typeof wasm.exports.probe_md_legacy_signal_rejected === "function" ? wasm.exports.probe_md_legacy_signal_rejected() : -1,
        evidenceCount: typeof wasm.exports.probe_widget_evidence_count === "function" ? wasm.exports.probe_widget_evidence_count() : -1,
        signalCount: typeof wasm.exports.probe_widget_signal_count === "function" ? wasm.exports.probe_widget_signal_count() : -1,
        signalLinkTotal: typeof wasm.exports.probe_widget_signal_link_total === "function" ? wasm.exports.probe_widget_signal_link_total() : -1,
        signalLinkEvidenceSeq: typeof wasm.exports.probe_widget_signal_link_evidence_seq === "function" ? wasm.exports.probe_widget_signal_link_evidence_seq() : -1,
        domParse: typeof wasm.exports.probe_widget_dom_parse_total === "function" ? wasm.exports.probe_widget_dom_parse_total() : -1,
        domFallback: typeof wasm.exports.probe_widget_dom_fallback_total === "function" ? wasm.exports.probe_widget_dom_fallback_total() : -1,
        domDrop: typeof wasm.exports.probe_widget_dom_drop_total === "function" ? wasm.exports.probe_widget_dom_drop_total() : -1,
        domDropCapacity: typeof wasm.exports.probe_widget_dom_drop_capacity_total === "function" ? wasm.exports.probe_widget_dom_drop_capacity_total() : -1,
        domDropRenderOverflow: typeof wasm.exports.probe_widget_dom_drop_render_overflow_total === "function" ? wasm.exports.probe_widget_dom_drop_render_overflow_total() : -1,
        domRenderP95: typeof wasm.exports.probe_widget_dom_render_p95_us === "function" ? wasm.exports.probe_widget_dom_render_p95_us() : -1,
        domRenderBudget: typeof wasm.exports.probe_widget_dom_render_budget_us === "function" ? wasm.exports.probe_widget_dom_render_budget_us() : -1,
        domRenderOverBudget: typeof wasm.exports.probe_widget_dom_render_over_budget === "function" ? wasm.exports.probe_widget_dom_render_over_budget() : -1,
        tapeParse: typeof wasm.exports.probe_widget_tape_parse_total === "function" ? wasm.exports.probe_widget_tape_parse_total() : -1,
        tapeFallback: typeof wasm.exports.probe_widget_tape_fallback_total === "function" ? wasm.exports.probe_widget_tape_fallback_total() : -1,
        tapeDrop: typeof wasm.exports.probe_widget_tape_drop_total === "function" ? wasm.exports.probe_widget_tape_drop_total() : -1,
        tapeDropCapacity: typeof wasm.exports.probe_widget_tape_drop_capacity_total === "function" ? wasm.exports.probe_widget_tape_drop_capacity_total() : -1,
        tapeDropRenderOverflow: typeof wasm.exports.probe_widget_tape_drop_render_overflow_total === "function" ? wasm.exports.probe_widget_tape_drop_render_overflow_total() : -1,
        tapeRenderP95: typeof wasm.exports.probe_widget_tape_render_p95_us === "function" ? wasm.exports.probe_widget_tape_render_p95_us() : -1,
        tapeRenderBudget: typeof wasm.exports.probe_widget_tape_render_budget_us === "function" ? wasm.exports.probe_widget_tape_render_budget_us() : -1,
        tapeRenderOverBudget: typeof wasm.exports.probe_widget_tape_render_over_budget === "function" ? wasm.exports.probe_widget_tape_render_over_budget() : -1,
        evidenceParse: typeof wasm.exports.probe_widget_evidence_parse_total === "function" ? wasm.exports.probe_widget_evidence_parse_total() : -1,
        evidenceFallback: typeof wasm.exports.probe_widget_evidence_fallback_total === "function" ? wasm.exports.probe_widget_evidence_fallback_total() : -1,
        evidenceDrop: typeof wasm.exports.probe_widget_evidence_drop_total === "function" ? wasm.exports.probe_widget_evidence_drop_total() : -1,
        evidenceDropCapacity: typeof wasm.exports.probe_widget_evidence_drop_capacity_total === "function" ? wasm.exports.probe_widget_evidence_drop_capacity_total() : -1,
        evidenceDropRenderOverflow: typeof wasm.exports.probe_widget_evidence_drop_render_overflow_total === "function" ? wasm.exports.probe_widget_evidence_drop_render_overflow_total() : -1,
        evidenceRenderP95: typeof wasm.exports.probe_widget_evidence_render_p95_us === "function" ? wasm.exports.probe_widget_evidence_render_p95_us() : -1,
        evidenceRenderBudget: typeof wasm.exports.probe_widget_evidence_render_budget_us === "function" ? wasm.exports.probe_widget_evidence_render_budget_us() : -1,
        evidenceRenderOverBudget: typeof wasm.exports.probe_widget_evidence_render_over_budget === "function" ? wasm.exports.probe_widget_evidence_render_over_budget() : -1,
        signalParse: typeof wasm.exports.probe_widget_signal_parse_total === "function" ? wasm.exports.probe_widget_signal_parse_total() : -1,
        signalFallback: typeof wasm.exports.probe_widget_signal_fallback_total === "function" ? wasm.exports.probe_widget_signal_fallback_total() : -1,
        signalDrop: typeof wasm.exports.probe_widget_signal_drop_total === "function" ? wasm.exports.probe_widget_signal_drop_total() : -1,
        signalDropCapacity: typeof wasm.exports.probe_widget_signal_drop_capacity_total === "function" ? wasm.exports.probe_widget_signal_drop_capacity_total() : -1,
        signalDropRenderOverflow: typeof wasm.exports.probe_widget_signal_drop_render_overflow_total === "function" ? wasm.exports.probe_widget_signal_drop_render_overflow_total() : -1,
        signalRenderP95: typeof wasm.exports.probe_widget_signal_render_p95_us === "function" ? wasm.exports.probe_widget_signal_render_p95_us() : -1,
        signalRenderBudget: typeof wasm.exports.probe_widget_signal_render_budget_us === "function" ? wasm.exports.probe_widget_signal_render_budget_us() : -1,
        signalRenderOverBudget: typeof wasm.exports.probe_widget_signal_render_over_budget === "function" ? wasm.exports.probe_widget_signal_render_over_budget() : -1,
        statsDropCapacity: typeof wasm.exports.probe_widget_stats_drop_capacity_total === "function" ? wasm.exports.probe_widget_stats_drop_capacity_total() : -1,
        statsDropRenderOverflow: typeof wasm.exports.probe_widget_stats_drop_render_overflow_total === "function" ? wasm.exports.probe_widget_stats_drop_render_overflow_total() : -1,
        statsRenderBudget: typeof wasm.exports.probe_widget_stats_render_budget_us === "function" ? wasm.exports.probe_widget_stats_render_budget_us() : -1,
        statsRenderOverBudget: typeof wasm.exports.probe_widget_stats_render_over_budget === "function" ? wasm.exports.probe_widget_stats_render_over_budget() : -1,
        evidenceState: typeof wasm.exports.probe_widget_evidence_state === "function" ? wasm.exports.probe_widget_evidence_state() : -1,
        signalState: typeof wasm.exports.probe_widget_signal_state === "function" ? wasm.exports.probe_widget_signal_state() : -1,
        layoutVersion: typeof wasm.exports.probe_layout_version === "function" ? wasm.exports.probe_layout_version() : -1,
        layoutMigrated: typeof wasm.exports.probe_layout_migrated === "function" ? wasm.exports.probe_layout_migrated() : -1,
        layoutLink: typeof wasm.exports.probe_layout_link_enabled === "function" ? wasm.exports.probe_layout_link_enabled() : -1,
    });

    wasm.exports._start();

    // Optional animation loop (step export).
    if (typeof wasm.exports.step === "function") {
        const odinCtx = wasm.exports.default_context_ptr();
        let prev = performance.now();
        let lastStepNow = 0;
        function frame(now) {
            syncCanvasSize();
            perfHudOnFrame(now);
            const hasPendingInput =
                keyPressedBits !== 0 ||
                keyReleasedBits !== 0 ||
                mousePressedBits !== 0 ||
                mouseReleasedBits !== 0 ||
                mouseScrollX !== 0 ||
                mouseScrollY !== 0;
            const idleThrottleActive =
                IDLE_STEP_INTERVAL_MS > 0 &&
                wsState === 2 &&
                wsMsgQueue.length === 0 &&
                (lastWsMsgTs <= 0 || (now - lastWsMsgTs) >= IDLE_QUIET_MS) &&
                !canvasSizeDirty &&
                !hasPendingInput;
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
        const cfg = window.__mr_get_runtime_config();
        const wsUrl = cfg.ws_url || cfg.default_ws_url;
        outputEl.textContent = `WASM loaded. ws=${wsUrl}`;
    }
} catch (err) {
    console.error("Failed to load WASM:", err);
    if (outputEl) {
        outputEl.classList.add("error");
        outputEl.textContent = "Failed to load WASM:\n" + err.message + "\n\n" + err.stack;
    }
}
