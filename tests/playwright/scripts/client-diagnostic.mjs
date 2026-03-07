#!/usr/bin/env node
// Market Raccoon — Comprehensive Client Diagnostic via Playwright
// Usage: npx playwright test tests/playwright/client-diagnostic.mjs --reporter=list
//   or:  node tests/playwright/client-diagnostic.mjs  (standalone mode)

import { chromium } from "playwright";
import { writeFileSync, mkdirSync } from "fs";
import { join } from "path";

const BASE_URL = process.env.MR_URL || "http://localhost:8090";
const SCREENSHOT_DIR = join(import.meta.dirname || ".", "screenshots");
const REPORT = [];
const BUGS = [];
let screenshotIdx = 0;

mkdirSync(SCREENSHOT_DIR, { recursive: true });

function log(msg) {
    const ts = new Date().toISOString().slice(11, 23);
    console.log(`[${ts}] ${msg}`);
    REPORT.push({ ts, msg });
}

function bug(id, severity, description, evidence = "") {
    BUGS.push({ id, severity, description, evidence });
    log(`BUG-${id} [${severity}] ${description}${evidence ? " — " + evidence : ""}`);
}

async function screenshot(page, label) {
    screenshotIdx++;
    const name = `${String(screenshotIdx).padStart(2, "0")}-${label.replace(/\s+/g, "-").toLowerCase()}.png`;
    const path = join(SCREENSHOT_DIR, name);
    await page.screenshot({ path, fullPage: false });
    log(`Screenshot: ${name}`);
    return path;
}

async function sleep(ms) {
    return new Promise((r) => setTimeout(r, ms));
}

// Collect console messages from the page.
function attachConsoleCollector(page) {
    const messages = [];
    page.on("console", (msg) => {
        messages.push({ type: msg.type(), text: msg.text() });
    });
    page.on("pageerror", (err) => {
        messages.push({ type: "pageerror", text: String(err) });
    });
    return messages;
}

// ------------------------------------------------------------------
// Diagnostic Tests
// ------------------------------------------------------------------

async function testPageLoad(page) {
    log("=== TEST: Page Load ===");
    const response = await page.goto(BASE_URL, { waitUntil: "networkidle", timeout: 30000 });
    const status = response.status();
    log(`HTTP status: ${status}`);
    if (status !== 200) bug("LOAD-1", "P0", "Page did not return 200", `status=${status}`);

    // Check title
    const title = await page.title();
    log(`Page title: "${title}"`);
    if (!title.includes("Market Raccoon")) bug("LOAD-2", "P2", "Title missing 'Market Raccoon'", `got="${title}"`);

    // Wait for canvas to appear
    const canvas = await page.waitForSelector("canvas#canvas", { timeout: 10000 }).catch(() => null);
    if (!canvas) {
        bug("LOAD-3", "P0", "Canvas element not found after 10s");
        return false;
    }
    log("Canvas element found");

    // Wait for WASM to initialize (output text changes from "Loading WASM...")
    await sleep(3000);
    const outputText = await page.$eval("#output", (el) => el.textContent).catch(() => "");
    log(`Output element text: "${outputText.slice(0, 120)}"`);
    if (outputText.includes("Loading WASM")) {
        bug("LOAD-4", "P0", "WASM still loading after 3s", `text="${outputText.slice(0, 80)}"`);
    }

    await screenshot(page, "initial-load");
    return true;
}

async function testCanvasRendering(page) {
    log("=== TEST: Canvas Rendering ===");
    // Check canvas dimensions
    const dims = await page.$eval("canvas#canvas", (c) => ({
        w: c.width,
        h: c.height,
        cw: c.clientWidth,
        ch: c.clientHeight,
    }));
    log(`Canvas: ${dims.w}x${dims.h} (client: ${dims.cw}x${dims.ch})`);
    if (dims.w <= 0 || dims.h <= 0) {
        bug("RENDER-1", "P0", "Canvas has zero dimensions", `${dims.w}x${dims.h}`);
    }

    // Check if canvas has non-blank content by sampling pixel data
    const hasContent = await page.evaluate(() => {
        const c = document.getElementById("canvas");
        if (!c) return false;
        const ctx = c.getContext("2d");
        if (!ctx) return false;
        // Sample 5 points across the canvas
        const points = [
            [c.width * 0.25, c.height * 0.25],
            [c.width * 0.5, c.height * 0.5],
            [c.width * 0.75, c.height * 0.75],
            [c.width * 0.25, c.height * 0.75],
            [c.width * 0.75, c.height * 0.25],
        ];
        let nonBlack = 0;
        for (const [x, y] of points) {
            const d = ctx.getImageData(Math.floor(x), Math.floor(y), 1, 1).data;
            if (d[0] > 5 || d[1] > 5 || d[2] > 5) nonBlack++;
        }
        return nonBlack >= 2;
    });
    log(`Canvas has rendered content: ${hasContent}`);
    if (!hasContent) {
        bug("RENDER-2", "P1", "Canvas appears blank (all sampled pixels near black)");
    }

    await screenshot(page, "canvas-rendering");
}

async function testWebSocketConnection(page, consoleMessages) {
    log("=== TEST: WebSocket Connection ===");
    // Wait for WS connection
    await sleep(5000);

    const wsLogs = consoleMessages.filter(
        (m) => m.text.includes("[ws]") || m.text.includes("[md-")
    );
    log(`WS-related console messages: ${wsLogs.length}`);
    for (const m of wsLogs.slice(0, 20)) {
        log(`  ${m.type}: ${m.text.slice(0, 200)}`);
    }

    const connected = wsLogs.some((m) => m.text.includes("connected"));
    log(`WS connected: ${connected}`);
    if (!connected) {
        bug("WS-1", "P0", "WebSocket did not connect within 5s");
    }

    // Check WS state via page evaluation
    const wsState = await page.evaluate(() => {
        return {
            state: window.wsState || (typeof wsState !== "undefined" ? wsState : "unavailable"),
            queueLen: typeof wsMsgQueue !== "undefined" ? wsMsgQueue.length : -1,
        };
    }).catch(() => ({ state: "eval-error", queueLen: -1 }));
    log(`WS state object: ${JSON.stringify(wsState)}`);

    // Check if messages are flowing
    const msgCount = consoleMessages.filter(
        (m) => m.text.includes("event") || m.text.includes("ack") || m.text.includes("subscribe")
    ).length;
    log(`Event/ack/subscribe messages: ${msgCount}`);

    await screenshot(page, "ws-connection");
}

async function testTimeframeSwitching(page, consoleMessages) {
    log("=== TEST: Timeframe Switching ===");
    // Keys 1-9 switch timeframes: 1s, 5s, 1m, 5m, 15m, 30m, 1h, 4h, 1d
    const tfLabels = ["1s", "5s", "1m", "5m", "15m", "30m", "1h", "4h", "1d"];

    for (let i = 0; i < tfLabels.length; i++) {
        const key = String(i + 1);
        const beforeMsgCount = consoleMessages.length;
        await page.keyboard.press(key);
        await sleep(800);
        log(`Pressed key '${key}' for TF ${tfLabels[i]}`);
    }

    // Screenshot at 1m timeframe (most common)
    await page.keyboard.press("3");
    await sleep(1500);
    await screenshot(page, "timeframe-1m");

    // Screenshot at 1h timeframe
    await page.keyboard.press("7");
    await sleep(1500);
    await screenshot(page, "timeframe-1h");

    // Back to 1m for remaining tests
    await page.keyboard.press("3");
    await sleep(500);
}

async function testKeyboardShortcuts(page) {
    log("=== TEST: Keyboard Shortcuts ===");

    // Toggle indicators
    const indicators = [
        { key: "m", name: "MA" },
        { key: "b", name: "BBands" },
        { key: "v", name: "VWAP" },
        { key: "r", name: "RSI" },
        { key: "i", name: "MACD" },
        { key: "h", name: "Funding" },
        { key: "j", name: "Liq" },
        { key: "k", name: "Trade Counter" },
    ];

    for (const ind of indicators) {
        await page.keyboard.press(ind.key);
        await sleep(400);
        log(`Toggled indicator: ${ind.name} (key: ${ind.key})`);
    }
    await screenshot(page, "indicators-all-on");

    // Toggle them back off
    for (const ind of indicators) {
        await page.keyboard.press(ind.key);
        await sleep(200);
    }
    await sleep(500);
    await screenshot(page, "indicators-all-off");

    // Test focus mode (F)
    await page.keyboard.press("f");
    await sleep(600);
    await screenshot(page, "focus-mode");
    await page.keyboard.press("Escape");
    await sleep(400);

    // Test Zen mode (Z)
    await page.keyboard.press("z");
    await sleep(600);
    await screenshot(page, "zen-mode");
    await page.keyboard.press("Escape");
    await sleep(400);

    // Test detail panel (S)
    await page.keyboard.press("s");
    await sleep(400);
    await screenshot(page, "detail-panel");
    await page.keyboard.press("s");
    await sleep(300);

    // Test help overlay (Shift+/)
    await page.keyboard.press("Shift+/");
    await sleep(400);
    await screenshot(page, "help-overlay");
    await page.keyboard.press("Escape");
    await sleep(300);
}

async function testStreamPicker(page) {
    log("=== TEST: Stream Picker ===");
    // Press G to open stream picker
    await page.keyboard.press("g");
    await sleep(600);
    await screenshot(page, "stream-picker-open");

    // Close it
    await page.keyboard.press("Escape");
    await sleep(300);
}

async function testExchangeManager(page) {
    log("=== TEST: Exchange Manager ===");
    // Ctrl+K opens exchange manager
    await page.keyboard.press("Control+k");
    await sleep(600);
    await screenshot(page, "exchange-manager");

    // Close it
    await page.keyboard.press("Escape");
    await sleep(300);
}

async function testCompareMode(page) {
    log("=== TEST: Compare Mode ===");
    // Press C to enter compare mode
    await page.keyboard.press("c");
    await sleep(800);
    await screenshot(page, "compare-mode");

    // Tab to add another stream
    await page.keyboard.press("Tab");
    await sleep(600);
    await screenshot(page, "compare-mode-multi");

    // Exit compare mode
    await page.keyboard.press("Escape");
    await sleep(400);
}

async function testStreamCycling(page) {
    log("=== TEST: Stream Cycling ===");
    // Tab cycles to next stream
    await page.keyboard.press("Tab");
    await sleep(1000);
    await screenshot(page, "stream-cycle-1");

    // Shift+Tab cycles back
    await page.keyboard.press("Shift+Tab");
    await sleep(1000);
    await screenshot(page, "stream-cycle-back");
}

async function testMouseInteraction(page) {
    log("=== TEST: Mouse Interaction ===");
    const canvas = await page.$("canvas#canvas");
    if (!canvas) {
        bug("MOUSE-1", "P1", "Canvas not found for mouse test");
        return;
    }
    const box = await canvas.boundingBox();
    if (!box) return;

    // Click on canvas center (should focus a cell)
    await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
    await sleep(300);

    // Scroll up (zoom in) on canvas
    await page.mouse.wheel(0, -300);
    await sleep(500);
    await screenshot(page, "mouse-zoom-in");

    // Scroll down (zoom out)
    await page.mouse.wheel(0, 600);
    await sleep(500);
    await screenshot(page, "mouse-zoom-out");

    // Reset zoom
    await page.mouse.wheel(0, -300);
    await sleep(300);

    // Drag to pan (mouse down, move, mouse up)
    const cx = box.x + box.width / 2;
    const cy = box.y + box.height / 2;
    await page.mouse.move(cx, cy);
    await page.mouse.down();
    await page.mouse.move(cx - 100, cy, { steps: 10 });
    await sleep(200);
    await page.mouse.up();
    await sleep(300);
    await screenshot(page, "mouse-pan");
}

async function testResyncFlow(page) {
    log("=== TEST: Resync Flow ===");
    // Ctrl+R triggers resync
    await page.keyboard.press("Control+r");
    await sleep(2000);
    await screenshot(page, "resync-flow");
}

async function testConsoleErrors(consoleMessages) {
    log("=== TEST: Console Error Analysis ===");
    const errors = consoleMessages.filter((m) => m.type === "error" || m.type === "pageerror");
    const warnings = consoleMessages.filter((m) => m.type === "warning");

    log(`Total console errors: ${errors.length}`);
    log(`Total console warnings: ${warnings.length}`);

    for (const err of errors) {
        log(`  ERROR: ${err.text.slice(0, 300)}`);
    }
    for (const warn of warnings.slice(0, 10)) {
        log(`  WARN: ${warn.text.slice(0, 200)}`);
    }

    if (errors.length > 0) {
        const criticalErrors = errors.filter(
            (e) => !e.text.includes("[ws] error") && !e.text.includes("[ws] closed")
        );
        if (criticalErrors.length > 0) {
            bug(
                "CONSOLE-1",
                "P1",
                `${criticalErrors.length} non-WS console errors`,
                criticalErrors.map((e) => e.text.slice(0, 100)).join("; ")
            );
        }
    }
}

async function testPerformance(page) {
    log("=== TEST: Performance Metrics ===");
    const perf = await page.evaluate(() => {
        const entries = performance.getEntriesByType("navigation");
        const nav = entries[0] || {};
        return {
            domContentLoaded: Math.round(nav.domContentLoadedEventEnd - nav.startTime),
            load: Math.round(nav.loadEventEnd - nav.startTime),
            wasmSize: performance
                .getEntriesByType("resource")
                .filter((r) => r.name.includes("wasm"))
                .map((r) => ({ name: r.name.split("/").pop(), size: r.transferSize, duration: Math.round(r.duration) }))[0] || null,
            jsSize: performance
                .getEntriesByType("resource")
                .filter((r) => r.name.includes("main.js") || r.name.includes("runtime.js"))
                .map((r) => ({ name: r.name.split("/").pop(), size: r.transferSize, duration: Math.round(r.duration) }))[0] || null,
        };
    });
    log(`DOMContentLoaded: ${perf.domContentLoaded}ms`);
    log(`Full load: ${perf.load}ms`);
    if (perf.wasmSize) log(`WASM: ${(perf.wasmSize.size / 1024).toFixed(0)}KB, ${perf.wasmSize.duration}ms`);
    if (perf.jsSize) log(`JS host: ${(perf.jsSize.size / 1024).toFixed(0)}KB, ${perf.jsSize.duration}ms`);

    if (perf.domContentLoaded > 5000) {
        bug("PERF-1", "P2", "DOMContentLoaded > 5s", `${perf.domContentLoaded}ms`);
    }
}

async function testDataFlow(page) {
    log("=== TEST: Data Flow Validation ===");
    // Wait and let data accumulate
    await sleep(5000);

    // Check runtime config
    const config = await page.evaluate(() => {
        if (typeof window.__mr_get_runtime_config === "function") {
            return window.__mr_get_runtime_config();
        }
        return null;
    });
    if (config) {
        log(`Runtime config: mode=${config.mode}, ws_url=${config.ws_url || "(default)"}, default=${config.default_ws_url}`);
    } else {
        bug("DATA-1", "P1", "__mr_get_runtime_config not available");
    }

    await screenshot(page, "data-flow-5s");

    // Wait longer for more data
    await sleep(10000);
    await screenshot(page, "data-flow-15s");
}

async function testResponsiveness(page, browser) {
    log("=== TEST: Viewport Responsiveness ===");
    // Narrow viewport (mobile-ish)
    await page.setViewportSize({ width: 480, height: 800 });
    await sleep(1000);
    await screenshot(page, "viewport-480x800");

    // Tablet
    await page.setViewportSize({ width: 768, height: 1024 });
    await sleep(1000);
    await screenshot(page, "viewport-768x1024");

    // Wide desktop
    await page.setViewportSize({ width: 1920, height: 1080 });
    await sleep(1000);
    await screenshot(page, "viewport-1920x1080");

    // Ultra-wide
    await page.setViewportSize({ width: 2560, height: 1440 });
    await sleep(1000);
    await screenshot(page, "viewport-2560x1440");

    // Reset to standard
    await page.setViewportSize({ width: 1280, height: 720 });
    await sleep(500);
}

// ------------------------------------------------------------------
// Main
// ------------------------------------------------------------------

async function main() {
    log("========================================");
    log("Market Raccoon — Client Diagnostic Suite");
    log(`Target: ${BASE_URL}`);
    log("========================================");

    const browser = await chromium.launch({
        headless: true,
        args: ["--disable-gpu", "--no-sandbox"],
    });
    const context = await browser.newContext({
        viewport: { width: 1280, height: 720 },
        deviceScaleFactor: 1,
    });
    const page = await context.newPage();
    const consoleMessages = attachConsoleCollector(page);

    try {
        // 1. Page Load
        const loaded = await testPageLoad(page);
        if (!loaded) {
            log("FATAL: Page did not load. Aborting remaining tests.");
            return;
        }

        // 2. Canvas Rendering
        await testCanvasRendering(page);

        // 3. WebSocket Connection
        await testWebSocketConnection(page, consoleMessages);

        // 4. Performance Metrics
        await testPerformance(page);

        // 5. Data Flow
        await testDataFlow(page);

        // 6. Timeframe Switching
        await testTimeframeSwitching(page, consoleMessages);

        // 7. Keyboard Shortcuts
        await testKeyboardShortcuts(page);

        // 8. Stream Picker
        await testStreamPicker(page);

        // 9. Exchange Manager
        await testExchangeManager(page);

        // 10. Stream Cycling
        await testStreamCycling(page);

        // 11. Compare Mode
        await testCompareMode(page);

        // 12. Mouse Interaction
        await testMouseInteraction(page);

        // 13. Resync Flow
        await testResyncFlow(page);

        // 14. Responsiveness
        await testResponsiveness(page, browser);

        // 15. Console Error Analysis (run last to capture everything)
        await testConsoleErrors(consoleMessages);

    } finally {
        await browser.close();
    }

    // ------------------------------------------------------------------
    // Report
    // ------------------------------------------------------------------
    log("\n========================================");
    log("DIAGNOSTIC SUMMARY");
    log("========================================");
    log(`Total tests: 15`);
    log(`Total bugs found: ${BUGS.length}`);
    log(`Screenshots: ${screenshotIdx}`);
    log(`Console messages captured: ${consoleMessages.length}`);

    if (BUGS.length > 0) {
        log("\n--- BUGS ---");
        for (const b of BUGS) {
            log(`  BUG-${b.id} [${b.severity}] ${b.description}`);
            if (b.evidence) log(`    Evidence: ${b.evidence}`);
        }
    } else {
        log("\nNo bugs detected.");
    }

    // Write JSON report
    const reportPath = join(SCREENSHOT_DIR, "diagnostic-report.json");
    writeFileSync(
        reportPath,
        JSON.stringify({ timestamp: new Date().toISOString(), bugs: BUGS, report: REPORT, consoleMessages: consoleMessages.slice(0, 500) }, null, 2)
    );
    log(`\nReport written to: ${reportPath}`);
}

main().catch((err) => {
    console.error("Diagnostic failed:", err);
    process.exit(1);
});
