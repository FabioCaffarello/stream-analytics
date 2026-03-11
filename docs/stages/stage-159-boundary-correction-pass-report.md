# Stage 159 — Foundational Boundary Correction Pass

**Date:** 2026-03-10
**Branch:** codex/s9-legacy-removal-cutover
**Type:** Refactoring — Ubiquitous Language + Naming Alignment
**Risk:** Zero — pure renames, no behavioral change

## Objective

Apply small, safe foundational corrections to reduce boundary violations identified in the S158 architectural audit and semantic boundary audit.

## Changes Applied

### P1-C1: Session_Health → Delivery_Health (Client)

**Problem:** Client types named `Session_Health_*` query `/api/v1/session/dashboard`, which returns **delivery health** data (instrument freshness, resync coverage, artifact availability, venue counts). The name `Session_Health` implies WebSocket connection health, violating the ubiquitous language (N2/N3 naming rules).

**Fix:** Renamed all types, procs, files, and route enum to `Delivery_Health_*`.

| Old Name | New Name |
|----------|----------|
| `Session_Health_Result` | `Delivery_Health_Result` |
| `Session_Health_Freshness` | `Delivery_Health_Freshness` |
| `Session_Health_Resync` | `Delivery_Health_Resync` |
| `Session_Health_Artifact` | `Delivery_Health_Artifact` |
| `Session_Health_Artifact_Coverage` | `Delivery_Health_Artifact_Coverage` |
| `Session_Health_Summary` | `Delivery_Health_Summary` |
| `Session_Health_State` | `Delivery_Health_State` |
| `session_health_parse_json` | `delivery_health_parse_json` |
| `poll_session_health` | `poll_delivery_health` |
| Route `.Session_Health` | Route `.Delivery_Health` |
| `session_health.odin` | `delivery_health.odin` |
| `session_health_test.odin` | `delivery_health_test.odin` |
| `build_session_health.odin` | `build_delivery_health.odin` |

**Files modified:** 8
- `services/delivery_health.odin` (renamed + types)
- `services/delivery_health_test.odin` (renamed + test names)
- `app/app.odin` (Route enum, state struct, poll call)
- `app/actions.odin` (route reference)
- `app/build_delivery_health.odin` (renamed + all procs)
- `app/build_markets.odin` (type + proc reference)
- `app/build_ui.odin` (route reference)
- `app/page_module.odin` (route + proc registrations)

### P1-C2: layer_marketdata.odin → drain_marketdata.odin (Client)

**Problem:** File `app/layer_marketdata.odin` name implies it belongs in `layers/` package. It actually lives in `app/` and its primary proc is `drain_layer_marketdata` — a data drain that syncs data from the layer store into app state.

**Fix:** Renamed file to `drain_marketdata.odin`. No code changes — Odin packages are directory-based, filenames are organizational only.

## Deferred Items (Assessed, Not Actioned)

| Priority | Item | Reason Deferred |
|----------|------|-----------------|
| **P0** | Extract `shared/contracts/` to own module | 50+ files, all core imports — requires dedicated stage |
| **P0** | Remove Entity_World dual-path rendering | High effort, staged removal already tracked |
| **P1** | Move `shared/ownership/` to `core/marketmodel/` | 10 actor imports affected, medium effort |
| **P1** | Move `shared/ticksize/` to adapters | Only used by own test (zero external consumers) — flagged as dead code in shared |
| **P1** | `signal/` vs `signals/` naming unification | Medium effort, requires careful domain analysis |
| **P2** | Backend `StreamState` → `StreamAnomalyState` | Cross-language naming alignment |
| **P2** | Split `layer_strategies.odin` (1,698 LOC) | Intentional cohesion, low ROI |
| **P3** | `legacy_handler.go` in `interfaces/ws/` | Not found — may already be removed |

## Backend Finding: `shared/ticksize/` Dead Code

`internal/shared/ticksize/` has **zero external consumers** — only imported by its own test file. This represents dead code in the shared foundation. Options:
1. Remove entirely if not planned for use
2. Move to `internal/adapters/exchange/common/` if it will be consumed by adapters

## Test Validation

| Package | Tests | Status |
|---------|-------|--------|
| services | 246 | PASS |
| md_common | 512 | PASS |
| app | 472 | PASS |
| layers | 57 | PASS |
| **Total** | **1,287** | **ALL PASS** |

## Guard Rail Compliance

- Cell_Surface_View ceiling (10 fields): HOLDS
- Data_Readiness variants (6 max): HOLDS
- Pure derivation only: HOLDS
- Layer hierarchy (ports → services → layers → app): HOLDS
- services/ never imports layers/ or app/: HOLDS
- layers/ never imports app/: HOLDS
- Zero cyclic dependencies: CONFIRMED

## Impact Assessment

- **Behavioral change:** None — pure rename refactoring
- **API change:** None — all types are client-internal
- **UI change:** Page header now reads "Delivery Health" instead of "Session Health" (more accurate)
- **Test change:** 7 test functions renamed (`test_session_health_*` → `test_delivery_health_*`)
- **Breaking change risk:** Zero — client is a single compilation unit
