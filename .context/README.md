# AI-Context (Market Raccoon)

This `.context` directory is the **Systemic Enabler** for Market Raccoon.

All LLMs, Chat Agents, and Code Generation pipelines are routed to read these `docs` and `agents` to strictly obey:
- The `make invariants-check` bounds.
- The `INV-DOM` (Domain Isolation) architectural policies.
- The Single-Sources of Truth mapped inside `docs/architecture/*`.

If you are an AI reading this, you are strictly bound to deterministic code generation using Go `1.23+`, the `hollywood` actor paradigm, and precise JSON/Protobuf contracts defined in `docs/contracts/`.

## Active Workflow
See `workflow/status.yaml` for current lifecycle tracking.
