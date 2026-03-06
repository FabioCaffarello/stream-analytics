---
name: Api Design
description: Design RESTful APIs following best practices
phases: [P, R]
---

# API Design Skill

## API Surface In This Repo
Current runtime HTTP endpoints are in the server interface layer (`internal/interfaces/http`) and wired from `cmd/server`.

## Design Rules
- Keep handlers thin; business decisions belong in core use cases.
- Return consistent error envelopes aligned with shared problem semantics.
- Prefer stable, explicit resource naming for runtime endpoints.
- Make behavior observable and testable.

## Naming And Contracts
- Use lowercase, noun-oriented paths.
- Keep endpoint purpose explicit (health, snapshot, reload, control).
- Document request/response payloads and status code behavior.

## Versioning Guidance
- For breaking API changes, introduce versioned path or explicit migration strategy.
- For event payload changes, follow contract versioning in `docs/contracts/event-bus.md`.
