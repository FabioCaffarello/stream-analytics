# M5 Probes Delta vs Frozen Baseline (2026-03-05)

## Context
- Source baseline: `.context/evidence/m1-playwright-baseline-2026-03-05-frozen.json`
- Current baseline: `.context/evidence/m1-playwright-baseline-2026-03-05.json`
- URL: `http://127.0.0.1:8090`
- Cacheless: browser cache disabled + storage cleanup via probe script

## Delta Summary
- keyboard.timeframe_switches_delta: previous=1, current=1, delta=0
- keyboard.stream_count_delta: previous=0, current=0, delta=0
- keyboard.ack_delta: previous=10, current=10, delta=0
- click.timeframe_switches_delta: previous=3, current=3, delta=0
- click.stream_count_delta: previous=0, current=0, delta=0
- click.ack_delta: previous=24, current=24, delta=0
- online.subscribe_ack_count: previous=6, current=6, delta=0
- online.tape_drop_pct: previous=0, current=0, delta=0

## Regression Checks
- Determinism check (click): PASS
- Stream stability check (click): PASS
- Tape drop budget check (`<=20%`): PASS
- Warnings: none

## Conclusion
Post-M5 runtime probes are consistent with frozen baseline for deterministic interaction and stream stability, with tape drop within budget.
