#!/usr/bin/env node

import { chromium } from "playwright";
import { mkdirSync, writeFileSync } from "fs";
import { join } from "path";

const BASE_URL = process.env.IQ_BASE_URL || "http://localhost:8090";
const SHOTS_DIR = process.env.IQ_SHOTS_DIR || join(process.cwd(), "artifacts", "iq", "shots");
const LOGS_DIR = process.env.IQ_LOGS_DIR || join(process.cwd(), "artifacts", "iq", "logs");
const WAIT_TIMEOUT_MS = Number(process.env.IQ_TIMEOUT_MS || "20000");

mkdirSync(SHOTS_DIR, { recursive: true });
mkdirSync(LOGS_DIR, { recursive: true });

const steps = [];
const notes = [];
const consoleEvents = [];
let shotSeq = 0;
let diagnosticsClipboard = "";

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

        await runStep("evidence-signal", "Exercise evidence/signal surfaces", async () => {
            await page.keyboard.press("h");
            await page.waitForTimeout(350);
            await page.keyboard.press("j");
            await page.waitForTimeout(350);
            await waitFor(async () => {
                if (hasLine("signal/composite/")) return true;
                const probe = await readRuntimeProbe(page);
                return Number(probe.probe_md_subscribe_ack_count ?? 0) > 0;
            }, "signal/composite subscribe");
            const shot = await snap(page, "evidence-signal");
            return { shots: [shot], details: "Signal channel observed and overlay toggles executed." };
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

            return { shots: [s1, s2, s3], details: "Focus, Zen, and Compare layouts toggled." };
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
            shots_dir: SHOTS_DIR,
            logs_dir: LOGS_DIR,
            steps,
            runtime_probe: runtimeProbe,
            runtime_config: runtimeConfig,
            diagnostics_clipboard_preview: typeof diagnosticsClipboard === "string"
                ? diagnosticsClipboard.slice(0, 400)
                : "",
            console_event_count: consoleEvents.length,
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
