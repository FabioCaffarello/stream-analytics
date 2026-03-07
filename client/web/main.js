// main.js — Market Raccoon web client bootstrap orchestrator.
//
// Single ESM entry point. Delegates all work to runtime.js which
// assembles modules and starts the WASM frame loop.

import { boot } from "./runtime.js";

const outputEl = document.getElementById("output");

try {
    await boot(outputEl);
} catch (err) {
    console.error("Failed to load WASM:", err);
    if (outputEl) {
        outputEl.classList.add("error");
        outputEl.textContent = "Failed to load WASM:\n" + err.message + "\n\n" + err.stack;
    }
}
