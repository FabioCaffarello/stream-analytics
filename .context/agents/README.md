# Agent Handbook

This directory contains operational playbooks for AI agents working in this repository.

## Available Agents
- [Code Reviewer](./code-reviewer.md) — Risk-first review with findings and drift checks.
- [Bug Fixer](./bug-fixer.md) — Defect triage and minimal safe regression fixes.
- [Feature Developer](./feature-developer.md) — Feature delivery with contract and invariant safety.
- [Refactoring Specialist](./refactoring-specialist.md) — Structural improvements without behavior drift.
- [Test Writer](./test-writer.md) — High-value deterministic unit/integration tests.
- [Documentation Writer](./documentation-writer.md) — Docs maintenance anchored to canonical sources.
- [Performance Optimizer](./performance-optimizer.md) — Measured hot-path optimization.
- [Strategic Planner](./strategic-planner.md) — Pareto/SWOT analysis, PRDs, milestones, ADRs, RFCs.

## Standard Playbook Contract
Every agent playbook follows the same sections:
1. `Token Budget Rules`
2. `Mission`
3. `Inputs (arquivos a ler)`
4. `Output Contract`
5. `Non-goals`
6. `Validation Checklist` (max 8 items)

## Token Economy Rules (Global)
- Start from `.context/docs/truth-pack.md` and only then open targeted files.
- Prefer `.context/docs/feature-packs/*.md` as the first scoped context for feature work.
- Do not paste full ADR/RFC content; cite `filename` + section.
- If context is missing, ask explicitly: `cole o trecho X do arquivo Y`.

## Source-of-Truth Navigation
- Context bridge: [`../docs/truth-pack.md`](../docs/truth-pack.md)
- Feature packs: [`../docs/feature-packs/`](../docs/feature-packs/)
- Canonical map: [`../../docs/architecture/TRUTH-MAP.md`](../../docs/architecture/TRUTH-MAP.md)
- Invariants index: [`../../docs/architecture/system-invariants.md`](../../docs/architecture/system-invariants.md)

## Related Resources
- [Documentation Index](../docs/README.md)
- [Agent Knowledge Base](../../AGENTS.md)
- [Contributor Guidelines](../../CONTRIBUTING.md)
