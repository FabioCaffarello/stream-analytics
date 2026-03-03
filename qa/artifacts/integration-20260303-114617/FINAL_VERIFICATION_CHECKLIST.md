# Final Verification Checklist

Run ID: `integration-20260303-114617`
Date: 2026-03-03

## Stack health
- [x] `make up PROCESSOR_REPLICAS=2` executed and stack rebuilt.
- [x] All containers healthy:
```text
market-raccoon-client       Up (healthy)
compose-processor-2         Up (healthy)
compose-processor-1         Up (healthy)
compose-consumer-1          Up (healthy)
market-raccoon-store        Up (healthy)
market-raccoon-server       Up (healthy)
market-raccoon-grafana      Up (healthy)
market-raccoon-timescale    Up (healthy)
market-raccoon-prometheus   Up (healthy)
market-raccoon-clickhouse   Up (healthy)
market-raccoon-nats         Up (healthy)
```

## UI flow + contract cross-check
- [x] Connect/profile + WS connected.
  - Evidence: `logs/console-post-fix.txt:2-5`
- [x] HELLO/ACK handshake completed.
  - Evidence: `hello_ok` + `hello_ack_recv` in `logs/console-post-fix.txt:11,13`
- [x] Subscribe/unsubscribe actions acknowledged.
  - Evidence: `ack_recv op=subscribe|unsubscribe` in `logs/console-post-fix.txt:14-41`
- [x] Resync/metrics HUD/evidence panel/diagnostics button exercised in UI.
  - Evidence screenshots:
    - `screenshots/12-hud-evidence-panel.png`
    - `screenshots/13-hud-panel-visible.png`
    - `screenshots/14-after-copy-diagnostics-click.png`
    - `screenshots/16-post-fix-hud-copy.png`
- [x] Backpressure/limits/reconnect/drops cross-check from backend logs.
  - Evidence: `compose-consumer-1.log` counters show `ws_backpressure_drops_total:0`, `ws_reconnect_total:0`, `depth_gaps_total:0`, `parse_error_by_code:{}`.
- [x] No post-fix `SYS_NOT_FOUND` contract error.
  - Evidence: `post_fix_sys_not_found=0`.
- [x] Legacy toggle default stays OFF in client fallback path.
  - Evidence: `client/web/odin.js:361` now sets `allowLegacy=false` by default.

## Router/shard coherence with 2 processors
- [x] Processor shard 0/2 active and consuming durable `processor-v4-s0`.
  - Evidence: `logs/post-fix/compose-processor-1.log:1,2,8`
- [x] Processor shard 1/2 active and consuming durable `processor-v4-s1`.
  - Evidence: `logs/post-fix/compose-processor-2.log:1,2,8`
- [x] Both processors showing steady heartbeat/progress.
  - Evidence: `aggruntime: processor heartbeat` lines in both processor logs.

## Regression checks
- [x] Go deterministic short suite passed: `make test-short`.
- [x] No WARN/ERROR in core service post-fix logs (server/consumer/processor-1/processor-2).

## Artifacts
- Startup logs: `qa/artifacts/integration-20260303-114617/logs/startup`
- Pre-fix session logs: `qa/artifacts/integration-20260303-114617/logs/final`
- Post-fix logs: `qa/artifacts/integration-20260303-114617/logs/post-fix`
- Console traces: `qa/artifacts/integration-20260303-114617/logs/console-after-ui.txt`, `qa/artifacts/integration-20260303-114617/logs/console-post-fix.txt`
- Screenshots: `qa/artifacts/integration-20260303-114617/screenshots`
- Bug list: `qa/artifacts/integration-20260303-114617/BUG_LIST.md`
