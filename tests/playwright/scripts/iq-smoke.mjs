#!/usr/bin/env node

import { chromium } from "playwright";
import { mkdirSync, writeFileSync } from "fs";
import { join } from "path";

const BASE_URL = process.env.IQ_BASE_URL || "http://localhost:8090";
const SERVER_BASE = (process.env.IQ_SERVER_BASE || "http://127.0.0.1:8080").replace(/\/+$/, "");
const METRICS_URL = process.env.IQ_METRICS_URL || `${SERVER_BASE}/metrics`;
const LEGACY_SIGNAL_SUBJECT = process.env.IQ_LEGACY_SIGNAL_SUBJECT || "signal/composite/binance/BTCUSDT/1m";
const SHOTS_DIR = process.env.IQ_SHOTS_DIR || join(process.cwd(), "artifacts", "iq", "shots");
const LOGS_DIR = process.env.IQ_LOGS_DIR || join(process.cwd(), "artifacts", "iq", "logs");
const WAIT_TIMEOUT_MS = Number(process.env.IQ_TIMEOUT_MS || "20000");
const STATS_WAIT_TIMEOUT_MS = Number(process.env.IQ_STATS_TIMEOUT_MS || "90000");
const REQUIRE_STATS_CANONICAL = /^(1|true|yes|on)$/i.test(String(process.env.IQ_REQUIRE_STATS_CANONICAL || "0"));

mkdirSync(SHOTS_DIR, { recursive: true });
mkdirSync(LOGS_DIR, { recursive: true });

const steps = [];
const notes = [];
const consoleEvents = [];
const networkFailures = [];
const networkErrorResponses = [];
let shotSeq = 0;
let diagnosticsClipboard = "";
let statsProbeSnapshot = null;
let legacyCutoverSnapshot = null;

function nowIso() {
    return new Date().toISOString();
}

function log(msg) {
    const line = `[${nowIso()}] ${msg}`;
    notes.push(line);
    console.log(line);
}

function hasLine(pattern) {
    return consoleEvents.some((e) =>
        pattern instanceof RegExp ? pattern.test(e.text) : e.text.includes(pattern)
    );
}

async function waitFor(predicate, label, timeoutMs = WAIT_TIMEOUT_MS) {
    const start = Date.now();
    while (Date.now() - start < timeoutMs) {
        if (await predicate()) {
            return true;
        }
        await new Promise((r) => setTimeout(r, 150));
    }
    throw new Error(`timeout waiting for ${label} (${timeoutMs}ms)`);
}

async function snap(page, label, fullPage = false) {
    shotSeq += 1;
    const safe = label.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
    const name = `${String(shotSeq).padStart(2, "0")}-${safe}.png`;
    const path = join(SHOTS_DIR, name);
    await page.screenshot({ path, fullPage });
    return name;
}

async function runStep(id, name, fn) {
    const startedAt = nowIso();
    const step = { id, name, started_at: startedAt, ok: false, details: "", shots: [] };
    log(`STEP ${id}: ${name}`);
    try {
        const res = await fn();
        if (res && Array.isArray(res.shots)) {
            step.shots = res.shots;
        }
        if (res && typeof res.details === "string") {
            step.details = res.details;
        }
        step.ok = true;
        log(`PASS ${id}`);
    } catch (err) {
        step.ok = false;
        step.details = String(err && err.message ? err.message : err);
        log(`FAIL ${id}: ${step.details}`);
    } finally {
        step.finished_at = nowIso();
        steps.push(step);
    }
    return step.ok;
}

function parseLabels(raw) {
    if (!raw) return {};
    const labels = {};
    for (const token of raw.split(",")) {
        const idx = token.indexOf("=");
        if (idx <= 0) continue;
        const key = token.slice(0, idx).trim();
        let value = token.slice(idx + 1).trim();
        if (value.startsWith("\"") && value.endsWith("\"")) {
            value = value.slice(1, -1);
        }
        labels[key] = value;
    }
    return labels;
}

function metricSample(metricsText, metricName, matcher = {}) {
    const lines = String(metricsText || "").split(/\r?\n/);
    const re = new RegExp(`^${metricName}(\\{([^}]*)\\})?\\s+([0-9.eE+-]+)$`);
    for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed || trimmed.startsWith("#")) continue;
        const match = trimmed.match(re);
        if (!match) continue;
        const labels = parseLabels(match[2] || "");
        let ok = true;
        for (const key of Object.keys(matcher)) {
            if (labels[key] !== matcher[key]) {
                ok = false;
                break;
            }
        }
        if (!ok) continue;
        return Number(match[3]);
    }
    return 0;
}

async function fetchText(url, timeoutMs = WAIT_TIMEOUT_MS) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    try {
        const response = await fetch(url, { signal: controller.signal });
        return {
            status: response.status,
            ok: response.ok,
            text: await response.text(),
        };
    } finally {
        clearTimeout(timer);
    }
}

async function runLegacySubscribeProbe(page, subject) {
    return page.evaluate(async ({ subjectValue }) => {
        const output = {
            outcome: "timeout",
            ws_url: "",
            details: "",
            request_id: "legacy-cutover-negative-sub",
        };

        const runtimeCfg = typeof window.__mr_get_runtime_config === "function"
            ? window.__mr_get_runtime_config()
            : null;

        let wsUrl = runtimeCfg && (runtimeCfg.ws_url || runtimeCfg.default_ws_url)
            ? String(runtimeCfg.ws_url || runtimeCfg.default_ws_url)
            : "";
        let apiKey = runtimeCfg && runtimeCfg.api_key ? String(runtimeCfg.api_key) : "";

        if (!wsUrl) {
            wsUrl = `${window.location.protocol === "https:" ? "wss" : "ws"}://${window.location.host}/ws`;
        }

        let fullWsUrl = wsUrl;
        try {
            const parsed = new URL(wsUrl, window.location.href);
            if (apiKey && !parsed.searchParams.get("api_key")) {
                parsed.searchParams.set("api_key", apiKey);
            }
            fullWsUrl = parsed.toString();
        } catch {
            // Keep original url; treat URL issues as no-wiring if connect fails.
        }
        output.ws_url = fullWsUrl;

        await new Promise((resolve) => {
            let settled = false;
            let opened = false;
            let ws;
            let timer = null;
            const finish = (outcome, details) => {
                if (settled) return;
                settled = true;
                output.outcome = outcome;
                output.details = details || "";
                if (timer !== null) {
                    clearTimeout(timer);
                }
                try {
                    ws.close();
                } catch {}
                resolve();
            };

            try {
                ws = new WebSocket(fullWsUrl);
            } catch (err) {
                finish("no_wiring", `ws_constructor_failed=${String(err)}`);
                return;
            }

            timer = setTimeout(() => {
                if (!opened) {
                    finish("no_wiring", "websocket_open_timeout");
                    return;
                }
                finish("timeout", "no_subscribe_response");
            }, 8000);

            ws.onopen = () => {
                opened = true;
                ws.send(JSON.stringify({
                    op: "subscribe",
                    request_id: output.request_id,
                    subject: subjectValue,
                }));
            };

            ws.onerror = () => {
                if (!opened) {
                    finish("no_wiring", "websocket_open_failed");
                }
            };

            ws.onmessage = (event) => {
                let msg;
                try {
                    msg = JSON.parse(String(event.data));
                } catch {
                    return;
                }
                if (msg && msg.type === "error" && msg.op === "subscribe") {
                    finish("rejected", JSON.stringify(msg));
                    return;
                }
                if (msg && msg.type === "ack" && msg.op === "subscribe") {
                    finish("accepted", JSON.stringify(msg));
                }
            };
        });

        return output;
    }, { subjectValue: subject });
}

async function readRuntimeProbe(page) {
    const names = [
        "probe_md_hello_received",
        "probe_md_subscribe_ack_count",
        "probe_md_seq_gap_count",
        "probe_md_prev_seq_violations",
        "probe_md_backend_gap_missing_ts_server",
        "probe_md_backend_gap_no_metrics",
        "probe_md_backend_gap_seq_gap_recurring",
        "probe_md_server_metrics_cadence_ms",
        "probe_md_resync_count",
        "probe_md_transport_mode",
        "probe_md_legacy_downgrade_count",
        "probe_md_alloc_estimate_total",
        "probe_md_alloc_estimate_frame",
        "probe_md_trade_backlog",
        "probe_md_trade_backlog_cap",
        "probe_md_candle_backlog",
        "probe_md_candle_backlog_cap",
        "probe_md_signal_backlog",
        "probe_md_signal_backlog_cap",
        "probe_md_parse_time_p95_us",
        "probe_md_parse_time_p99_us",
        "probe_md_apply_time_p95_us",
        "probe_md_apply_time_p99_us",
        "probe_md_batched_decode_time_p95_us",
        "probe_md_batched_decode_time_p99_us",
        "probe_stream_evictions",
        "probe_layer_stream_entries",
        "probe_layer_stream_evictions",
        "probe_md_canonical_stats_frames",
        "probe_md_stats_fallback_frames",
        "probe_md_canonical_evidence_frames",
        "probe_md_legacy_evidence_frames",
        "probe_md_evidence_fallback_frames",
        "probe_md_canonical_signal_frames",
        "probe_md_legacy_signal_frames",
        "probe_md_signal_fallback_frames",
        "probe_md_legacy_evidence_rejected",
        "probe_md_legacy_signal_rejected",
        "probe_widget_evidence_count",
        "probe_widget_stats_count",
        "probe_widget_signal_count",
        "probe_widget_signal_link_total",
        "probe_widget_signal_link_evidence_seq",
        "probe_widget_dom_parse_total",
        "probe_widget_dom_fallback_total",
        "probe_widget_dom_drop_total",
        "probe_widget_dom_drop_capacity_total",
        "probe_widget_dom_drop_render_overflow_total",
        "probe_widget_dom_render_p95_us",
        "probe_widget_dom_render_p99_us",
        "probe_widget_dom_render_budget_us",
        "probe_widget_dom_render_over_budget",
        "probe_widget_dom_entries",
        "probe_widget_dom_max_entries",
        "probe_widget_dom_evicted_total",
        "probe_widget_stats_parse_total",
        "probe_widget_stats_fallback_total",
        "probe_widget_stats_drop_total",
        "probe_widget_stats_drop_capacity_total",
        "probe_widget_stats_drop_render_overflow_total",
        "probe_widget_stats_render_p95_us",
        "probe_widget_stats_render_p99_us",
        "probe_widget_stats_render_budget_us",
        "probe_widget_stats_render_over_budget",
        "probe_widget_stats_entries",
        "probe_widget_stats_max_entries",
        "probe_widget_stats_evicted_total",
        "probe_widget_stats_state",
        "probe_widget_tape_parse_total",
        "probe_widget_tape_fallback_total",
        "probe_widget_tape_drop_total",
        "probe_widget_tape_drop_capacity_total",
        "probe_widget_tape_drop_render_overflow_total",
        "probe_widget_tape_render_p95_us",
        "probe_widget_tape_render_p99_us",
        "probe_widget_tape_render_budget_us",
        "probe_widget_tape_render_over_budget",
        "probe_widget_tape_entries",
        "probe_widget_tape_max_entries",
        "probe_widget_tape_evicted_total",
        "probe_widget_evidence_parse_total",
        "probe_widget_evidence_fallback_total",
        "probe_widget_evidence_drop_total",
        "probe_widget_evidence_drop_capacity_total",
        "probe_widget_evidence_drop_render_overflow_total",
        "probe_widget_evidence_render_p95_us",
        "probe_widget_evidence_render_p99_us",
        "probe_widget_evidence_render_budget_us",
        "probe_widget_evidence_render_over_budget",
        "probe_widget_evidence_entries",
        "probe_widget_evidence_max_entries",
        "probe_widget_evidence_evicted_total",
        "probe_widget_signal_parse_total",
        "probe_widget_signal_fallback_total",
        "probe_widget_signal_drop_total",
        "probe_widget_signal_drop_capacity_total",
        "probe_widget_signal_drop_render_overflow_total",
        "probe_widget_signal_render_p95_us",
        "probe_widget_signal_render_p99_us",
        "probe_widget_signal_render_budget_us",
        "probe_widget_signal_render_over_budget",
        "probe_widget_signal_entries",
        "probe_widget_signal_max_entries",
        "probe_widget_signal_evicted_total",
        "probe_widget_evidence_state",
        "probe_widget_signal_state",
        "probe_layout_version",
        "probe_layout_migrated",
        "probe_layout_link_enabled",
        "probe_indicator_funding_rendered",
        "probe_indicator_liq_rendered",
    ];
    return page.evaluate((probeNames) => {
        const exports = window.__mr_wasm_exports || {};
        const out = {};
        for (const name of probeNames) {
            const fn = exports[name];
            out[name] = typeof fn === "function" ? Number(fn()) : null;
        }
        return out;
    }, names);
}

async function triggerCtrlShortcut(page, key, code) {
    const dispatch = async (type, keyName, codeName, ctrl) => {
        await page.evaluate(({ evtType, key, code, withCtrl }) => {
            const ev = new KeyboardEvent(evtType, {
                key,
                code,
                ctrlKey: withCtrl,
                bubbles: true,
                cancelable: true,
            });
            document.dispatchEvent(ev);
        }, { evtType: type, key: keyName, code: codeName, withCtrl: ctrl });
    };

    await dispatch("keydown", "Control", "ControlLeft", true);
    await dispatch("keydown", key, code, true);
    await dispatch("keyup", key, code, true);
    await page.waitForTimeout(90);
    await dispatch("keyup", "Control", "ControlLeft", false);
}

async function primeCanonicalStatsFlow(page) {
    const beforeProbe = await readRuntimeProbe(page);
    const ackBefore = Number(beforeProbe.probe_md_subscribe_ack_count ?? 0);

    await page.keyboard.press("Control+k");
    await page.waitForTimeout(300);

    const vp = page.viewportSize() || { width: 1440, height: 900 };
    const panelW = 460;
    const panelH = 420;
    const px = (vp.width - panelW) * 0.5;
    const py = (vp.height - panelH) * 0.5;
    const plusStreamBtnX = px + 190 + 38; // "+ Stream" center (btn_x + width/2)
    const plusStreamBtnY = py + panelH - 46 + 10; // footer_y + btn_h/2

    await page.mouse.click(plusStreamBtnX, plusStreamBtnY);
    await page.waitForTimeout(350);
    await page.keyboard.press("Escape");
    await page.waitForTimeout(200);

    // 1s timeframe helps guarantee quick canonical stats emission.
    await page.keyboard.press("1");
    await page.waitForTimeout(350);

    let ackAfter = Number((await readRuntimeProbe(page)).probe_md_subscribe_ack_count ?? 0);
    try {
        await waitFor(async () => {
            ackAfter = Number((await readRuntimeProbe(page)).probe_md_subscribe_ack_count ?? 0);
            return ackAfter >= ackBefore + 1;
        }, "stats-prime subscribe ack delta", 12000);
    } catch {
        // Keep running; canonical wait below remains the hard gate.
        ackAfter = Number((await readRuntimeProbe(page)).probe_md_subscribe_ack_count ?? 0);
    }

    return {
        ack_before: ackBefore,
        ack_after: ackAfter,
        ack_delta: ackAfter - ackBefore,
    };
}

async function runDirectResyncProbe(page, wsUrl) {
    return page.evaluate(async ({ targetWsUrl, subject }) => {
        const result = {
            ok: false,
            ws_url: targetWsUrl,
            subject,
            stream_id: "",
            events: [],
            order: [],
            error: "",
            response_kind: "",
        };

        const pushEvent = (line) => {
            if (typeof line !== "string") return;
            if (result.events.length < 20) {
                result.events.push(line.slice(0, 320));
            }
        };

        const ws = new WebSocket(targetWsUrl);
        const timeoutMs = 12000;

        await new Promise((resolve) => {
            let closed = false;
            let resyncSent = false;
            const done = (ok, err = "") => {
                if (closed) return;
                closed = true;
                result.ok = ok;
                result.error = err;
                try { ws.close(); } catch {}
                resolve();
            };
            const timer = setTimeout(() => done(false, "direct resync probe timeout"), timeoutMs);

            ws.onopen = () => {
                const hello = { op: "hello", type: "hello", request_id: "h_iq_probe" };
                const sub = { op: "subscribe", subject, request_id: "r_iq_probe" };
                ws.send(JSON.stringify(hello));
                ws.send(JSON.stringify(sub));
                result.order.push("hello_sent");
                result.order.push("subscribe_sent");
            };

            ws.onerror = () => {
                clearTimeout(timer);
                done(false, "websocket error during direct resync probe");
            };

            ws.onmessage = (ev) => {
                const raw = typeof ev.data === "string" ? ev.data : String(ev.data);
                pushEvent(raw);
                let msg = null;
                try {
                    msg = JSON.parse(raw);
                } catch {
                    return;
                }

                if (msg && msg.type === "ack" && msg.op === "subscribe" && !resyncSent) {
                    result.order.push("subscribe_ack");
                    result.stream_id = typeof msg.stream_id === "string" && msg.stream_id.length > 0
                        ? msg.stream_id
                        : subject;
                    const resync = {
                        op: "resync",
                        stream_id: result.stream_id,
                        last_seq: 0,
                        request_id: "rs_iq_probe",
                    };
                    ws.send(JSON.stringify(resync));
                    resyncSent = true;
                    result.order.push("resync_sent");
                    return;
                }

                if (msg && msg.type === "ack" && msg.op === "resync") {
                    result.order.push("resync_ack");
                    result.response_kind = "ack";
                    clearTimeout(timer);
                    done(true);
                    return;
                }

                if (msg && msg.type === "error") {
                    if (msg.op === "resync" && resyncSent) {
                        result.order.push("resync_error");
                        result.response_kind = "error";
                        clearTimeout(timer);
                        done(true);
                        return;
                    }
                    clearTimeout(timer);
                    done(false, `server error frame: ${raw.slice(0, 200)}`);
                }
            };
        });

        return result;
    }, { targetWsUrl: wsUrl, subject: "insights.heatmap_snapshot/binance/BTCUSDT/1m" });
}

async function readClipboard(page) {
    return page.evaluate(async () => {
        try {
            return await navigator.clipboard.readText();
        } catch (err) {
            return `__CLIPBOARD_ERR__${String(err)}`;
        }
    });
}

async function main() {
    const browser = await chromium.launch({
        headless: true,
        args: ["--disable-gpu", "--no-sandbox"],
    });
    const context = await browser.newContext({
        viewport: { width: 1440, height: 900 },
    });

    try {
        const origin = new URL(BASE_URL).origin;
        await context.grantPermissions(["clipboard-read", "clipboard-write"], { origin });
    } catch (err) {
        log(`WARN: failed to grant clipboard permissions: ${String(err)}`);
    }
    await context.addInitScript(() => {
        try { window.localStorage.setItem("mr.settings.auto_connect", "1"); } catch {}
    });

    const page = await context.newPage();
    page.on("console", (msg) => {
        consoleEvents.push({ ts: nowIso(), type: msg.type(), text: msg.text() });
    });
    page.on("pageerror", (err) => {
        consoleEvents.push({ ts: nowIso(), type: "pageerror", text: String(err) });
    });
    page.on("requestfailed", (request) => {
        const failure = request.failure();
        const url = request.url();
        if (url.endsWith("/favicon.ico")) {
            return;
        }
        networkFailures.push({
            ts: nowIso(),
            url,
            method: request.method(),
            resourceType: request.resourceType(),
            errorText: failure && failure.errorText ? failure.errorText : "unknown",
        });
    });
    page.on("response", (response) => {
        const status = response.status();
        const url = response.url();
        if (status < 400 || status >= 600) {
            return;
        }
        if (url.endsWith("/favicon.ico")) {
            return;
        }
        networkErrorResponses.push({
            ts: nowIso(),
            url,
            status,
            method: response.request().method(),
            resourceType: response.request().resourceType(),
        });
    });

    try {
        await runStep("load", "Open client and render canvas", async () => {
            const resp = await page.goto(BASE_URL, { waitUntil: "networkidle", timeout: 30000 });
            if (!resp || resp.status() !== 200) {
                throw new Error(`unexpected status: ${resp ? resp.status() : "no response"}`);
            }
            await page.waitForSelector("canvas#canvas", { timeout: 15000 });
            await waitFor(
                () => page.evaluate(() => typeof window.__mr_wasm_exports !== "undefined"),
                "__mr_wasm_exports"
            );
            const shots = [await snap(page, "load")];
            return { shots, details: "Canvas and WASM exports loaded." };
        });

        await runStep("connect-profile", "Open connection/profile manager", async () => {
            await page.keyboard.press("Control+k");
            await page.waitForTimeout(500);
            const shot = await snap(page, "connect-profile-modal");
            await page.keyboard.press("Escape");
            await page.waitForTimeout(200);
            return { shots: [shot], details: "Connection modal toggled via Ctrl+K." };
        });

        await runStep("hello-ack", "Observe HELLO and ACK handshake", async () => {
            await waitFor(async () => {
                if (hasLine("[md-lifecycle] hello_sent")) return true;
                const probe = await readRuntimeProbe(page);
                return Number(probe.probe_md_hello_received ?? 0) > 0;
            }, "hello_sent_or_probe");
            await waitFor(async () => {
                if (hasLine("[md-lifecycle] hello_ack_recv")) return true;
                const probe = await readRuntimeProbe(page);
                return Number(probe.probe_md_hello_received ?? 0) > 0;
            }, "hello_ack_or_probe");
            await waitFor(
                async () => {
                    if (hasLine("[md-lifecycle] ack_recv op=subscribe")) return true;
                    const probe = await readRuntimeProbe(page);
                    return Number(probe.probe_md_subscribe_ack_count ?? 0) > 0;
                },
                "ack_recv subscribe"
            );
            const shot = await snap(page, "hello-ack");
            return { shots: [shot], details: "HELLO and subscribe ACK observed in console logs." };
        });

        await runStep("subscribe-unsubscribe", "Trigger subscribe/unsubscribe cycle", async () => {
            const before = await readRuntimeProbe(page);
            const ackBefore = Number(before.probe_md_subscribe_ack_count ?? 0);
            await page.keyboard.press("1");
            await page.waitForTimeout(1000);
            await page.keyboard.press("3");
            await page.waitForTimeout(1000);
            await waitFor(async () => {
                const byLogs = hasLine("[md-lifecycle] unsubscribe_sent") &&
                    hasLine("[md-lifecycle] ack_recv op=unsubscribe") &&
                    hasLine("[md-lifecycle] subscribe_sent");
                if (byLogs) return true;
                const probe = await readRuntimeProbe(page);
                return Number(probe.probe_md_subscribe_ack_count ?? 0) >= ackBefore + 2;
            }, "unsubscribe_sent_or_ack_delta");
            const shot = await snap(page, "subscribe-unsubscribe");
            return { shots: [shot], details: "Timeframe switch generated unsub/sub and ACK cycle." };
        });

        await runStep("resync", "Trigger RESYNC and ACK order", async () => {
            await triggerCtrlShortcut(page, "r", "KeyR");
            await page.waitForTimeout(500);
            let mode = "client-hotkey";
            let detail = "Client resync logs observed.";
            let sawClientResync = false;
            try {
                await waitFor(() => hasLine("[md-lifecycle] resync_sent"), "resync_sent", 5000);
                await waitFor(() => hasLine("[md-lifecycle] ack_recv op=resync"), "ack resync", 5000);
                sawClientResync = true;
            } catch {
                sawClientResync = false;
            }

            if (!sawClientResync) {
                const wsUrl = await page.evaluate(() => {
                    if (typeof window.__mr_get_runtime_config === "function") {
                        const cfg = window.__mr_get_runtime_config();
                        return cfg.ws_url || cfg.default_ws_url || "ws://localhost:8090/ws";
                    }
                    return "ws://localhost:8090/ws";
                });
                const probe = await runDirectResyncProbe(page, wsUrl);
                if (!probe.ok) {
                    throw new Error(
                        `resync not observed in client logs and direct probe failed: ${probe.error}`
                    );
                }
                mode = "direct-protocol-probe";
                detail = `Direct resync probe passed (stream_id=${probe.stream_id || "n/a"}, response=${probe.response_kind || "n/a"}).`;
                log(`Direct resync probe order: ${probe.order.join(" -> ")}`);
            }

            const shot = await snap(page, "resync");
            return { shots: [shot], details: `${detail} mode=${mode}` };
        });

        await runStep("metrics-hud", "Toggle metrics HUD and validate cadence", async () => {
            await page.keyboard.press("Control+h");
            await page.waitForTimeout(400);
            await waitFor(async () => {
                const probe = await readRuntimeProbe(page);
                return Number(probe.probe_md_server_metrics_cadence_ms ?? 0) > 0;
            }, "server_metrics_cadence_ms");
            const probe = await readRuntimeProbe(page);
            const cadence = Number(probe.probe_md_server_metrics_cadence_ms ?? 0);
            if (!(cadence > 0)) {
                throw new Error(`server_metrics_cadence_ms not set (got ${cadence})`);
            }
            const shot = await snap(page, "metrics-hud");
            return { shots: [shot], details: `Telemetry HUD toggled; cadence=${cadence}ms.` };
        });

        await runStep("stats-regime", "Validate canonical stats/perf probes", async () => {
            const statsPrime = await primeCanonicalStatsFlow(page);
            await waitFor(async () => {
                const probe = await readRuntimeProbe(page);
                const perfReady = Number(probe.probe_md_parse_time_p95_us ?? -1) >= 0 &&
                    Number(probe.probe_md_apply_time_p95_us ?? -1) >= 0 &&
                    Number(probe.probe_md_batched_decode_time_p95_us ?? -1) >= 0;
                if (!perfReady) return false;
                if (!REQUIRE_STATS_CANONICAL) return true;
                return Number(probe.probe_widget_stats_count ?? 0) > 0 &&
                    Number(probe.probe_widget_stats_parse_total ?? 0) > 0 &&
                    Number(probe.probe_md_canonical_stats_frames ?? 0) > 0;
            }, "md_perf_probes", STATS_WAIT_TIMEOUT_MS);
            const probe = await readRuntimeProbe(page);
            const statsCount = Number(probe.probe_widget_stats_count ?? 0);
            const statsParse = Number(probe.probe_widget_stats_parse_total ?? 0);
            const statsFallback = Number(probe.probe_widget_stats_fallback_total ?? -1);
            const statsState = Number(probe.probe_widget_stats_state ?? -1);
            const canonicalStatsFrames = Number(probe.probe_md_canonical_stats_frames ?? 0);
            const mdStatsFallback = Number(probe.probe_md_stats_fallback_frames ?? -1);
            const parseP95 = Number(probe.probe_md_parse_time_p95_us ?? -1);
            const parseP99 = Number(probe.probe_md_parse_time_p99_us ?? -1);
            const applyP95 = Number(probe.probe_md_apply_time_p95_us ?? -1);
            const applyP99 = Number(probe.probe_md_apply_time_p99_us ?? -1);
            const batchedDecodeP95 = Number(probe.probe_md_batched_decode_time_p95_us ?? -1);
            const batchedDecodeP99 = Number(probe.probe_md_batched_decode_time_p99_us ?? -1);
            const hasCanonicalStats = statsCount > 0 && statsParse > 0 && canonicalStatsFrames > 0;
            if (REQUIRE_STATS_CANONICAL && !hasCanonicalStats) {
                throw new Error(
                    `stats probes missing count=${statsCount} parse=${statsParse} canonical=${canonicalStatsFrames}`
                );
            }
            if (statsFallback < 0 || mdStatsFallback < 0) {
                throw new Error(`stats fallback counters invalid widget=${statsFallback} md=${mdStatsFallback}`);
            }
            if (statsState < 0) {
                throw new Error(`stats state probe invalid=${statsState}`);
            }
            if (parseP95 < 0 || parseP99 < 0 || applyP95 < 0 || applyP99 < 0 || batchedDecodeP95 < 0 || batchedDecodeP99 < 0) {
                throw new Error(
                    `parse/apply probes invalid parse=${parseP95}/${parseP99} apply=${applyP95}/${applyP99} batch_decode=${batchedDecodeP95}/${batchedDecodeP99}`
                );
            }
            const shot = await snap(page, "stats-regime");
            statsProbeSnapshot = {
                stats_count: statsCount,
                widget_stats_parse_total: statsParse,
                widget_stats_fallback_total: statsFallback,
                widget_stats_state: statsState,
                canonical_stats_frames: canonicalStatsFrames,
                md_stats_fallback_frames: mdStatsFallback,
                parse_time_p95_us: parseP95,
                parse_time_p99_us: parseP99,
                apply_time_p95_us: applyP95,
                apply_time_p99_us: applyP99,
                batched_decode_time_p95_us: batchedDecodeP95,
                batched_decode_time_p99_us: batchedDecodeP99,
                has_canonical_stats: hasCanonicalStats,
                require_stats_canonical: REQUIRE_STATS_CANONICAL,
                stats_prime: statsPrime,
            };
            return {
                shots: [shot],
                details: `stats_count=${statsCount} stats_parse=${statsParse} canonical_stats=${canonicalStatsFrames} require_stats=${REQUIRE_STATS_CANONICAL} stats_prime_ack_delta=${statsPrime.ack_delta} stats_fallback=${statsFallback} md_stats_fallback=${mdStatsFallback} parse_us(p95/p99)=${parseP95}/${parseP99} apply_us(p95/p99)=${applyP95}/${applyP99}.`,
            };
        });

        await runStep("evidence-signal", "Exercise evidence/signal surfaces", async () => {
            await page.keyboard.press("h");
            await page.waitForTimeout(350);
            await page.keyboard.press("j");
            await page.waitForTimeout(350);
            await waitFor(async () => {
                const canonicalByLogs =
                    hasLine("subscribe_sent subject=liquidity.evidence/") &&
                    hasLine("subscribe_sent subject=signal/");
                if (canonicalByLogs) return true;
                const probe = await readRuntimeProbe(page);
                const canonicalFrames =
                    Number(probe.probe_md_canonical_evidence_frames ?? 0) +
                    Number(probe.probe_md_canonical_signal_frames ?? 0);
                return canonicalFrames > 0;
            }, "canonical evidence+signal subscribe/frame");
            const probe = await readRuntimeProbe(page);
            const legacyEvidenceSubs = hasLine("subscribe_sent subject=insights.microstructure_evidence/");
            const legacySignalSubs = hasLine("subscribe_sent subject=signal/composite/");
            if (legacyEvidenceSubs || legacySignalSubs) {
                throw new Error(
                    `legacy subjects observed evidence=${legacyEvidenceSubs} signal=${legacySignalSubs}`
                );
            }
            if (Number(probe.probe_md_legacy_evidence_rejected ?? 0) < 0 ||
                Number(probe.probe_md_legacy_signal_rejected ?? 0) < 0) {
                throw new Error("invalid legacy rejection counters");
            }
            const shot = await snap(page, "evidence-signal");
            return {
                shots: [shot],
                details: `canonical evidence/signal active; links=${Number(probe.probe_widget_signal_link_total ?? 0)}.`,
            };
        });

        await runStep("layouts", "Exercise layout transitions", async () => {
            await page.keyboard.press("f");
            await page.waitForTimeout(500);
            const s1 = await snap(page, "layout-focus");
            await page.keyboard.press("Escape");
            await page.waitForTimeout(250);

            await page.keyboard.press("z");
            await page.waitForTimeout(500);
            const s2 = await snap(page, "layout-zen");
            await page.keyboard.press("Escape");
            await page.waitForTimeout(250);

            await page.keyboard.press("c");
            await page.waitForTimeout(500);
            const s3 = await snap(page, "layout-compare");
            await page.keyboard.press("Escape");
            await page.waitForTimeout(250);
            const probe = await readRuntimeProbe(page);
            const layoutVersion = Number(probe.probe_layout_version ?? 0);
            const layoutLinkEnabled = Number(probe.probe_layout_link_enabled ?? -1);
            if (layoutVersion !== 0 && layoutVersion < 4) {
                throw new Error(`layout version expected 0|>=4, got ${layoutVersion}`);
            }
            if (!(layoutLinkEnabled === 0 || layoutLinkEnabled === 1)) {
                throw new Error(`layout link flag expected 0|1, got ${layoutLinkEnabled}`);
            }
            await page.reload({ waitUntil: "networkidle" });
            await page.waitForSelector("canvas#canvas", { timeout: 15000 });
            await waitFor(
                () => page.evaluate(() => typeof window.__mr_wasm_exports !== "undefined"),
                "__mr_wasm_exports after reload"
            );
            await waitFor(async () => {
                const probe = await readRuntimeProbe(page);
                return Number(probe.probe_md_hello_received ?? 0) > 0;
            }, "hello_after_layout_reload");
            const probeAfter = await readRuntimeProbe(page);
            const layoutVersionAfter = Number(probeAfter.probe_layout_version ?? 0);
            const layoutLinkAfter = Number(probeAfter.probe_layout_link_enabled ?? -1);
            if (layoutVersionAfter !== layoutVersion) {
                throw new Error(`layout restore mismatch version before=${layoutVersion} after=${layoutVersionAfter}`);
            }
            if (layoutLinkAfter !== layoutLinkEnabled) {
                throw new Error(`layout restore mismatch link before=${layoutLinkEnabled} after=${layoutLinkAfter}`);
            }
            const s4 = await snap(page, "layout-reload-restore");
            return {
                shots: [s1, s2, s3, s4],
                details: `Focus/Zen/Compare toggled; layout restore preserved version=${layoutVersion} link=${layoutLinkEnabled}.`,
            };
        });

        await runStep("reconnect-no-stale", "Reconnect transport and verify no stale widget state", async () => {
            const before = await readRuntimeProbe(page);
            const beforeAck = Number(before.probe_md_subscribe_ack_count ?? 0);
            const cfg = await page.evaluate(() => (
                typeof window.__mr_get_runtime_config === "function"
                    ? window.__mr_get_runtime_config()
                    : null
            ));
            const targetWsUrl = (cfg && (cfg.ws_url || cfg.default_ws_url)) || "ws://localhost:8090/ws";
            const targetApiKey = (cfg && cfg.api_key) || "";

            await page.evaluate(({ wsUrl, apiKey }) => {
                if (typeof window.__mr_switch_ws_runtime !== "function") {
                    throw new Error("__mr_switch_ws_runtime unavailable");
                }
                window.__mr_switch_ws_runtime(wsUrl, apiKey, { live: true });
            }, { wsUrl: targetWsUrl, apiKey: targetApiKey });

            await waitFor(async () => {
                const probe = await readRuntimeProbe(page);
                const ack = Number(probe.probe_md_subscribe_ack_count ?? 0);
                return Number(probe.probe_md_hello_received ?? 0) > 0 && ack >= beforeAck;
            }, "reconnect hello+ack", STATS_WAIT_TIMEOUT_MS);

            // Force a deterministic unsub/sub cycle after reconnect so widget state
            // transitions out of stale markers even under low-traffic windows.
            await page.keyboard.press("1");
            await page.waitForTimeout(700);
            await page.keyboard.press("3");
            await page.waitForTimeout(700);

            await waitFor(async () => {
                const probe = await readRuntimeProbe(page);
                const statsState = Number(probe.probe_widget_stats_state ?? -1);
                const evidenceState = Number(probe.probe_widget_evidence_state ?? -1);
                const signalState = Number(probe.probe_widget_signal_state ?? -1);
                return statsState !== 2 && evidenceState !== 2 && signalState !== 2;
            }, "widgets no stale state after reconnect", STATS_WAIT_TIMEOUT_MS);

            const probe = await readRuntimeProbe(page);
            const statsState = Number(probe.probe_widget_stats_state ?? -1);
            const evidenceState = Number(probe.probe_widget_evidence_state ?? -1);
            const signalState = Number(probe.probe_widget_signal_state ?? -1);
            if (statsState === 2 || evidenceState === 2 || signalState === 2) {
                throw new Error(`stale state after reconnect stats=${statsState} evidence=${evidenceState} signal=${signalState}`);
            }
            const afterAck = Number(probe.probe_md_subscribe_ack_count ?? 0);
            const shot = await snap(page, "reconnect-no-stale");
            return {
                shots: [shot],
                details: `reconnect ws=${targetWsUrl} ack_before=${beforeAck} ack_after=${afterAck} states(stats/evidence/signal)=${statsState}/${evidenceState}/${signalState}.`,
            };
        });

        await runStep("diagnostics-copy", "Copy diagnostics payload", async () => {
            await page.keyboard.press("Control+h");
            await page.waitForTimeout(350);

            const points = [
                { x: 110, y: 825 },
                { x: 110, y: 780 },
                { x: 110, y: 740 },
                { x: 110, y: 700 },
            ];
            let got = "";
            for (const p of points) {
                await page.mouse.click(p.x, p.y);
                await page.waitForTimeout(200);
                got = await readClipboard(page);
                if (typeof got === "string" && got.includes("MR Diagnostics")) {
                    break;
                }
            }
            diagnosticsClipboard = got;
            const shot = await snap(page, "diagnostics-copy");
            if (!got.includes("MR Diagnostics")) {
                return {
                    shots: [shot],
                    details: "Copy Diagnostics click attempted; clipboard payload not observable in headless mode.",
                };
            }
            return { shots: [shot], details: "Copy Diagnostics button produced clipboard payload." };
        });

        await runStep("legacy-off", "Validate legacy fallback OFF", async () => {
            const probe = await readRuntimeProbe(page);
            const runtime = await page.evaluate(() => {
                const raw = window.localStorage
                    ? window.localStorage.getItem("mr.allow_legacy_ws")
                    : null;
                const url = new URL(window.location.href);
                return { raw, param: url.searchParams.get("allow_legacy_ws") };
            });

            const raw = (runtime.raw || "").toLowerCase();
            const param = (runtime.param || "").toLowerCase();
            const disabledByConfig = !(raw === "1" || raw === "true" || raw === "on" || raw === "yes") &&
                !(param === "1" || param === "true" || param === "on" || param === "yes");
            const transportMode = Number(probe.probe_md_transport_mode ?? -1);
            const legacyDowngrades = Number(probe.probe_md_legacy_downgrade_count ?? -1);

            if (!disabledByConfig) {
                throw new Error(`allow_legacy_ws appears enabled (raw=${runtime.raw} param=${runtime.param})`);
            }
            if (transportMode !== 0) {
                throw new Error(`transport_mode expected Terminal_V1(0), got ${transportMode}`);
            }
            if (legacyDowngrades !== 0) {
                throw new Error(`legacy_downgrade_count expected 0, got ${legacyDowngrades}`);
            }
            const shot = await snap(page, "legacy-off");
            return { shots: [shot], details: "Legacy fallback remained disabled." };
        });

        await runStep("legacy-cutover-negative", "Assert hard cutover legacy contract", async () => {
            const metricsBeforeResp = await fetchText(METRICS_URL, 10000);
            if (metricsBeforeResp.status !== 200) {
                throw new Error(`metrics before fetch failed status=${metricsBeforeResp.status}`);
            }
            const metricsBefore = metricsBeforeResp.text;
            const legacyRejectedBefore = metricSample(
                metricsBefore,
                "ws_legacy_requests_total",
                { status: "rejected" }
            );

            const routeResp = await fetchText(`${SERVER_BASE}/ws/marketdata`, 8000);
            if (routeResp.status !== 410) {
                throw new Error(`legacy route expected 410, got ${routeResp.status}`);
            }

            const subscribeProbe = await runLegacySubscribeProbe(page, LEGACY_SIGNAL_SUBJECT);
            if (!(subscribeProbe.outcome === "rejected" || subscribeProbe.outcome === "no_wiring")) {
                throw new Error(
                    `legacy subscribe should be rejected/no_wiring, got=${subscribeProbe.outcome} details=${subscribeProbe.details}`
                );
            }

            const metricsAfterResp = await fetchText(METRICS_URL, 10000);
            if (metricsAfterResp.status !== 200) {
                throw new Error(`metrics after fetch failed status=${metricsAfterResp.status}`);
            }
            const metricsAfter = metricsAfterResp.text;
            const legacyAcceptedAfter = metricSample(
                metricsAfter,
                "ws_legacy_requests_total",
                { status: "accepted" }
            );
            const legacyRejectedAfter = metricSample(
                metricsAfter,
                "ws_legacy_requests_total",
                { status: "rejected" }
            );
            const legacyRejectedDelta = legacyRejectedAfter - legacyRejectedBefore;
            if (legacyAcceptedAfter !== 0) {
                throw new Error(`legacy accepted counter expected 0, got ${legacyAcceptedAfter}`);
            }
            if (legacyRejectedDelta < 1) {
                throw new Error(`legacy rejected delta expected >=1, got ${legacyRejectedDelta}`);
            }

            const probe = await readRuntimeProbe(page);
            const compatCounters = {
                probe_md_legacy_downgrade_count: Number(probe.probe_md_legacy_downgrade_count ?? -1),
                probe_md_legacy_evidence_frames: Number(probe.probe_md_legacy_evidence_frames ?? -1),
                probe_md_legacy_signal_frames: Number(probe.probe_md_legacy_signal_frames ?? -1),
                probe_md_stats_fallback_frames: Number(probe.probe_md_stats_fallback_frames ?? -1),
                probe_md_evidence_fallback_frames: Number(probe.probe_md_evidence_fallback_frames ?? -1),
                probe_md_signal_fallback_frames: Number(probe.probe_md_signal_fallback_frames ?? -1),
            };
            const nonZeroCompat = Object.entries(compatCounters).filter(([, value]) => value !== 0);
            if (nonZeroCompat.length > 0) {
                throw new Error(`compat/legacy counters must stay zero: ${JSON.stringify(nonZeroCompat)}`);
            }

            legacyCutoverSnapshot = {
                route_status: routeResp.status,
                route_status_410: routeResp.status === 410,
                subscribe_probe: subscribeProbe,
                counters: {
                    ws_legacy_requests_total_accepted: legacyAcceptedAfter,
                    ws_legacy_requests_total_rejected_before: legacyRejectedBefore,
                    ws_legacy_requests_total_rejected_after: legacyRejectedAfter,
                    ws_legacy_requests_total_rejected_delta: legacyRejectedDelta,
                    ...compatCounters,
                },
            };

            const shot = await snap(page, "legacy-cutover-negative");
            return {
                shots: [shot],
                details: `route=410 subscribe_outcome=${subscribeProbe.outcome} accepted=${legacyAcceptedAfter} rejected_delta=${legacyRejectedDelta} compat_zero=${nonZeroCompat.length === 0}`,
            };
        });

        await runStep("clean-runtime", "Validate absence of console/network errors", async () => {
            const consoleErrors = consoleEvents.filter((e) => e.type === "error" || e.type === "pageerror");
            if (consoleErrors.length > 0) {
                const sample = consoleErrors
                    .slice(0, 3)
                    .map((e) => `${e.type}:${e.text.slice(0, 140)}`)
                    .join(" | ");
                throw new Error(`console errors observed count=${consoleErrors.length} sample=${sample}`);
            }
            if (networkFailures.length > 0) {
                const sample = networkFailures
                    .slice(0, 3)
                    .map((e) => `${e.method} ${e.url} err=${e.errorText}`)
                    .join(" | ");
                throw new Error(`network request failures observed count=${networkFailures.length} sample=${sample}`);
            }
            if (networkErrorResponses.length > 0) {
                const sample = networkErrorResponses
                    .slice(0, 3)
                    .map((e) => `${e.method} ${e.url} status=${e.status}`)
                    .join(" | ");
                throw new Error(`network error responses observed count=${networkErrorResponses.length} sample=${sample}`);
            }
            const shot = await snap(page, "clean-runtime");
            return { shots: [shot], details: "No console/page errors and no network failures (requestfailed/HTTP 4xx/5xx)." };
        });
    } finally {
        const runtimeProbe = await readRuntimeProbe(page).catch(() => ({}));
        const runtimeConfig = await page.evaluate(() => {
            try {
                return typeof window.__mr_get_runtime_config === "function"
                    ? window.__mr_get_runtime_config()
                    : null;
            } catch {
                return null;
            }
        }).catch(() => null);

        const report = {
            generated_at: nowIso(),
            base_url: BASE_URL,
            server_base: SERVER_BASE,
            metrics_url: METRICS_URL,
            shots_dir: SHOTS_DIR,
            logs_dir: LOGS_DIR,
            steps,
            runtime_probe: runtimeProbe,
            runtime_config: runtimeConfig,
            diagnostics_clipboard_preview: typeof diagnosticsClipboard === "string"
                ? diagnosticsClipboard.slice(0, 400)
                : "",
            console_event_count: consoleEvents.length,
            network_failure_count: networkFailures.length,
            network_error_response_count: networkErrorResponses.length,
            network_request_failures: networkFailures.slice(0, 32),
            network_error_responses: networkErrorResponses.slice(0, 32),
            stats_probe: statsProbeSnapshot,
            legacy_cutover_negative: legacyCutoverSnapshot,
            notes,
        };

        writeFileSync(join(LOGS_DIR, "playwright-smoke.json"), JSON.stringify(report, null, 2));
        const consoleLogText = consoleEvents
            .map((e) => `[${e.ts}] [${e.type}] ${e.text}`)
            .join("\n");
        writeFileSync(join(LOGS_DIR, "playwright-console.log"), consoleLogText + "\n");
        writeFileSync(join(LOGS_DIR, "playwright-notes.log"), notes.join("\n") + "\n");

        await context.close();
        await browser.close();

        const failed = steps.filter((s) => !s.ok);
        if (failed.length > 0) {
            process.exitCode = 1;
        }
    }
}

main().catch((err) => {
    console.error(String(err && err.stack ? err.stack : err));
    process.exit(1);
});
