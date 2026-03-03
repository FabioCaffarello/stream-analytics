# Soak Analysis (latest)

- run_dir:         .context/evidence/terminal-v1-soak-20260302-195340
- soak_exit: 0
- client_soak_result: PASS

## Server stability
- goroutines: t0=15, t10m=19, t20m=17, t30m=17
- process_heap_alloc_bytes: t0=5.157712e+06, t10m=8.745104e+06, t20m=4.726592e+06, t30m=8.611496e+06
- ws_clients_connected: t10m=2, t30m=1
- ws_subscriptions_active: t10m=24, t30m=6
- resync_total@t30m: 0
- ws_drops_total{reason=unknown}@t30m: 0

## Processor heap (manual final snapshot)
- processor1 process_heap_alloc_bytes@t30m-manual: 3.708108e+07
- processor2 process_heap_alloc_bytes@t30m-manual: 3.1746712e+07

## Notes
- Soak used fault injection (server restart every 120s, 6x), so memory trend includes reconnect churn.
- Auto-captured processor host-port metrics remain affected by stale host ports (60909/60910); manual snapshots via container-local 127.0.0.1:8082 were added.
- New code changes in this workspace (legacy/v1 path isolation and ws_clients_connected_by_mode metric) were implemented after this soak run and are covered by unit tests, but require a fresh deploy+soak for runtime evidence.
