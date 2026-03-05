#!/usr/bin/env node

import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, existsSync } from "fs";
import { join, dirname } from "path";
import { tmpdir } from "os";
import { fileURLToPath } from "url";
import { spawnSync } from "child_process";
import { CI_STRICT_PROFILE_PATH, resolveIQProfile } from "./profile_loader.mjs";

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(__dirname, "..", "..");
const analyzeScript = join(repoRoot, "scripts", "iq", "analyze_iq_run.mjs");

function histogramSeries(metric, channel, points) {
    return points
        .map(([le, count]) => `${metric}_bucket{channel="${channel}",le="${le}"} ${count}`)
        .join("\n");
}

function buildMetricsText({
    tradeLatencyPoints,
    statsLatencyPoints,
    tradeBytesPoints,
    statsBytesPoints,
    dropsTotal,
    queueLen,
    queueCapacity,
    routerEntries,
    routerEvicted,
    batchFallbackEvents,
}) {
    return [
        histogramSeries("ws_publish_to_deliver_latency_seconds", "trade", tradeLatencyPoints),
        histogramSeries("ws_publish_to_deliver_latency_seconds", "stats", statsLatencyPoints),
        histogramSeries("ws_wire_bytes", "trade", tradeBytesPoints),
        histogramSeries("ws_wire_bytes", "stats", statsBytesPoints),
        `ws_drops_total{reason="queue_full"} ${dropsTotal}`,
        `ws_queue_len ${queueLen}`,
        `ws_queue_capacity ${queueCapacity}`,
        `router_stream_state_entries ${routerEntries}`,
        `router_stream_state_active_total ${Math.max(0, routerEntries - 1)}`,
        `delivery_router_stream_state_evicted_total ${routerEvicted}`,
        `ws_batch_fallback_events_total ${batchFallbackEvents}`,
    ].join("\n") + "\n";
}

function writeIqRunFixture(runDir, { metricsText, runtimeProbe, statsProbe, overallPass = true }) {
    const logsDir = join(runDir, "logs");
    mkdirSync(logsDir, { recursive: true });
    writeFileSync(
        join(logsDir, "playwright-smoke.json"),
        JSON.stringify({
            steps: [{ id: "boot", name: "boot", ok: true, details: "ok" }],
            runtime_probe: runtimeProbe || {},
            stats_probe: statsProbe || {},
        })
    );
    writeFileSync(join(logsDir, "server.metrics.prom"), metricsText || "");
    writeFileSync(join(logsDir, "compose.all.log"), "");
    writeFileSync(join(logsDir, "playwright-console.log"), "");
    writeFileSync(
        join(runDir, "summary.json"),
        JSON.stringify({
            overall_pass: overallPass,
        })
    );
}

test("resolveIQProfile applies ci-strict profile defaults", () => {
    const profile = resolveIQProfile({ IQ_PROFILE: "ci-strict" });

    assert.equal(profile.effectiveProfileName, "ci-strict");
    assert.equal(profile.sourcePath, CI_STRICT_PROFILE_PATH);
    assert.equal(profile.effectiveValues.IQ_STRICT, "1");
    assert.equal(profile.effectiveValues.IQ_REQUIRE_STATS_CANONICAL, "1");
    assert.equal(profile.effectiveValues.IQ_ALLOW_STATS_FALLBACK, "0");
    assert.equal(profile.valid, true);
    assert.equal(profile.validationErrors.length, 0);
    assert.match(profile.fingerprint.hash, /^sha256:[0-9a-f]{64}$/);
});

test("resolveIQProfile flags ci-strict relax override", () => {
    const profile = resolveIQProfile({
        IQ_PROFILE: "ci-strict",
        IQ_REQUIRE_STATS_CANONICAL: "0",
        IQ_ALLOW_STATS_FALLBACK: "1",
    });

    assert.equal(profile.effectiveProfileName, "ci-strict");
    assert.equal(profile.valid, false);
    assert.ok(
        profile.validationErrors.some((item) => item.includes("IQ_REQUIRE_STATS_CANONICAL=0")),
        "expected IQ_REQUIRE_STATS_CANONICAL relax violation"
    );
    assert.ok(
        profile.validationErrors.some((item) => item.includes("IQ_ALLOW_STATS_FALLBACK=1")),
        "expected IQ_ALLOW_STATS_FALLBACK relax violation"
    );
});

test("analyze report includes Effective IQ Profile header", () => {
    const runRoot = mkdtempSync(join(tmpdir(), "iq-profile-test-"));
    const runDir = join(runRoot, "20260305T000000Z");
    const logsDir = join(runDir, "logs");

    mkdirSync(logsDir, { recursive: true });
    writeFileSync(
        join(logsDir, "playwright-smoke.json"),
        JSON.stringify({
            steps: [{ id: "boot", name: "boot", ok: true, details: "ok" }],
            runtime_probe: {},
            stats_probe: {},
        })
    );
    writeFileSync(join(logsDir, "server.metrics.prom"), "");
    writeFileSync(join(logsDir, "compose.all.log"), "");
    writeFileSync(join(logsDir, "playwright-console.log"), "");

    const result = spawnSync(process.execPath, [analyzeScript, runDir], {
        cwd: repoRoot,
        env: {
            ...process.env,
            IQ_PROFILE: "ci-strict",
            PROCESSOR_REPLICAS: "2",
        },
        encoding: "utf8",
    });

    assert.ok(result.status === 0 || result.status === 1, `unexpected exit status ${result.status}`);

    const reportPath = join(runDir, "report.md");
    assert.ok(existsSync(reportPath), "expected report.md to be generated");

    const report = readFileSync(reportPath, "utf8");
    assert.match(report, /^## Effective Profile Fingerprint$/m);
    assert.match(report, /- fingerprint_hash: `sha256:[0-9a-f]{64}`/);
    assert.match(report, /^## Effective IQ Profile$/m);
    assert.match(report, /- effective profile: `ci-strict`/);
    assert.match(report, /\| IQ_REQUIRE_STATS_CANONICAL \| 1 \| 1 \|/);
});

test("analyze scorecard is deterministic and sorted with baseline diff", () => {
    const runRoot = mkdtempSync(join(tmpdir(), "iq-scorecard-test-"));
    const baselineDir = join(runRoot, "20260305T000000Z");
    const runDir = join(runRoot, "20260305T001500Z");

    const baselineMetrics = buildMetricsText({
        tradeLatencyPoints: [["0.1", 94], ["0.2", 96], ["0.5", 99], ["+Inf", 100]],
        statsLatencyPoints: [["0.1", 99], ["+Inf", 100]],
        tradeBytesPoints: [["2048", 96], ["4096", 100], ["+Inf", 100]],
        statsBytesPoints: [["1024", 100], ["+Inf", 100]],
        dropsTotal: 0,
        queueLen: 10,
        queueCapacity: 100,
        routerEntries: 100,
        routerEvicted: 0,
        batchFallbackEvents: 0,
    });
    const baselineProbe = {
        probe_md_trade_backlog: 10,
        probe_md_trade_backlog_cap: 100,
        probe_md_candle_backlog: 1,
        probe_md_candle_backlog_cap: 32,
        probe_md_signal_backlog: 2,
        probe_md_signal_backlog_cap: 64,
        probe_layer_stream_entries: 12,
        probe_layer_stream_evictions: 0,
        probe_md_stats_fallback_frames: 0,
        probe_md_evidence_fallback_frames: 0,
        probe_md_signal_fallback_frames: 0,
        probe_md_legacy_downgrade_count: 0,
    };
    writeIqRunFixture(baselineDir, {
        metricsText: baselineMetrics,
        runtimeProbe: baselineProbe,
        statsProbe: { md_stats_fallback_frames: 0 },
        overallPass: true,
    });

    const currentMetrics = buildMetricsText({
        tradeLatencyPoints: [["0.2", 80], ["0.5", 95], ["1", 100], ["+Inf", 100]],
        statsLatencyPoints: [["0.1", 99], ["+Inf", 100]],
        tradeBytesPoints: [["4096", 95], ["8192", 100], ["+Inf", 100]],
        statsBytesPoints: [["1024", 100], ["+Inf", 100]],
        dropsTotal: 5,
        queueLen: 65,
        queueCapacity: 100,
        routerEntries: 140,
        routerEvicted: 3,
        batchFallbackEvents: 2,
    });
    const currentProbe = {
        probe_md_trade_backlog: 35,
        probe_md_trade_backlog_cap: 100,
        probe_md_candle_backlog: 8,
        probe_md_candle_backlog_cap: 32,
        probe_md_signal_backlog: 16,
        probe_md_signal_backlog_cap: 64,
        probe_layer_stream_entries: 30,
        probe_layer_stream_evictions: 5,
        probe_md_stats_fallback_frames: 2,
        probe_md_evidence_fallback_frames: 1,
        probe_md_signal_fallback_frames: 1,
        probe_md_legacy_downgrade_count: 1,
    };
    writeIqRunFixture(runDir, {
        metricsText: currentMetrics,
        runtimeProbe: currentProbe,
        statsProbe: { md_stats_fallback_frames: 2 },
        overallPass: true,
    });

    const runOnce = () => spawnSync(process.execPath, [analyzeScript, runDir], {
        cwd: repoRoot,
        env: {
            ...process.env,
            BASELINE_IQ_DIR: baselineDir,
            IQ_PROFILE: "ci-strict",
            PROCESSOR_REPLICAS: "2",
        },
        encoding: "utf8",
    });

    const first = runOnce();
    assert.ok(first.status === 0 || first.status === 1, `unexpected first exit status ${first.status}`);
    const scorecardPath = join(runDir, "scorecard.json");
    assert.ok(existsSync(scorecardPath), "expected scorecard.json to be generated");
    const firstScorecardRaw = readFileSync(scorecardPath, "utf8");
    const firstScorecard = JSON.parse(firstScorecardRaw);
    const firstKeys = firstScorecard.items.map((item) => item.key);
    assert.deepEqual(firstKeys, [...firstKeys].sort(), "scorecard keys must be sorted");
    assert.ok(
        firstScorecard.top_regressions.some((row) => row.key === "channel/trade" && row.metric === "lat_p99_ms"),
        "expected trade latency regression in top_regressions"
    );
    assert.equal(firstScorecard.baseline.status, "available");

    const second = runOnce();
    assert.ok(second.status === 0 || second.status === 1, `unexpected second exit status ${second.status}`);
    const secondScorecardRaw = readFileSync(scorecardPath, "utf8");
    assert.equal(secondScorecardRaw, firstScorecardRaw, "scorecard output should be deterministic for same inputs");

    const report = readFileSync(join(runDir, "report.md"), "utf8");
    assert.match(report, /^## Scorecard por stream\/canal$/m);
    assert.match(report, /- baseline status: `available`/);
});
