# Project Rules and Guidelines

> Auto-generated from .context/docs on 2026-02-17T17:21:42.055Z

## README

---
type: doc
name: README
description: Documentation index and navigation for .context guides
category: index
generated: 2026-02-12
status: filled
docStatus: ACTIVE
last_reviewed: "2026-02-17"
scaffoldVersion: "2.0.0"
---

# Documentation Index

This folder is the operational knowledge base for contributors and AI agents working in Market Raccoon.

## Core Guides
- [Start Here](./00-START-HERE.md)
- [Project Overview](./project-overview.md)
- [Development Workflow](./development-workflow.md)
- [Testing Strategy](./testing-strategy.md)
- [Tooling & Productivity Guide](./tooling.md)

## Architecture Sources (Primary)
- [Architecture Overview](../../docs/architecture/README.md)
- [System Invariants](../../docs/architecture/system-invariants.md)
- [Event Bus Contract](../../docs/contracts/event-bus.md)
- [Heatmap Architecture](../../docs/architecture/heatmap.md)
- [ADRs](../../docs/adrs)

## Repository Snapshot
- `cmd/` - Binary entrypoints (`consumer`, `processor`, `server`, `store`).
- `internal/core/` - Domain and application use cases by bounded context.
- `internal/actors/` - Actor runtime and subsystem orchestration.
- `internal/adapters/` - Adapter implementations (bus, etc.).
- `internal/interfaces/` - HTTP and boundary-facing interfaces.
- `internal/shared/` - Shared primitives (`problem`, `result`, `envelope`, naming/ids).
- `scripts/` - Workspace utility scripts used by Make targets.

## How To Use This Folder
1. Start with `project-overview.md` to understand architecture and entry points.
2. Follow `development-workflow.md` for day-to-day coding and PR flow.
3. Apply `testing-strategy.md` before requesting review.
4. Use `tooling.md` for local setup, linting, reproducibility, and CI parity.

## Maintenance Rules
- Keep documentation aligned with `Makefile`, `go.work`, and `.github/workflows/ci.yml`.
- When adding/changing a subsystem, update both docs and agent playbooks.
- Treat docs as versioned engineering assets, not optional notes.
