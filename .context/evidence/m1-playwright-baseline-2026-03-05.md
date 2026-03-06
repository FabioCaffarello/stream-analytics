# M1 Playwright Baseline (2026-03-05)

- URL: `http://127.0.0.1:8090`
- Cache disabled: `true`
- Storage reset: `true`

## Scenarios

### Cold Start
- tf: 2, stream_count: 1, ack: 0
- interactive_count(DOM): 0
- screenshot: `/Volumes/OWC Express 1M2/Develop/market-raccoon/.context/evidence/screenshots/m1/2026-03-05-cold-start.png`

### Online Baseline
- tf: 2, stream_count: 1, ack: 6
- tape drop rate: 0% (drop=0, parse=0)
- screenshot: `/Volumes/OWC Express 1M2/Develop/market-raccoon/.context/evidence/screenshots/m1/2026-03-05-online-baseline.png`

### Keyboard TF Switch (key=2)
- tf delta: -1, timeframe_switches delta: 1
- ack delta: 10, stream_count delta: 0
- screenshot: `/Volumes/OWC Express 1M2/Develop/market-raccoon/.context/evidence/screenshots/m1/2026-03-05-keyboard-tf-switch.png`

### Click TF Switch (3 clicks)
- clicks: 3
- timeframe_switches delta: 3
- stream_count delta: 0
- ack delta: 24
- screenshot: `/Volumes/OWC Express 1M2/Develop/market-raccoon/.context/evidence/screenshots/m1/2026-03-05-click-tf-switch.png`

## Warnings
- none

## Files
- json: `/Volumes/OWC Express 1M2/Develop/market-raccoon/.context/evidence/m1-playwright-baseline-2026-03-05.json`
- md: `/Volumes/OWC Express 1M2/Develop/market-raccoon/.context/evidence/m1-playwright-baseline-2026-03-05.md`
