// odin.js -- Minimal ESM loader for Odin js_wasm32 WebAssembly modules.
//
// This file implements the host-side bindings that the Odin runtime expects
// when targeting js_wasm32.  It is intentionally small: only the "env" and
// "odin_env" import namespaces are provided, which is everything an Odin
// program that uses fmt.println / core:fmt needs.
//
// Architecture notes (from research on thetarnav/odin-wasm and Odin's
// vendor/wasm/js/odin.js):
//
//   1.  Odin's js_wasm32 target produces a *standard* WebAssembly module.
//       There is NO custom binary format -- it uses the normal .wasm spec.
//
//   2.  The module does NOT run autonomously.  It requires a JS host to
//       supply two import namespaces:
//         - "env"      : may provide `memory` (if --import-memory is used)
//         - "odin_env" : write, trap, abort, time_now, tick_now, math fns,
//                        rand_bytes, etc.
//
//   3.  After instantiation the host calls exports._start() which triggers
//       Odin's runtime init + the user's main() procedure.
//
//   4.  If the Odin program exports a `step(dt: f64, ctx: rawptr) -> bool`
//       procedure, the host can drive a requestAnimationFrame loop.
//       Otherwise _start() is sufficient (batch / one-shot programs).
//
//   5.  exports.default_context_ptr() returns a rawptr to the implicit
//       Odin context -- required for calling any exported Odin proc that
//       uses the implicit context (which is nearly all of them).
//
//   6.  The thetarnav/odin-wasm project rewrites the official IIFE-based
//       odin.js into ES modules (wasm/runtime.js, wasm/env.js,
//       wasm/memory.js, etc.) with JSDoc types and tree-shakeable exports.
//       The architecture is identical; only the module format differs.
//
// This file follows the ESM approach but is self-contained (no npm deps).
// When odin-wasm is added as a dependency later, this file can be replaced
// by importing from "odin-wasm/wasm/runtime.js".

// ---------------------------------------------------------------------------
// Memory helpers
// ---------------------------------------------------------------------------

const TEXT_DECODER = new TextDecoder();
const TEXT_ENCODER = new TextEncoder();

/**
 * Read an Odin string (ptr + len) out of WASM linear memory.
 *
 * @param {ArrayBuffer} buffer - The WASM memory buffer.
 * @param {number}      ptr    - Byte offset of the first character.
 * @param {number}      len    - Byte length of the string.
 * @returns {string}
 */
function loadStringRaw(buffer, ptr, len) {
    return TEXT_DECODER.decode(new Uint8Array(buffer, ptr, len));
}

// ---------------------------------------------------------------------------
// Console write buffer  (fd 1 = stdout, fd 2 = stderr)
// ---------------------------------------------------------------------------

/** @type {string} */
let writeBuffer = "";
/** @type {number|null} */
let lastFd = null;

/**
 * @param {number} fd  - File descriptor (1=stdout, 2=stderr).
 * @param {string} str - The string fragment to write.
 */
function writeToConsole(fd, str) {
    if (fd !== 1 && fd !== 2) {
        throw new Error(`odin_env.write: invalid fd=${fd}`);
    }
    // Flush on newline or fd change.
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
// Build the import object that Odin's js_wasm32 runtime expects.
// ---------------------------------------------------------------------------

/**
 * @typedef {Object} OdinExports
 * @property {WebAssembly.Memory} memory
 * @property {() => void}         _start
 * @property {() => void}         [_end]
 * @property {() => number}       default_context_ptr
 * @property {(dt: number, ctx: number) => boolean} [step]
 */

/**
 * Mutable reference so import closures can read the live memory/exports
 * after instantiation.
 */
const wasm = {
    /** @type {OdinExports|null} */
    exports: null,
    /** @type {WebAssembly.Memory|null} */
    memory: null,
};

/**
 * The two import namespaces required by every Odin js_wasm32 binary.
 * Additional namespaces (odin_dom, webgl, webgl2, ctx2d) are only needed
 * when the Odin code imports the corresponding vendor packages.
 *
 * @type {WebAssembly.Imports}
 */
const imports = {
    env: {},

    odin_env: {
        /**
         * write(fd, ptr, len) -- called by Odin's runtime for fmt output.
         */
        write(fd, ptr, len) {
            const str = loadStringRaw(wasm.memory.buffer, ptr, len);
            writeToConsole(fd, str);
        },

        trap() {
            throw new Error("odin: trap");
        },

        alert(ptr, len) {
            alert(loadStringRaw(wasm.memory.buffer, ptr, len));
        },

        abort() {
            throw new Error("odin: abort");
        },

        evaluate(ptr, len) {
            eval.call(null, loadStringRaw(wasm.memory.buffer, ptr, len));
        },

        // Returns i64 (bigint) milliseconds since epoch.
        time_now: () => BigInt(Date.now()),

        // Returns f64 milliseconds (high-resolution).
        tick_now: () => performance.now(),

        time_sleep(_duration_ms) {
            // No-op in browser -- can't block the main thread.
        },

        // Math intrinsics that the Odin runtime imports rather than
        // compiling via WASM intrinsics.
        sqrt:    Math.sqrt,
        sin:     Math.sin,
        cos:     Math.cos,
        pow:     Math.pow,
        fmuladd: (x, y, z) => x * y + z,
        ln:      Math.log,
        exp:     Math.exp,
        ldexp:   (x, exp) => x * Math.pow(2, exp),

        rand_bytes(addr, len) {
            const view = new Uint8Array(wasm.memory.buffer, addr, len);
            crypto.getRandomValues(view);
        },
    },
};

// ---------------------------------------------------------------------------
// Fetch, compile, instantiate, and run.
// ---------------------------------------------------------------------------

const outputEl = document.getElementById("output");

try {
    const wasmPath = "app.wasm";
    const wasmFetch = fetch(wasmPath);

    /** @type {WebAssembly.WebAssemblyInstantiatedSource} */
    const source = typeof WebAssembly.instantiateStreaming === "function"
        ? await WebAssembly.instantiateStreaming(wasmFetch, imports)
        : await wasmFetch
              .then(r => r.arrayBuffer())
              .then(bytes => WebAssembly.instantiate(bytes, imports));

    wasm.exports = /** @type {OdinExports} */ (source.instance.exports);
    wasm.memory  = wasm.exports.memory;

    // ------------------------------------------------------------------
    // _start() invokes Odin's runtime init + user main().
    // For a one-shot program (like fmt.println) this is all that's needed.
    // ------------------------------------------------------------------
    wasm.exports._start();

    // ------------------------------------------------------------------
    // Optional: if the Odin module exports a `step` procedure, drive a
    // requestAnimationFrame loop.  This is the standard pattern from
    // Odin's vendor/wasm/js/odin.js for interactive/game programs.
    // ------------------------------------------------------------------
    if (typeof wasm.exports.step === "function") {
        const odinCtx = wasm.exports.default_context_ptr();
        let prev = performance.now();

        /** @param {number} now */
        function frame(now) {
            const dt = (now - prev) * 0.001; // seconds
            prev = now;
            const keepGoing = wasm.exports.step(dt, odinCtx);
            if (keepGoing) {
                requestAnimationFrame(frame);
            } else if (typeof wasm.exports._end === "function") {
                wasm.exports._end();
            }
        }
        requestAnimationFrame(frame);
    } else {
        // Batch program -- call _end if it exists.
        if (typeof wasm.exports._end === "function") {
            wasm.exports._end();
        }
    }

    if (outputEl) {
        outputEl.textContent = "WASM loaded successfully. Check the browser console for output.";
    }
} catch (err) {
    console.error("Failed to load WASM:", err);
    if (outputEl) {
        outputEl.classList.add("error");
        outputEl.textContent = "Failed to load WASM:\n" + err.message + "\n\n" + err.stack;
    }
}
