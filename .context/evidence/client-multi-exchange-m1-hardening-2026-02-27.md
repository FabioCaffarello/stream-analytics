# Client Multi-Exchange M1 Hardening (2026-02-27)

## Objective
Aplicar hardening imediato de runtime (native/web) para reduzir risco de corrida em reconnect/shutdown e melhorar observabilidade de backpressure no web bridge.

## Changes Applied
- `client/src/platform/native/marketdata_native.odin`
  - `reconnect_count` passou a ser cumulativo (nao reseta apos sucesso).
  - adicionado `reconnect_streak` para contagem de tentativas consecutivas de reconnect.
  - adicionada funcao `native_should_stop` com lock para eliminar leitura concorrente de `should_stop`.
  - `reader_thread_proc` agora evita log de read error durante shutdown e sai limpo quando `should_stop`.
  - resubscribe apos reconnect agora usa snapshot de `active_subs` para evitar corrida com subscribe/unsubscribe concorrente.
- `client/src/platform/web/marketdata_web.odin`
  - adicionado foreign proc `ws_drop_count`.
  - `web_metrics.drop_count` agora soma drops do runtime Odin e drops da fila JS (`wsMsgDropCount`), refletindo pressao end-to-end.
- `client/web/odin.js`
  - adicionado export `ws_drop_count()` para expor contador de drops da fila JS para WASM.

## Validation Commands
```bash
make -C client build-native
make -C client build-wasm
make -C client check-core-imports
make -C client check-core
client/scripts/soak-native.sh --duration-sec 45 --sample-sec 2 --log-ms 1000 --restart-server-every-sec 20 --restart-server-max-count 1 --out-dir client/build/soak-m1-quick-20260227 -- --ws-url=ws://127.0.0.1:8080/ws --api-key=prod_key_1
```

## Results
- Build native: `PASS`
- Build wasm: `PASS`
- Core import gate: `PASS`
- Core check: `PASS`
- Soak quick + fault injection: `PASS`
  - `fault_restarts=1`
  - `reconnect_attempt_logs=3`
  - `reconnect_success_logs=1`
  - `subscribe_ack_logs=24`
  - `last_soak_line` com `conn=Connected`, `drop=0`.

## Playwright MCP Check (web)
- Navegacao em `http://127.0.0.1:8090/?ws_perf_debug=1` com conexao WS e ACKs de subscribe.
- Restart de `server` durante sessao ativa:
  - reconnection errors transitorias esperadas durante bootstrap;
  - recuperacao final com reconnect + resubscribe confirmado.

## Evidence Anchors
- Soak summary: `client/build/soak-m1-quick-20260227/summary.txt`
- Soak app log: `client/build/soak-m1-quick-20260227/app.log`
- Soak fault log: `client/build/soak-m1-quick-20260227/fault.log`
