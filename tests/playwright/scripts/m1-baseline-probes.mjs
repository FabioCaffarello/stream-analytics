#!/usr/bin/env node

import { chromium } from "playwright";
import { mkdirSync, writeFileSync } from "fs";
import { dirname, join, resolve } from "path";
import { fileURLToPath } from "url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);
const REPO_ROOT = resolve(__dirname, "../../..");
const EVIDENCE_DIR = resolve(REPO_ROOT, ".context/evidence");
const SCREENSHOT_DIR = resolve(EVIDENCE_DIR, "screenshots/m1");

const BASE_URL = process.env.MR_URL || "http://localhost:8090";
const VIEWPORT = { width: 1280, height: 900 };
const DATE = new Date().toISOString().slice(0, 10);

const OUT_JSON = resolve(EVIDENCE_DIR, `m1-playwright-baseline-${DATE}.json`);
const OUT_MD = resolve(EVIDENCE_DIR, `m1-playwright-baseline-${DATE}.md`);

mkdirSync(EVIDENCE_DIR, { recursive: true });
mkdirSync(SCREENSHOT_DIR, { recursive: true });

function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms));
}

function toPct(num, den) {
  if (!den || den <= 0) return 0;
  return Number(((num / den) * 100).toFixed(2));
}

function summarizeLifecycle(messages) {
  const isLifecycle = (s) => s.includes("[md-lifecycle]");
  return {
    total: messages.length,
    errors: messages.filter((m) => m.type === "error" || m.type === "pageerror").length,
    lifecycle_total: messages.filter((m) => isLifecycle(m.text)).length,
    subscribe_sent: messages.filter((m) => m.text.includes("subscribe_sent")).length,
    unsubscribe_sent: messages.filter((m) => m.text.includes("unsubscribe_sent")).length,
    ack_recv: messages.filter((m) => m.text.includes("ack_recv")).length,
    reconnect_requested: messages.filter((m) => m.text.includes("reconnect_requested")).length,
    desync_recovery: messages.filter((m) => m.text.includes("desync_recovery")).length,
    resync_sent: messages.filter((m) => m.text.includes("resync_sent")).length,
  };
}

async function ensureNoCache(page, cdp) {
  await cdp.send("Network.enable");
  await cdp.send("Network.clearBrowserCache");
  await cdp.send("Network.clearBrowserCookies");
  await cdp.send("Network.setCacheDisabled", { cacheDisabled: true });
  // Defensive check so report documents the applied policy.
  const disabled = await cdp.send("Network.getCacheDisabled").catch(() => ({ cacheDisabled: true }));
  return !!disabled.cacheDisabled;
}

async function clearWebStorage(page) {
  await page.evaluate(async () => {
    try { localStorage.clear(); } catch {}
    try { sessionStorage.clear(); } catch {}
    try {
      if (typeof indexedDB !== "undefined" && typeof indexedDB.databases === "function") {
        const dbs = await indexedDB.databases();
        for (const db of dbs) {
          if (db && db.name) {
            try { indexedDB.deleteDatabase(db.name); } catch {}
          }
        }
      }
    } catch {}
  });
}

async function waitForWasm(page, timeoutMs = 15_000) {
  await page.waitForFunction(() => !!window.__mr_wasm_exports, { timeout: timeoutMs });
}

async function waitForOnlineAck(page, minAck = 4, timeoutMs = 15_000) {
  try {
    await page.waitForFunction(
      (targetAck) => {
        const ex = window.__mr_wasm_exports;
        return !!ex && typeof ex.probe_md_subscribe_ack_count === "function" && ex.probe_md_subscribe_ack_count() >= targetAck;
      },
      minAck,
      { timeout: timeoutMs },
    );
    return true;
  } catch {
    return false;
  }
}

async function readRuntime(page) {
  return page.evaluate(() => {
    const ex = window.__mr_wasm_exports;
    const probeFn = window.__mr_widget_probe;
    const probe = typeof probeFn === "function" ? probeFn() : null;

    const safe = (fn, fallback = null) => {
      try {
        return typeof fn === "function" ? fn() : fallback;
      } catch {
        return fallback;
      }
    };

    return {
      tf_index: safe(ex?.probe_active_tf_index, null),
      timeframe_switches_total: safe(ex?.probe_timeframe_switches_total, null),
      stream_count: safe(ex?.probe_stream_count, null),
      stream_switches_total: safe(ex?.probe_stream_switches_total, null),
      subscribe_ack_count: safe(ex?.probe_md_subscribe_ack_count, null),
      ui_actions_enqueued_total: safe(ex?.probe_ui_actions_enqueued_total, null),
      active_live_candle: safe(ex?.probe_active_live_candle, null),
      active_live_stats: safe(ex?.probe_active_live_stats, null),
      active_live_heatmap: safe(ex?.probe_active_live_heatmap, null),
      active_live_vpvr: safe(ex?.probe_active_live_vpvr, null),
      tape_parse_total: probe ? probe.tapeParse : null,
      tape_drop_total: probe ? probe.tapeDrop : null,
      dom_parse_total: probe ? probe.domParse : null,
      dom_drop_total: probe ? probe.domDrop : null,
      md_alloc_estimate_total: probe ? probe.mdAlloc : null,
      layout_version: probe ? probe.layoutVersion : null,
      layout_migrated: probe ? probe.layoutMigrated : null,
    };
  });
}

async function readDomShape(page) {
  return page.evaluate(() => {
    const all = Array.from(document.querySelectorAll("*"));
    const interactive = all.filter((el) => {
      if (["A", "BUTTON", "INPUT", "SELECT", "TEXTAREA"].includes(el.tagName)) return true;
      return !!el.getAttribute("role");
    });
    return {
      title: document.title,
      body_text: (document.body?.innerText || "").trim().slice(0, 200),
      interactive_count: interactive.length,
      has_canvas: !!document.querySelector("canvas"),
      has_output_pre: !!document.querySelector("#output"),
    };
  });
}

async function scenarioScreenshot(page, name) {
  const path = join(SCREENSHOT_DIR, `${DATE}-${name}.png`);
  await page.screenshot({ path, fullPage: true });
  return path;
}

function delta(before, after, key) {
  const a = before?.[key];
  const b = after?.[key];
  if (typeof a !== "number" || typeof b !== "number") return null;
  return b - a;
}

function buildMarkdown(report) {
  const c = report.scenarios.cold_start;
  const o = report.scenarios.online_baseline;
  const k = report.scenarios.keyboard_tf_switch;
  const cl = report.scenarios.click_tf_switch;

  const lines = [];
  lines.push(`# M1 Playwright Baseline (${report.date})`);
  lines.push("");
  lines.push(`- URL: \`${report.base_url}\``);
  lines.push(`- Cache disabled: \`${report.cache_disabled}\``);
  lines.push(`- Storage reset: \`${report.storage_cleared}\``);
  lines.push("");
  lines.push("## Scenarios");
  lines.push("");
  lines.push("### Cold Start");
  lines.push(`- tf: ${c.metrics.tf_index}, stream_count: ${c.metrics.stream_count}, ack: ${c.metrics.subscribe_ack_count}`);
  lines.push(`- interactive_count(DOM): ${c.dom.interactive_count}`);
  lines.push(`- screenshot: \`${c.screenshot}\``);
  lines.push("");
  lines.push("### Online Baseline");
  lines.push(`- tf: ${o.metrics.tf_index}, stream_count: ${o.metrics.stream_count}, ack: ${o.metrics.subscribe_ack_count}`);
  lines.push(`- tape drop rate: ${o.derived.tape_drop_pct}% (drop=${o.metrics.tape_drop_total}, parse=${o.metrics.tape_parse_total})`);
  lines.push(`- screenshot: \`${o.screenshot}\``);
  lines.push("");
  lines.push("### Keyboard TF Switch (key=2)");
  lines.push(`- tf delta: ${k.derived.tf_delta}, timeframe_switches delta: ${k.derived.timeframe_switches_delta}`);
  lines.push(`- ack delta: ${k.derived.ack_delta}, stream_count delta: ${k.derived.stream_count_delta}`);
  lines.push(`- screenshot: \`${k.screenshot}\``);
  lines.push("");
  lines.push("### Click TF Switch (3 clicks)");
  lines.push(`- clicks: ${cl.clicks.length}`);
  lines.push(`- timeframe_switches delta: ${cl.derived.timeframe_switches_delta}`);
  lines.push(`- stream_count delta: ${cl.derived.stream_count_delta}`);
  lines.push(`- ack delta: ${cl.derived.ack_delta}`);
  lines.push(`- screenshot: \`${cl.screenshot}\``);
  lines.push("");

  lines.push("## Warnings");
  if (report.warnings.length === 0) {
    lines.push("- none");
  } else {
    for (const w of report.warnings) {
      lines.push(`- ${w}`);
    }
  }
  lines.push("");
  lines.push("## Files");
  lines.push(`- json: \`${report.output_json}\``);
  lines.push(`- md: \`${report.output_md}\``);

  return lines.join("\n");
}

async function main() {
  const browser = await chromium.launch({ headless: true, args: ["--disable-gpu", "--no-sandbox"] });
  const context = await browser.newContext({ viewport: VIEWPORT });
  const page = await context.newPage();

  const consoleMessages = [];
  page.on("console", (msg) => {
    consoleMessages.push({
      type: msg.type(),
      text: msg.text(),
      ts: Date.now(),
    });
  });
  page.on("pageerror", (err) => {
    consoleMessages.push({ type: "pageerror", text: String(err), ts: Date.now() });
  });

  const cdp = await context.newCDPSession(page);
  const cacheDisabled = await ensureNoCache(page, cdp);

  // Cold start with clean storage/cookies/cache.
  await page.goto(BASE_URL, { waitUntil: "domcontentloaded", timeout: 30_000 });
  await waitForWasm(page);
  await clearWebStorage(page);
  await ensureNoCache(page, cdp);
  await page.reload({ waitUntil: "domcontentloaded" });
  await waitForWasm(page);
  await sleep(1_200);

  const coldLogStart = 0;
  const coldMetrics = await readRuntime(page);
  const coldDom = await readDomShape(page);
  const coldScreenshot = await scenarioScreenshot(page, "cold-start");
  const coldLogs = consoleMessages.slice(coldLogStart);

  // Online baseline from clean state.
  const onlineLogStart = consoleMessages.length;
  await page.evaluate(() => localStorage.setItem("mr.settings.auto_connect", "1"));
  await ensureNoCache(page, cdp);
  await page.reload({ waitUntil: "domcontentloaded" });
  await waitForWasm(page);
  const onlineAckReady = await waitForOnlineAck(page, 4, 15_000);
  await sleep(1_000);
  const onlineMetrics = await readRuntime(page);
  const onlineScreenshot = await scenarioScreenshot(page, "online-baseline");
  const onlineLogs = consoleMessages.slice(onlineLogStart);

  // Keyboard TF switch scenario.
  const keyLogStart = consoleMessages.length;
  const keyBefore = await readRuntime(page);
  await page.keyboard.press("2");
  await sleep(800);
  const keyAfter = await readRuntime(page);
  const keyScreenshot = await scenarioScreenshot(page, "keyboard-tf-switch");
  const keyLogs = consoleMessages.slice(keyLogStart);

  // Click TF switch scenario.
  const clickLogStart = consoleMessages.length;
  const clickBefore = await readRuntime(page);
  const clickPoints = [440, 470, 500];
  const clickSteps = [];
  let stepLogStart = clickLogStart;

  for (const x of clickPoints) {
    await page.mouse.click(x, 14);
    await sleep(450);
    const afterStep = await readRuntime(page);
    const stepLogs = consoleMessages.slice(stepLogStart);
    clickSteps.push({
      x,
      y: 14,
      metrics: afterStep,
      lifecycle: summarizeLifecycle(stepLogs),
    });
    stepLogStart = consoleMessages.length;
  }

  const clickAfter = await readRuntime(page);
  const clickScreenshot = await scenarioScreenshot(page, "click-tf-switch");
  const clickLogs = consoleMessages.slice(clickLogStart);

  const report = {
    date: DATE,
    generated_at: new Date().toISOString(),
    base_url: BASE_URL,
    viewport: VIEWPORT,
    cache_disabled: cacheDisabled,
    storage_cleared: true,
    scenarios: {
      cold_start: {
        metrics: coldMetrics,
        dom: coldDom,
        lifecycle: summarizeLifecycle(coldLogs),
        screenshot: coldScreenshot,
      },
      online_baseline: {
        online_ack_ready: onlineAckReady,
        metrics: onlineMetrics,
        lifecycle: summarizeLifecycle(onlineLogs),
        derived: {
          tape_drop_pct: toPct(onlineMetrics.tape_drop_total, onlineMetrics.tape_parse_total),
          dom_drop_pct: toPct(onlineMetrics.dom_drop_total, onlineMetrics.dom_parse_total),
        },
        screenshot: onlineScreenshot,
      },
      keyboard_tf_switch: {
        before: keyBefore,
        after: keyAfter,
        lifecycle: summarizeLifecycle(keyLogs),
        derived: {
          tf_delta: delta(keyBefore, keyAfter, "tf_index"),
          timeframe_switches_delta: delta(keyBefore, keyAfter, "timeframe_switches_total"),
          stream_count_delta: delta(keyBefore, keyAfter, "stream_count"),
          ack_delta: delta(keyBefore, keyAfter, "subscribe_ack_count"),
          ui_actions_delta: delta(keyBefore, keyAfter, "ui_actions_enqueued_total"),
        },
        screenshot: keyScreenshot,
      },
      click_tf_switch: {
        before: clickBefore,
        after: clickAfter,
        clicks: clickSteps,
        lifecycle: summarizeLifecycle(clickLogs),
        derived: {
          timeframe_switches_delta: delta(clickBefore, clickAfter, "timeframe_switches_total"),
          stream_count_delta: delta(clickBefore, clickAfter, "stream_count"),
          ack_delta: delta(clickBefore, clickAfter, "subscribe_ack_count"),
          ui_actions_delta: delta(clickBefore, clickAfter, "ui_actions_enqueued_total"),
        },
        screenshot: clickScreenshot,
      },
    },
    warnings: [],
    output_json: OUT_JSON,
    output_md: OUT_MD,
  };

  // Rule-based warnings for quick triage.
  const kDelta = report.scenarios.keyboard_tf_switch.derived.timeframe_switches_delta;
  if (typeof kDelta === "number" && kDelta !== 1) {
    report.warnings.push(`Keyboard TF switch expected delta=1, observed delta=${kDelta}`);
  }

  const clickTfDelta = report.scenarios.click_tf_switch.derived.timeframe_switches_delta;
  if (typeof clickTfDelta === "number" && clickTfDelta > clickPoints.length) {
    report.warnings.push(`Click TF switches exceed clicks (${clickTfDelta} switches for ${clickPoints.length} clicks)`);
  }

  const clickStreamDelta = report.scenarios.click_tf_switch.derived.stream_count_delta;
  if (typeof clickStreamDelta === "number" && clickStreamDelta > 0) {
    report.warnings.push(`Stream count increased after TF clicks (delta=${clickStreamDelta})`);
  }

  const tapeDropPct = report.scenarios.online_baseline.derived.tape_drop_pct;
  if (tapeDropPct > 20) {
    report.warnings.push(`Tape drop rate above budget candidate (${tapeDropPct}%)`);
  }

  writeFileSync(OUT_JSON, JSON.stringify(report, null, 2));
  writeFileSync(OUT_MD, buildMarkdown(report));

  await browser.close();

  console.log(`M1 baseline generated:`);
  console.log(`- ${OUT_JSON}`);
  console.log(`- ${OUT_MD}`);
  if (report.warnings.length > 0) {
    console.log("Warnings:");
    for (const w of report.warnings) {
      console.log(`- ${w}`);
    }
  }
}

main().catch((err) => {
  console.error("m1-baseline-probes failed:", err);
  process.exit(1);
});
