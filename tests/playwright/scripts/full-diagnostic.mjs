#!/usr/bin/env node

import { chromium } from "playwright";
import { mkdirSync, writeFileSync } from "fs";
import { join } from "path";

const BASE_URL = process.env.BASE_URL || "http://localhost:8090";
const SHOTS_DIR = join(process.cwd(), "tests/playwright/screenshots/full-diag");
mkdirSync(SHOTS_DIR, { recursive: true });

const consoleEvents = [];
const errors = [];
const results = [];
let shotSeq = 0;

function log(msg) { console.log(`[${new Date().toISOString()}] ${msg}`); }

async function snap(page, label) {
    shotSeq++;
    const name = `${String(shotSeq).padStart(2, "0")}-${label}.png`;
    const path = join(SHOTS_DIR, name);
    await page.screenshot({ path, fullPage: true });
    log(`  screenshot: ${name}`);
    return path;
}

async function step(name, fn) {
    log(`--- ${name} ---`);
    const t0 = Date.now();
    try {
        await fn();
        const ms = Date.now() - t0;
        results.push({ name, ok: true, ms });
        log(`  PASS (${ms}ms)`);
    } catch (err) {
        const ms = Date.now() - t0;
        results.push({ name, ok: false, ms, error: err.message });
        log(`  FAIL (${ms}ms): ${err.message}`);
    }
}

async function main() {
    const browser = await chromium.launch({
        headless: true,
        args: ["--disable-gpu", "--no-sandbox"],
    });
    const context = await browser.newContext({
        viewport: { width: 1920, height: 1080 },
    });
    const page = await context.newPage();

    // Collect ALL console messages and errors
    page.on("console", (msg) => {
        const text = msg.text();
        consoleEvents.push({ type: msg.type(), text });
        if (msg.type() === "error") errors.push(text);
    });
    page.on("pageerror", (err) => {
        errors.push(`PAGE_ERROR: ${String(err)}`);
    });

    try {
        // ==================== 1. PAGE LOAD ====================
        await step("1-page-load", async () => {
            const resp = await page.goto(BASE_URL, { waitUntil: "networkidle", timeout: 30000 });
            if (!resp || resp.status() !== 200) throw new Error(`HTTP ${resp?.status()}`);
            await page.waitForSelector("canvas#canvas", { timeout: 15000 });
            await snap(page, "page-load");
        });

        // ==================== 2. WASM INIT ====================
        await step("2-wasm-init", async () => {
            await page.waitForFunction(
                () => typeof window.__mr_wasm_exports !== "undefined",
                { timeout: 15000 }
            );
            await snap(page, "wasm-init");
        });

        // ==================== 3. WAIT FOR DATA (WS connect + initial data) ====================
        await step("3-data-flow-5s", async () => {
            await page.waitForTimeout(5000);
            await snap(page, "data-flow-5s");
        });

        // ==================== 4. DATA FLOW 15s ====================
        await step("4-data-flow-15s", async () => {
            await page.waitForTimeout(10000);
            await snap(page, "data-flow-15s");
        });

        // ==================== 5. TIMEFRAME SWITCHES ====================
        for (const [key, label] of [["1", "1m"], ["2", "5m"], ["3", "15m"], ["4", "30m"], ["5", "1h"], ["6", "4h"]]) {
            await step(`5-tf-${label}`, async () => {
                await page.keyboard.press(key);
                await page.waitForTimeout(2000);
                await snap(page, `tf-${label}`);
            });
        }

        // Go back to 1m for remaining tests
        await page.keyboard.press("1");
        await page.waitForTimeout(2000);

        // ==================== 6. INDICATORS ====================
        await step("6-indicator-panel", async () => {
            await page.keyboard.press("i");
            await page.waitForTimeout(1500);
            await snap(page, "indicator-panel");
        });

        // Toggle some individual indicators
        await step("6b-toggle-indicators", async () => {
            // Toggle MA
            await page.keyboard.press("m");
            await page.waitForTimeout(1000);
            await snap(page, "indicator-ma-on");
            // Toggle BBands
            await page.keyboard.press("b");
            await page.waitForTimeout(1000);
            await snap(page, "indicator-bbands-on");
            // Toggle RSI
            await page.keyboard.press("r");
            await page.waitForTimeout(1000);
            await snap(page, "indicator-rsi-on");
            // Toggle VWAP
            await page.keyboard.press("v");
            await page.waitForTimeout(1000);
            await snap(page, "indicator-vwap-on");
        });

        // Close indicator panel
        await page.keyboard.press("i");
        await page.waitForTimeout(500);

        // ==================== 7. DETAIL PANEL ====================
        await step("7-detail-panel", async () => {
            await page.keyboard.press("d");
            await page.waitForTimeout(2000);
            await snap(page, "detail-panel");
            await page.keyboard.press("d");
            await page.waitForTimeout(500);
        });

        // ==================== 8. STREAM PICKER ====================
        await step("8-stream-picker", async () => {
            await page.keyboard.press("p");
            await page.waitForTimeout(2000);
            await snap(page, "stream-picker");
            await page.keyboard.press("Escape");
            await page.waitForTimeout(500);
        });

        // ==================== 9. HELP OVERLAY ====================
        await step("9-help-overlay", async () => {
            await page.keyboard.press("?");
            await page.waitForTimeout(1500);
            await snap(page, "help-overlay");
            await page.keyboard.press("?");
            await page.waitForTimeout(500);
        });

        // ==================== 10. ZEN MODE ====================
        await step("10-zen-mode", async () => {
            await page.keyboard.press("z");
            await page.waitForTimeout(2000);
            await snap(page, "zen-mode");
            await page.keyboard.press("Escape");
            await page.waitForTimeout(500);
        });

        // ==================== 11. FOCUS MODE ====================
        await step("11-focus-mode", async () => {
            await page.keyboard.press("f");
            await page.waitForTimeout(2000);
            await snap(page, "focus-mode");
            await page.keyboard.press("Escape");
            await page.waitForTimeout(500);
        });

        // ==================== 12. COMPARE MODE ====================
        await step("12-compare-mode", async () => {
            await page.keyboard.press("c");
            await page.waitForTimeout(3000);
            await snap(page, "compare-mode");
        });

        // Test compare mode with TF switch
        await step("12b-compare-tf-switch", async () => {
            await page.keyboard.press("3");
            await page.waitForTimeout(2000);
            await snap(page, "compare-tf-15m");
            await page.keyboard.press("1");
            await page.waitForTimeout(2000);
            await snap(page, "compare-tf-1m");
            await page.keyboard.press("Escape");
            await page.waitForTimeout(500);
        });

        // ==================== 13. METRICS HUD ====================
        await step("13-metrics-hud", async () => {
            // Ctrl+H toggles metrics HUD
            await page.keyboard.down("Control");
            await page.keyboard.press("h");
            await page.keyboard.up("Control");
            await page.waitForTimeout(2000);
            await snap(page, "metrics-hud");
        });

        // ==================== 14. WIDGET CATALOG ====================
        await step("14-widget-catalog", async () => {
            await page.keyboard.press("w");
            await page.waitForTimeout(2000);
            await snap(page, "widget-catalog");
            await page.keyboard.press("Escape");
            await page.waitForTimeout(500);
        });

        // ==================== 15. EXCHANGE MANAGER ====================
        await step("15-exchange-manager", async () => {
            await page.keyboard.press("e");
            await page.waitForTimeout(2000);
            await snap(page, "exchange-manager");
            await page.keyboard.press("Escape");
            await page.waitForTimeout(500);
        });

        // ==================== 16. RUNTIME PROBES ====================
        await step("16-runtime-probes", async () => {
            const probeNames = [
                "probe_md_hello_received",
                "probe_md_subscribe_ack_count",
                "probe_md_seq_gap_count",
                "probe_md_prev_seq_violations",
                "probe_md_server_metrics_cadence_ms",
                "probe_md_resync_count",
                "probe_md_transport_mode",
                "probe_md_legacy_downgrade_count",
                "probe_indicator_funding_rendered",
                "probe_indicator_liq_rendered",
            ];
            const probes = await page.evaluate((names) => {
                const exports = window.__mr_wasm_exports || {};
                const out = {};
                for (const name of names) {
                    const fn = exports[name];
                    out[name] = typeof fn === "function" ? Number(fn()) : null;
                }
                return out;
            }, probeNames);
            log(`  probes: ${JSON.stringify(probes, null, 2)}`);
        });

        // ==================== 17. STABILITY - wait additional time ====================
        await step("17-stability-30s-total", async () => {
            await page.waitForTimeout(10000);
            await snap(page, "stability-30s");
        });

        // ==================== 18. CANVAS PIXEL CHECK ====================
        await step("18-canvas-pixel-check", async () => {
            const hasPixels = await page.evaluate(() => {
                const c = document.querySelector("canvas#canvas");
                if (!c) return false;
                const ctx = c.getContext("2d");
                if (!ctx) return false;
                // Sample a few points to verify rendering
                const w = c.width, h = c.height;
                const points = [
                    [w / 2, h / 2],
                    [w / 4, h / 4],
                    [3 * w / 4, h / 2],
                    [w / 2, 3 * h / 4],
                ];
                let nonBlack = 0;
                for (const [x, y] of points) {
                    const d = ctx.getImageData(Math.floor(x), Math.floor(y), 1, 1).data;
                    if (d[0] + d[1] + d[2] > 30) nonBlack++;
                }
                return nonBlack >= 2;
            });
            if (!hasPixels) throw new Error("Canvas appears mostly blank/black");
        });

        // ==================== 19. SCROLL / ZOOM ====================
        await step("19-scroll-zoom", async () => {
            // Scroll right (should scroll candle chart)
            await page.mouse.move(960, 540);
            await page.mouse.wheel(0, 200);
            await page.waitForTimeout(500);
            await snap(page, "scroll-right");
            // Scroll left
            await page.mouse.wheel(0, -200);
            await page.waitForTimeout(500);
            await snap(page, "scroll-left");
        });

        // ==================== 20. FINAL STATE ====================
        await step("20-final-state", async () => {
            await snap(page, "final-state");
        });

    } finally {
        // Gather summary
        const passed = results.filter(r => r.ok).length;
        const failed = results.filter(r => !r.ok).length;
        const errCount = errors.length;
        const consoleErrs = consoleEvents.filter(e => e.type === "error");

        log(`\n========== SUMMARY ==========`);
        log(`Steps: ${passed} passed, ${failed} failed out of ${results.length}`);
        log(`Console errors: ${consoleErrs.length}`);
        log(`Page errors: ${errors.filter(e => e.startsWith("PAGE_ERROR")).length}`);

        if (failed > 0) {
            log(`\nFAILED STEPS:`);
            for (const r of results.filter(r => !r.ok)) {
                log(`  - ${r.name}: ${r.error}`);
            }
        }
        if (consoleErrs.length > 0) {
            log(`\nCONSOLE ERRORS (first 20):`);
            for (const e of consoleErrs.slice(0, 20)) {
                log(`  - ${e.text.slice(0, 200)}`);
            }
        }

        // Write full report
        const report = { results, errors, consoleErrors: consoleErrs, consoleEventCount: consoleEvents.length };
        writeFileSync(join(SHOTS_DIR, "report.json"), JSON.stringify(report, null, 2));
        writeFileSync(
            join(SHOTS_DIR, "console.log"),
            consoleEvents.map(e => `[${e.type}] ${e.text}`).join("\n")
        );

        await context.close();
        await browser.close();

        if (failed > 0) process.exitCode = 1;
    }
}

main().catch(err => {
    console.error(err);
    process.exit(1);
});
