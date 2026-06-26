#!/usr/bin/env node
// Validates that docs/contracts/boundedness-matrix.md exists and is parseable.
// Fails if the matrix file is missing or malformed.
import { readFileSync } from "fs";
import { resolve } from "path";

const root = new URL("../../", import.meta.url).pathname.replace(/%20/g, " ");
const matrixPath = resolve(root, "docs/contracts/boundedness-matrix.md");

let content;
try {
  content = readFileSync(matrixPath, "utf8");
} catch {
  console.error(
    `[boundedness-matrix] ERROR: matrix file not found at ${matrixPath}`
  );
  process.exit(1);
}

if (!content.includes("## Matrix By Subsystem")) {
  console.error(
    "[boundedness-matrix] ERROR: matrix file is missing '## Matrix By Subsystem' section"
  );
  process.exit(1);
}

const catalogMatch = content.match(/^Catalog:\s*`([^`]+)`/m);
if (!catalogMatch) {
  console.error(
    "[boundedness-matrix] ERROR: matrix file is missing 'Catalog:' header"
  );
  process.exit(1);
}

console.log(
  `[boundedness-matrix] OK — catalog ${catalogMatch[1]} validated`
);
