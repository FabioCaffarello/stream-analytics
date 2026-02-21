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
            ctx.fillStyle = `rgba(${(r * 255) | 0},${(g * 255) | 0},${(b * 255) | 0},${a})`;
            ctx.fillRect(0, 0, canvas.width, canvas.height);
        },

        canvas_fill_rect(x, y, w, h, r, g, b, a) {
            if (!ctx) return;
            ctx.fillStyle = `rgba(${(r * 255) | 0},${(g * 255) | 0},${(b * 255) | 0},${a})`;
            ctx.fillRect(x, y, w, h);
        },

        canvas_fill_text(ptr, len, x, y, size, r, g, b, a) {
            if (!ctx) return;
            const text = loadStringRaw(wasm.memory.buffer, ptr, len);
            ctx.fillStyle = `rgba(${(r * 255) | 0},${(g * 255) | 0},${(b * 255) | 0},${a})`;
            ctx.font = `${size}px monospace`;
            ctx.fillText(text, x, y);
        },

        canvas_measure_text(ptr, len, size) {
            if (!ctx) return 0;
            const text = loadStringRaw(wasm.memory.buffer, ptr, len);
            ctx.font = `${size}px monospace`;
            return ctx.measureText(text).width;
        },
    },
};

// ---------------------------------------------------------------------------
// Instantiate and run
// ---------------------------------------------------------------------------

const outputEl = document.getElementById("output");

try {
    const source = typeof WebAssembly.instantiateStreaming === "function"
        ? await WebAssembly.instantiateStreaming(fetch("app.wasm"), imports)
        : await fetch("app.wasm")
              .then(r => r.arrayBuffer())
              .then(bytes => WebAssembly.instantiate(bytes, imports));

    wasm.exports = source.instance.exports;
    wasm.memory  = wasm.exports.memory;

    wasm.exports._start();

    // Optional animation loop (step export).
    if (typeof wasm.exports.step === "function") {
        const odinCtx = wasm.exports.default_context_ptr();
        let prev = performance.now();
        function frame(now) {
            const dt = (now - prev) * 0.001;
            prev = now;
            if (wasm.exports.step(dt, odinCtx)) {
                requestAnimationFrame(frame);
            } else if (typeof wasm.exports._end === "function") {
                wasm.exports._end();
            }
        }
        requestAnimationFrame(frame);
    } else if (typeof wasm.exports._end === "function") {
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
