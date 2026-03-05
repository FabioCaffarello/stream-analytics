#!/usr/bin/env node

import { existsSync, readFileSync } from "fs";
import { dirname, join } from "path";
import { fileURLToPath } from "url";

const __dirname = dirname(fileURLToPath(import.meta.url));

export const RELEASE_PROFILE_PATH = join(__dirname, "profiles", "release.env");

export const CRITICAL_PROFILE_DEFAULTS = Object.freeze({
    IQ_STRICT: "0",
    IQ_REQUIRE_STATS_CANONICAL: "0",
    IQ_FALLBACK_STRICT: "0",
    IQ_LEGACY_STRICT: "0",
    IQ_ALLOW_BATCHED_FALLBACK: "0",
    IQ_ALLOW_STATS_FALLBACK: "0",
    IQ_ALLOW_UNEXPECTED_SKIPS: "0",
    IQ_WIRE_BUDGET_CHANNELS: "trade,book_snapshot,stats,candle",
    IQ_WIRE_P95_BUDGET_MS: "2000",
    IQ_WIRE_P99_BUDGET_MS: "5000",
    IQ_WIRE_BYTES_P95_BUDGET: "65536",
    IQ_WIRE_BYTES_P99_BUDGET: "131072",
});

const RELEASE_REQUIRED_VALUES = Object.freeze({
    IQ_STRICT: "1",
    IQ_REQUIRE_STATS_CANONICAL: "1",
    IQ_FALLBACK_STRICT: "1",
    IQ_LEGACY_STRICT: "1",
    IQ_ALLOW_BATCHED_FALLBACK: "0",
    IQ_ALLOW_STATS_FALLBACK: "0",
    IQ_ALLOW_UNEXPECTED_SKIPS: "0",
});

function normalizeProfileName(profileRaw, ciRaw) {
    const profile = String(profileRaw || "").trim().toLowerCase();
    if (profile === "release" || profile === "releaselike") {
        return "release";
    }
    if (profile) {
        return profile;
    }
    return envBoolValue(ciRaw, false) ? "release" : "default";
}

function profilePathFor(name) {
    if (name === "release") {
        return RELEASE_PROFILE_PATH;
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

function releaseRelaxViolationsFor(effectiveValues) {
    const violations = [];

    for (const [key, expected] of Object.entries(RELEASE_REQUIRED_VALUES)) {
        const actual = String(effectiveValues[key] ?? "").trim();
        if (actual !== expected) {
            violations.push(`${key}=${actual || "<unset>"} (expected ${expected})`);
        }
    }

    const wireChannels = parseBudgetChannels(effectiveValues.IQ_WIRE_BUDGET_CHANNELS);
    if (wireChannels.length === 0) {
        violations.push("IQ_WIRE_BUDGET_CHANNELS=<empty> (p95/p99 thresholds disabled)");
    }

    const numericThresholds = [
        "IQ_WIRE_P95_BUDGET_MS",
        "IQ_WIRE_P99_BUDGET_MS",
        "IQ_WIRE_BYTES_P95_BUDGET",
        "IQ_WIRE_BYTES_P99_BUDGET",
    ];
    for (const key of numericThresholds) {
        if (parseFinitePositiveNumber(effectiveValues[key]) === null) {
            const current = String(effectiveValues[key] ?? "").trim();
            violations.push(`${key}=${current || "<unset>"} (must be > 0)`);
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
    const profileName = normalizeProfileName(env.IQ_PROFILE, env.CI);
    const sourcePath = profilePathFor(profileName);
    const sourceValues = parseEnvFile(sourcePath);
    const defaults = { ...CRITICAL_PROFILE_DEFAULTS };
    const effectiveValues = { ...defaults, ...sourceValues };

    for (const key of Object.keys(effectiveValues)) {
        if (env[key] !== undefined) {
            effectiveValues[key] = String(env[key]);
        }
    }

    const effectiveProfileName = sourcePath ? profileName : "default";
    const diffs = profileDiffs(defaults, effectiveValues);
    const releaseRelaxViolations = effectiveProfileName === "release"
        ? releaseRelaxViolationsFor(effectiveValues)
        : [];

    return {
        requestedProfile: String(env.IQ_PROFILE || "").trim() || null,
        effectiveProfileName,
        sourcePath,
        defaults,
        sourceValues,
        effectiveValues,
        diffs,
        releaseRelaxViolations,
    };
}
