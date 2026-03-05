#!/usr/bin/env node

import { readFileSync, writeFileSync, existsSync, readdirSync } from "fs";
import { join } from "path";

function readText(path) {
    if (!existsSync(path)) return "";
    return readFileSync(path, "utf8");
}

function readJSON(path) {
    if (!existsSync(path)) return null;
    try {
        return JSON.parse(readFileSync(path, "utf8"));
    } catch {
        return null;
    }
}

function parseLabels(raw) {
    if (!raw) return {};
    const labels = {};
    const parts = raw.split(",");
    for (const part of parts) {
        const idx = part.indexOf("=");
        if (idx <= 0) continue;
        const key = part.slice(0, idx).trim();
        let value = part.slice(idx + 1).trim();
        if (value.startsWith("\"") && value.endsWith("\"")) {
            value = value.slice(1, -1);
        }
        labels[key] = value;
    }
    return labels;
}

function metricSamples(metricsText, name) {
    const samples = [];
    const lines = metricsText.split(/\r?\n/);
    for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed || trimmed.startsWith("#")) continue;
        const match = trimmed.match(new RegExp(`^${name}(\\{([^}]*)\\})?\\s+([0-9.eE+-]+)$`));
        if (!match) continue;
        const labels = parseLabels(match[2] || "");
        const value = Number(match[3]);
        samples.push({ labels, value, raw: trimmed });
    }
    return samples;
}

function labelGroupKey(labels, exclude = []) {
    const out = [];
    const ignored = new Set(exclude);
    for (const key of Object.keys(labels).sort()) {
        if (ignored.has(key)) continue;
        out.push(`${key}=${labels[key]}`);
    }
    return out.join(",");
}

function parseHistogramLe(raw) {
    if (raw === "+Inf") return Number.POSITIVE_INFINITY;
    const v = Number(raw);
    return Number.isFinite(v) ? v : Number.NaN;
}

function histogramQuantilesByLabel(metricsText, baseName, groupLabel, quantiles) {
    const buckets = metricSamples(metricsText, `${baseName}_bucket`);
    const groups = new Map();
    for (const sample of buckets) {
        const leRaw = sample.labels.le;
        const le = parseHistogramLe(leRaw);
        if (!Number.isFinite(le) && le !== Number.POSITIVE_INFINITY) continue;
        const group = sample.labels[groupLabel] || "unknown";
        const key = `${group}|${labelGroupKey(sample.labels, ["le"])}`;
        if (!groups.has(key)) {
            groups.set(key, { group, buckets: [] });
        }
        groups.get(key).buckets.push({ le, count: Number(sample.value) || 0 });
    }

    const out = new Map();
    for (const value of groups.values()) {
        value.buckets.sort((a, b) => a.le - b.le);
        const totalSample = value.buckets.find((b) => b.le === Number.POSITIVE_INFINITY);
        const total = totalSample ? totalSample.count : 0;
        const qValues = {};
        for (const q of quantiles) {
            if (!(q > 0 && q <= 1) || total <= 0) {
                qValues[q] = null;
                continue;
            }
            const target = total * q;
            const bucket = value.buckets.find((b) => b.count >= target);
            qValues[q] = bucket ? bucket.le : null;
        }
        out.set(value.group, { count: total, quantiles: qValues });
    }
    return out;
}

function excerpt(lines, includes, max = 6) {
    const out = [];
    for (const line of lines) {
        const ok = includes.some((p) =>
            p instanceof RegExp ? p.test(line) : line.includes(p)
        );
        if (ok) out.push(line);
        if (out.length >= max) break;
    }
    return out;
}

function statusIcon(ok) {
    return ok ? "PASS" : "FAIL";
}

function fmt(v) {
    return v === null || v === undefined ? "n/a" : String(v);
}

function envBool(name) {
    const raw = String(process.env[name] || "").trim().toLowerCase();
    return raw === "1" || raw === "true" || raw === "yes" || raw === "on";
}

function listIqRunDirs(runDir) {
    const baseDir = join(runDir, "..");
    if (!existsSync(baseDir)) return [];
    const out = [];
    for (const name of readdirSync(baseDir)) {
        const dir = join(baseDir, name);
        if (existsSync(join(dir, "logs", "playwright-smoke.json"))) {
            out.push(dir);
        }
    }
    out.sort();
    return out.filter((dir) => dir <= runDir);
}

function readRunStatsFallback(runDir) {
    const smoke = readJSON(join(runDir, "logs", "playwright-smoke.json"));
    const summary = readJSON(join(runDir, "summary.json"));
    const probe = smoke && smoke.runtime_probe ? smoke.runtime_probe : {};
    const statsProbe = smoke && smoke.stats_probe ? smoke.stats_probe : {};
    const fallbackRaw = statsProbe.md_stats_fallback_frames ?? probe.probe_md_stats_fallback_frames;
    const fallback = Number(fallbackRaw);
    return {
        hasValue: Number.isFinite(fallback),
        fallback,
        overallPass: Boolean(summary && summary.overall_pass === true),
    };
}

function statsFallbackZeroStreak(runDir) {
    const runDirs = listIqRunDirs(runDir);
    let streak = 0;
    for (let i = runDirs.length - 1; i >= 0; i--) {
        const sample = readRunStatsFallback(runDirs[i]);
        if (!sample.overallPass || !sample.hasValue || sample.fallback !== 0) {
            break;
        }
        streak += 1;
    }
    return { streak, observedRuns: runDirs.length };
}

const runDir = process.argv[2];
if (!runDir) {
    console.error("usage: node scripts/iq/analyze_iq_run.mjs <run_dir>");
    process.exit(2);
}

const logsDir = join(runDir, "logs");
const smokePath = join(logsDir, "playwright-smoke.json");
const consolePath = join(logsDir, "playwright-console.log");
const metricsPath = join(logsDir, "server.metrics.prom");
const composePath = join(logsDir, "compose.all.log");
const reportPath = join(runDir, "report.md");
const summaryPath = join(runDir, "summary.json");

const smoke = readJSON(smokePath) || { steps: [], runtime_probe: {} };
const consoleText = readText(consolePath);
const metricsText = readText(metricsPath);
const composeText = readText(composePath);

const consoleLines = consoleText.split(/\r?\n/).filter(Boolean);
const composeLines = composeText.split(/\r?\n/).filter(Boolean);
const probe = smoke.runtime_probe || {};
const statsProbe = smoke.stats_probe || {};

const routerModeSamples = metricSamples(metricsText, "delivery_router_coherence_mode")
    .filter((s) => s.value > 0);
const routerViolationSamples = metricSamples(metricsText, "delivery_router_coherence_violations_total");
const routerViolationsTotal = routerViolationSamples.reduce((acc, s) => acc + s.value, 0);
const batchFallbackSamples = metricSamples(metricsText, "ws_batch_fallback_events_total");
const batchFallbackEventsTotal = batchFallbackSamples.reduce((acc, s) => acc + s.value, 0);
const legacyRouteSamples = metricSamples(metricsText, "ws_legacy_requests_total");
const legacyRouteAcceptedTotal = legacyRouteSamples
    .filter((s) => s.labels.status === "accepted")
    .reduce((acc, s) => acc + s.value, 0);
const legacyRouteRejectedTotal = legacyRouteSamples
    .filter((s) => s.labels.status === "rejected")
    .reduce((acc, s) => acc + s.value, 0);
const legacyRouteTotal = legacyRouteAcceptedTotal + legacyRouteRejectedTotal;
const allowBatchedFallback = envBool("IQ_ALLOW_BATCHED_FALLBACK");
const batchFallbackRemovalRuns = Number.parseInt(process.env.IQ_BATCHED_FALLBACK_ZERO_RUNS || "5", 10) || 5;
const allowStatsFallback = envBool("IQ_ALLOW_STATS_FALLBACK");
const statsFallbackRemovalRuns = Number.parseInt(process.env.IQ_STATS_FALLBACK_ZERO_RUNS || "5", 10) || 5;
const allowUnexpectedSkips = envBool("IQ_ALLOW_UNEXPECTED_SKIPS");
const statsFallbackStreak = statsFallbackZeroStreak(runDir);
const wireBudgetChannels = String(process.env.IQ_WIRE_BUDGET_CHANNELS || "trade,book_snapshot,stats,candle")
    .split(",")
    .map((v) => v.trim())
    .filter(Boolean);
const wireP95BudgetMs = Number.parseFloat(process.env.IQ_WIRE_P95_BUDGET_MS || "2000");
const wireP99BudgetMs = Number.parseFloat(process.env.IQ_WIRE_P99_BUDGET_MS || "5000");
const routerStateMaxEntries = Number.parseInt(process.env.IQ_ROUTER_STREAM_STATE_MAX || "2048", 10) || 2048;

const wireLatencyByChannel = histogramQuantilesByLabel(
    metricsText,
    "ws_publish_to_deliver_latency_seconds",
    "channel",
    [0.95, 0.99]
);
const routerStreamEntries = metricSamples(metricsText, "router_stream_state_entries")[0]?.value ?? 0;
const routerStreamActive = metricSamples(metricsText, "router_stream_state_active_total")[0]?.value ?? 0;
const routerStreamEvicted = metricSamples(metricsText, "delivery_router_stream_state_evicted_total")[0]?.value ?? 0;
const wsMissingTsSamples = metricSamples(metricsText, "ws_contract_violations_total")
    .filter((s) => s.labels.reason === "missing_ts_server");
const wsMissingTsTotal = wsMissingTsSamples.reduce((acc, s) => acc + s.value, 0);
const unexpectedSkipTotals = composeLines
    .map((line) => {
        const match = line.match(/skip_unexpected_total[^0-9]*([0-9]+)/i);
        return match ? Number(match[1]) : null;
    })
    .filter((v) => Number.isFinite(v));
const unexpectedSkipTotal = unexpectedSkipTotals.length > 0 ? Math.max(...unexpectedSkipTotals) : 0;

const resyncSentIdx = consoleLines.findIndex((l) => l.includes("[md-lifecycle] resync_sent"));
const resyncAckIdx = consoleLines.findIndex((l) => l.includes("[md-lifecycle] ack_recv op=resync"));
const desyncRecoveryIdx = consoleLines.findIndex((l) => l.includes("[md-lifecycle] desync_recovery resync"));
const resyncStep = (Array.isArray(smoke.steps) ? smoke.steps : []).find((s) => s.id === "resync");
const sawCanonicalEvidenceSub = consoleLines.some((l) => l.includes("subscribe_sent subject=liquidity.evidence/"));
const sawCanonicalSignalSub = consoleLines.some((l) => l.includes("subscribe_sent subject=signal/"));
const sawLegacyEvidenceSub = consoleLines.some((l) => l.includes("subscribe_sent subject=insights.microstructure_evidence/"));
const sawLegacySignalSub = consoleLines.some((l) => l.includes("subscribe_sent subject=signal/composite/"));
const hardConsoleErrors = consoleLines.filter((l) => l.includes("[error]") || l.includes("[pageerror]"));
const networkRequestFailures = Array.isArray(smoke.network_request_failures) ? smoke.network_request_failures : [];
const networkErrorResponses = Array.isArray(smoke.network_error_responses) ? smoke.network_error_responses : [];
const networkFailureCount = Number.isFinite(Number(smoke.network_failure_count))
    ? Number(smoke.network_failure_count)
    : networkRequestFailures.length;
const networkErrorResponseCount = Number.isFinite(Number(smoke.network_error_response_count))
    ? Number(smoke.network_error_response_count)
    : networkErrorResponses.length;

const checks = [];

function addCheck(id, title, ok, evidence, excerptPatterns = []) {
    checks.push({ id, title, ok, evidence, excerptPatterns });
}

const missingTsGap = probe.probe_md_backend_gap_missing_ts_server;
addCheck(
    "ts_server_present",
    "ts_server present",
    Number(missingTsGap) === 0 && wsMissingTsTotal === 0,
    `client_missing_ts_gap=${fmt(missingTsGap)} ws_contract_missing_ts=${wsMissingTsTotal}`,
    ["missing_ts_server", "ws_contract_violations_total"]
);

const recurringSeqGaps = probe.probe_md_backend_gap_seq_gap_recurring;
addCheck(
    "seq_monotonic",
    "seq monotonic",
    Number(recurringSeqGaps) === 0,
    `client_recurring_seq_gaps=${fmt(recurringSeqGaps)} router_coherence_violations_total=${routerViolationsTotal}`,
    ["seq_gap", "coherence_violations_total", "seq_non_monotonic"]
);

addCheck(
    "unexpected_skip_zero",
    "unexpected skip/canonicalization zero",
    allowUnexpectedSkips || unexpectedSkipTotal === 0,
    `skip_unexpected_total=${unexpectedSkipTotal} allow_override=${allowUnexpectedSkips}`,
    ["skip_unexpected_total", "canonicalization_error", "parse_error"]
);

addCheck(
    "batched_fastpath_fallback",
    "batched fast-path fallback",
    allowBatchedFallback || batchFallbackEventsTotal === 0,
    `batched_fallback_events=${batchFallbackEventsTotal} allow_override=${allowBatchedFallback}`,
    ["ws_batch_fallback_events_total"]
);

const canonicalStatsFrames = Number(
    statsProbe.canonical_stats_frames ?? probe.probe_md_canonical_stats_frames ?? -1
);
const widgetStatsParseTotal = Number(
    statsProbe.widget_stats_parse_total ?? probe.probe_widget_stats_parse_total ?? -1
);
addCheck(
    "stats_canonical",
    "stats canonical delivery",
    canonicalStatsFrames > 0 && widgetStatsParseTotal > 0,
    `canonical_stats_frames=${canonicalStatsFrames} widget_stats_parse_total=${widgetStatsParseTotal}`,
    ["aggregation.stats", "canonical_stats_frames"]
);

const statsFallbackFrames = Number(
    statsProbe.md_stats_fallback_frames ?? probe.probe_md_stats_fallback_frames ?? -1
);
addCheck(
    "stats_fallback_counter",
    "stats fallback counter",
    statsFallbackFrames >= 0,
    `md_stats_fallback_frames=${statsFallbackFrames} zero_streak=${statsFallbackStreak.streak}/${statsFallbackRemovalRuns} allow_override=${allowStatsFallback}`,
    ["stats_fallback_frames", "aggregation.stats"]
);

const prevSeqViolations = probe.probe_md_prev_seq_violations;
addCheck(
    "prev_seq_chaining",
    "prev_seq chaining",
    Number(prevSeqViolations) === 0,
    `client_prev_seq_violations=${fmt(prevSeqViolations)}`,
    ["prev_seq", "snapshot_seq_violations"]
);

const cadence = probe.probe_md_server_metrics_cadence_ms;
const noMetricsGap = probe.probe_md_backend_gap_no_metrics;
addCheck(
    "metrics_cadence",
    "metrics cadence",
    Number(cadence) > 0 && Number(noMetricsGap) === 0,
    `server_metrics_cadence_ms=${fmt(cadence)} backend_gap_no_metrics=${fmt(noMetricsGap)}`,
    ["metrics_cadence", "backend_gap_no_metrics", "metrics"]
);

const resyncOrderedByLog = resyncSentIdx >= 0 &&
    resyncAckIdx > resyncSentIdx &&
    (desyncRecoveryIdx < 0 || desyncRecoveryIdx < resyncSentIdx);
const resyncOrdered = resyncOrderedByLog || Boolean(resyncStep && resyncStep.ok);
addCheck(
    "resync_order",
    "resync order",
    resyncOrdered,
    `desync_recovery_idx=${desyncRecoveryIdx} resync_sent_idx=${resyncSentIdx} resync_ack_idx=${resyncAckIdx} smoke_resync_step_ok=${Boolean(resyncStep && resyncStep.ok)}`,
    ["desync_recovery resync", "resync_sent", "ack_recv op=resync"]
);

const activeModes = routerModeSamples
    .map((s) => s.labels.mode || "unknown")
    .filter((v, i, arr) => arr.indexOf(v) === i);
addCheck(
    "router_coherence_mode",
    "router coherence mode",
    activeModes.length > 0,
    `modes=${activeModes.join(",") || "none"} violations_total=${routerViolationsTotal}`,
    ["delivery_router_coherence_mode", "delivery_router_coherence_violations_total"]
);

addCheck(
    "canonical_subjects",
    "canonical evidence/signal subjects",
    sawCanonicalEvidenceSub && sawCanonicalSignalSub && !sawLegacyEvidenceSub && !sawLegacySignalSub,
    `canonical_evidence_sub=${sawCanonicalEvidenceSub} canonical_signal_sub=${sawCanonicalSignalSub} legacy_evidence_sub=${sawLegacyEvidenceSub} legacy_signal_sub=${sawLegacySignalSub}`,
    ["subscribe_sent subject=liquidity.evidence/", "subscribe_sent subject=signal/", "signal/composite/", "insights.microstructure_evidence/"]
);

addCheck(
    "legacy_route_zero",
    "legacy route requests zero",
    legacyRouteTotal === 0,
    `ws_legacy_requests_total accepted=${legacyRouteAcceptedTotal} rejected=${legacyRouteRejectedTotal} total=${legacyRouteTotal}`,
    ["ws_legacy_requests_total", "/ws/marketdata", "legacy route"]
);

const legacyDowngradeCount = Number(probe.probe_md_legacy_downgrade_count ?? -1);
addCheck(
    "legacy_downgrade_zero",
    "legacy downgrade zero",
    legacyDowngradeCount === 0,
    `probe_md_legacy_downgrade_count=${legacyDowngradeCount}`,
    ["legacy_downgrade", "probe_md_legacy_downgrade_count"]
);

const legacyEvidenceRejected = Number(probe.probe_md_legacy_evidence_rejected ?? -1);
const legacySignalRejected = Number(probe.probe_md_legacy_signal_rejected ?? -1);
const legacyEvidenceFrames = Number(probe.probe_md_legacy_evidence_frames ?? -1);
const legacySignalFrames = Number(probe.probe_md_legacy_signal_frames ?? -1);
const evidenceFallbackFrames = Number(probe.probe_md_evidence_fallback_frames ?? -1);
const signalFallbackFrames = Number(probe.probe_md_signal_fallback_frames ?? -1);
addCheck(
    "legacy_fallback_zero",
    "legacy/fallback zero",
    legacyEvidenceFrames === 0 &&
        legacySignalFrames === 0 &&
        evidenceFallbackFrames === 0 &&
        signalFallbackFrames === 0 &&
        legacyEvidenceRejected === 0 &&
        legacySignalRejected === 0,
    `legacy_evidence_frames=${legacyEvidenceFrames} legacy_signal_frames=${legacySignalFrames} evidence_fallback_frames=${evidenceFallbackFrames} signal_fallback_frames=${signalFallbackFrames} legacy_evidence_rejected=${legacyEvidenceRejected} legacy_signal_rejected=${legacySignalRejected}`,
    ["legacy_evidence_frames", "legacy_signal_frames", "evidence_fallback_frames", "signal_fallback_frames", "legacy_evidence_rejected", "legacy_signal_rejected"]
);

const compatStatsFallback = Number(
    statsProbe.md_stats_fallback_frames ?? probe.probe_md_stats_fallback_frames ?? -1
);
addCheck(
    "compat_fallback_zero",
    "no fallback/compat path hit",
    compatStatsFallback === 0 &&
        evidenceFallbackFrames === 0 &&
        signalFallbackFrames === 0 &&
        batchFallbackEventsTotal === 0,
    `threshold=0 stats_fallback_frames=${compatStatsFallback} evidence_fallback_frames=${evidenceFallbackFrames} signal_fallback_frames=${signalFallbackFrames} ws_batch_fallback_events=${batchFallbackEventsTotal}`,
    ["ws_batch_fallback_events_total", "stats_fallback_frames", "evidence_fallback_frames", "signal_fallback_frames"]
);

const wireObserved = [];
const wireViolations = [];
for (const channel of wireBudgetChannels) {
    const sample = wireLatencyByChannel.get(channel);
    if (!sample || !(sample.count > 0)) {
        continue;
    }
    const p95s = sample.quantiles[0.95];
    const p99s = sample.quantiles[0.99];
    const p95ms = Number.isFinite(p95s) ? p95s * 1000 : null;
    const p99ms = Number.isFinite(p99s) ? p99s * 1000 : null;
    wireObserved.push(`${channel}:count=${sample.count},p95_ms=${fmt(p95ms)},p99_ms=${fmt(p99ms)}`);
    if ((p95ms !== null && p95ms > wireP95BudgetMs) || (p99ms !== null && p99ms > wireP99BudgetMs)) {
        wireViolations.push(channel);
    }
}
addCheck(
    "wire_budget_p95_p99",
    "wire budgets p95/p99",
    wireObserved.length > 0 && wireViolations.length === 0,
    `threshold_ms(p95<=${wireP95BudgetMs},p99<=${wireP99BudgetMs}) observed=${wireObserved.join(";") || "none"} violations=${wireViolations.join(",") || "none"}`,
    ["ws_publish_to_deliver_latency_seconds_bucket", "ws_publish_to_deliver_latency_seconds_count"]
);

addCheck(
    "bounded_state_eviction",
    "bounded state/eviction not growing",
    routerStreamEntries >= 0 &&
        routerStreamActive >= 0 &&
        routerStreamActive <= routerStreamEntries &&
        routerStreamEntries <= routerStateMaxEntries &&
        routerStreamEvicted >= 0,
    `threshold_entries<=${routerStateMaxEntries} entries=${routerStreamEntries} active=${routerStreamActive} evicted_total=${routerStreamEvicted}`,
    ["router_stream_state_entries", "router_stream_state_active_total", "delivery_router_stream_state_evicted_total"]
);

const allocFrame = Number(probe.probe_md_alloc_estimate_frame ?? -1);
addCheck(
    "alloc_counter_frame",
    "alloc/frame counter",
    allocFrame >= 0,
    `alloc_estimate_frame=${allocFrame}`,
    ["alloc_estimate_frame"]
);

const signalCount = Number(probe.probe_widget_signal_count ?? 0);
const evidenceCount = Number(probe.probe_widget_evidence_count ?? 0);
const signalLinkTotal = Number(probe.probe_widget_signal_link_total ?? -1);
const linkHealthy = signalLinkTotal >= 0 && (signalCount <= 0 || evidenceCount <= 0 || signalLinkTotal > 0);
addCheck(
    "signal_evidence_link",
    "signal→evidence link",
    linkHealthy,
    `signal_count=${signalCount} evidence_count=${evidenceCount} link_total=${signalLinkTotal}`,
    ["signal", "evidence", "link"]
);

const layoutVersion = Number(probe.probe_layout_version ?? 0);
const layoutLinkEnabled = Number(probe.probe_layout_link_enabled ?? -1);
addCheck(
    "layout_versioned",
    "layout v4/v5 active when persisted",
    (layoutVersion === 0 || layoutVersion >= 4) && (layoutLinkEnabled === 0 || layoutLinkEnabled === 1),
    `layout_version=${layoutVersion} layout_link_enabled=${layoutLinkEnabled} layout_migrated=${fmt(probe.probe_layout_migrated)}`,
    ["layout_version", "layout_link_enabled", "layout_migrated"]
);

addCheck(
    "console_network_clean",
    "console/network clean",
    hardConsoleErrors.length === 0 && networkFailureCount === 0 && networkErrorResponseCount === 0,
    `console_errors=${hardConsoleErrors.length} request_failures=${networkFailureCount} error_responses=${networkErrorResponseCount}`,
    ["[error]", "[pageerror]", "requestfailed"]
);

function addWidgetBudgetChecks(idPrefix, title, probePrefix) {
    const renderP95 = Number(probe[`probe_widget_${probePrefix}_render_p95_us`] ?? -1);
    const renderBudget = Number(probe[`probe_widget_${probePrefix}_render_budget_us`] ?? -1);
    const renderOverBudget = Number(probe[`probe_widget_${probePrefix}_render_over_budget`] ?? -1);
    addCheck(
        `${idPrefix}_render_budget`,
        `${title} render budget`,
        renderP95 >= 0 && renderBudget > 0 && renderP95 <= renderBudget && renderOverBudget === 0,
        `render_p95_us=${renderP95} render_budget_us=${renderBudget} render_over_budget=${renderOverBudget}`,
        [`probe_widget_${probePrefix}_render_p95_us`, `probe_widget_${probePrefix}_render_budget_us`]
    );

    const dropTotal = Number(probe[`probe_widget_${probePrefix}_drop_total`] ?? -1);
    const dropCapacity = Number(probe[`probe_widget_${probePrefix}_drop_capacity_total`] ?? -1);
    const dropRenderOverflow = Number(probe[`probe_widget_${probePrefix}_drop_render_overflow_total`] ?? -1);
    addCheck(
        `${idPrefix}_drop_reasons_bounded`,
        `${title} drop reasons bounded`,
        dropTotal >= 0 &&
            dropCapacity >= 0 &&
            dropRenderOverflow >= 0 &&
            dropTotal === dropCapacity + dropRenderOverflow,
        `drop_total=${dropTotal} drop_capacity_total=${dropCapacity} drop_render_overflow_total=${dropRenderOverflow}`,
        [`probe_widget_${probePrefix}_drop_total`, `probe_widget_${probePrefix}_drop_capacity_total`, `probe_widget_${probePrefix}_drop_render_overflow_total`]
    );
}

addWidgetBudgetChecks("stats_widget", "stats widget", "stats");
addWidgetBudgetChecks("dom_widget", "dom widget", "dom");
addWidgetBudgetChecks("tape_widget", "tape widget", "tape");
addWidgetBudgetChecks("evidence_widget", "evidence widget", "evidence");
addWidgetBudgetChecks("signal_widget", "signal widget", "signal");

const smokeSteps = Array.isArray(smoke.steps) ? smoke.steps : [];
const smokePass = smokeSteps.every((s) => s.ok);
const invariantsPass = checks.every((c) => c.ok);
const overallPass = smokePass && invariantsPass;

const failedChecks = checks.filter((c) => !c.ok);
const failedSteps = smokeSteps.filter((s) => !s.ok);

const logExcerptSections = [];
for (const check of checks) {
    const exFromConsole = excerpt(consoleLines, check.excerptPatterns, 6);
    const exFromCompose = excerpt(composeLines, check.excerptPatterns, 4);
    const exFromMetrics = excerpt(metricsText.split(/\r?\n/), check.excerptPatterns, 4);
    const merged = [...exFromConsole, ...exFromCompose, ...exFromMetrics].slice(0, 10);
    if (merged.length > 0) {
        logExcerptSections.push({ title: check.title, lines: merged });
    }
}

const markdown = [];
markdown.push("# IQ Loop Report");
markdown.push("");
markdown.push(`- run_dir: \`${runDir}\``);
markdown.push(`- generated_at: \`${new Date().toISOString()}\``);
markdown.push(`- status: **${overallPass ? "PASS" : "FAIL"}**`);
markdown.push("");
markdown.push("## Smoke Checklist");
markdown.push("");
markdown.push("| Step | Status | Details |");
markdown.push("|---|---|---|");
for (const step of smokeSteps) {
    markdown.push(`| ${step.id} (${step.name}) | ${statusIcon(Boolean(step.ok))} | ${step.details || ""} |`);
}
if (smokeSteps.length === 0) {
    markdown.push("| (none) | FAIL | missing playwright smoke report |");
}
markdown.push("");
markdown.push("## Invariant Checks");
markdown.push("");
markdown.push("| Invariant | Status | Evidence |");
markdown.push("|---|---|---|");
for (const check of checks) {
    markdown.push(`| ${check.title} | ${statusIcon(check.ok)} | ${check.evidence} |`);
}
markdown.push("");
markdown.push("## Strangler Removal Criteria");
markdown.push("");
markdown.push(`- batched fast-path fallback removal requires \`batched_fallback_events=0\` for **${batchFallbackRemovalRuns}** consecutive IQ runs.`);
markdown.push(`- current run batched_fallback_events: \`${batchFallbackEventsTotal}\``);
markdown.push(`- override active: \`${allowBatchedFallback}\` (set via \`IQ_ALLOW_BATCHED_FALLBACK=1\`)`);
markdown.push("");
markdown.push(`- stats fallback removal requires \`md_stats_fallback_frames=0\` for **${statsFallbackRemovalRuns}** consecutive IQ PASS runs.`);
markdown.push(`- current run md_stats_fallback_frames: \`${statsFallbackFrames}\``);
markdown.push(`- current consecutive zero streak: \`${statsFallbackStreak.streak}\` PASS runs (observed runs: \`${statsFallbackStreak.observedRuns}\`).`);
markdown.push(`- override active: \`${allowStatsFallback}\` (set via \`IQ_ALLOW_STATS_FALLBACK=1\`)`);
markdown.push("");
markdown.push("- unexpected skip/canonicalization gate requires `skip_unexpected_total=0` (from runtime logs).");
markdown.push(`- current run skip_unexpected_total: \`${unexpectedSkipTotal}\``);
markdown.push(`- override active: \`${allowUnexpectedSkips}\` (set via \`IQ_ALLOW_UNEXPECTED_SKIPS=1\`)`);
markdown.push("");
markdown.push("- legacy cutover gate requires `ws_legacy_requests_total=0` and `probe_md_legacy_downgrade_count=0` (no override).");
markdown.push(`- current run ws_legacy_requests_total: \`${legacyRouteTotal}\` (accepted=\`${legacyRouteAcceptedTotal}\`, rejected=\`${legacyRouteRejectedTotal}\`)`);
markdown.push(`- current run probe_md_legacy_downgrade_count: \`${legacyDowngradeCount}\``);
markdown.push("");
markdown.push("## Failures");
markdown.push("");
if (failedSteps.length === 0 && failedChecks.length === 0) {
    markdown.push("No failures.");
} else {
    for (const step of failedSteps) {
        markdown.push(`- smoke step \`${step.id}\`: ${step.details || "failed"}`);
    }
    for (const check of failedChecks) {
        markdown.push(`- invariant \`${check.id}\`: ${check.evidence}`);
    }
}
markdown.push("");
markdown.push("## Reproduction Steps");
markdown.push("");
markdown.push("```bash");
markdown.push("make up PROCESSOR_REPLICAS=2");
markdown.push("node tests/playwright/iq-smoke.mjs");
markdown.push("docker compose -f deploy/compose/docker-compose.yml --env-file deploy/envs/local.env --profile core --profile obs --profile client logs --no-color --timestamps");
markdown.push("```");
markdown.push("");
markdown.push("## Log Excerpts");
markdown.push("");
if (logExcerptSections.length === 0) {
    markdown.push("_No matching excerpts found._");
} else {
    for (const section of logExcerptSections) {
        markdown.push(`### ${section.title}`);
        markdown.push("");
        markdown.push("```text");
        markdown.push(...section.lines);
        markdown.push("```");
        markdown.push("");
    }
}

writeFileSync(reportPath, markdown.join("\n") + "\n");
const summary = {
    generated_at: new Date().toISOString(),
    overall_pass: overallPass,
    smoke_pass: smokePass,
    invariants_pass: invariantsPass,
    stats_fallback_zero_streak: statsFallbackStreak.streak,
    stats_fallback_required_runs: statsFallbackRemovalRuns,
    failed_steps: failedSteps.map((s) => ({ id: s.id, details: s.details || "" })),
    failed_checks: failedChecks.map((c) => ({ id: c.id, evidence: c.evidence })),
};
writeFileSync(summaryPath, JSON.stringify(summary, null, 2));

process.exit(overallPass ? 0 : 1);
