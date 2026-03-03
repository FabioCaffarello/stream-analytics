# Market-Raccoon Integration QA Bug List

Run ID: `integration-20260303-114617`
Environment: `make up PROCESSOR_REPLICAS=2`
Date: 2026-03-03

## MR-QA-001
- Severity: High (correctness / protocol determinism)
- Status: Fixed
- Reproduction steps:
  1. Open `http://localhost:8090`.
  2. Connect and wait for HELLO/ACK + initial subscriptions.
  3. Trigger unsubscribe/reconcile flows from Markets/HUD interactions repeatedly.
  4. Observe client console logs.
- Expected: Unsubscribe is idempotent; if subject is already absent, client should not emit another unsubscribe frame.
- Actual: Client repeatedly emitted unsubscribe for missing subjects and server returned repeated `SYS_NOT_FOUND` errors.
- Suspected root cause (file+line):
  - `client/src/platform/web/marketdata_web.odin:780-788` (unsubscribe fallback derived subject and sent unsubscribe without confirming subject still tracked).
- Fix implemented:
  - Guarded fallback unsubscribe path by checking `find_web_sub_by_subject` before sending frame.
  - File: `client/src/platform/web/marketdata_web.odin:780-790`.
- Exact log excerpts:
```text
qa/artifacts/integration-20260303-114617/logs/console-after-ui.txt:38
[LOG] [ws] Error: code=SYS_NOT_FOUND error_code=ERROR_CODE_NOT_FOUND action_hint=ACTION_HINT_NONE msg=not subscribed to subject "marketdata.trade/binance/BTCUSDT/raw" op=unsubscribe rid=r117

qa/artifacts/integration-20260303-114617/logs/console-after-ui.txt:62
[LOG] [ws] Error: code=SYS_NOT_FOUND error_code=ERROR_CODE_NOT_FOUND action_hint=ACTION_HINT_NONE msg=not subscribed to subject "marketdata.trade/binance/BTCUSDT/raw" op=unsubscribe rid=r129
```
- Post-fix evidence:
```text
pre_fix_sys_not_found=19
post_fix_sys_not_found=0
pre_fix_unsubscribe_sent=24
post_fix_unsubscribe_sent=5
```

## MR-QA-002
- Severity: Medium (policy / safety default)
- Status: Fixed
- Reproduction steps:
  1. Inspect web client runtime default path for legacy fallback toggle.
  2. Confirm no explicit URL param/localStorage/global override is required to set base value.
- Expected: Legacy transport remains OFF by default (explicit opt-in only).
- Actual: Base fallback value was ON in JS bridge (`let allowLegacy = true`), violating required default.
- Suspected root cause (file+line):
  - `client/web/odin.js:361` in `readAllowLegacyWsOverride()`.
- Fix implemented:
  - Changed fallback to `let allowLegacy = false` with explicit comment.
  - File: `client/web/odin.js:361`.
- Exact evidence excerpt:
```text
git diff -- client/web/odin.js
@@ -358,7 +358,7 @@ function readAllowLegacyWsOverride() {
-    let allowLegacy = true; // one release default
+    let allowLegacy = false; // Terminal_V1-only by default; explicit opt-in required.
```

## Notes
- Infra/container startup warnings/errors captured separately (Grafana migration warnings, Timescale init warnings/errors) in:
  - `qa/artifacts/integration-20260303-114617/logs/startup`
  - `qa/artifacts/integration-20260303-114617/logs/final`
  - `qa/artifacts/integration-20260303-114617/logs/post-fix`
- Those infra messages did not produce contract violations in WS UI flow after fixes.
