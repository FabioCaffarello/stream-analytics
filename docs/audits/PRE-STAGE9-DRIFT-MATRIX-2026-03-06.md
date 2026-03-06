# Pre-Stage 9 Drift Matrix

**Status:** Current
**Last updated:** 2026-03-06

## As-Implemented vs Intended

| Area / Component | Intended Architecture | As Implemented Before Hardening | Drift | Severity | Corrective Action |
| --- | --- | --- | --- | --- | --- |
| Executor intake boundary | `cmd/executor` consumes only `strategy.intent.*`. | `cmd/executor/bootstrap.go` and `deploy/configs/executor.jsonc` tolerated `strategy.>` filters. | Execution BC could subscribe beyond the official contract family. | critical | Narrowed helper logic and deploy config to `strategy.intent.>` only. |
| Portfolio intake boundary | `cmd/portfolio` consumes only `execution.event.*`. | `cmd/portfolio/bootstrap.go` and `deploy/configs/portfolio.jsonc` tolerated `execution.>` filters. | Portfolio BC could subscribe beyond the execution event contract family. | critical | Narrowed helper logic and deploy config to `execution.event.>` only. |
| Delivery envelope governance | Delivery recognizes canonical lifecycle streams and canonical BC ownership. | `internal/core/delivery/domain/envelope_policy.go` omitted `strategy.intent`, `execution.event`, `portfolio.state` and labeled signal ownership as `signals`. | Delivery governance lagged behind the frozen semantic model. | critical | Added lifecycle types and normalized signal ownership to `signal`. |
| Delivery backpressure policy | Canonical lifecycle events should outrank legacy compatibility streams and use reachable type keys. | `internal/core/delivery/domain/backpressure_policy.go` prioritized `signal.composite` but not the canonical lifecycle chain; some keys were version-suffixed and unreachable. | Pressure handling did not reflect runtime semantic priorities. | high | Added canonical lifecycle priorities and corrected type keys. |
| Server delivery filters | Server observability/delivery should see `signal.event`, `strategy.intent`, `execution.event`, and `portfolio.state`. | `deploy/configs/server.jsonc` only subscribed to marketdata/aggregation/insights plus `signal.event`. | Docs promised more lifecycle visibility than runtime had. | high | Added lifecycle filter subjects to top-level and delivery NATS config. |
| Lifecycle consumer docs | Current consumers should describe the currently wired chain, not future persistence plans. | Contracts/registry docs listed storage as a present consumer for `strategy.intent`, `execution.event`, and `portfolio.state`. | Documentary truth overstated current persistence/read-model readiness. | high | Removed storage from current consumer lists and documented it as not yet wired. |
| Subject examples | Lifecycle examples should match actual subject taxonomy. | One subject validation test/doc example used `portfolio.state.v1.global.GLOBAL`. | Example contradicted the real `{venue}.{instrument}` pattern. | medium | Updated example/test to `portfolio.state.v1.binance.BTCUSDT`. |
| Historical stage docs | Stage docs should be explicitly historical once superseded. | Stage 1 and Stage 2 docs could still be read as current-state guidance. | Old transitional posture could be mistaken for the present architecture. | medium | Added explicit historical notes and updated sequencing pointers. |
| Boundedness truth anchor | Documentation anchors must point to live code so gates stay meaningful. | `docs/contracts/boundedness-matrix.md` pointed to a stale line for `delivery.session_outbound_queue_size`. | Invariants gate failed on documentary drift. | medium | Repointed the anchor to the actual default assignment in `internal/shared/config/loader.go`. |
| Legacy signal compatibility | `signal.composite` remains compatibility-only and must not regain operational authority. | Delivery/router/session compatibility branches still exist. | Residual legacy surface remains in the tree. | low | Left in place as controlled residue; documented as compatibility-only. |
| Ownership naming residue | Ownership salts should reflect current BC names, but continuity matters. | `SubsystemStrategist` remains the ownership salt in shared ownership logic. | Naming drift persists for continuity reasons. | low | Kept unchanged; recorded as a deliberate migration concern, not opportunistic cleanup. |

## Net Effect

The dominant drifts before hardening were not deep domain-model violations. They were boundary, governance, and truth-surface mismatches that could have undermined Stage 9 by making the architecture look cleaner on paper than it was in runtime and config.

After hardening:

- canonical intake is explicit,
- delivery governance knows the lifecycle chain,
- server observability can actually see that chain,
- docs/registry describe the implemented truth,
- and invariant gates are green again.
