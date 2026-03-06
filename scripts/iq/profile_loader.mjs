#!/usr/bin/env node

import { createHash } from "crypto";
import { existsSync, readFileSync } from "fs";
import { dirname, join } from "path";
import { fileURLToPath, pathToFileURL } from "url";

const __dirname = dirname(fileURLToPath(import.meta.url));

export const CI_STRICT_PROFILE_NAME = "ci-strict";
export const CI_STRICT_PROFILE_PATH = join(__dirname, "profiles", "ci-strict.env");

export const CRITICAL_PROFILE_DEFAULTS = Object.freeze({
    IQ_STRICT: "1",
    IQ_REQUIRE_STATS_CANONICAL: "1",
    IQ_FALLBACK_STRICT: "1",
    IQ_LEGACY_STRICT: "1",
    IQ_ALLOW_BATCHED_FALLBACK: "0",
    IQ_ALLOW_STATS_FALLBACK: "0",
    IQ_ALLOW_UNEXPECTED_SKIPS: "0",
    IQ_WIRE_BUDGET_CHANNELS: "trade,book_snapshot,stats,candle",
    IQ_WIRE_P95_BUDGET_MS: "5000",
    IQ_WIRE_P99_BUDGET_MS: "5000",
    IQ_WIRE_BYTES_P95_BUDGET: "65536",
    IQ_WIRE_BYTES_P99_BUDGET: "131072",
    IQ_ROUTER_STREAM_STATE_MAX: "2048",
    IQ_LAYER_STREAM_STATE_MAX: "2048",
    PROCESSOR_REPLICAS: "2",
});

const CI_STRICT_REQUIRED_VALUES = Object.freeze({
    ...CRITICAL_PROFILE_DEFAULTS,
});

const CI_STRICT_FORBIDDEN_OVERRIDES = Object.freeze([
    "IQ_WIRE_P95_BUDGET_MS_BY_CHANNEL",
    "IQ_WIRE_P99_BUDGET_MS_BY_CHANNEL",
    "IQ_WIRE_BYTES_P95_BUDGET_BY_CHANNEL",
    "IQ_WIRE_BYTES_P99_BUDGET_BY_CHANNEL",
]);

function normalizeProfileName(profileRaw) {
    return String(profileRaw || "")
        .trim()
        .toLowerCase()
        .replace(/_/g, "-");
}

function profilePathFor(name) {
    if (name === CI_STRICT_PROFILE_NAME) {
        return CI_STRICT_PROFILE_PATH;
    }
    return null;
}

export function parseEnvFile(filePath) {
    const out = {};
    if (!filePath || !existsSync(filePath)) {
        return out;
    }

    const lines = readFileSync(filePath, "utf8").split(/\r?\n/);
    for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed || trimmed.startsWith("#")) {
            continue;
        }

        const idx = trimmed.indexOf("=");
        if (idx <= 0) {
            continue;
        }

        const key = trimmed.slice(0, idx).trim();
        const value = trimmed.slice(idx + 1).trim();
        if (!key) {
            continue;
        }

        out[key] = value;
    }

    return out;
}

export function envBoolValue(value, fallback = false) {
    if (value === undefined || value === null) {
        return fallback;
    }

    const raw = String(value).trim().toLowerCase();
    if (!raw) {
        return fallback;
    }

    if (["1", "true", "yes", "on"].includes(raw)) {
        return true;
    }

    if (["0", "false", "no", "off"].includes(raw)) {
        return false;
    }

    return fallback;
}

function parseBudgetChannels(raw) {
    return String(raw || "")
        .split(",")
        .map((value) => value.trim())
        .filter(Boolean);
}

function parseFinitePositiveNumber(raw) {
    const value = Number.parseFloat(String(raw || "").trim());
    if (!Number.isFinite(value) || value <= 0) {
        return null;
    }
    return value;
}

function parseFinitePositiveInt(raw) {
    const value = Number.parseInt(String(raw || "").trim(), 10);
    if (!Number.isFinite(value) || value <= 0) {
        return null;
    }
    return value;
}

function normalizeModeValue(raw) {
    return String(raw || "").trim().toLowerCase();
}

function detectExecutionMode(env) {
    const ci = envBoolValue(env.CI, false);
    const releaseByFlag = [
        env.RELEASE,
        env.IQ_RELEASE,
        env.RELEASE_MODE,
        env.IQ_RELEASE_MODE,
    ].some((value) => envBoolValue(value, false));
    const releaseByRunMode = [
        env.RUN_MODE,
        env.MARKET_RACCOON_MODE,
        env.IQ_RUN_MODE,
        env.IQ_MODE,
    ]
        .map((value) => normalizeModeValue(value))
        .some((value) => ["prod", "production", "release"].includes(value));

    const release = releaseByFlag || releaseByRunMode;
    let modeLabel = "local";
    if (ci && release) {
        modeLabel = "ci+release";
    } else if (ci) {
        modeLabel = "ci";
    } else if (release) {
        modeLabel = "release";
    }

    return {
        ci,
        release,
        modeLabel,
    };
}

function stableStringify(value) {
    if (Array.isArray(value)) {
        return `[${value.map((item) => stableStringify(item)).join(",")}]`;
    }
    if (value && typeof value === "object") {
        const keys = Object.keys(value).sort();
        return `{${keys.map((key) => `${JSON.stringify(key)}:${stableStringify(value[key])}`).join(",")}}`;
    }
    return JSON.stringify(value);
}

function buildEffectiveProfileFingerprint(profileName, effectiveValues) {
    const fingerprintObject = {
        profile_name: profileName,
        budgets: {
            p95_ms: Number.parseFloat(effectiveValues.IQ_WIRE_P95_BUDGET_MS),
            p99_ms: Number.parseFloat(effectiveValues.IQ_WIRE_P99_BUDGET_MS),
            bytes_p95: Number.parseFloat(effectiveValues.IQ_WIRE_BYTES_P95_BUDGET),
            bytes_p99: Number.parseFloat(effectiveValues.IQ_WIRE_BYTES_P99_BUDGET),
        },
        caps: {
            router_stream_state_max: Number.parseInt(effectiveValues.IQ_ROUTER_STREAM_STATE_MAX, 10),
            layer_stream_state_max: Number.parseInt(effectiveValues.IQ_LAYER_STREAM_STATE_MAX, 10),
        },
        legacy_flags: {
            strict: envBoolValue(effectiveValues.IQ_STRICT, false),
            fallback_strict: envBoolValue(effectiveValues.IQ_FALLBACK_STRICT, false),
            legacy_strict: envBoolValue(effectiveValues.IQ_LEGACY_STRICT, false),
            require_stats_canonical: envBoolValue(effectiveValues.IQ_REQUIRE_STATS_CANONICAL, false),
            allow_batched_fallback: envBoolValue(effectiveValues.IQ_ALLOW_BATCHED_FALLBACK, false),
            allow_stats_fallback: envBoolValue(effectiveValues.IQ_ALLOW_STATS_FALLBACK, false),
            allow_unexpected_skips: envBoolValue(effectiveValues.IQ_ALLOW_UNEXPECTED_SKIPS, false),
        },
        replica_count: Number.parseInt(effectiveValues.PROCESSOR_REPLICAS, 10),
    };

    const json = stableStringify(fingerprintObject);
    const hash = `sha256:${createHash("sha256").update(json).digest("hex")}`;

    return {
        object: fingerprintObject,
        json,
        hash,
    };
}

function ciStrictViolationsFor({ requestedRaw, requestedNorm, effectiveValues, env }) {
    const violations = [];
    const mode = detectExecutionMode(env);

    if (!requestedRaw && (mode.ci || mode.release)) {
        violations.push(`IQ_PROFILE must be explicitly set to "ci-strict" in ${mode.modeLabel} mode`);
    }

    if (requestedNorm !== CI_STRICT_PROFILE_NAME) {
        violations.push(`IQ_PROFILE=${requestedRaw || "<unset>"} (expected ${CI_STRICT_PROFILE_NAME})`);
    }

    for (const [key, expected] of Object.entries(CI_STRICT_REQUIRED_VALUES)) {
        const actual = String(effectiveValues[key] ?? "").trim();
        if (actual !== expected) {
            violations.push(`${key}=${actual || "<unset>"} (expected ${expected})`);
        }
    }

    const wireChannels = parseBudgetChannels(effectiveValues.IQ_WIRE_BUDGET_CHANNELS);
    if (wireChannels.length === 0) {
        violations.push("IQ_WIRE_BUDGET_CHANNELS=<empty> (p95/p99 thresholds disabled)");
    }

    for (const key of [
        "IQ_WIRE_P95_BUDGET_MS",
        "IQ_WIRE_P99_BUDGET_MS",
        "IQ_WIRE_BYTES_P95_BUDGET",
        "IQ_WIRE_BYTES_P99_BUDGET",
    ]) {
        if (parseFinitePositiveNumber(effectiveValues[key]) === null) {
            const current = String(effectiveValues[key] ?? "").trim();
            violations.push(`${key}=${current || "<unset>"} (must be > 0)`);
        }
    }

    for (const key of ["IQ_ROUTER_STREAM_STATE_MAX", "IQ_LAYER_STREAM_STATE_MAX", "PROCESSOR_REPLICAS"]) {
        if (parseFinitePositiveInt(effectiveValues[key]) === null) {
            const current = String(effectiveValues[key] ?? "").trim();
            violations.push(`${key}=${current || "<unset>"} (must be integer > 0)`);
        }
    }

    for (const key of CI_STRICT_FORBIDDEN_OVERRIDES) {
        if (env[key] !== undefined && String(env[key]).trim() !== "") {
            violations.push(`${key} override is forbidden in ci-strict profile`);
        }
    }

    return violations;
}

function profileDiffs(defaults, effective) {
    return Object.keys(effective)
        .sort()
        .filter((key) => String(defaults[key] ?? "") !== String(effective[key] ?? ""))
        .map((key) => ({
            key,
            defaultValue: String(defaults[key] ?? "<unset>"),
            effectiveValue: String(effective[key] ?? "<unset>"),
        }));
}

export function resolveIQProfile(env = process.env) {
    const requestedRaw = String(env.IQ_PROFILE || "").trim();
    const requestedNorm = normalizeProfileName(requestedRaw) || CI_STRICT_PROFILE_NAME;
    const sourcePath = profilePathFor(CI_STRICT_PROFILE_NAME);
    const sourceValues = parseEnvFile(sourcePath);
    const defaults = { ...CRITICAL_PROFILE_DEFAULTS };
    const effectiveValues = { ...defaults, ...sourceValues };

    for (const key of Object.keys(effectiveValues)) {
        if (env[key] !== undefined) {
            effectiveValues[key] = String(env[key]).trim();
        }
    }

    const validationErrors = ciStrictViolationsFor({
        requestedRaw,
        requestedNorm,
        effectiveValues,
        env,
    });

    const fingerprint = buildEffectiveProfileFingerprint(CI_STRICT_PROFILE_NAME, effectiveValues);

    return {
        requestedProfile: requestedRaw || null,
        effectiveProfileName: CI_STRICT_PROFILE_NAME,
        sourcePath,
        defaults,
        sourceValues,
        effectiveValues,
        diffs: profileDiffs(defaults, effectiveValues),
        validationErrors,
        valid: validationErrors.length === 0,
        fingerprint,
    };
}

function shellEscape(raw) {
    return `'${String(raw).replace(/'/g, `'\\''`)}'`;
}

function emitShellExports(profile) {
    const assignments = {
        IQ_PROFILE: CI_STRICT_PROFILE_NAME,
        IQ_EFFECTIVE_PROFILE_NAME: profile.effectiveProfileName,
        IQ_EFFECTIVE_PROFILE_SOURCE: profile.sourcePath || "embedded defaults",
        IQ_EFFECTIVE_PROFILE_FINGERPRINT_HASH: profile.fingerprint.hash,
        IQ_EFFECTIVE_PROFILE_FINGERPRINT_JSON: profile.fingerprint.json,
        ...profile.effectiveValues,
    };

    return Object.keys(assignments)
        .sort()
        .map((key) => `export ${key}=${shellEscape(assignments[key])}`)
        .join("\n");
}

function runCli() {
    if (!process.argv.includes("--shell-export")) {
        return;
    }

    const profile = resolveIQProfile(process.env);
    if (!profile.valid) {
        for (const violation of profile.validationErrors) {
            console.error(`IQ_PROFILE_VALIDATION ${violation}`);
        }
        process.exit(1);
    }

    process.stdout.write(`${emitShellExports(profile)}\n`);
}

const mainHref = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === mainHref) {
    runCli();
}
