#!/usr/bin/env node

import { readFileSync, writeFileSync, existsSync } from "fs";
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

const routerModeSamples = metricSamples(metricsText, "delivery_router_coherence_mode")
    .filter((s) => s.value > 0);
const routerViolationSamples = metricSamples(metricsText, "delivery_router_coherence_violations_total");
const routerViolationsTotal = routerViolationSamples.reduce((acc, s) => acc + s.value, 0);
const batchFallbackSamples = metricSamples(metricsText, "ws_batch_fallback_events_total");
const batchFallbackEventsTotal = batchFallbackSamples.reduce((acc, s) => acc + s.value, 0);
const allowBatchedFallback = envBool("IQ_ALLOW_BATCHED_FALLBACK");
const batchFallbackRemovalRuns = Number.parseInt(process.env.IQ_BATCHED_FALLBACK_ZERO_RUNS || "5", 10) || 5;
const wsMissingTsSamples = metricSamples(metricsText, "ws_contract_violations_total")
    .filter((s) => s.labels.reason === "missing_ts_server");
const wsMissingTsTotal = wsMissingTsSamples.reduce((acc, s) => acc + s.value, 0);

const resyncSentIdx = consoleLines.findIndex((l) => l.includes("[md-lifecycle] resync_sent"));
const resyncAckIdx = consoleLines.findIndex((l) => l.includes("[md-lifecycle] ack_recv op=resync"));
const desyncRecoveryIdx = consoleLines.findIndex((l) => l.includes("[md-lifecycle] desync_recovery resync"));
const resyncStep = (Array.isArray(smoke.steps) ? smoke.steps : []).find((s) => s.id === "resync");
const sawCanonicalEvidenceSub = consoleLines.some((l) => l.includes("subscribe_sent subject=liquidity.evidence/"));
const sawCanonicalSignalSub = consoleLines.some((l) => l.includes("subscribe_sent subject=signal/"));
const sawLegacyEvidenceSub = consoleLines.some((l) => l.includes("subscribe_sent subject=insights.microstructure_evidence/"));
const sawLegacySignalSub = consoleLines.some((l) => l.includes("subscribe_sent subject=signal/composite/"));

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
    "batched_fastpath_fallback",
    "batched fast-path fallback",
    allowBatchedFallback || batchFallbackEventsTotal === 0,
    `batched_fallback_events=${batchFallbackEventsTotal} allow_override=${allowBatchedFallback}`,
    ["ws_batch_fallback_events_total"]
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

const legacyEvidenceRejected = Number(probe.probe_md_legacy_evidence_rejected ?? -1);
const legacySignalRejected = Number(probe.probe_md_legacy_signal_rejected ?? -1);
addCheck(
    "legacy_rejections_non_negative",
    "legacy rejection counters",
    legacyEvidenceRejected >= 0 && legacySignalRejected >= 0,
    `legacy_evidence_rejected=${legacyEvidenceRejected} legacy_signal_rejected=${legacySignalRejected}`,
    ["legacy_evidence_rejected", "legacy_signal_rejected"]
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
    failed_steps: failedSteps.map((s) => ({ id: s.id, details: s.details || "" })),
    failed_checks: failedChecks.map((c) => ({ id: c.id, evidence: c.evidence })),
};
writeFileSync(summaryPath, JSON.stringify(summary, null, 2));

process.exit(overallPass ? 0 : 1);
