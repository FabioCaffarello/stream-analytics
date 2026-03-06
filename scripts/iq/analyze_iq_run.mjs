#!/usr/bin/env node

import { createHash } from "crypto";
import { spawnSync } from "child_process";
import { readFileSync, writeFileSync, existsSync, readdirSync, lstatSync, readlinkSync } from "fs";
import { join, resolve, dirname, basename } from "path";
import { envBoolValue, resolveIQProfile } from "./profile_loader.mjs";
import { validateBoundednessMatrix } from "./validate_boundedness_matrix.mjs";

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

function sortJSONValue(value) {
    if (Array.isArray(value)) {
        return value.map((item) => sortJSONValue(item));
    }
    if (value && typeof value === "object") {
        const out = {};
        const keys = Object.keys(value).sort();
        for (const key of keys) {
            out[key] = sortJSONValue(value[key]);
        }
        return out;
    }
    return value;
}

function stableJSONString(value, indent = 2) {
    return JSON.stringify(sortJSONValue(value), null, indent);
}

function sha256Hex(raw) {
    return createHash("sha256").update(raw).digest("hex");
}

function parseRunTimestampFromDir(runDirPath) {
    const runID = basename(resolve(runDirPath));
    const match = runID.match(/^(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})(\d{2})Z$/);
    if (!match) {
        return {
            run_id: runID,
            started_at_utc: null,
        };
    }
    const [, year, month, day, hour, minute, second] = match;
    const iso = new Date(Date.UTC(
        Number(year),
        Number(month) - 1,
        Number(day),
        Number(hour),
        Number(minute),
        Number(second)
    )).toISOString();
    return {
        run_id: runID,
        started_at_utc: iso,
    };
}

function resolveCommitHash() {
    const fromEnv = String(
        process.env.GIT_COMMIT ||
        process.env.COMMIT_SHA ||
        process.env.GITHUB_SHA ||
        ""
    ).trim();
    if (fromEnv) {
        return fromEnv;
    }
    const result = spawnSync("git", ["rev-parse", "HEAD"], {
        cwd: process.cwd(),
        encoding: "utf8",
    });
    if (result.status === 0) {
        const value = String(result.stdout || "").trim();
        if (value) {
            return value;
        }
    }
    return "unknown";
}

function matrixCapSnapshot(validation, id, fallback = null) {
    const value = Number(validation?.effectiveCaps?.[id]);
    if (Number.isFinite(value) && value > 0) {
        return value;
    }
    return fallback;
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

function parseChannelBudgetMap(raw) {
    const out = new Map();
    const text = String(raw || "").trim();
    if (!text) return out;
    for (const token of text.split(",")) {
        const part = token.trim();
        if (!part) continue;
        let idx = part.indexOf("=");
        if (idx < 0) idx = part.indexOf(":");
        if (idx <= 0) continue;
        const channel = part.slice(0, idx).trim();
        const budget = Number.parseFloat(part.slice(idx + 1).trim());
        if (!channel || !Number.isFinite(budget) || budget < 0) continue;
        out.set(channel, budget);
    }
    return out;
}

function parseOptionalNumber(raw) {
    const text = String(raw ?? "").trim();
    if (!text) return null;
    const value = Number.parseFloat(text);
    return Number.isFinite(value) ? value : null;
}

function parseOptionalInt(raw) {
    const text = String(raw ?? "").trim();
    if (!text) return null;
    const value = Number.parseInt(text, 10);
    return Number.isFinite(value) ? value : null;
}

function isNonNegativeNumber(value) {
    return Number.isFinite(value) && value >= 0;
}

const BACKPRESSURE_DROP_REASONS = new Set([
    "queue_full",
    "drop_oldest",
    "priority_drop",
    "priority_drop_self",
    "slow_client_disconnect",
]);

const SCORECARD_FALLBACK_COUNTER_KEYS = [
    "md_stats_fallback_frames",
    "md_evidence_fallback_frames",
    "md_signal_fallback_frames",
    "ws_batch_fallback_events",
    "md_legacy_downgrade_count",
];

function lstatSafe(path) {
    try {
        return lstatSync(path);
    } catch {
        return null;
    }
}

function normalizeNumber(value) {
    if (value === null || value === undefined) return null;
    if (typeof value === "string" && value.trim() === "") return null;
    const numeric = Number(value);
    return Number.isFinite(numeric) ? numeric : null;
}

function roundNumber(value, digits = 6) {
    if (!Number.isFinite(value)) return null;
    const factor = 10 ** digits;
    return Math.round(value * factor) / factor;
}

function emptyFallbackCounters() {
    const out = {};
    for (const key of SCORECARD_FALLBACK_COUNTER_KEYS) {
        out[key] = null;
    }
    return out;
}

function normalizeFallbackCounters(raw) {
    const out = emptyFallbackCounters();
    for (const key of SCORECARD_FALLBACK_COUNTER_KEYS) {
        out[key] = normalizeNumber(raw && raw[key]);
    }
    return out;
}

function emptyScorecardMetrics() {
    return {
        lat_p95_ms: null,
        lat_p99_ms: null,
        bytes_p95: null,
        bytes_p99: null,
        drops_total: null,
        backlog: null,
        cap: null,
        backlog_utilization: null,
        fallback_total: null,
        fallback_counters: emptyFallbackCounters(),
    };
}

function normalizeScorecardMetrics(raw) {
    return {
        lat_p95_ms: normalizeNumber(raw && raw.lat_p95_ms),
        lat_p99_ms: normalizeNumber(raw && raw.lat_p99_ms),
        bytes_p95: normalizeNumber(raw && raw.bytes_p95),
        bytes_p99: normalizeNumber(raw && raw.bytes_p99),
        drops_total: normalizeNumber(raw && raw.drops_total),
        backlog: normalizeNumber(raw && raw.backlog),
        cap: normalizeNumber(raw && raw.cap),
        backlog_utilization: normalizeNumber(raw && raw.backlog_utilization),
        fallback_total: normalizeNumber(raw && raw.fallback_total),
        fallback_counters: normalizeFallbackCounters(raw && raw.fallback_counters),
    };
}

function parseScorecardRatioThreshold(name, fallback) {
    const value = parseOptionalNumber(process.env[name]);
    if (value !== null && value > 1) return value;
    return fallback;
}

function parseScorecardDeltaThreshold(name, fallback) {
    const value = parseOptionalNumber(process.env[name]);
    if (value !== null && value >= 0) return value;
    return fallback;
}

function toBacklogUtilization(backlog, cap) {
    if (!Number.isFinite(backlog) || !Number.isFinite(cap) || cap <= 0) {
        return null;
    }
    return backlog / cap;
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

function buildScorecardSnapshot({
    metricsText,
    smoke,
    wireBudgetChannels,
    routerStateMaxEntries,
    layerStateMaxEntries,
}) {
    const probe = smoke && smoke.runtime_probe ? smoke.runtime_probe : {};
    const statsProbe = smoke && smoke.stats_probe ? smoke.stats_probe : {};
    const wireLatencyByChannel = histogramQuantilesByLabel(
        metricsText,
        "ws_publish_to_deliver_latency_seconds",
        "channel",
        [0.95, 0.99]
    );
    const wireBytesByChannel = histogramQuantilesByLabel(
        metricsText,
        "ws_wire_bytes",
        "channel",
        [0.95, 0.99]
    );
    const dropsByChannel = new Map();
    let wsBackpressureDropsTotal = 0;
    const wsDropSamples = metricSamples(metricsText, "ws_drops_total");
    for (const sample of wsDropSamples) {
        const reason = sample.labels.reason || "";
        if (!BACKPRESSURE_DROP_REASONS.has(reason)) {
            continue;
        }
        const value = normalizeNumber(sample.value) ?? 0;
        wsBackpressureDropsTotal += value;
        const channel = sample.labels.channel || "unknown";
        dropsByChannel.set(channel, (dropsByChannel.get(channel) || 0) + value);
    }

    const wsQueueLen = normalizeNumber(metricSamples(metricsText, "ws_queue_len")[0]?.value);
    const wsQueueCapacity = normalizeNumber(metricSamples(metricsText, "ws_queue_capacity")[0]?.value);
    const routerEntries = normalizeNumber(metricSamples(metricsText, "router_stream_state_entries")[0]?.value);
    const routerEvicted = normalizeNumber(metricSamples(metricsText, "delivery_router_stream_state_evicted_total")[0]?.value);
    const layerEntries = normalizeNumber(probe.probe_layer_stream_entries);
    const layerEvictions = normalizeNumber(probe.probe_layer_stream_evictions);
    const batchFallbackEvents = metricSamples(metricsText, "ws_batch_fallback_events_total")
        .reduce((acc, sample) => acc + (normalizeNumber(sample.value) ?? 0), 0);

    const fallbackCounters = normalizeFallbackCounters({
        md_stats_fallback_frames: statsProbe.md_stats_fallback_frames ?? probe.probe_md_stats_fallback_frames,
        md_evidence_fallback_frames: probe.probe_md_evidence_fallback_frames,
        md_signal_fallback_frames: probe.probe_md_signal_fallback_frames,
        ws_batch_fallback_events: batchFallbackEvents,
        md_legacy_downgrade_count: probe.probe_md_legacy_downgrade_count,
    });
    const fallbackTotal = SCORECARD_FALLBACK_COUNTER_KEYS
        .reduce((acc, key) => acc + (fallbackCounters[key] ?? 0), 0);

    const channels = new Set(wireBudgetChannels);
    for (const key of wireLatencyByChannel.keys()) channels.add(key);
    for (const key of wireBytesByChannel.keys()) channels.add(key);
    for (const key of dropsByChannel.keys()) channels.add(key);

    const rows = [];
    for (const channel of Array.from(channels).sort()) {
        const latencySample = wireLatencyByChannel.get(channel);
        const bytesSample = wireBytesByChannel.get(channel);
        const p95s = latencySample ? latencySample.quantiles[0.95] : null;
        const p99s = latencySample ? latencySample.quantiles[0.99] : null;
        const metrics = emptyScorecardMetrics();
        metrics.lat_p95_ms = Number.isFinite(p95s) ? roundNumber(p95s * 1000) : null;
        metrics.lat_p99_ms = Number.isFinite(p99s) ? roundNumber(p99s * 1000) : null;
        metrics.bytes_p95 = Number.isFinite(bytesSample && bytesSample.quantiles[0.95])
            ? roundNumber(bytesSample.quantiles[0.95])
            : null;
        metrics.bytes_p99 = Number.isFinite(bytesSample && bytesSample.quantiles[0.99])
            ? roundNumber(bytesSample.quantiles[0.99])
            : null;
        metrics.drops_total = normalizeNumber(dropsByChannel.get(channel));
        rows.push({
            key: `channel/${channel}`,
            scope: "channel",
            metrics,
        });
    }

    for (const stream of ["trade", "candle", "signal"]) {
        const backlog = normalizeNumber(probe[`probe_md_${stream}_backlog`]);
        const cap = normalizeNumber(probe[`probe_md_${stream}_backlog_cap`]);
        const metrics = emptyScorecardMetrics();
        metrics.backlog = backlog;
        metrics.cap = cap;
        metrics.backlog_utilization = roundNumber(toBacklogUtilization(backlog, cap));
        rows.push({
            key: `stream/${stream}`,
            scope: "stream",
            metrics,
        });
    }

    const wsDeliveryMetrics = emptyScorecardMetrics();
    wsDeliveryMetrics.drops_total = roundNumber(wsBackpressureDropsTotal);
    wsDeliveryMetrics.backlog = wsQueueLen;
    wsDeliveryMetrics.cap = wsQueueCapacity;
    wsDeliveryMetrics.backlog_utilization = roundNumber(toBacklogUtilization(wsQueueLen, wsQueueCapacity));
    rows.push({
        key: "subsystem/ws_delivery",
        scope: "subsystem",
        metrics: wsDeliveryMetrics,
    });

    const routerStateMetrics = emptyScorecardMetrics();
    routerStateMetrics.backlog = routerEntries;
    routerStateMetrics.cap = normalizeNumber(routerStateMaxEntries);
    routerStateMetrics.backlog_utilization = roundNumber(
        toBacklogUtilization(routerEntries, routerStateMaxEntries)
    );
    routerStateMetrics.drops_total = routerEvicted;
    rows.push({
        key: "subsystem/router_state",
        scope: "subsystem",
        metrics: routerStateMetrics,
    });

    const layerStateMetrics = emptyScorecardMetrics();
    layerStateMetrics.backlog = layerEntries;
    layerStateMetrics.cap = normalizeNumber(layerStateMaxEntries);
    layerStateMetrics.backlog_utilization = roundNumber(
        toBacklogUtilization(layerEntries, layerStateMaxEntries)
    );
    layerStateMetrics.drops_total = layerEvictions;
    rows.push({
        key: "subsystem/layer_state",
        scope: "subsystem",
        metrics: layerStateMetrics,
    });

    const fallbackMetrics = emptyScorecardMetrics();
    fallbackMetrics.fallback_total = roundNumber(fallbackTotal);
    fallbackMetrics.fallback_counters = fallbackCounters;
    rows.push({
        key: "subsystem/fallback_path",
        scope: "subsystem",
        metrics: fallbackMetrics,
    });

    return rows.sort((a, b) => a.key.localeCompare(b.key));
}

function scorecardSeverityFromMetric(metric, deltaValue, ratioValue) {
    if (metric === "drops_total" || metric === "backlog_utilization" || metric === "fallback_total") {
        return Math.max(0, normalizeNumber(deltaValue) ?? 0);
    }
    if (metric === "cap") {
        return Math.max(0, (normalizeNumber(ratioValue) ?? 1) - 1);
    }
    if (metric === "lat_p95_ms" || metric === "lat_p99_ms" || metric === "bytes_p95" || metric === "bytes_p99") {
        return Math.max(0, (normalizeNumber(ratioValue) ?? 1) - 1);
    }
    return 0;
}

function buildScorecard(currentRows, baselineRows, baselineMeta, thresholds) {
    const currentMap = new Map(currentRows.map((row) => [row.key, row]));
    const baselineMap = new Map((baselineRows || []).map((row) => [row.key, row]));
    const allKeys = new Set([...currentMap.keys(), ...baselineMap.keys()]);
    const orderedKeys = Array.from(allKeys).sort();
    const items = [];
    const topRegressions = [];

    const metricsOrder = [
        "lat_p95_ms",
        "lat_p99_ms",
        "bytes_p95",
        "bytes_p99",
        "drops_total",
        "backlog_utilization",
        "cap",
        "fallback_total",
    ];

    for (const key of orderedKeys) {
        const currentRow = currentMap.get(key);
        const baselineRow = baselineMap.get(key);
        const scope = (currentRow && currentRow.scope) || (baselineRow && baselineRow.scope) || "unknown";
        const metrics = normalizeScorecardMetrics(currentRow && currentRow.metrics);
        const baseline = normalizeScorecardMetrics(baselineRow && baselineRow.metrics);
        const hasBaseline = baselineMeta.status === "available";

        const delta = {
            lat_p95_ms: null,
            lat_p95_ratio: null,
            lat_p99_ms: null,
            lat_p99_ratio: null,
            bytes_p95: null,
            bytes_p95_ratio: null,
            bytes_p99: null,
            bytes_p99_ratio: null,
            drops_total: null,
            backlog: null,
            cap: null,
            cap_ratio: null,
            backlog_utilization: null,
            fallback_total: null,
            fallback_counters: emptyFallbackCounters(),
        };
        const regression = {
            lat_p95: false,
            lat_p99: false,
            bytes_p95: false,
            bytes_p99: false,
            drops: false,
            backlog_utilization: false,
            cap: false,
            fallback: false,
            any: false,
            reasons: [],
        };

        const compareMetric = (metric, ratioField, ratioThreshold, deltaThreshold, regressionField) => {
            const currentValue = metrics[metric];
            const baselineValue = baseline[metric];
            if (!hasBaseline || !Number.isFinite(currentValue) || !Number.isFinite(baselineValue)) {
                return;
            }
            const deltaValue = roundNumber(currentValue - baselineValue);
            delta[metric] = deltaValue;
            if (ratioField) {
                const ratioValue = baselineValue === 0
                    ? null
                    : roundNumber(currentValue / baselineValue);
                delta[ratioField] = ratioValue;
            }

            let isRegression = false;
            if (ratioField && ratioThreshold !== null) {
                const ratioValue = delta[ratioField];
                if (baselineValue === 0) {
                    isRegression = currentValue > 0;
                } else if (Number.isFinite(ratioValue)) {
                    isRegression = ratioValue > ratioThreshold;
                }
            } else if (deltaThreshold !== null) {
                isRegression = Number.isFinite(deltaValue) && deltaValue > deltaThreshold;
            }
            if (!isRegression) {
                return;
            }
            regression[regressionField] = true;
            regression.any = true;
            regression.reasons.push(metric);
            topRegressions.push({
                key,
                scope,
                metric,
                current: currentValue,
                baseline: baselineValue,
                delta: deltaValue,
                ratio: ratioField ? delta[ratioField] : null,
                severity: roundNumber(scorecardSeverityFromMetric(metric, deltaValue, ratioField ? delta[ratioField] : null)),
            });
        };

        compareMetric("lat_p95_ms", "lat_p95_ratio", thresholds.lat_p95_ratio_max, null, "lat_p95");
        compareMetric("lat_p99_ms", "lat_p99_ratio", thresholds.lat_p99_ratio_max, null, "lat_p99");
        compareMetric("bytes_p95", "bytes_p95_ratio", thresholds.bytes_p95_ratio_max, null, "bytes_p95");
        compareMetric("bytes_p99", "bytes_p99_ratio", thresholds.bytes_p99_ratio_max, null, "bytes_p99");
        compareMetric("drops_total", null, null, thresholds.drops_delta_max, "drops");
        compareMetric("backlog_utilization", null, null, thresholds.backlog_utilization_delta_max, "backlog_utilization");
        compareMetric("cap", "cap_ratio", thresholds.cap_ratio_max, null, "cap");
        compareMetric("fallback_total", null, null, thresholds.fallback_delta_max, "fallback");

        if (hasBaseline) {
            if (Number.isFinite(metrics.backlog) && Number.isFinite(baseline.backlog)) {
                delta.backlog = roundNumber(metrics.backlog - baseline.backlog);
            }
            for (const counterKey of SCORECARD_FALLBACK_COUNTER_KEYS) {
                const currentValue = normalizeNumber(metrics.fallback_counters[counterKey]);
                const baselineValue = normalizeNumber(baseline.fallback_counters[counterKey]);
                if (Number.isFinite(currentValue) && Number.isFinite(baselineValue)) {
                    delta.fallback_counters[counterKey] = roundNumber(currentValue - baselineValue);
                }
            }
        }

        const regressionScore = roundNumber(topRegressions
            .filter((entry) => entry.key === key)
            .reduce((acc, entry) => acc + (entry.severity || 0), 0));

        items.push({
            key,
            scope,
            metrics,
            baseline,
            delta,
            regression,
            regression_score: regressionScore,
        });
    }

    topRegressions.sort((a, b) =>
        (b.severity - a.severity) ||
        a.key.localeCompare(b.key) ||
        a.metric.localeCompare(b.metric)
    );

    return {
        schema_version: "iq.scorecard.v1",
        baseline: baselineMeta,
        thresholds,
        items,
        top_regressions: topRegressions.slice(0, 10),
    };
}

function resolveBaselineCandidate(runDir) {
    const explicitBaselineDir = String(process.env.BASELINE_IQ_DIR || "").trim();
    if (explicitBaselineDir) {
        return {
            source: "BASELINE_IQ_DIR",
            candidate_dir: resolve(process.cwd(), explicitBaselineDir),
            pointer_path: null,
        };
    }

    const pointerPath = join(process.cwd(), "artifacts", "iq", "latest_pass");
    const pointerStat = lstatSafe(pointerPath);
    if (!pointerStat) {
        return {
            source: "artifacts/iq/latest_pass",
            candidate_dir: null,
            pointer_path: pointerPath,
            reason: "latest_pass pointer missing",
        };
    }
    if (pointerStat.isSymbolicLink()) {
        try {
            const linkTarget = readlinkSync(pointerPath);
            return {
                source: "artifacts/iq/latest_pass",
                candidate_dir: resolve(dirname(pointerPath), linkTarget),
                pointer_path: pointerPath,
            };
        } catch {
            return {
                source: "artifacts/iq/latest_pass",
                candidate_dir: null,
                pointer_path: pointerPath,
                reason: "failed to read latest_pass symlink",
            };
        }
    }

    const raw = readText(pointerPath).trim();
    if (!raw) {
        return {
            source: "artifacts/iq/latest_pass",
            candidate_dir: null,
            pointer_path: pointerPath,
            reason: "latest_pass file empty",
        };
    }
    return {
        source: "artifacts/iq/latest_pass",
        candidate_dir: resolve(dirname(pointerPath), raw),
        pointer_path: pointerPath,
    };
}

function loadBaselineSnapshot(runDir, wireBudgetChannels, routerStateMaxEntries, layerStateMaxEntries) {
    const candidate = resolveBaselineCandidate(runDir);
    const baselineMeta = {
        status: "no_baseline",
        source: candidate.source,
        run_dir: candidate.candidate_dir,
        pointer_path: candidate.pointer_path || null,
        reason: candidate.reason || "baseline not resolved",
    };
    if (!candidate.candidate_dir) {
        return { baselineMeta, baselineRows: null };
    }

    const currentResolved = resolve(runDir);
    const baselineResolved = resolve(candidate.candidate_dir);
    if (currentResolved === baselineResolved) {
        baselineMeta.reason = "baseline points to current run";
        return { baselineMeta, baselineRows: null };
    }

    const baselineSummary = readJSON(join(candidate.candidate_dir, "summary.json"));
    if (!baselineSummary || baselineSummary.overall_pass !== true) {
        baselineMeta.reason = "baseline summary missing or non-pass";
        return { baselineMeta, baselineRows: null };
    }

    const baselineSmoke = readJSON(join(candidate.candidate_dir, "logs", "playwright-smoke.json"));
    const baselineMetricsText = readText(join(candidate.candidate_dir, "logs", "server.metrics.prom"));
    if (!baselineSmoke || !baselineMetricsText.trim()) {
        baselineMeta.reason = "baseline smoke/metrics missing";
        return { baselineMeta, baselineRows: null };
    }

    baselineMeta.status = "available";
    baselineMeta.reason = null;
    const baselineRows = buildScorecardSnapshot({
        metricsText: baselineMetricsText,
        smoke: baselineSmoke,
        wireBudgetChannels,
        routerStateMaxEntries,
        layerStateMaxEntries,
    });
    return { baselineMeta, baselineRows };
}

const runDir = process.argv[2];
if (!runDir) {
    console.error("usage: node scripts/iq/analyze_iq_run.mjs <run_dir>");
    process.exit(2);
}

const logsDir = join(runDir, "logs");
const smokePath = join(logsDir, "playwright-smoke.json");
const legacyNegativePath = join(logsDir, "legacy-negative.json");
const consolePath = join(logsDir, "playwright-console.log");
const metricsPath = join(logsDir, "server.metrics.prom");
const composePath = join(logsDir, "compose.all.log");
const reportPath = join(runDir, "report.md");
const summaryPath = join(runDir, "summary.json");
const scorecardPath = join(runDir, "scorecard.json");

const smoke = readJSON(smokePath) || { steps: [], runtime_probe: {} };
const legacyNegative = readJSON(legacyNegativePath);
const consoleText = readText(consolePath);
const metricsText = readText(metricsPath);
const composeText = readText(composePath);

const consoleLines = consoleText.split(/\r?\n/).filter(Boolean);
const composeLines = composeText.split(/\r?\n/).filter(Boolean);
const probe = smoke.runtime_probe || {};
const statsProbe = smoke.stats_probe || {};
const iqProfile = resolveIQProfile(process.env);
const profileValue = (name, fallback = "") =>
    iqProfile.effectiveValues[name] ?? (process.env[name] ?? fallback);
const strictProfile = envBoolValue(profileValue("IQ_STRICT", "0"), false);
const fallbackStrict = envBoolValue(profileValue("IQ_FALLBACK_STRICT", "0"), false);
const legacyStrict = envBoolValue(profileValue("IQ_LEGACY_STRICT", "0"), false);

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
const legacyNegativePass = Boolean(legacyNegative && legacyNegative.overall_pass === true);
const legacyNegativeCounters = legacyNegative && legacyNegative.counters ? legacyNegative.counters : {};
const legacyNegativeRejectedDelta = Number(
    legacyNegativeCounters.ws_legacy_requests_rejected_delta ?? Number.NaN
);
const legacyNegativeSubjectInvalidDelta = Number(
    legacyNegativeCounters.ws_query_rejected_subject_invalid_delta ?? Number.NaN
);
const legacyNegativeDowngradeProbe = Number(
    legacyNegativeCounters.probe_md_legacy_downgrade_count ?? Number.NaN
);
const allowBatchedFallback = envBoolValue(profileValue("IQ_ALLOW_BATCHED_FALLBACK", "0"), false);
const batchFallbackRemovalRuns = Number.parseInt(process.env.IQ_BATCHED_FALLBACK_ZERO_RUNS || "5", 10) || 5;
const allowStatsFallback = envBoolValue(profileValue("IQ_ALLOW_STATS_FALLBACK", "0"), false);
const requireStatsCanonical = envBoolValue(profileValue("IQ_REQUIRE_STATS_CANONICAL", "0"), false);
const statsFallbackRemovalRuns = Number.parseInt(process.env.IQ_STATS_FALLBACK_ZERO_RUNS || "5", 10) || 5;
const allowUnexpectedSkips = envBoolValue(profileValue("IQ_ALLOW_UNEXPECTED_SKIPS", "0"), false);
const statsFallbackStreak = statsFallbackZeroStreak(runDir);
const wireBudgetChannels = String(profileValue("IQ_WIRE_BUDGET_CHANNELS", "trade,book_snapshot,stats,candle"))
    .split(",")
    .map((v) => v.trim())
    .filter(Boolean);
const wireP95BudgetMs = Number.parseFloat(profileValue("IQ_WIRE_P95_BUDGET_MS", "2000"));
const wireP99BudgetMs = Number.parseFloat(profileValue("IQ_WIRE_P99_BUDGET_MS", "5000"));
const wireP95BudgetMsByChannel = parseChannelBudgetMap(process.env.IQ_WIRE_P95_BUDGET_MS_BY_CHANNEL);
const wireP99BudgetMsByChannel = parseChannelBudgetMap(process.env.IQ_WIRE_P99_BUDGET_MS_BY_CHANNEL);
const wireBytesP95Budget = Number.parseFloat(profileValue("IQ_WIRE_BYTES_P95_BUDGET", "65536"));
const wireBytesP99Budget = Number.parseFloat(profileValue("IQ_WIRE_BYTES_P99_BUDGET", "131072"));
const wireBytesP95BudgetByChannel = parseChannelBudgetMap(process.env.IQ_WIRE_BYTES_P95_BUDGET_BY_CHANNEL);
const wireBytesP99BudgetByChannel = parseChannelBudgetMap(process.env.IQ_WIRE_BYTES_P99_BUDGET_BY_CHANNEL);
const wsQueueUtilizationMax = Number.parseFloat(process.env.IQ_WS_QUEUE_UTILIZATION_MAX || "0.85");
const wsLagMaxMs = Number.parseFloat(process.env.IQ_WS_LAG_MAX_MS || "2000000");
const subscribeAckMin = Number.parseInt(process.env.IQ_SUBSCRIBE_ACK_MIN || "1", 10);
const wsBackpressureDropsMax = Number.parseFloat(process.env.IQ_WS_BACKPRESSURE_DROPS_MAX || "0");
const logSpamMaxPerSignature = Number.parseInt(process.env.IQ_LOG_SPAM_MAX_PER_SIGNATURE || "20", 10);
const routerStateMaxEntries = Number.parseInt(profileValue("IQ_ROUTER_STREAM_STATE_MAX", "2048"), 10) || 2048;
const layerStateMaxEntries = Number.parseInt(profileValue("IQ_LAYER_STREAM_STATE_MAX", String(routerStateMaxEntries)), 10) || routerStateMaxEntries;
const profileReplicaCount = Number.parseInt(profileValue("PROCESSOR_REPLICAS", "2"), 10) || 2;

const mdParseP95BudgetUs = parseOptionalNumber(process.env.IQ_MD_PARSE_P95_BUDGET_US);
const mdParseP99BudgetUs = parseOptionalNumber(process.env.IQ_MD_PARSE_P99_BUDGET_US);
const mdApplyP95BudgetUs = parseOptionalNumber(process.env.IQ_MD_APPLY_P95_BUDGET_US);
const mdApplyP99BudgetUs = parseOptionalNumber(process.env.IQ_MD_APPLY_P99_BUDGET_US);
const mdBatchDecodeP95BudgetUs = parseOptionalNumber(process.env.IQ_MD_BATCH_DECODE_P95_BUDGET_US);
const mdBatchDecodeP99BudgetUs = parseOptionalNumber(process.env.IQ_MD_BATCH_DECODE_P99_BUDGET_US);
const mdAllocEstimateFrameBudget = parseOptionalNumber(process.env.IQ_MD_ALLOC_ESTIMATE_FRAME_BUDGET);
const mdAllocEstimateTotalBudget = parseOptionalNumber(process.env.IQ_MD_ALLOC_ESTIMATE_TOTAL_BUDGET);

const widgetBudgetNames = String(process.env.IQ_WIDGET_BUDGETS || "stats,dom,tape,evidence,signal")
    .split(",")
    .map((v) => v.trim().toLowerCase())
    .filter(Boolean);
const widgetRenderP95BudgetUs = parseOptionalNumber(process.env.IQ_WIDGET_RENDER_P95_BUDGET_US);
const widgetRenderP99BudgetUs = parseOptionalNumber(process.env.IQ_WIDGET_RENDER_P99_BUDGET_US);
const widgetRenderP95BudgetUsByWidget = parseChannelBudgetMap(process.env.IQ_WIDGET_RENDER_P95_BUDGET_US_BY_WIDGET);
const widgetRenderP99BudgetUsByWidget = parseChannelBudgetMap(process.env.IQ_WIDGET_RENDER_P99_BUDGET_US_BY_WIDGET);
const widgetMaxEntriesDefault = parseOptionalInt(process.env.IQ_WIDGET_MAX_ENTRIES);
const widgetMaxEntriesByWidget = parseChannelBudgetMap(process.env.IQ_WIDGET_MAX_ENTRIES_BY_WIDGET);

const wireLatencyByChannel = histogramQuantilesByLabel(
    metricsText,
    "ws_publish_to_deliver_latency_seconds",
    "channel",
    [0.95, 0.99]
);
const wireBytesByChannel = histogramQuantilesByLabel(
    metricsText,
    "ws_wire_bytes",
    "channel",
    [0.95, 0.99]
);
const routerStreamEntries = metricSamples(metricsText, "router_stream_state_entries")[0]?.value ?? 0;
const routerStreamActive = metricSamples(metricsText, "router_stream_state_active_total")[0]?.value ?? 0;
const routerStreamEvicted = metricSamples(metricsText, "delivery_router_stream_state_evicted_total")[0]?.value ?? 0;
const wsQueueLen = metricSamples(metricsText, "ws_queue_len")[0]?.value ?? 0;
const wsQueueCapacity = metricSamples(metricsText, "ws_queue_capacity")[0]?.value ?? 0;
const wsQueueUtilization = wsQueueCapacity > 0 ? wsQueueLen / wsQueueCapacity : 0;
const wsLagSamples = metricSamples(metricsText, "ws_lag_ms");
const wsLagMaxObserved = wsLagSamples.reduce((max, s) => Math.max(max, Number(s.value) || 0), 0);
const subscribeAckCount = Number(probe.probe_md_subscribe_ack_count ?? -1);
const wsBackpressureDropsTotal = metricSamples(metricsText, "ws_drops_total")
    .filter((s) => BACKPRESSURE_DROP_REASONS.has(s.labels.reason || ""))
    .reduce((acc, s) => acc + (Number(s.value) || 0), 0);
const spamSignatureCounts = new Map();
for (const line of composeLines) {
    if (!line.includes("sampled")) {
        continue;
    }
    const normalized = line
        .replace(/^[^|]+\|\s*/, "")
        .replace(/\"time\":\"[^\"]+\"/g, "\"time\":\"<ts>\"")
        .replace(/[0-9a-f]{12,}/gi, "<hex>")
        .replace(/[0-9]+/g, "<n>")
        .trim();
    if (!normalized) continue;
    spamSignatureCounts.set(normalized, (spamSignatureCounts.get(normalized) || 0) + 1);
}
const spamEntries = Array.from(spamSignatureCounts.entries())
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
const spamMaxCount = spamEntries.length > 0 ? spamEntries[0][1] : 0;
const spamTop = spamEntries.slice(0, 3).map(([sig, count]) => `${count}x:${sig.slice(0, 120)}`);
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

const mdParseP95 = Number(probe.probe_md_parse_time_p95_us ?? -1);
const mdParseP99 = Number(probe.probe_md_parse_time_p99_us ?? -1);
const mdApplyP95 = Number(probe.probe_md_apply_time_p95_us ?? -1);
const mdApplyP99 = Number(probe.probe_md_apply_time_p99_us ?? -1);
const mdBatchDecodeP95 = Number(probe.probe_md_batched_decode_time_p95_us ?? -1);
const mdBatchDecodeP99 = Number(probe.probe_md_batched_decode_time_p99_us ?? -1);
const mdAllocEstimateTotal = Number(probe.probe_md_alloc_estimate_total ?? -1);
const mdAllocEstimateFrame = Number(probe.probe_md_alloc_estimate_frame ?? -1);
const mdTradeBacklog = Number(probe.probe_md_trade_backlog ?? -1);
const mdTradeBacklogCap = Number(probe.probe_md_trade_backlog_cap ?? -1);
const mdCandleBacklog = Number(probe.probe_md_candle_backlog ?? -1);
const mdCandleBacklogCap = Number(probe.probe_md_candle_backlog_cap ?? -1);
const mdSignalBacklog = Number(probe.probe_md_signal_backlog ?? -1);
const mdSignalBacklogCap = Number(probe.probe_md_signal_backlog_cap ?? -1);
const layerStreamEntries = Number(probe.probe_layer_stream_entries ?? -1);
const layerStreamEvictions = Number(probe.probe_layer_stream_evictions ?? -1);
const scorecardThresholds = {
    lat_p95_ratio_max: parseScorecardRatioThreshold("IQ_SCORECARD_LAT_P95_RATIO_MAX", 1.10),
    lat_p99_ratio_max: parseScorecardRatioThreshold("IQ_SCORECARD_LAT_P99_RATIO_MAX", 1.10),
    bytes_p95_ratio_max: parseScorecardRatioThreshold("IQ_SCORECARD_BYTES_P95_RATIO_MAX", 1.10),
    bytes_p99_ratio_max: parseScorecardRatioThreshold("IQ_SCORECARD_BYTES_P99_RATIO_MAX", 1.10),
    drops_delta_max: parseScorecardDeltaThreshold("IQ_SCORECARD_DROPS_DELTA_MAX", 0),
    backlog_utilization_delta_max: parseScorecardDeltaThreshold("IQ_SCORECARD_BACKLOG_UTILIZATION_DELTA_MAX", 0.05),
    cap_ratio_max: parseScorecardRatioThreshold("IQ_SCORECARD_CAP_RATIO_MAX", 1.10),
    fallback_delta_max: parseScorecardDeltaThreshold("IQ_SCORECARD_FALLBACK_DELTA_MAX", 0),
};
const currentScorecardRows = buildScorecardSnapshot({
    metricsText,
    smoke,
    wireBudgetChannels,
    routerStateMaxEntries,
    layerStateMaxEntries,
});
const { baselineMeta, baselineRows } = loadBaselineSnapshot(
    runDir,
    wireBudgetChannels,
    routerStateMaxEntries,
    layerStateMaxEntries
);
const scorecard = buildScorecard(
    currentScorecardRows,
    baselineRows,
    baselineMeta,
    scorecardThresholds
);
const scorecardRegressionItems = scorecard.items.filter((item) => item.regression.any).length;

const widgetProbeNames = {
    stats: "stats",
    dom: "dom",
    tape: "tape",
    evidence: "evidence",
    signal: "signal",
};

const checks = [];

function addCheck(id, title, ok, evidence, excerptPatterns = []) {
    checks.push({ id, title, ok, evidence, excerptPatterns });
}

let boundednessValidation;
try {
    boundednessValidation = validateBoundednessMatrix({
        repoRoot: process.cwd(),
        matrixPath: "docs/contracts/boundedness-matrix.md",
        enforceFullCatalog: true,
    });
} catch (err) {
    boundednessValidation = {
        ok: false,
        checkedEntries: 0,
        checkedAnchors: 0,
        errors: [`validator exception: ${err instanceof Error ? err.message : String(err)}`],
    };
}
const boundednessErrorPreview = boundednessValidation.errors.slice(0, 3).join("; ");
const boundednessEvidence = boundednessValidation.ok
    ? `entries=${boundednessValidation.checkedEntries} anchors=${boundednessValidation.checkedAnchors} drift=0`
    : `entries=${boundednessValidation.checkedEntries} anchors=${boundednessValidation.checkedAnchors} errors=${boundednessValidation.errors.length} preview=${boundednessErrorPreview}${boundednessValidation.errors.length > 3 ? "; ..." : ""}`;
addCheck(
    "boundedness_matrix_valid",
    "boundedness matrix valid",
    boundednessValidation.ok,
    boundednessEvidence,
    ["boundedness_matrix_valid", "router_stream_state_entries", "ws_queue_capacity"]
);

addCheck(
    "profile_contract_guardrail",
    "ci-strict profile guardrail",
    iqProfile.valid,
    `requested=${iqProfile.requestedProfile || "<unset>"} effective=${iqProfile.effectiveProfileName} strict=${strictProfile} fallback_strict=${fallbackStrict} legacy_strict=${legacyStrict} replica_count=${profileReplicaCount} violations=${iqProfile.validationErrors.join(";") || "none"}`,
    ["IQ_PROFILE", "IQ_PROFILE_VALIDATION", "IQ_REQUIRE_STATS_CANONICAL", "IQ_ALLOW_STATS_FALLBACK", "IQ_ALLOW_BATCHED_FALLBACK", "IQ_ALLOW_UNEXPECTED_SKIPS"]
);

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
    requireStatsCanonical
        ? (canonicalStatsFrames > 0 && widgetStatsParseTotal > 0)
        : (canonicalStatsFrames >= 0 && widgetStatsParseTotal >= 0),
    `canonical_stats_frames=${canonicalStatsFrames} widget_stats_parse_total=${widgetStatsParseTotal} require_stats_canonical=${requireStatsCanonical}`,
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
    "legacy_route_never_accepted",
    "legacy route never accepted",
    legacyRouteAcceptedTotal === 0,
    `ws_legacy_requests_total accepted=${legacyRouteAcceptedTotal} rejected=${legacyRouteRejectedTotal} total=${legacyRouteTotal}`,
    ["ws_legacy_requests_total", "/ws/marketdata", "legacy route"]
);

addCheck(
    "legacy_negative_probe",
    "legacy negative probe",
    legacyNegativePass &&
        Number.isFinite(legacyNegativeRejectedDelta) &&
        legacyNegativeRejectedDelta >= 1 &&
        Number.isFinite(legacyNegativeSubjectInvalidDelta) &&
        legacyNegativeSubjectInvalidDelta >= 2 &&
        Number.isFinite(legacyNegativeDowngradeProbe) &&
        legacyNegativeDowngradeProbe === 0,
    `legacy_negative_json=${legacyNegative ? "present" : "missing"} pass=${legacyNegativePass} rejected_delta=${fmt(legacyNegativeRejectedDelta)} subject_invalid_delta=${fmt(legacyNegativeSubjectInvalidDelta)} probe_md_legacy_downgrade_count=${fmt(legacyNegativeDowngradeProbe)}`,
    ["legacy-negative", "ws_legacy_requests_total", "subject_invalid", "probe_md_legacy_downgrade_count"]
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
const wireP95BudgetFor = (channel) => wireP95BudgetMsByChannel.get(channel) ?? wireP95BudgetMs;
const wireP99BudgetFor = (channel) => wireP99BudgetMsByChannel.get(channel) ?? wireP99BudgetMs;
for (const channel of wireBudgetChannels) {
    const sample = wireLatencyByChannel.get(channel);
    if (!sample || !(sample.count > 0)) {
        continue;
    }
    const p95s = sample.quantiles[0.95];
    const p99s = sample.quantiles[0.99];
    const p95ms = Number.isFinite(p95s) ? p95s * 1000 : null;
    const p99ms = Number.isFinite(p99s) ? p99s * 1000 : null;
    const p95Budget = wireP95BudgetFor(channel);
    const p99Budget = wireP99BudgetFor(channel);
    wireObserved.push(`${channel}:count=${sample.count},p95_ms=${fmt(p95ms)},p99_ms=${fmt(p99ms)},budget_ms(p95<=${p95Budget},p99<=${p99Budget})`);
    if ((p95ms !== null && p95ms > p95Budget) || (p99ms !== null && p99ms > p99Budget)) {
        wireViolations.push(channel);
    }
}
addCheck(
    "wire_budget_p95_p99",
    "wire budgets p95/p99",
    wireObserved.length > 0 && wireViolations.length === 0,
    `default_threshold_ms(p95<=${wireP95BudgetMs},p99<=${wireP99BudgetMs}) observed=${wireObserved.join(";") || "none"} violations=${wireViolations.join(",") || "none"}`,
    ["ws_publish_to_deliver_latency_seconds_bucket", "ws_publish_to_deliver_latency_seconds_count"]
);

const wireBytesObserved = [];
const wireBytesViolations = [];
const wireBytesP95BudgetFor = (channel) => wireBytesP95BudgetByChannel.get(channel) ?? wireBytesP95Budget;
const wireBytesP99BudgetFor = (channel) => wireBytesP99BudgetByChannel.get(channel) ?? wireBytesP99Budget;
for (const channel of wireBudgetChannels) {
    const sample = wireBytesByChannel.get(channel);
    if (!sample || !(sample.count > 0)) {
        continue;
    }
    const p95 = sample.quantiles[0.95];
    const p99 = sample.quantiles[0.99];
    const p95Budget = wireBytesP95BudgetFor(channel);
    const p99Budget = wireBytesP99BudgetFor(channel);
    wireBytesObserved.push(`${channel}:count=${sample.count},p95_bytes=${fmt(p95)},p99_bytes=${fmt(p99)},budget_bytes(p95<=${p95Budget},p99<=${p99Budget})`);
    if ((p95 !== null && p95 > p95Budget) || (p99 !== null && p99 > p99Budget)) {
        wireBytesViolations.push(channel);
    }
}
addCheck(
    "wire_bytes_budget_p95_p99",
    "wire bytes budgets p95/p99",
    wireBytesObserved.length > 0 && wireBytesViolations.length === 0,
    `default_threshold_bytes(p95<=${wireBytesP95Budget},p99<=${wireBytesP99Budget}) observed=${wireBytesObserved.join(";") || "none"} violations=${wireBytesViolations.join(",") || "none"}`,
    ["ws_wire_bytes_bucket", "ws_wire_bytes_count"]
);

const mdPerfBudgetRows = [
    {
        id: "parse",
        p95: mdParseP95,
        p99: mdParseP99,
        b95: mdParseP95BudgetUs,
        b99: mdParseP99BudgetUs,
    },
    {
        id: "apply",
        p95: mdApplyP95,
        p99: mdApplyP99,
        b95: mdApplyP95BudgetUs,
        b99: mdApplyP99BudgetUs,
    },
    {
        id: "batch_decode",
        p95: mdBatchDecodeP95,
        p99: mdBatchDecodeP99,
        b95: mdBatchDecodeP95BudgetUs,
        b99: mdBatchDecodeP99BudgetUs,
    },
];
const mdPerfBudgetViolations = [];
const mdPerfObserved = [];
for (const row of mdPerfBudgetRows) {
    mdPerfObserved.push(`${row.id}:p95_us=${fmt(row.p95)} p99_us=${fmt(row.p99)} budget_us(p95<=${fmt(row.b95)},p99<=${fmt(row.b99)})`);
    if (!isNonNegativeNumber(row.p95) || !isNonNegativeNumber(row.p99)) {
        mdPerfBudgetViolations.push(`${row.id}:missing_probe`);
        continue;
    }
    if (row.b95 !== null && row.p95 > row.b95) {
        mdPerfBudgetViolations.push(`${row.id}:p95>${row.b95}`);
    }
    if (row.b99 !== null && row.p99 > row.b99) {
        mdPerfBudgetViolations.push(`${row.id}:p99>${row.b99}`);
    }
}
addCheck(
    "md_perf_budget_p95_p99",
    "md perf budgets p95/p99",
    mdPerfBudgetViolations.length === 0,
    `observed=${mdPerfObserved.join(";")} violations=${mdPerfBudgetViolations.join(",") || "none"}`,
    ["probe_md_parse_time_p95_us", "probe_md_apply_time_p95_us", "probe_md_batched_decode_time_p95_us"]
);

const allocBudgetViolations = [];
if (!isNonNegativeNumber(mdAllocEstimateFrame)) {
    allocBudgetViolations.push("alloc_estimate_frame:missing_probe");
}
if (!isNonNegativeNumber(mdAllocEstimateTotal)) {
    allocBudgetViolations.push("alloc_estimate_total:missing_probe");
}
if (mdAllocEstimateFrameBudget !== null && isNonNegativeNumber(mdAllocEstimateFrame) && mdAllocEstimateFrame > mdAllocEstimateFrameBudget) {
    allocBudgetViolations.push(`alloc_estimate_frame>${mdAllocEstimateFrameBudget}`);
}
if (mdAllocEstimateTotalBudget !== null && isNonNegativeNumber(mdAllocEstimateTotal) && mdAllocEstimateTotal > mdAllocEstimateTotalBudget) {
    allocBudgetViolations.push(`alloc_estimate_total>${mdAllocEstimateTotalBudget}`);
}
addCheck(
    "alloc_estimate_budget",
    "alloc estimate budget",
    allocBudgetViolations.length === 0,
    `alloc_estimate_frame=${mdAllocEstimateFrame} alloc_estimate_total=${mdAllocEstimateTotal} budget(frame<=${fmt(mdAllocEstimateFrameBudget)},total<=${fmt(mdAllocEstimateTotalBudget)}) violations=${allocBudgetViolations.join(",") || "none"}`,
    ["alloc_estimate_frame", "alloc_estimate_total"]
);

const backlogRows = [
    { id: "trade", backlog: mdTradeBacklog, cap: mdTradeBacklogCap },
    { id: "candle", backlog: mdCandleBacklog, cap: mdCandleBacklogCap },
    { id: "signal", backlog: mdSignalBacklog, cap: mdSignalBacklogCap },
];
const backlogViolations = [];
const backlogObserved = [];
for (const row of backlogRows) {
    backlogObserved.push(`${row.id}:entries=${row.backlog} cap=${row.cap}`);
    if (!isNonNegativeNumber(row.backlog) || !isNonNegativeNumber(row.cap)) {
        backlogViolations.push(`${row.id}:missing_probe`);
        continue;
    }
    if (row.backlog > row.cap) {
        backlogViolations.push(`${row.id}:entries_gt_cap`);
    }
}
addCheck(
    "md_backlog_bounded",
    "md backlog bounded",
    backlogViolations.length === 0,
    `observed=${backlogObserved.join(";")} violations=${backlogViolations.join(",") || "none"}`,
    ["probe_md_trade_backlog", "probe_md_candle_backlog", "probe_md_signal_backlog"]
);

addCheck(
    "layer_stream_bounded",
    "layer stream bounded",
    isNonNegativeNumber(layerStreamEntries) &&
        isNonNegativeNumber(layerStreamEvictions) &&
        layerStreamEntries <= layerStateMaxEntries,
    `entries=${layerStreamEntries} evicted_total=${layerStreamEvictions} threshold_entries<=${layerStateMaxEntries}`,
    ["probe_layer_stream_entries", "probe_layer_stream_evictions"]
);

addCheck(
    "queue_utilization",
    "queue utilization bounded",
    wsQueueLen >= 0 && wsQueueCapacity >= 0 && wsQueueUtilization <= wsQueueUtilizationMax,
    `queue_len=${wsQueueLen} queue_capacity=${wsQueueCapacity} utilization=${wsQueueUtilization.toFixed(4)} max=${wsQueueUtilizationMax}`,
    ["ws_queue_len", "ws_queue_capacity"]
);

addCheck(
    "js_ack_lag",
    "js ack/lag budget",
    subscribeAckCount >= subscribeAckMin && wsLagMaxObserved <= wsLagMaxMs,
    `subscribe_ack_count=${subscribeAckCount} min_ack=${subscribeAckMin} ws_lag_max_ms=${wsLagMaxObserved} lag_budget_ms<=${wsLagMaxMs}`,
    ["ack_recv op=subscribe", "ws_lag_ms"]
);

addCheck(
    "backpressure_drop_budget",
    "drops/backpressure budget",
    wsBackpressureDropsTotal <= wsBackpressureDropsMax,
    `ws_backpressure_drops_total=${wsBackpressureDropsTotal} budget<=${wsBackpressureDropsMax}`,
    ["ws_drops_total", "queue_full", "priority_drop"]
);

addCheck(
    "log_spam",
    "no spam logs",
    spamMaxCount <= logSpamMaxPerSignature,
    `sampled_log_signature_max=${spamMaxCount} threshold<=${logSpamMaxPerSignature} top=${spamTop.join(";") || "none"}`,
    ["sampled"]
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

const allocFrame = mdAllocEstimateFrame;
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

const widgetPerfRows = [];

function addWidgetBudgetChecks(idPrefix, title, widgetName, probePrefix) {
    const renderP95 = Number(probe[`probe_widget_${probePrefix}_render_p95_us`] ?? -1);
    const renderP99 = Number(probe[`probe_widget_${probePrefix}_render_p99_us`] ?? -1);
    const renderBudgetProbe = Number(probe[`probe_widget_${probePrefix}_render_budget_us`] ?? -1);
    const renderOverBudget = Number(probe[`probe_widget_${probePrefix}_render_over_budget`] ?? -1);
    const entries = Number(probe[`probe_widget_${probePrefix}_entries`] ?? -1);
    const maxEntries = Number(probe[`probe_widget_${probePrefix}_max_entries`] ?? -1);
    const evictedTotal = Number(probe[`probe_widget_${probePrefix}_evicted_total`] ?? -1);
    const budgetEnabled = widgetBudgetNames.includes(widgetName);
    const budgetP95FromMap = widgetRenderP95BudgetUsByWidget.get(widgetName);
    const budgetP99FromMap = widgetRenderP99BudgetUsByWidget.get(widgetName);
    const renderP95Budget = budgetEnabled
        ? (budgetP95FromMap ?? widgetRenderP95BudgetUs ?? (renderBudgetProbe > 0 ? renderBudgetProbe : null))
        : null;
    const renderP99Budget = budgetEnabled
        ? (budgetP99FromMap ?? widgetRenderP99BudgetUs ?? (renderBudgetProbe > 0 ? renderBudgetProbe * 2 : null))
        : null;
    const maxEntriesBudgetRaw = budgetEnabled
        ? (widgetMaxEntriesByWidget.get(widgetName) ?? widgetMaxEntriesDefault)
        : null;
    const maxEntriesBudget = maxEntriesBudgetRaw === null ? null : Math.floor(maxEntriesBudgetRaw);

    addCheck(
        `${idPrefix}_render_budget`,
        `${title} render budget`,
        isNonNegativeNumber(renderP95) &&
            isNonNegativeNumber(renderP99) &&
            renderBudgetProbe > 0 &&
            renderOverBudget === 0 &&
            (renderP95Budget === null || renderP95 <= renderP95Budget) &&
            (renderP99Budget === null || renderP99 <= renderP99Budget),
        `render_p95_us=${renderP95} render_p99_us=${renderP99} runtime_budget_us=${renderBudgetProbe} env_budget_us(p95<=${fmt(renderP95Budget)},p99<=${fmt(renderP99Budget)}) render_over_budget=${renderOverBudget}`,
        [`probe_widget_${probePrefix}_render_p95_us`, `probe_widget_${probePrefix}_render_p99_us`, `probe_widget_${probePrefix}_render_budget_us`]
    );

    addCheck(
        `${idPrefix}_entries_bounded`,
        `${title} entries bounded`,
        isNonNegativeNumber(entries) &&
            isNonNegativeNumber(maxEntries) &&
            isNonNegativeNumber(evictedTotal) &&
            entries <= maxEntries &&
            (maxEntriesBudget === null || maxEntries <= maxEntriesBudget),
        `entries=${entries} max_entries=${maxEntries} evicted_total=${evictedTotal} env_max_entries<=${fmt(maxEntriesBudget)}`,
        [`probe_widget_${probePrefix}_entries`, `probe_widget_${probePrefix}_max_entries`, `probe_widget_${probePrefix}_evicted_total`]
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

    widgetPerfRows.push({
        id: widgetName,
        title,
        renderP95,
        renderP99,
        renderBudgetProbe,
        renderP95Budget,
        renderP99Budget,
        entries,
        maxEntries,
        evictedTotal,
    });
}

addWidgetBudgetChecks("stats_widget", "stats widget", "stats", widgetProbeNames.stats);
addWidgetBudgetChecks("dom_widget", "dom widget", "dom", widgetProbeNames.dom);
addWidgetBudgetChecks("tape_widget", "tape widget", "tape", widgetProbeNames.tape);
addWidgetBudgetChecks("evidence_widget", "evidence widget", "evidence", widgetProbeNames.evidence);
addWidgetBudgetChecks("signal_widget", "signal widget", "signal", widgetProbeNames.signal);

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

const widgetHotPaths = widgetPerfRows
    .filter((row) => isNonNegativeNumber(row.renderP99))
    .sort((a, b) => b.renderP99 - a.renderP99 || b.renderP95 - a.renderP95);
const top3Widgets = widgetHotPaths.slice(0, 3);

const perfHotPathRows = [
    { id: "md.parse", p95: mdParseP95, p99: mdParseP99 },
    { id: "md.apply", p95: mdApplyP95, p99: mdApplyP99 },
    { id: "md.batch_decode", p95: mdBatchDecodeP95, p99: mdBatchDecodeP99 },
    ...widgetPerfRows.map((row) => ({ id: `widget.${row.id}.render`, p95: row.renderP95, p99: row.renderP99 })),
]
    .filter((row) => isNonNegativeNumber(row.p95) && isNonNegativeNumber(row.p99))
    .sort((a, b) => b.p99 - a.p99 || b.p95 - a.p95);
const top3HotPaths = perfHotPathRows.slice(0, 3);

const memoryPressureRows = [
    { id: "alloc_estimate_frame", value: mdAllocEstimateFrame, unit: "alloc_estimate/frame" },
    { id: "alloc_estimate_total", value: mdAllocEstimateTotal, unit: "alloc_estimate_total" },
    { id: "backlog.trade", value: mdTradeBacklog, unit: "entries" },
    { id: "backlog.candle", value: mdCandleBacklog, unit: "entries" },
    { id: "backlog.signal", value: mdSignalBacklog, unit: "entries" },
    ...widgetPerfRows.map((row) => ({ id: `widget.${row.id}.entries`, value: row.entries, unit: "entries" })),
    ...widgetPerfRows.map((row) => ({ id: `widget.${row.id}.evicted_total`, value: row.evictedTotal, unit: "evicted_total" })),
]
    .filter((row) => isNonNegativeNumber(row.value))
    .sort((a, b) => b.value - a.value);
const top3MemoryPressure = memoryPressureRows.slice(0, 3);
const effectiveProfileEntries = Object.keys(iqProfile.effectiveValues)
    .sort()
    .map((key) => ({
        key,
        effectiveValue: String(iqProfile.effectiveValues[key] ?? "<unset>"),
        defaultValue: String(iqProfile.defaults[key] ?? "<unset>"),
    }));
const effectiveProfileDiffMap = new Map(
    iqProfile.diffs.map((entry) => [entry.key, entry])
);
const checkByID = new Map(checks.map((check) => [check.id, check]));
const criticalInvariants = [
    {
        id: "p1_ts_server_present",
        label: "ts_server present",
        check_id: "ts_server_present",
        enabled: true,
    },
    {
        id: "p2_seq_monotonic",
        label: "seq monotonic",
        check_id: "seq_monotonic",
        enabled: true,
    },
    {
        id: "p3_prev_seq_chaining",
        label: "prev_seq chaining",
        check_id: "prev_seq_chaining",
        enabled: true,
    },
    {
        id: "p4_canonical_subjects",
        label: "canonical evidence/signal subjects",
        check_id: "canonical_subjects",
        enabled: legacyStrict,
    },
    {
        id: "p5_legacy_route_never_accepted",
        label: "legacy route never accepted",
        check_id: "legacy_route_never_accepted",
        enabled: legacyStrict,
    },
    {
        id: "p6_no_compat_fallback_path",
        label: "no fallback/compat path hit",
        check_id: "compat_fallback_zero",
        enabled: fallbackStrict && !allowBatchedFallback && !allowStatsFallback && !allowUnexpectedSkips,
    },
    {
        id: "p7_wire_latency_budget_p95_p99",
        label: "wire budgets p95/p99",
        check_id: "wire_budget_p95_p99",
        enabled: wireBudgetChannels.length > 0 &&
            Number.isFinite(wireP95BudgetMs) && wireP95BudgetMs > 0 &&
            Number.isFinite(wireP99BudgetMs) && wireP99BudgetMs > 0,
    },
    {
        id: "p8_wire_bytes_budget_p95_p99",
        label: "wire bytes budgets p95/p99",
        check_id: "wire_bytes_budget_p95_p99",
        enabled: wireBudgetChannels.length > 0 &&
            Number.isFinite(wireBytesP95Budget) && wireBytesP95Budget > 0 &&
            Number.isFinite(wireBytesP99Budget) && wireBytesP99Budget > 0,
    },
    {
        id: "p9_backpressure_drop_budget",
        label: "drops/backpressure budget",
        check_id: "backpressure_drop_budget",
        enabled: true,
    },
    {
        id: "p10_md_backlog_bounded",
        label: "md backlog bounded",
        check_id: "md_backlog_bounded",
        enabled: true,
    },
].map((entry) => ({
    ...entry,
    runtime_ok: Boolean(checkByID.get(entry.check_id)?.ok),
}));
const criticalEnabled = criticalInvariants
    .filter((entry) => entry.enabled)
    .map((entry) => entry.id);
const criticalDisabled = criticalInvariants
    .filter((entry) => !entry.enabled)
    .map((entry) => entry.id);

const generatedAtISO = new Date().toISOString();
const runTimestamp = parseRunTimestampFromDir(runDir);
const commitHash = resolveCommitHash();
const effectiveProfileProofPath = join(runDir, "effective-profile.json");
const effectiveProfileProof = {
    schema_version: "iq.effective_profile.v1",
    profile_name: iqProfile.effectiveProfileName,
    requested_profile: iqProfile.requestedProfile || "<unset>",
    run: {
        run_dir: runDir,
        run_id: runTimestamp.run_id,
        started_at_utc: runTimestamp.started_at_utc,
        generated_at_utc: generatedAtISO,
    },
    commit: {
        hash: commitHash,
    },
    replica_count: profileReplicaCount,
    budgets: {
        wire_latency_ms: {
            channels: wireBudgetChannels,
            p95: wireP95BudgetMs,
            p99: wireP99BudgetMs,
        },
        wire_bytes: {
            channels: wireBudgetChannels,
            p95: wireBytesP95Budget,
            p99: wireBytesP99Budget,
        },
    },
    caps: {
        router_stream_state_max: {
            value: routerStateMaxEntries,
            boundedness_matrix: {
                path: "docs/contracts/boundedness-matrix.md",
                id: "iq.router_stream_state_max_budget",
                cap: matrixCapSnapshot(boundednessValidation, "iq.router_stream_state_max_budget", routerStateMaxEntries),
            },
        },
        layer_stream_state_max: {
            value: layerStateMaxEntries,
            boundedness_matrix: {
                path: "docs/contracts/boundedness-matrix.md",
                id: "iq.layer_stream_state_max_budget",
                cap: matrixCapSnapshot(boundednessValidation, "iq.layer_stream_state_max_budget", layerStateMaxEntries),
            },
        },
        wire_bytes_p95_budget: {
            value: wireBytesP95Budget,
            boundedness_matrix: {
                path: "docs/contracts/boundedness-matrix.md",
                id: "iq.wire_bytes_p95_budget_default",
                cap: matrixCapSnapshot(boundednessValidation, "iq.wire_bytes_p95_budget_default", wireBytesP95Budget),
            },
        },
        wire_bytes_p99_budget: {
            value: wireBytesP99Budget,
            boundedness_matrix: {
                path: "docs/contracts/boundedness-matrix.md",
                id: "iq.wire_bytes_p99_budget_default",
                cap: matrixCapSnapshot(boundednessValidation, "iq.wire_bytes_p99_budget_default", wireBytesP99Budget),
            },
        },
    },
    legacy_flags: {
        strict: strictProfile,
        fallback_strict: fallbackStrict,
        legacy_strict: legacyStrict,
        require_stats_canonical: requireStatsCanonical,
        allow_batched_fallback: allowBatchedFallback,
        allow_stats_fallback: allowStatsFallback,
        allow_unexpected_skips: allowUnexpectedSkips,
    },
    critical_invariants: {
        enabled: criticalEnabled,
        disabled: criticalDisabled,
        items: criticalInvariants,
    },
    profile_validation: {
        valid: iqProfile.valid,
        errors: iqProfile.validationErrors,
        profile_loader_fingerprint_hash: iqProfile.fingerprint.hash,
    },
};
const effectiveProfileProofRaw = `${stableJSONString(effectiveProfileProof, 2)}\n`;
const effectiveProfileProofHash = `sha256:${sha256Hex(effectiveProfileProofRaw)}`;

const markdown = [];
markdown.push("# IQ Loop Report");
markdown.push("");
markdown.push(`- run_dir: \`${runDir}\``);
markdown.push(`- generated_at: \`${generatedAtISO}\``);
markdown.push(`- status: **${overallPass ? "PASS" : "FAIL"}**`);
markdown.push("");
markdown.push("## Profile");
markdown.push("");
markdown.push(`- profile_name: \`${iqProfile.effectiveProfileName}\``);
markdown.push(`- effective_profile_artifact: \`effective-profile.json\``);
markdown.push(`- effective_profile_fingerprint_hash: \`${effectiveProfileProofHash}\``);
markdown.push(`- profile_loader_fingerprint_hash: \`${iqProfile.fingerprint.hash}\``);
markdown.push(`- critical_invariants_enabled: \`${criticalEnabled.join(",") || "none"}\``);
markdown.push(`- critical_invariants_disabled: \`${criticalDisabled.join(",") || "none"}\``);
markdown.push("");
markdown.push("| Critical Invariant | Enabled | Runtime Check | Status |");
markdown.push("|---|---|---|---|");
for (const entry of criticalInvariants) {
    markdown.push(`| ${entry.label} | ${entry.enabled ? "ON" : "OFF"} | ${entry.check_id} | ${statusIcon(entry.runtime_ok)} |`);
}
markdown.push("");
markdown.push("## Effective Profile Fingerprint");
markdown.push("");
markdown.push(`- profile_name: \`${iqProfile.fingerprint.object.profile_name}\``);
markdown.push(`- replica_count: \`${iqProfile.fingerprint.object.replica_count}\``);
markdown.push(`- fingerprint_hash: \`${iqProfile.fingerprint.hash}\``);
markdown.push("- fingerprint_json:");
markdown.push("```json");
markdown.push(iqProfile.fingerprint.json);
markdown.push("```");
markdown.push("");
markdown.push("## Effective IQ Profile");
markdown.push("");
markdown.push(`- requested profile: \`${iqProfile.requestedProfile || "<unset>"}\``);
markdown.push(`- effective profile: \`${iqProfile.effectiveProfileName}\``);
markdown.push(`- profile source: \`${iqProfile.sourcePath || "embedded defaults"}\``);
markdown.push(`- profile valid: \`${iqProfile.valid}\``);
markdown.push(`- strict flags: strict=\`${strictProfile}\` fallback_strict=\`${fallbackStrict}\` legacy_strict=\`${legacyStrict}\``);
if (iqProfile.validationErrors.length > 0) {
    markdown.push(`- profile contract violations: \`${iqProfile.validationErrors.join("; ")}\``);
}
markdown.push("");
markdown.push("| Key | Effective | Default |");
markdown.push("|---|---|---|");
for (const row of effectiveProfileEntries) {
    const diff = effectiveProfileDiffMap.get(row.key);
    const effectiveCell = diff ? `${row.effectiveValue} *(diff)*` : row.effectiveValue;
    markdown.push(`| ${row.key} | ${effectiveCell} | ${row.defaultValue} |`);
}
markdown.push("");
markdown.push("## Perf+Memory Baseline");
markdown.push("");
markdown.push(`- md parse us: p95=\`${mdParseP95}\` p99=\`${mdParseP99}\``);
markdown.push(`- md apply us: p95=\`${mdApplyP95}\` p99=\`${mdApplyP99}\``);
markdown.push(`- md batch decode us: p95=\`${mdBatchDecodeP95}\` p99=\`${mdBatchDecodeP99}\``);
markdown.push(`- alloc estimate: frame=\`${mdAllocEstimateFrame}\` total=\`${mdAllocEstimateTotal}\``);
markdown.push(`- layer stream: entries=\`${layerStreamEntries}\` evicted_total=\`${layerStreamEvictions}\``);
markdown.push("");
markdown.push("### Top-3 Widgets (Render Cost)");
markdown.push("");
if (top3Widgets.length === 0) {
    markdown.push("- none (missing widget render probes)");
} else {
    for (const row of top3Widgets) {
        markdown.push(`- ${row.id}: render_us(p95/p99)=${row.renderP95}/${row.renderP99} entries=${row.entries}/${row.maxEntries} evicted=${row.evictedTotal}`);
    }
}
markdown.push("");
markdown.push("### Top-3 Hot Paths (Overall)");
markdown.push("");
if (top3HotPaths.length === 0) {
    markdown.push("- none (missing perf probes)");
} else {
    for (const row of top3HotPaths) {
        markdown.push(`- ${row.id}: us(p95/p99)=${row.p95}/${row.p99}`);
    }
}
markdown.push("");
markdown.push("### Top-3 Memory Pressure Counters");
markdown.push("");
if (top3MemoryPressure.length === 0) {
    markdown.push("- none (missing memory probes)");
} else {
    for (const row of top3MemoryPressure) {
        markdown.push(`- ${row.id}: ${row.value} ${row.unit}`);
    }
}
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
markdown.push(`- stats canonical strict gate: \`${requireStatsCanonical}\` (set via \`IQ_REQUIRE_STATS_CANONICAL=1\`).`);
markdown.push(`- current consecutive zero streak: \`${statsFallbackStreak.streak}\` PASS runs (observed runs: \`${statsFallbackStreak.observedRuns}\`).`);
markdown.push(`- override active: \`${allowStatsFallback}\` (set via \`IQ_ALLOW_STATS_FALLBACK=1\`)`);
markdown.push("");
markdown.push("- unexpected skip/canonicalization gate requires `skip_unexpected_total=0` (from runtime logs).");
markdown.push(`- current run skip_unexpected_total: \`${unexpectedSkipTotal}\``);
markdown.push(`- override active: \`${allowUnexpectedSkips}\` (set via \`IQ_ALLOW_UNEXPECTED_SKIPS=1\`)`);
markdown.push("");
markdown.push("- legacy cutover gate requires `ws_legacy_requests_total{status=\"accepted\"}=0` and `probe_md_legacy_downgrade_count=0`.");
markdown.push(`- current run ws_legacy_requests_total: accepted=\`${legacyRouteAcceptedTotal}\` rejected=\`${legacyRouteRejectedTotal}\``);
markdown.push(`- current run probe_md_legacy_downgrade_count: \`${legacyDowngradeCount}\``);
markdown.push(`- legacy negative probe artifact: \`${legacyNegative ? "present" : "missing"}\` (\`logs/legacy-negative.json\`)`);
if (legacyNegative) {
    markdown.push(`- legacy negative probe deltas: rejected=\`${fmt(legacyNegativeRejectedDelta)}\` subject_invalid=\`${fmt(legacyNegativeSubjectInvalidDelta)}\``);
}
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
markdown.push("IQ_PROFILE=ci-strict PROCESSOR_REPLICAS=2 ./scripts/iq_loop.sh");
markdown.push("# or manual");
markdown.push("make up PROCESSOR_REPLICAS=2");
markdown.push("IQ_PROFILE=ci-strict node tests/playwright/scripts/iq-smoke.mjs");
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
markdown.push("## Scorecard por stream/canal");
markdown.push("");
markdown.push(`- baseline status: \`${scorecard.baseline.status}\``);
markdown.push(`- baseline source: \`${scorecard.baseline.source}\``);
markdown.push(`- baseline run_dir: \`${scorecard.baseline.run_dir || "n/a"}\``);
if (scorecard.baseline.reason) {
    markdown.push(`- baseline note: \`${scorecard.baseline.reason}\``);
}
markdown.push(`- scorecard artifact: \`scorecard.json\``);
markdown.push(`- scorecard regressions: \`${scorecardRegressionItems}\` items / \`${scorecard.top_regressions.length}\` top entries`);
markdown.push("");
if (scorecard.top_regressions.length === 0) {
    markdown.push("- no regressions detected");
} else {
    markdown.push("| Key | Metric | Current | Baseline | Delta | Ratio | Severity |");
    markdown.push("|---|---|---|---|---|---|---|");
    for (const row of scorecard.top_regressions) {
        markdown.push(`| ${row.key} | ${row.metric} | ${fmt(row.current)} | ${fmt(row.baseline)} | ${fmt(row.delta)} | ${fmt(row.ratio)} | ${fmt(row.severity)} |`);
    }
}
markdown.push("");

writeFileSync(effectiveProfileProofPath, effectiveProfileProofRaw);
writeFileSync(scorecardPath, JSON.stringify(scorecard, null, 2) + "\n");
writeFileSync(reportPath, markdown.join("\n") + "\n");
const summary = {
    generated_at: generatedAtISO,
    overall_pass: overallPass,
    smoke_pass: smokePass,
    invariants_pass: invariantsPass,
    stats_fallback_zero_streak: statsFallbackStreak.streak,
    stats_fallback_required_runs: statsFallbackRemovalRuns,
    effective_profile: {
        requested: iqProfile.requestedProfile || "<unset>",
        effective: iqProfile.effectiveProfileName,
        source: iqProfile.sourcePath || "embedded defaults",
        valid: iqProfile.valid,
        strict: strictProfile,
        fallback_strict: fallbackStrict,
        legacy_strict: legacyStrict,
        validation_errors: iqProfile.validationErrors,
        fingerprint: {
            hash: iqProfile.fingerprint.hash,
            json: iqProfile.fingerprint.json,
            value: iqProfile.fingerprint.object,
        },
        values: iqProfile.effectiveValues,
        diffs: iqProfile.diffs,
    },
    effective_profile_proof: {
        path: "effective-profile.json",
        hash: effectiveProfileProofHash,
        value: effectiveProfileProof,
    },
    critical_invariants: effectiveProfileProof.critical_invariants,
    failed_steps: failedSteps.map((s) => ({ id: s.id, details: s.details || "" })),
    failed_checks: failedChecks.map((c) => ({ id: c.id, evidence: c.evidence })),
    legacy_negative: legacyNegative || null,
    baseline: {
        md_parse_p95_us: mdParseP95,
        md_parse_p99_us: mdParseP99,
        md_apply_p95_us: mdApplyP95,
        md_apply_p99_us: mdApplyP99,
        md_batch_decode_p95_us: mdBatchDecodeP95,
        md_batch_decode_p99_us: mdBatchDecodeP99,
        md_alloc_estimate_frame: mdAllocEstimateFrame,
        md_alloc_estimate_total: mdAllocEstimateTotal,
        layer_stream_entries: layerStreamEntries,
        layer_stream_evictions: layerStreamEvictions,
    },
    top3_widgets_render: top3Widgets.map((row) => ({
        widget: row.id,
        render_p95_us: row.renderP95,
        render_p99_us: row.renderP99,
        entries: row.entries,
        max_entries: row.maxEntries,
        evicted_total: row.evictedTotal,
    })),
    top3_hot_paths: top3HotPaths,
    top3_memory_pressure: top3MemoryPressure,
    scorecard: {
        baseline: scorecard.baseline,
        thresholds: scorecard.thresholds,
        regression_items: scorecardRegressionItems,
        top_regressions: scorecard.top_regressions,
    },
};
writeFileSync(summaryPath, JSON.stringify(summary, null, 2) + "\n");

process.exit(overallPass ? 0 : 1);
