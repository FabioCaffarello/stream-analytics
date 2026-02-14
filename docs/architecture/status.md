# Architecture Status

**Status:** Active
**Last updated:** 2026-02-14

## Readiness Note

- Overload policy for VPVR (`ADR-0013`) is **Accepted / Done / Production-ready**.
- Emit/delivery degradation (`compress`, `cadence`, `delta-drop`) is deterministic and does not change builder final state per window.
- `window_close` final snapshot remains mandatory under overload.
- Ack boundary evidence remains `ack-on-commit` only, validated with overload integration tests.
