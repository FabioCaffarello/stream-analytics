#!/usr/bin/env node
// Quick validation of DIAG-1 (compat mismatch) and DIAG-2 (stream picker duplicates) fixes.

import { chromium } from "playwright";
import { writeFileSync, mkdirSync } from "fs";
import { join } from "path";

const BASE_URL = process.env.MR_URL || "http://localhost:8090";
const SCREENSHOT_DIR = join(import.meta.dirname || ".", "screenshots");
mkdirSync(SCREENSHOT_DIR, { recursive: true });

async function sleep(ms) { return new Promise((r) => setTimeout(r, ms)); }

async function main() {
    const browser = await chromium.launch({ headless: true, args: ["--disable-gpu", "--no-sandbox"] });
    const context = await browser.newContext({ viewport: { width: 1280, height: 720 } });
    const page = await context.newPage();

    const consoleMessages = [];
    page.on("console", (msg) => consoleMessages.push({ type: msg.type(), text: msg.text() }));

    console.log("Navigating to", BASE_URL);
    await page.goto(BASE_URL, { waitUntil: "networkidle", timeout: 30000 });

    // Wait for WS connect and data flow
    console.log("Waiting for data flow (15s)...");
    await sleep(15000);

    // Take screenshot — check status bar for DESYNC
    await page.screenshot({ path: join(SCREENSHOT_DIR, "fix-01-status-bar.png") });

    // Check console for compat mismatch
    const compatLogs = consoleMessages.filter((m) => m.text.includes("compat") || m.text.includes("DESYNC") || m.text.includes("Protocol_Invalid"));
    console.log(`\n=== DIAG-1: Compat Mismatch ===`);
    console.log(`Compat/DESYNC console messages: ${compatLogs.length}`);
    for (const m of compatLogs) console.log(`  ${m.type}: ${m.text.slice(0, 200)}`);
    if (compatLogs.length === 0) console.log("  PASS: No compat mismatch detected");

    // Open stream picker and check for duplicates
    await page.keyboard.press("g");
    await sleep(800);
    await page.screenshot({ path: join(SCREENSHOT_DIR, "fix-02-stream-picker.png") });

    console.log(`\n=== DIAG-2: Stream Picker Duplicates ===`);
    // Check screenshot manually

    // Switch TFs rapidly to test seq gap behavior
    console.log("\n=== TF Switching Stress Test ===");
    for (const key of ["1", "3", "7", "5", "2", "9", "3"]) {
        await page.keyboard.press("Escape"); // close picker
        await sleep(100);
        await page.keyboard.press(key);
        await sleep(500);
    }
    await sleep(3000);
    await page.screenshot({ path: join(SCREENSHOT_DIR, "fix-03-tf-stress.png") });

    // Check for desync after TF switching
    const lateLogs = consoleMessages.filter((m) =>
        m.text.includes("DESYNC") || m.text.includes("compat") || m.text.includes("seq gap")
    );
    console.log(`DESYNC messages after TF stress: ${lateLogs.length}`);

    // Test compare mode
    console.log("\n=== DIAG-3: Compare Mode ===");
    await page.keyboard.press("c");
    await sleep(3000);
    await page.screenshot({ path: join(SCREENSHOT_DIR, "fix-04-compare-mode.png") });
    await page.keyboard.press("Escape");
    await sleep(300);

    // Check WS lifecycle
    const wsLogs = consoleMessages.filter((m) => m.text.includes("[md-lifecycle]"));
    console.log(`\n=== WS Lifecycle Summary ===`);
    console.log(`Total lifecycle messages: ${wsLogs.length}`);
    for (const m of wsLogs.slice(0, 30)) console.log(`  ${m.text.slice(0, 200)}`);

    console.log(`\nTotal console messages: ${consoleMessages.length}`);
    console.log(`Errors: ${consoleMessages.filter((m) => m.type === "error").length}`);

    await browser.close();
}

main().catch((err) => {
    console.error("Validation failed:", err);
    process.exit(1);
});
