#!/usr/bin/env node

import test from "node:test";
import assert from "node:assert/strict";
import { dirname, join } from "path";
import { fileURLToPath } from "url";
import { validateBoundednessMatrix } from "./validate_boundedness_matrix.mjs";

const __dirname = dirname(fileURLToPath(import.meta.url));
const fixturesRoot = join(__dirname, "testdata", "boundedness_matrix");

test("boundedness matrix validator passes minimal fixture", () => {
    const result = validateBoundednessMatrix({
        repoRoot: join(fixturesRoot, "minimal_ok"),
        matrixPath: "docs/contracts/boundedness-matrix.yaml",
        enforceFullCatalog: false,
    });

    assert.equal(result.ok, true, `expected minimal fixture to pass: ${result.errors.join(" | ")}`);
    assert.equal(result.driftCount, 0);
    assert.equal(result.checkedEntries, 1);
});

test("boundedness matrix validator detects cap drift fixture", () => {
    const result = validateBoundednessMatrix({
        repoRoot: join(fixturesRoot, "drift_cap"),
        matrixPath: "docs/contracts/boundedness-matrix.yaml",
        enforceFullCatalog: false,
    });

    assert.equal(result.ok, false, "expected drift fixture to fail");
    assert.ok(
        result.errors.some((msg) => msg.includes("cap drift detected")),
        `expected cap drift error, got: ${result.errors.join(" | ")}`
    );
});
