// runtime.js — WASM import object assembly and frame loop.
//
// Pulls foreign procs from each module and assembles the import object
// that the Odin js_wasm32 runtime expects (env + odin_env namespaces).

import { loadStringRaw, wasmRef } from "./modules/memory.js";
import { initCanvas } from "./modules/canvas.js";
import { initInput } from "./modules/input.js";
import { initWebSocket } from "./modules/websocket.js";
import { createStorageProcs } from "./modules/storage.js";
import {
    PERF_HUD_ENABLED,
    IDLE_STEP_INTERVAL_MS,
    IDLE_QUIET_MS,
    initPerfHud,
} from "./modules/perf.js";
import { installProbeHooks } from "./modules/probes.js";

// --- console write buffer (fd 1 = stdout, fd 2 = stderr) ---

function createConsoleWriter(perfHud) {
    let writeBuffer = "";
    let lastFd = null;

    return function writeToConsole(fd, str) {
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
            perfHud.consumeConsoleLine(writeBuffer);
            (fd === 2 ? console.error : console.log)(writeBuffer);
            writeBuffer = "";
            lastFd = null;
        } else {
            writeBuffer += str;
        }
    };
}

// --- build import object ---

function buildImports(canvasMod, inputMod, wsMod, storageMod, writeToConsole) {
    // Buffer accessor — always reads current memory after instantiation.
    const buf = () => wasmRef.memory.buffer;

    return {
        env: {},

        odin_env: {
            // runtime I/O
            write(fd, ptr, len) {
                writeToConsole(fd, loadStringRaw(buf(), ptr, len));
            },
            trap()  { throw new Error("odin: trap"); },
            abort() { throw new Error("odin: abort"); },
            alert(ptr, len) { alert(loadStringRaw(buf(), ptr, len)); },
            evaluate(ptr, len) { eval.call(null, loadStringRaw(buf(), ptr, len)); },

            // time
            time_now:   () => BigInt(Date.now()),
            tick_now:   () => performance.now(),
            time_sleep() {},

            // math intrinsics
            sqrt:    Math.sqrt,
            sin:     Math.sin,
            cos:     Math.cos,
            pow:     Math.pow,
            fmuladd: (x, y, z) => x * y + z,
            ln:      Math.log,
            exp:     Math.exp,
            ldexp:   (x, exp) => x * 2 ** exp,

            // crypto
            rand_bytes(addr, len) {
                crypto.getRandomValues(new Uint8Array(buf(), addr, len));
            },

            // Canvas2D
            canvas_clear: canvasMod.canvas_clear,
            canvas_fill_rect: canvasMod.canvas_fill_rect,
            canvas_fill_text(ptr, len, x, y, size, r, g, b, a) {
                canvasMod.canvas_fill_text(ptr, len, x, y, size, r, g, b, a, buf());
            },
            canvas_measure_text(ptr, len, size) {
                return canvasMod.canvas_measure_text(ptr, len, size, buf());
            },
            canvas_line: canvasMod.canvas_line,
            canvas_clip_push: canvasMod.canvas_clip_push,
            canvas_clip_pop: canvasMod.canvas_clip_pop,
            canvas_width: canvasMod.canvas_width,
            canvas_height: canvasMod.canvas_height,

            // WebSocket
            ws_connect(url_ptr, url_len, hdr_ptr, hdr_len) {
                wsMod.ws_connect(url_ptr, url_len, hdr_ptr, hdr_len, buf());
            },
            ws_send(ptr, len) {
                wsMod.ws_send(ptr, len, buf());
            },
            ws_close: wsMod.ws_close,
            ws_state: wsMod.ws_state,
            ws_drop_count: wsMod.ws_drop_count,
            ws_poll_msg(buf_ptr, buf_len) {
                return wsMod.ws_poll_msg(buf_ptr, buf_len, buf());
            },

            // Input
            key_state: inputMod.key_state,
            key_pressed_state: inputMod.key_pressed_state,
            key_released_state: inputMod.key_released_state,
            mouse_x: inputMod.mouse_x,
            mouse_y: inputMod.mouse_y,
            mouse_buttons: inputMod.mouse_buttons,
            mouse_pressed_buttons: inputMod.mouse_pressed_buttons,
            mouse_released_buttons: inputMod.mouse_released_buttons,
            mouse_scroll_x: inputMod.mouse_scroll_x,
            mouse_scroll_y: inputMod.mouse_scroll_y,
            modifier_state: inputMod.modifier_state,

            // Storage / clipboard / HTTP
            web_settings_load(key_ptr, key_len, out_ptr, out_cap) {
                return storageMod.web_settings_load(key_ptr, key_len, out_ptr, out_cap, buf());
            },
            web_settings_save(key_ptr, key_len, value_ptr, value_len) {
                return storageMod.web_settings_save(key_ptr, key_len, value_ptr, value_len, buf());
            },
            web_clipboard_write(text_ptr, text_len) {
                return storageMod.web_clipboard_write(text_ptr, text_len, buf());
            },
            http_get_sync(url_ptr, url_len, out_ptr, out_cap) {
                return storageMod.http_get_sync(url_ptr, url_len, out_ptr, out_cap, buf());
            },
            // S126: HTTP PUT + workspace sync/load.
            http_put_sync(url_ptr, url_len, body_ptr, body_len, out_ptr, out_cap) {
                return storageMod.http_put_sync(url_ptr, url_len, body_ptr, body_len, out_ptr, out_cap, buf());
            },
            web_workspace_load() {
                return storageMod.web_workspace_load(buf());
            },
            web_workspace_sync() {
                return storageMod.web_workspace_sync(buf());
            },
        },
    };
}

// --- frame loop ---

function startFrameLoop(canvasMod, inputMod, wsMod, perfHud) {
    if (typeof wasmRef.exports.step !== "function") {
        canvasMod.syncSize(true);
        if (typeof wasmRef.exports._end === "function") {
            wasmRef.exports._end();
        }
        return;
    }

    const odinCtx = wasmRef.exports.default_context_ptr();
    let prev = performance.now();
    let lastStepNow = 0;

    function frame(now) {
        canvasMod.syncSize();
        perfHud.onFrame(now, wsMod, canvasMod);

        const idleThrottleActive =
            IDLE_STEP_INTERVAL_MS > 0 &&
            wsMod.getState() === 2 &&
            wsMod.getQueueLength() === 0 &&
            (wsMod.getLastMsgTs() <= 0 || (now - wsMod.getLastMsgTs()) >= IDLE_QUIET_MS) &&
            !canvasMod.isSizeDirty() &&
            !inputMod.hasPendingInput();

        if (idleThrottleActive && lastStepNow > 0 && (now - lastStepNow) < IDLE_STEP_INTERVAL_MS) {
            perfHud.recordIdleSkip();
            requestAnimationFrame(frame);
            return;
        }

        const dt = (now - prev) * 0.001;
        prev = now;
        lastStepNow = now;
        const stepCpuStart = PERF_HUD_ENABLED ? performance.now() : 0;
        const shouldContinue = wasmRef.exports.step(dt, odinCtx);
        if (PERF_HUD_ENABLED) {
            perfHud.onStep(now, dt, performance.now() - stepCpuStart);
        }
        if (shouldContinue) {
            requestAnimationFrame(frame);
        } else if (typeof wasmRef.exports._end === "function") {
            wasmRef.exports._end();
        }
    }

    requestAnimationFrame(frame);
}

// --- public API ---

export async function boot(outputEl) {
    const perfHud = initPerfHud();
    const canvasMod = initCanvas();
    const inputMod = initInput(canvasMod.canvas);
    const wsMod = initWebSocket();
    const storageMod = createStorageProcs();
    const writeToConsole = createConsoleWriter(perfHud);
    const imports = buildImports(canvasMod, inputMod, wsMod, storageMod, writeToConsole);

    const wasmUrl = "app.wasm";
    const source = typeof WebAssembly.instantiateStreaming === "function"
        ? await WebAssembly.instantiateStreaming(fetch(wasmUrl), imports)
        : await fetch(wasmUrl)
              .then(r => r.arrayBuffer())
              .then(bytes => WebAssembly.instantiate(bytes, imports));

    wasmRef.exports = source.instance.exports;
    wasmRef.memory  = wasmRef.exports.memory;

    installProbeHooks(wasmRef.exports);

    wasmRef.exports._start();
    startFrameLoop(canvasMod, inputMod, wsMod, perfHud);

    if (outputEl) {
        const cfg = window.__mr_get_runtime_config();
        const wsUrl = cfg.ws_url || cfg.default_ws_url;
        outputEl.textContent = `WASM loaded. ws=${wsUrl}`;
    }
}
