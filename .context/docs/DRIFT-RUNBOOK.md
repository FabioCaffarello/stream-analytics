# Drift Runbook

**STATUS:** ACTIVE | **last_reviewed:** 2026-02-17

## Goal
- Detect drift between `.context/docs/feature-packs/*` and `docs/**` without editing runtime code.

## Run
- From repo root:
- `make docs-check`
- Optional focused gates:
- `./scripts/check-feature-pack-links.sh`
- `./scripts/check-pack-subjects.sh`
- `./scripts/check-truth-map.sh`

## Failure Interpretation
- `LINKS`:
- Broken/absolute/internal links in docs markdown.
- `TRUTH-MAP`:
- Missing mappings or orphan references in truth-map / execution sequence.
- `FEATURE-PACK`:
- Pack links not pointing to existing `docs/**` files.
- `PACK-SUBJECT`:
- Subject listed in pack not represented in `docs/contracts/event-bus.md`.
- `ADR` / `RFC` header failures:
- Governance debt in `docs/**` metadata sections (outside `.context`).

## Correction Order (Mandatory)
- Step 1: Fix canonical source in `docs/**` first.
- Step 2: Re-run `make docs-check`.
- Step 3: Update `.context/docs/feature-packs/*` to match canonical docs.
- Step 4: Re-run `make docs-check` until green (or only preexisting out-of-scope debt remains).

## Subject Drift Fix Flow
- If `PACK-SUBJECT` fails, inspect the failing subject and file:line.
- Confirm whether subject is valid canonical contract.
- If valid but missing, add/update subject representation in `docs/contracts/event-bus.md`.
- If invalid or deprecated, remove/replace it in the feature-pack.
- Keep placeholder format aligned (`{venue}`, `{instrument}`) only after canonical event exists.

## PR Hygiene
- Keep commits atomic:
- Gate scripts and Makefile integration in one commit.
- `.context/docs` runbook and pack updates in separate commit.
- Include `make docs-check` output in PR notes.
