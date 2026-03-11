# Documentation Curation — Historical Archive Strategy

**Date:** 2026-03-10
**Type:** Curation Audit
**Scope:** docs/stages, RFCs, audits, all T4 historical documents

---

## 1. Inventory Summary

| Category | Count | Location |
|----------|-------|----------|
| Stage reports | 135 | `docs/stages/` (S010–S158) |
| ADRs | 36 | `docs/adrs/` |
| PRDs | 6 | `docs/prds/` |
| RFCs (active) | 4 | `docs/rfcs/RFC-0012–0015` |
| RFCs (completed) | 11 | `docs/rfcs/RFC-0001–0011` |
| RFCs (archived misc) | 3 | `docs/rfcs/archive/`, `EXECUTION-SEQUENCE.md` |
| Audits (current) | 7 | `docs/audits/*-2026-03-10.md` |
| Audits (superseded) | 5 | `docs/audits/` (W4–W11 era) |
| Architecture docs | 19 | `docs/architecture/` |
| Wire contracts | 8 | `docs/contracts/` |

**Total documents:** ~200+

---

## 2. Classification Framework

### Category A — Histórico Relevante (Auxiliary Reference)

Stage reports that introduced **architectural landmarks** still cited in ADRs, PRDs, or the Authority Map. These document *why* the system evolved a certain way and are useful for onboarding and archaeological debugging.

### Category B — Histórico Secundário (Delivery Records)

Incremental delivery reports. Useful only for provenance ("what happened in stage N?"). No architectural content not already captured in T1 docs. Safe to mark as archive-only.

### Category C — Obsoleto (Superseded)

Documents whose content has been fully superseded by newer T1 docs or whose advice is no longer applicable. Candidates for archive banner.

---

## 3. Stage Report Classification

### Category A — Histórico Relevante (22 reports)

These introduced architectural foundations or landmark decisions still reflected in T1 docs.

| Stage | Title | Relevance |
|-------|-------|-----------|
| **S010** | Simulation Engine | Execution BC origin (ADR-0003) |
| **S011** | Execution Control Plane | GovernedExecutor design (PRD-0001) |
| **S013** | Cold Analytics Store | ClickHouse architecture (ADR-0006) |
| **S020** | First Client Vertical Slice | Client bootstrap architecture origin |
| **S024** | Legacy Removal Cutover | Store consolidation pattern |
| **S032** | Truth Alignment Canonical Protocol | Protocol canonicalization approach |
| **S052** | UI Shell Architecture | Client shell origin (ADR-0022) |
| **S062** | Legacy Cutover Layer Compat Removal | Major legacy removal precedent |
| **S065** | Backend Capability Audit | BC boundary validation |
| **S080** | Scene State Layout Determinism | Layout determinism model |
| **S097** | Legacy Retirement Performance Tightening | Final legacy removal |
| **S100** | Legacy Store Mirror Retirement | Dead code elimination precedent |
| **S105** | Dashboard Workspace Architecture | Split-tree origin (ADR-0025) |
| **S108** | Widget Host Data Context Contracts | Widget contract origin (ADR-0027/0028) |
| **S117** | Dashboard Operating Model | ADR-0031 origin |
| **S139** | Chart Interaction Foundation | Interaction model origin |
| **S143** | Stream Health Desync Model Hardening | ADR-0032 origin |
| **S147** | Orderflow Domain Blueprint | ADR-0033 origin |
| **S150** | Product Architecture Consolidation II | Major consolidation landmark |
| **S151** | Stream Health Recovery Completion | ADR-0034 origin |
| **S155** | Orderflow Contract Architecture | ADR-0035 origin |
| **S158** | Consolidation Product Guard Rails | Current guard rails baseline |

### Category B — Histórico Secundário (113 reports)

All remaining stage reports (S012, S014–S019, S021–S023, S025–S031, S033–S047, S051, S053–S061, S063–S064, S066–S079, S081, S085–S096, S098–S099, S101–S104, S106–S107, S109–S116, S118–S138, S140–S142, S144–S146, S148–S149, S152, S154, S156–S157).

These are incremental delivery records. Their architectural decisions are fully captured in ADRs and architecture docs.

---

## 4. RFC Classification

### Category A — Histórico Relevante

| RFC | Title | Reason |
|-----|-------|--------|
| RFC-0001 | Robustness Roadmap | Program origin story — explains why the hardening phases exist |

### Category B — Histórico Secundário

| RFC | Title | Reason |
|-----|-------|--------|
| RFC-0002–0010 | W1–W9 weekly RFCs | Each produced an ADR; RFC content fully captured |
| RFC-0011 | Product Parity MM | Superseded by PRD-0003 validated status |

### Category C — Obsoleto

| RFC | Title | Reason |
|-----|-------|--------|
| `EXECUTION-SEQUENCE.md` | Execution sequencing | Fully superseded by ADR-0003 + S010/S011 |
| `archive/W5.1-SWEEP-THROTTLING.md` | Sweep throttling | Implemented in ADR-0013; sweep details outdated |
| `archive/ADR-REVISIONS-patch-plan.md` | ADR revision plan | One-time migration completed |

### Active (keep as T3)

RFC-0012, RFC-0013, RFC-0014, RFC-0015 — still draft/active proposals.

---

## 5. Audit Classification

### Current (keep)

All `*-2026-03-10.md` audits are current snapshots. Keep as T4.

### Category C — Obsoleto

| Audit | Reason |
|-------|--------|
| `W4-W5-AUDIT.md` | Fully superseded by 2026-03-10 audits |
| `AUDIT-PACK-W11-finalization.md` | Fully superseded |
| `DRIFT-REPORT-W11.md` | Fully superseded |
| `PRE-STAGE9-ARCHITECTURAL-AUDIT-2026-03-06.md` | Superseded by 2026-03-10 architectural audit |
| `PRE-STAGE9-DRIFT-MATRIX-2026-03-06.md` | Superseded by 2026-03-10 audits |

---

## 6. Proposed Header Banners

Add a classification banner to the **first line** of each historical document.

### Category A — Histórico Relevante

```markdown
> **📋 HISTORICAL — Auxiliary Reference** | This stage report documents an architectural landmark. The authoritative specification is in the linked T1 doc(s). Consult for provenance and rationale only.
```

### Category B — Histórico Secundário

```markdown
> **📋 HISTORICAL — Delivery Record** | This is an incremental delivery report. It does not govern architecture. For current specifications, consult the [Authority Map](../architecture/AUTHORITY-MAP.md).
```

### Category C — Obsoleto

```markdown
> **⚠️ SUPERSEDED** | This document has been fully superseded. See [replacement doc] for current specifications.
```

### Completed RFCs (RFC-0001–0011)

```markdown
> **📋 HISTORICAL — Completed RFC** | This RFC was accepted and its decisions are captured in [ADR-NNNN]. Consult the ADR for the authoritative specification.
```

---

## 7. Documents That Merit Links in Authority Map

The Authority Map currently treats all 135 stage reports as undifferentiated T4. Recommend adding a "Landmark Stage Reports" subsection under T4:

```markdown
### Landmark Stage Reports (Auxiliary Reference)

For provenance on how major architectural decisions evolved:

| Stage | Landmark | T1 Authority |
|-------|----------|--------------|
| S010 | Simulation Engine origin | ADR-0003 |
| S020 | Client bootstrap architecture | architecture/README.md §Client |
| S052 | UI Shell origin | ADR-0022 |
| S065 | Backend BC boundary audit | ADR-0001 |
| S097 | Final legacy retirement | architecture/README.md |
| S105 | Split-tree workspace origin | ADR-0025 |
| S108 | Widget host contract origin | ADR-0027 |
| S117 | Dashboard operating model | ADR-0031 |
| S143 | Stream health model origin | ADR-0032 |
| S147 | Orderflow blueprint origin | ADR-0033 |
| S151 | Health recovery completion | ADR-0034 |
| S155 | Orderflow contracts origin | ADR-0035 |
| S158 | Current guard rails baseline | system-invariants.md |
```

---

## 8. Recommendations

### Do Now (low effort, high clarity)

1. **Add banners to all 135 stage reports** — Scripted bulk operation. Category A gets "Auxiliary Reference", Category B gets "Delivery Record".
2. **Add banners to completed RFCs (0001–0011)** — Each gets "Completed RFC → ADR-NNNN" link.
3. **Add banners to superseded audits** — 5 old audits get "SUPERSEDED" banner with link to replacement.
4. **Add "Landmark Stage Reports" section to AUTHORITY-MAP.md** — 13 entries linking back to T1.

### Do Later (medium effort)

5. **Move completed RFCs to `docs/rfcs/archive/`** — RFC-0001 through RFC-0011 are completed; physically move to archive folder alongside existing W5.1 and ADR-REVISIONS files.
6. **Create `docs/stages/README.md`** — Index page explaining stage report conventions, the A/B classification, and how to navigate. This prevents newcomers from treating stage reports as authoritative.

### Do Not Do

- **Do NOT delete any stage reports** — They are delivery receipts with provenance value.
- **Do NOT merge stage reports** — Each is a self-contained snapshot; merging destroys temporal accuracy.
- **Do NOT move stage reports to a different directory** — Existing links and conventions reference `docs/stages/`.
- **Do NOT add banners to T1/T2 docs** — These are actively maintained and don't need classification markers.

---

## 9. Expected Outcome

| Metric | Before | After |
|--------|--------|-------|
| Docs with classification banner | 0 | ~155 (135 stages + 11 RFCs + 5 audits + misc) |
| Stage reports linked in Authority Map | 0 | 13 landmarks |
| Completed RFCs in archive/ | 2 | 13 |
| Newcomer can distinguish T1 from T4 | Requires reading AUTHORITY-MAP | Banner visible on first line |

---

## Appendix: Full Category B Stage List

S012, S014, S015, S016, S017, S018, S019, S021, S022, S023, S025, S026, S027, S028, S029, S030, S031, S033, S034, S035, S036, S037, S038, S039, S040, S041, S042, S043, S044, S045, S046, S047, S051, S053, S054, S055, S056, S057, S058, S059, S060, S061, S063, S064, S066, S067, S068, S069, S070, S071, S072, S073, S074, S076, S077, S078, S079, S081, S085, S086, S087, S089, S090, S091, S092, S094, S095, S096, S098, S099, S101, S102, S104, S106, S107, S109, S110, S112, S113, S115, S116, S118, S119, S120, S121, S122, S123, S124, S125, S126, S127, S128, S129, S130, S132, S133, S134, S135, S136, S137, S138, S140, S141, S142, S144, S145, S146, S148, S149, S152, S154, S156, S157
