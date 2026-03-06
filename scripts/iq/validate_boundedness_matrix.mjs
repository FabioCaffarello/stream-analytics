#!/usr/bin/env node

import { existsSync, readFileSync } from "fs";
import { dirname, join, resolve } from "path";
import { fileURLToPath } from "url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const DEFAULT_REPO_ROOT = resolve(join(__dirname, "..", ".."));
export const DEFAULT_MATRIX_PATH = "docs/contracts/boundedness-matrix.md";

function parseArgs(argv) {
    const out = {
        repoRoot: DEFAULT_REPO_ROOT,
        matrixPath: DEFAULT_MATRIX_PATH,
        json: false,
    };

    for (let i = 0; i < argv.length; i += 1) {
        const arg = argv[i];
        if (arg === "--repo") {
            out.repoRoot = resolve(argv[i + 1] || DEFAULT_REPO_ROOT);
            i += 1;
            continue;
        }
        if (arg === "--matrix") {
            out.matrixPath = argv[i + 1] || DEFAULT_MATRIX_PATH;
            i += 1;
            continue;
        }
        if (arg === "--json") {
            out.json = true;
            continue;
        }
    }

    return out;
}

function createContext(repoRoot) {
    const cache = new Map();

    function read(file) {
        if (cache.has(file)) {
            return cache.get(file);
        }
        const abs = resolve(join(repoRoot, file));
        if (!existsSync(abs)) {
            throw new Error(`missing file: ${file}`);
        }
        const text = readFileSync(abs, "utf8");
        cache.set(file, text);
        return text;
    }

    function line(file, lineNumber) {
        const text = read(file);
        const lines = text.split(/\r?\n/);
        const idx = lineNumber - 1;
        if (idx < 0 || idx >= lines.length) {
            return null;
        }
        return lines[idx];
    }

    return {
        repoRoot,
        read,
        line,
    };
}

function parseJSONMatrix(raw, matrixPath) {
    try {
        return JSON.parse(raw);
    } catch (err) {
        throw new Error(`matrix parse error at ${matrixPath}: expected JSON-compatible YAML (${err.message})`);
    }
}

function extractJSONCodeFence(raw, matrixPath) {
    const markerStart = "<!-- boundedness-matrix:data:start -->";
    const markerEnd = "<!-- boundedness-matrix:data:end -->";

    const startIdx = raw.indexOf(markerStart);
    const endIdx = raw.indexOf(markerEnd);
    if (startIdx >= 0 && endIdx > startIdx) {
        const section = raw.slice(startIdx + markerStart.length, endIdx);
        const fenced = section.match(/```json\s*([\s\S]*?)```/i);
        if (fenced && fenced[1]) {
            return fenced[1].trim();
        }
    }

    const fallback = raw.match(/```json\s*([\s\S]*?)```/i);
    if (fallback && fallback[1]) {
        return fallback[1].trim();
    }

    throw new Error(
        `matrix parse error at ${matrixPath}: markdown must contain a JSON code fence between ${markerStart} and ${markerEnd}`
    );
}

function parseMatrixDocument(raw, matrixPath) {
    if (String(matrixPath).toLowerCase().endsWith(".md")) {
        return parseJSONMatrix(extractJSONCodeFence(raw, matrixPath), matrixPath);
    }
    return parseJSONMatrix(raw, matrixPath);
}

function requireMatch(text, regex, label) {
    const match = text.match(regex);
    if (!match) {
        throw new Error(`pattern not found for ${label}`);
    }
    return match.groups?.value ?? match[1];
}

function parsePositiveInt(raw, label) {
    const normalized = String(raw).trim().replaceAll("_", "");
    const value = Number.parseInt(normalized, 10);
    if (!Number.isInteger(value) || value <= 0) {
        throw new Error(`invalid positive int for ${label}: ${raw}`);
    }
    return value;
}

function parseSimpleIntExpression(raw, label) {
    const expr = String(raw).split("//")[0].trim();
    if (/^\d+$/.test(expr)) {
        return Number.parseInt(expr, 10);
    }

    const mult = expr.match(/^(\d+)\s*\*\s*(\d+)$/);
    if (mult) {
        return Number.parseInt(mult[1], 10) * Number.parseInt(mult[2], 10);
    }

    throw new Error(`unsupported int expression for ${label}: ${expr}`);
}

function extractGoAssignedInt(ctx, file, regex, label) {
    const text = ctx.read(file);
    const raw = requireMatch(text, regex, label);
    return parsePositiveInt(raw, label);
}

function extractGoConstExprInt(ctx, file, constName) {
    const text = ctx.read(file);
    const raw = requireMatch(
        text,
        new RegExp(`\\bconst\\s+${constName}\\s*=\\s*(?<value>[^\\n]+)`),
        `${file}:${constName}`
    );
    return parseSimpleIntExpression(raw, `${file}:${constName}`);
}

function extractOdinConstInt(ctx, file, constName) {
    const text = ctx.read(file);
    const raw = requireMatch(
        text,
        new RegExp(`\\b${constName}\\s*::\\s*(?<value>[^\\n]+)`),
        `${file}:${constName}`
    );
    return parseSimpleIntExpression(raw, `${file}:${constName}`);
}

function extractProfileDefaultInt(ctx, key) {
    const file = "scripts/iq/profile_loader.mjs";
    const text = ctx.read(file);
    const raw = requireMatch(
        text,
        new RegExp(`\\b${key}:\\s*\"(?<value>\\d+)\"`),
        `${file}:${key}`
    );
    return parsePositiveInt(raw, `${file}:${key}`);
}

function extractCIStrictEnvInt(ctx, key) {
    const file = "scripts/iq/profiles/ci-strict.env";
    const text = ctx.read(file);
    const raw = requireMatch(
        text,
        new RegExp(`^${key}=(?<value>\\d+)\\s*$`, "m"),
        `${file}:${key}`
    );
    return parsePositiveInt(raw, `${file}:${key}`);
}

function extractRouterBudgetDefault(ctx) {
    const file = "scripts/iq/analyze_iq_run.mjs";
    const text = ctx.read(file);
    const raw = requireMatch(
        text,
        /IQ_ROUTER_STREAM_STATE_MAX",\s*"(?<value>\d+)"\)/,
        `${file}:IQ_ROUTER_STREAM_STATE_MAX`
    );
    return parsePositiveInt(raw, `${file}:IQ_ROUTER_STREAM_STATE_MAX`);
}

const EXTRACTORS = Object.freeze({
    "backend.delivery.session_outbound_queue_size": (ctx) =>
        extractGoAssignedInt(
            ctx,
            "internal/shared/config/loader.go",
            /c\.Delivery\.SessionOutboundQueueSize = (?<value>\d+)/,
            "internal/shared/config/loader.go:SessionOutboundQueueSize"
        ),
    "backend.delivery.session_max_frame_bytes": (ctx) =>
        extractGoConstExprInt(ctx, "internal/actors/delivery/runtime/session.go", "readLimitBytes"),
    "backend.delivery.router_stream_state_entries_max": (ctx) =>
        extractGoAssignedInt(
            ctx,
            "internal/actors/delivery/runtime/router.go",
            /defaultMaxStreamStateEntries\s*=\s*(?<value>\d+)/,
            "internal/actors/delivery/runtime/router.go:defaultMaxStreamStateEntries"
        ),
    "backend.aggregation.candle_max_windows": (ctx) =>
        extractGoAssignedInt(
            ctx,
            "internal/shared/config/loader.go",
            /c\.Processor\.Candle\.MaxCandles = (?<value>[0-9_]+)/,
            "internal/shared/config/loader.go:Candle.MaxCandles"
        ),
    "backend.aggregation.candle_window_cap": (ctx) =>
        extractGoAssignedInt(
            ctx,
            "internal/shared/config/loader.go",
            /c\.Processor\.Candle\.WindowCap = (?<value>[0-9_]+)/,
            "internal/shared/config/loader.go:Candle.WindowCap"
        ),
    "backend.aggregation.stats_max_windows": (ctx) =>
        extractGoAssignedInt(
            ctx,
            "internal/shared/config/loader.go",
            /c\.Processor\.Stats\.MaxWindows = (?<value>[0-9_]+)/,
            "internal/shared/config/loader.go:Stats.MaxWindows"
        ),
    "backend.aggregation.stats_window_cap": (ctx) =>
        extractGoAssignedInt(
            ctx,
            "internal/shared/config/loader.go",
            /c\.Processor\.Stats\.WindowCap = (?<value>[0-9_]+)/,
            "internal/shared/config/loader.go:Stats.WindowCap"
        ),
    "backend.evidence.buffer_cap_per_kind": (ctx) =>
        extractGoAssignedInt(
            ctx,
            "internal/shared/config/loader.go",
            /c\.Evidence\.BufferCapPerKind = (?<value>[0-9_]+)/,
            "internal/shared/config/loader.go:Evidence.BufferCapPerKind"
        ),
    "backend.evidence.regime_max_streams": (ctx) =>
        extractGoAssignedInt(
            ctx,
            "internal/shared/config/loader.go",
            /c\.Evidence\.RegimeMaxStreams = (?<value>[0-9_]+)/,
            "internal/shared/config/loader.go:Evidence.RegimeMaxStreams"
        ),
    "backend.evidence.regime_history_cap": (ctx) =>
        extractGoAssignedInt(
            ctx,
            "internal/shared/config/loader.go",
            /c\.Evidence\.RegimeHistoryCap = (?<value>[0-9_]+)/,
            "internal/shared/config/loader.go:Evidence.RegimeHistoryCap"
        ),
    "backend.signal.rate_limit_per_min": (ctx) =>
        extractGoAssignedInt(
            ctx,
            "internal/shared/config/loader.go",
            /c\.Signals\.RateLimitPerMin = (?<value>[0-9_]+)/,
            "internal/shared/config/loader.go:Signals.RateLimitPerMin"
        ),
    "backend.signal.global_rate_limit_per_min": (ctx) =>
        extractGoAssignedInt(
            ctx,
            "internal/shared/config/loader.go",
            /c\.Signals\.GlobalRateLimitPerMin = (?<value>[0-9_]+)/,
            "internal/shared/config/loader.go:Signals.GlobalRateLimitPerMin"
        ),
    "backend.signal.max_subs_per_session": (ctx) =>
        extractGoAssignedInt(
            ctx,
            "internal/shared/config/loader.go",
            /c\.Signals\.MaxSubsPerSession = (?<value>[0-9_]+)/,
            "internal/shared/config/loader.go:Signals.MaxSubsPerSession"
        ),
    "backend.signal.window_cap": (ctx) =>
        extractGoAssignedInt(
            ctx,
            "internal/shared/config/loader.go",
            /c\.Signals\.WindowCap = (?<value>[0-9_]+)/,
            "internal/shared/config/loader.go:Signals.WindowCap"
        ),
    "client.native.trade_ring_cap": (ctx) =>
        extractOdinConstInt(ctx, "client/src/platform/native/marketdata_native.odin", "TRADE_RING_CAP"),
    "client.native.candle_ring_cap": (ctx) =>
        extractOdinConstInt(ctx, "client/src/platform/native/marketdata_native.odin", "CANDLE_RING_CAP"),
    "client.native.signal_ring_cap": (ctx) =>
        extractOdinConstInt(ctx, "client/src/platform/native/marketdata_native.odin", "SIGNAL_RING_CAP"),
    "client.web.trade_ring_cap": (ctx) =>
        extractOdinConstInt(ctx, "client/src/platform/web/marketdata_web.odin", "WEB_TRADE_RING_CAP"),
    "client.web.candle_ring_cap": (ctx) =>
        extractOdinConstInt(ctx, "client/src/platform/web/marketdata_web.odin", "WEB_CANDLE_RING_CAP"),
    "client.web.signal_ring_cap": (ctx) =>
        extractOdinConstInt(ctx, "client/src/platform/web/marketdata_web.odin", "WEB_SIGNAL_RING_CAP"),
    "client.widgets.stats_max_entries": (ctx) =>
        extractOdinConstInt(ctx, "client/src/core/services/stats_store.odin", "STATS_CAP"),
    "client.widgets.dom_max_entries": (ctx) =>
        extractOdinConstInt(ctx, "client/src/core/services/orderbook_store.odin", "OB_DEPTH_CAP") * 2,
    "client.widgets.tape_max_entries": (ctx) =>
        extractOdinConstInt(ctx, "client/src/core/services/trades_store.odin", "TRADES_CAP"),
    "client.widgets.evidence_max_entries": (ctx) =>
        extractOdinConstInt(ctx, "client/src/core/layers/market_store.odin", "EVIDENCE_RING_CAP"),
    "client.widgets.signal_max_entries": (ctx) => {
        const kinds = extractOdinConstInt(ctx, "client/src/core/services/signal_store.odin", "SIGNAL_KIND_CAP");
        const perKind = extractOdinConstInt(ctx, "client/src/core/services/signal_store.odin", "SIGNAL_PER_KIND_CAP");
        return kinds * perKind;
    },
    "iq.router_stream_state_max_budget": (ctx) =>
        extractRouterBudgetDefault(ctx),
    "iq.layer_stream_state_max_budget": (ctx) =>
        extractRouterBudgetDefault(ctx),
    "iq.wire_bytes_p95_budget_default": (ctx) => {
        const defaults = extractProfileDefaultInt(ctx, "IQ_WIRE_BYTES_P95_BUDGET");
        const ciStrict = extractCIStrictEnvInt(ctx, "IQ_WIRE_BYTES_P95_BUDGET");
        if (defaults !== ciStrict) {
            throw new Error(`profile mismatch for IQ_WIRE_BYTES_P95_BUDGET: defaults=${defaults} ci-strict=${ciStrict}`);
        }
        return defaults;
    },
    "iq.wire_bytes_p99_budget_default": (ctx) => {
        const defaults = extractProfileDefaultInt(ctx, "IQ_WIRE_BYTES_P99_BUDGET");
        const ciStrict = extractCIStrictEnvInt(ctx, "IQ_WIRE_BYTES_P99_BUDGET");
        if (defaults !== ciStrict) {
            throw new Error(`profile mismatch for IQ_WIRE_BYTES_P99_BUDGET: defaults=${defaults} ci-strict=${ciStrict}`);
        }
        return defaults;
    },
});

export const EXTRACTOR_IDS = Object.freeze(Object.keys(EXTRACTORS));

const EXTRACTOR_SOURCE_HINTS = Object.freeze({
    "backend.delivery.session_outbound_queue_size": "internal/shared/config/loader.go (applyDefaults delivery session queue)",
    "backend.delivery.session_max_frame_bytes": "internal/actors/delivery/runtime/session.go (readLimitBytes)",
    "backend.delivery.router_stream_state_entries_max": "internal/actors/delivery/runtime/router.go (defaultMaxStreamStateEntries)",
    "backend.aggregation.candle_max_windows": "internal/shared/config/loader.go (processor.candle.max_candles default)",
    "backend.aggregation.candle_window_cap": "internal/shared/config/loader.go (processor.candle.window_cap default)",
    "backend.aggregation.stats_max_windows": "internal/shared/config/loader.go (processor.stats.max_windows default)",
    "backend.aggregation.stats_window_cap": "internal/shared/config/loader.go (processor.stats.window_cap default)",
    "backend.evidence.buffer_cap_per_kind": "internal/shared/config/loader.go (evidence.buffer_cap_per_kind default)",
    "backend.evidence.regime_max_streams": "internal/shared/config/loader.go (evidence.regime_max_streams default)",
    "backend.evidence.regime_history_cap": "internal/shared/config/loader.go (evidence.regime_history_cap default)",
    "backend.signal.rate_limit_per_min": "internal/shared/config/loader.go (signals.rate_limit_per_min default)",
    "backend.signal.global_rate_limit_per_min": "internal/shared/config/loader.go (signals.global_rate_limit_per_min default)",
    "backend.signal.max_subs_per_session": "internal/shared/config/loader.go (signals.max_subs_per_session default)",
    "backend.signal.window_cap": "internal/shared/config/loader.go (signals.window_cap default)",
    "client.native.trade_ring_cap": "client/src/platform/native/marketdata_native.odin (TRADE_RING_CAP)",
    "client.native.candle_ring_cap": "client/src/platform/native/marketdata_native.odin (CANDLE_RING_CAP)",
    "client.native.signal_ring_cap": "client/src/platform/native/marketdata_native.odin (SIGNAL_RING_CAP)",
    "client.web.trade_ring_cap": "client/src/platform/web/marketdata_web.odin (WEB_TRADE_RING_CAP)",
    "client.web.candle_ring_cap": "client/src/platform/web/marketdata_web.odin (WEB_CANDLE_RING_CAP)",
    "client.web.signal_ring_cap": "client/src/platform/web/marketdata_web.odin (WEB_SIGNAL_RING_CAP)",
    "client.widgets.stats_max_entries": "client/src/core/services/stats_store.odin (STATS_CAP)",
    "client.widgets.dom_max_entries": "client/src/core/services/orderbook_store.odin (OB_DEPTH_CAP * 2)",
    "client.widgets.tape_max_entries": "client/src/core/services/trades_store.odin (TRADES_CAP)",
    "client.widgets.evidence_max_entries": "client/src/core/layers/market_store.odin (EVIDENCE_RING_CAP)",
    "client.widgets.signal_max_entries": "client/src/core/services/signal_store.odin (SIGNAL_KIND_CAP * SIGNAL_PER_KIND_CAP)",
    "iq.router_stream_state_max_budget": "scripts/iq/analyze_iq_run.mjs (IQ_ROUTER_STREAM_STATE_MAX default)",
    "iq.layer_stream_state_max_budget": "scripts/iq/analyze_iq_run.mjs (IQ_LAYER_STREAM_STATE_MAX default)",
    "iq.wire_bytes_p95_budget_default": "scripts/iq/profile_loader.mjs + scripts/iq/profiles/ci-strict.env",
    "iq.wire_bytes_p99_budget_default": "scripts/iq/profile_loader.mjs + scripts/iq/profiles/ci-strict.env",
});

function validateAnchor(ctx, entryID, anchor) {
    if (!anchor || typeof anchor !== "object") {
        return `entry ${entryID}: invalid anchor object`;
    }
    const file = String(anchor.file || "").trim();
    const snippet = String(anchor.snippet || "").trim();
    const line = anchor.line;

    if (!file) {
        return `entry ${entryID}: anchor missing file`;
    }
    if (!snippet) {
        return `entry ${entryID}: anchor missing snippet for ${file}`;
    }

    try {
        if (line !== undefined && line !== null) {
            const lineNumber = Number(line);
            if (!Number.isInteger(lineNumber) || lineNumber <= 0) {
                return `entry ${entryID}: anchor line must be positive integer for ${file}`;
            }
            const textLine = ctx.line(file, lineNumber);
            if (textLine === null) {
                return `entry ${entryID}: anchor line ${lineNumber} out of range for ${file}`;
            }
            if (!textLine.includes(snippet)) {
                return `entry ${entryID}: anchor mismatch at ${file}:${lineNumber} (snippet not found)`;
            }
            return null;
        }

        const text = ctx.read(file);
        if (!text.includes(snippet)) {
            return `entry ${entryID}: anchor snippet not found in ${file}`;
        }
        return null;
    } catch (err) {
        return `entry ${entryID}: ${err.message}`;
    }
}

function conflictingDuplicateErrors(entries, keyFn, label) {
    const buckets = new Map();
    const errors = [];

    for (const entry of entries) {
        const key = keyFn(entry);
        if (!buckets.has(key)) {
            buckets.set(key, new Set());
        }
        buckets.get(key).add(Number(entry.cap));
    }

    for (const [key, caps] of buckets.entries()) {
        if (caps.size > 1) {
            errors.push(`${label} conflict for ${key}: caps=${Array.from(caps).sort((a, b) => a - b).join(",")}`);
        }
    }

    return errors;
}

export function validateBoundednessMatrix(options = {}) {
    const repoRoot = resolve(options.repoRoot || DEFAULT_REPO_ROOT);
    const matrixPath = options.matrixPath || DEFAULT_MATRIX_PATH;
    const enforceFullCatalog = options.enforceFullCatalog !== false;
    const absMatrix = resolve(join(repoRoot, matrixPath));

    const errors = [];
    let checkedAnchors = 0;
    let checkedEntries = 0;
    let driftCount = 0;

    if (!existsSync(absMatrix)) {
        return {
            ok: false,
            errors: [`missing matrix file: ${matrixPath}`],
            checkedAnchors,
            checkedEntries,
            driftCount,
            matrixPath,
            repoRoot,
            effectiveCaps: {},
            catalogVersion: "unknown",
            duplicateConflictCount: 0,
        };
    }

    let matrix;
    try {
        const raw = readFileSync(absMatrix, "utf8");
        matrix = parseMatrixDocument(raw, matrixPath);
    } catch (err) {
        return {
            ok: false,
            errors: [err.message],
            checkedAnchors,
            checkedEntries,
            driftCount,
            matrixPath,
            repoRoot,
            effectiveCaps: {},
            catalogVersion: "unknown",
            duplicateConflictCount: 0,
        };
    }

    const entries = Array.isArray(matrix.entries) ? matrix.entries : [];
    if (entries.length === 0) {
        return {
            ok: false,
            errors: ["matrix entries must be a non-empty array"],
            checkedAnchors,
            checkedEntries,
            driftCount,
            matrixPath,
            repoRoot,
            effectiveCaps: {},
            catalogVersion: String(matrix.catalog_version || "unknown"),
            duplicateConflictCount: 0,
        };
    }

    const ctx = createContext(repoRoot);
    const effectiveCaps = {};

    const idConflicts = conflictingDuplicateErrors(entries, (entry) => String(entry.id || ""), "id");
    const structConflicts = conflictingDuplicateErrors(
        entries,
        (entry) => `${entry.subsystem || ""}|${entry.structure || ""}|${entry.unit || ""}`,
        "subsystem+structure+unit"
    );
    errors.push(...idConflicts, ...structConflicts);

    const matrixIDs = new Set();

    for (const entry of entries) {
        checkedEntries += 1;

        const id = String(entry.id || "").trim();
        if (!id) {
            errors.push(`entry #${checkedEntries}: missing id`);
            continue;
        }
        matrixIDs.add(id);

        if (!Object.prototype.hasOwnProperty.call(EXTRACTORS, id)) {
            errors.push(`entry ${id}: no allowlisted extractor`);
            continue;
        }

        const matrixCap = Number(entry.cap);
        if (!Number.isInteger(matrixCap) || matrixCap <= 0) {
            errors.push(`entry ${id}: cap must be positive integer, got ${entry.cap}`);
            continue;
        }

        try {
            const effectiveCap = EXTRACTORS[id](ctx);
            effectiveCaps[id] = effectiveCap;
            if (effectiveCap !== matrixCap) {
                driftCount += 1;
                const sourceHint = EXTRACTOR_SOURCE_HINTS[id] || "allowlisted source";
                errors.push(
                    `entry ${id}: cap drift detected (matrix=${matrixCap}, effective=${effectiveCap}) | action: update ${matrixPath} cap to ${effectiveCap} or update source ${sourceHint}`
                );
            }
        } catch (err) {
            const sourceHint = EXTRACTOR_SOURCE_HINTS[id] || "allowlisted source";
            errors.push(`entry ${id}: extractor failed (${err.message}) | source=${sourceHint}`);
        }

        const anchors = Array.isArray(entry.anchors) ? entry.anchors : [];
        if (anchors.length === 0) {
            errors.push(`entry ${id}: anchors must be a non-empty array`);
        }
        for (const anchor of anchors) {
            checkedAnchors += 1;
            const anchorErr = validateAnchor(ctx, id, anchor);
            if (anchorErr) {
                errors.push(anchorErr);
            }
        }
    }

    if (enforceFullCatalog) {
        for (const id of EXTRACTOR_IDS) {
            if (!matrixIDs.has(id)) {
                errors.push(`matrix missing required extractor id: ${id}`);
            }
        }
    }

    return {
        ok: errors.length === 0,
        errors,
        checkedAnchors,
        checkedEntries,
        driftCount,
        matrixPath,
        repoRoot,
        effectiveCaps,
        catalogVersion: String(matrix.catalog_version || "unknown"),
        duplicateConflictCount: idConflicts.length + structConflicts.length,
    };
}

function printResult(result, asJSON) {
    if (asJSON) {
        console.log(JSON.stringify(result, null, 2));
        return;
    }

    if (result.ok) {
        console.log(
            `boundedness_matrix_valid: PASS catalog=${result.catalogVersion} entries=${result.checkedEntries} anchors=${result.checkedAnchors}`
        );
        return;
    }

    console.error(
        `boundedness_matrix_valid: FAIL catalog=${result.catalogVersion} entries=${result.checkedEntries} errors=${result.errors.length}`
    );
    for (const err of result.errors) {
        console.error(`- ${err}`);
    }
}

const isMain = process.argv[1] === fileURLToPath(import.meta.url);
if (isMain) {
    const args = parseArgs(process.argv.slice(2));
    const result = validateBoundednessMatrix({
        repoRoot: args.repoRoot,
        matrixPath: args.matrixPath,
        enforceFullCatalog: true,
    });
    printResult(result, args.json);
    process.exit(result.ok ? 0 : 1);
}
