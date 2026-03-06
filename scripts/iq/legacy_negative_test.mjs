#!/usr/bin/env node

import { appendFileSync, existsSync, mkdirSync, readFileSync, writeFileSync } from "fs";
import { dirname, join } from "path";

const SERVER_BASE = (process.env.LEGACY_NEGATIVE_SERVER_BASE || "http://127.0.0.1:8080").replace(/\/+$/, "");
const METRICS_URL = process.env.LEGACY_NEGATIVE_METRICS_URL || `${SERVER_BASE}/metrics`;
const WS_URL = process.env.LEGACY_NEGATIVE_WS_URL || `${SERVER_BASE.replace(/^http/i, "ws")}/ws?api_key=prod_key_1`;
const LOG_PATH = process.env.LEGACY_NEGATIVE_LOG || join(process.cwd(), "artifacts", "iq", "logs", "legacy-negative.log");
const OUT_JSON = process.env.LEGACY_NEGATIVE_JSON || join(process.cwd(), "artifacts", "iq", "logs", "legacy-negative.json");
const METRICS_OUT = process.env.LEGACY_NEGATIVE_METRICS_OUT || join(process.cwd(), "artifacts", "iq", "logs", "legacy-negative.server.metrics.prom");
const PLAYWRIGHT_SMOKE_PATH = process.env.LEGACY_NEGATIVE_PLAYWRIGHT_SMOKE || join(process.cwd(), "artifacts", "iq", "logs", "playwright-smoke.json");
const HTTP_TIMEOUT_MS = Number(process.env.LEGACY_NEGATIVE_HTTP_TIMEOUT_MS || "7000");
const WS_TIMEOUT_MS = Number(process.env.LEGACY_NEGATIVE_WS_TIMEOUT_MS || "9000");

mkdirSync(dirname(LOG_PATH), { recursive: true });
mkdirSync(dirname(OUT_JSON), { recursive: true });

const rows = [];

function nowIso() {
    return new Date().toISOString();
}

function writeRow(row) {
    rows.push(row);
    const line = JSON.stringify(row);
    appendFileSync(LOG_PATH, `${line}\n`);
    console.log(line);
}

function emitCheck({
    testCase,
    expected,
    got,
    ok,
    metricName = "",
    metricValue = "",
}) {
    writeRow({
        ts: nowIso(),
        level: ok ? "INFO" : "ERROR",
        test_case: testCase,
        expected,
        got,
        metric_name: metricName,
        metric_value: metricValue,
    });
    return ok;
}

async function fetchText(url) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), HTTP_TIMEOUT_MS);
    try {
        const res = await fetch(url, { signal: controller.signal });
        const text = await res.text();
        return { ok: res.ok, status: res.status, text };
    } finally {
        clearTimeout(timer);
    }
}

function parseLabels(raw) {
    if (!raw) return {};
    const labels = {};
    for (const pair of raw.split(",")) {
        const idx = pair.indexOf("=");
        if (idx <= 0) continue;
        const key = pair.slice(0, idx).trim();
        let value = pair.slice(idx + 1).trim();
        if (value.startsWith("\"") && value.endsWith("\"")) {
            value = value.slice(1, -1);
        }
        labels[key] = value;
    }
    return labels;
}

function metricSample(metricsText, metricName, matcher = {}) {
    const lines = metricsText.split(/\r?\n/);
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

async function runLegacySubscribeCheck(subject, requestId) {
    return new Promise((resolve) => {
        let settled = false;
        const done = (out) => {
            if (settled) return;
            settled = true;
            clearTimeout(timer);
            try {
                ws.close();
            } catch {}
            resolve(out);
        };
        const ws = new WebSocket(WS_URL);
        const timer = setTimeout(() => {
            done({ ok: false, got: "timeout waiting websocket response" });
        }, WS_TIMEOUT_MS);

        ws.onopen = () => {
            ws.send(JSON.stringify({
                op: "subscribe",
                request_id: requestId,
                subject,
            }));
        };

        ws.onerror = () => {
            done({ ok: false, got: "websocket error during subscribe probe" });
        };

        ws.onmessage = (event) => {
            let msg;
            try {
                msg = JSON.parse(String(event.data));
            } catch {
                return;
            }
            if (msg && msg.type === "error" && msg.op === "subscribe") {
                done({ ok: true, got: JSON.stringify(msg) });
                return;
            }
            if (msg && msg.type === "ack" && msg.op === "subscribe") {
                done({ ok: false, got: JSON.stringify(msg) });
            }
        };
    });
}

function readLegacyDowngradeProbe(playwrightPath) {
    if (!existsSync(playwrightPath)) {
        return { ok: false, value: null, got: `missing file: ${playwrightPath}` };
    }
    try {
        const raw = readFileSync(playwrightPath, "utf8");
        const parsed = JSON.parse(raw);
        const value = Number(parsed?.runtime_probe?.probe_md_legacy_downgrade_count ?? Number.NaN);
        if (!Number.isFinite(value)) {
            return { ok: false, value: null, got: "probe value missing/invalid in playwright-smoke.json" };
        }
        return { ok: true, value, got: String(value) };
    } catch (err) {
        return { ok: false, value: null, got: `playwright json parse error: ${err instanceof Error ? err.message : String(err)}` };
    }
}

async function main() {
    writeFileSync(LOG_PATH, "");

    const metricsBeforeResp = await fetchText(METRICS_URL);
    if (!metricsBeforeResp.ok) {
        emitCheck({
            testCase: "metrics_before_fetch",
            expected: "HTTP 200",
            got: `status=${metricsBeforeResp.status}`,
            ok: false,
            metricName: "metrics_fetch_before",
            metricValue: metricsBeforeResp.status,
        });
        writeFileSync(OUT_JSON, JSON.stringify({ overall_pass: false, reason: "failed to fetch metrics before checks" }, null, 2));
        process.exit(1);
    }
    const metricsBefore = metricsBeforeResp.text;

    const legacyAcceptedBefore = metricSample(metricsBefore, "ws_legacy_requests_total", { status: "accepted" });
    const legacyRejectedBefore = metricSample(metricsBefore, "ws_legacy_requests_total", { status: "rejected" });
    const subjectInvalidBefore = metricSample(metricsBefore, "ws_query_rejected_total", { reason: "subject_invalid" });

    const routeResp = await fetchText(`${SERVER_BASE}/ws/marketdata`);
    const routeStatusOk = routeResp.status === 410;
    emitCheck({
        testCase: "legacy_route_status",
        expected: "410",
        got: String(routeResp.status),
        ok: routeStatusOk,
    });

    const evidenceSub = await runLegacySubscribeCheck(
        "insights.microstructure_evidence/binance/BTC-USDT/raw",
        "legacy-negative-evidence",
    );
    emitCheck({
        testCase: "legacy_evidence_subject_rejected",
        expected: "websocket subscribe error frame",
        got: evidenceSub.got,
        ok: evidenceSub.ok,
    });

    const signalSub = await runLegacySubscribeCheck(
        "signal/composite/binance/BTC-USDT/1m",
        "legacy-negative-signal",
    );
    emitCheck({
        testCase: "legacy_signal_subject_rejected",
        expected: "websocket subscribe error frame",
        got: signalSub.got,
        ok: signalSub.ok,
    });

    const metricsAfterResp = await fetchText(METRICS_URL);
    if (!metricsAfterResp.ok) {
        emitCheck({
            testCase: "metrics_after_fetch",
            expected: "HTTP 200",
            got: `status=${metricsAfterResp.status}`,
            ok: false,
            metricName: "metrics_fetch_after",
            metricValue: metricsAfterResp.status,
        });
        writeFileSync(OUT_JSON, JSON.stringify({ overall_pass: false, reason: "failed to fetch metrics after checks" }, null, 2));
        process.exit(1);
    }
    const metricsAfter = metricsAfterResp.text;
    writeFileSync(METRICS_OUT, metricsAfter);

    const legacyAcceptedAfter = metricSample(metricsAfter, "ws_legacy_requests_total", { status: "accepted" });
    const legacyRejectedAfter = metricSample(metricsAfter, "ws_legacy_requests_total", { status: "rejected" });
    const subjectInvalidAfter = metricSample(metricsAfter, "ws_query_rejected_total", { reason: "subject_invalid" });

    const acceptedNever = legacyAcceptedAfter === 0;
    emitCheck({
        testCase: "legacy_route_never_accepted",
        expected: "0",
        got: String(legacyAcceptedAfter),
        ok: acceptedNever,
        metricName: "ws_legacy_requests_total{status=\"accepted\"}",
        metricValue: legacyAcceptedAfter,
    });

    const rejectedDelta = legacyRejectedAfter - legacyRejectedBefore;
    const rejectedDeltaOk = rejectedDelta >= 1;
    emitCheck({
        testCase: "legacy_route_rejected_counter_delta",
        expected: ">=1",
        got: String(rejectedDelta),
        ok: rejectedDeltaOk,
        metricName: "ws_legacy_requests_total{status=\"rejected\"}",
        metricValue: rejectedDelta,
    });

    const subjectInvalidDelta = subjectInvalidAfter - subjectInvalidBefore;
    const subjectInvalidOk = subjectInvalidDelta >= 2;
    emitCheck({
        testCase: "legacy_subject_invalid_counter_delta",
        expected: ">=2",
        got: String(subjectInvalidDelta),
        ok: subjectInvalidOk,
        metricName: "ws_query_rejected_total{reason=\"subject_invalid\"}",
        metricValue: subjectInvalidDelta,
    });

    const probe = readLegacyDowngradeProbe(PLAYWRIGHT_SMOKE_PATH);
    const probeOk = probe.ok && probe.value === 0;
    emitCheck({
        testCase: "legacy_downgrade_probe_zero",
        expected: "0",
        got: probe.got,
        ok: probeOk,
        metricName: "probe_md_legacy_downgrade_count",
        metricValue: probe.ok ? probe.value : "missing",
    });

    const overallPass = rows.every((r) => r.level !== "ERROR");
    const summary = {
        generated_at: nowIso(),
        overall_pass: overallPass,
        config: {
            server_base: SERVER_BASE,
            metrics_url: METRICS_URL,
            ws_url: WS_URL,
            playwright_smoke_path: PLAYWRIGHT_SMOKE_PATH,
        },
        counters: {
            ws_legacy_requests_accepted_before: legacyAcceptedBefore,
            ws_legacy_requests_accepted_after: legacyAcceptedAfter,
            ws_legacy_requests_rejected_before: legacyRejectedBefore,
            ws_legacy_requests_rejected_after: legacyRejectedAfter,
            ws_legacy_requests_rejected_delta: rejectedDelta,
            ws_query_rejected_subject_invalid_before: subjectInvalidBefore,
            ws_query_rejected_subject_invalid_after: subjectInvalidAfter,
            ws_query_rejected_subject_invalid_delta: subjectInvalidDelta,
            probe_md_legacy_downgrade_count: probe.ok ? probe.value : null,
        },
        checks: rows,
    };
    writeFileSync(OUT_JSON, JSON.stringify(summary, null, 2));
    process.exit(overallPass ? 0 : 1);
}

main().catch((err) => {
    emitCheck({
        testCase: "legacy_negative_script_exception",
        expected: "no exception",
        got: err instanceof Error ? err.stack || err.message : String(err),
        ok: false,
    });
    writeFileSync(OUT_JSON, JSON.stringify({ overall_pass: false, error: err instanceof Error ? err.message : String(err), checks: rows }, null, 2));
    process.exit(1);
});
