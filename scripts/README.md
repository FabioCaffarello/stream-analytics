# Scripts — organization and usage

This repository groups helper scripts by purpose to make CI, soak harnesses and utilities discoverable and easy to run.

Top-level layout
- `scripts/ci/` — CI-related wrapper scripts and grouped checks.
  - `scripts/ci/docs/` — documentation guard wrappers (link/header/truth-map/registry).
  - `scripts/ci/guards/` — repository guard scripts (domain invariants, layering checks).
- `scripts/test/` — long-running test harnesses.
  - `scripts/test/soak/` — soak harness wrappers (per-harness orchestration).
  - `scripts/test/pipeline/` — pipeline-oriented soak harnesses (multi-exchange, production C4).
  - `scripts/test/util/` — test utilities used by `Makefile` (smoke, bench-check, bench-budget).
- `scripts/util/` — small helpers used by multiple Makefile targets (listing tests, changed modules).
- legacy scripts (if present) have been moved to `scripts/legacy/` or deleted where replaced.

Conventions
- All runnable scripts should be executable (`chmod +x`) and accept `--help`/`--out-file`/`--go-cache` where applicable.
- Prefer canonical script names (the implementation) over trivial wrappers. The `Makefile` points to canonical paths.
- Use `make` targets where available (preferred) — `Makefile` wires higher-level sequences and invariants checks.

Common commands
Run the local quick loop:

```bash
make quick
```

Run short tests across modules:

```bash
make test-short
```

Run smoke (compose readiness):

```bash
make smoke
# or directly
./scripts/test/util/smoke-compose.sh
```

Run pipeline soak harness (evidence is written under `.context/evidence/`):

```bash
make soak-pipeline
# or directly with flags:
./scripts/test/pipeline/soak-pipeline.sh --out-file .context/evidence/c4-pipeline-soak.txt --go-cache /tmp/go-build
```

Bench helpers:

```bash
make bench-hotpath
make bench-budget
```

Adding or moving scripts
- Place CI checks in `scripts/ci/` or `scripts/ci/docs` / `scripts/ci/guards` as appropriate.
- Place long-running test harnesses in `scripts/test/soak` or `scripts/test/pipeline`.
- Put reusable helpers in `scripts/test/util` or `scripts/util`.
- Make the file executable: `chmod +x scripts/.../your-script.sh`

Troubleshooting
- Pre-commit hooks may auto-fix EOF/trailing spaces. Re-add and re-commit if hooks modify files.
- If `make` triggers a missing tool, run `make install-tools`.

If you want, I can add a short `scripts/CONTRIBUTING.md` or examples per-harness next.
