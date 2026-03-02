# Client Multi-Exchange M0 Baseline (2026-02-27)

## Objective
Validar baseline operacional do client multi-exchange com backend escalado, fluxo native/web ativo e fault injection de reconnect.

## Commands Executed
```bash
make up PROCESSOR_REPLICAS=2
make smoke
make -C client run-native-compose NATIVE_FLAGS='--soak-seconds=20 --soak-log-ms=1000 --soak-multi'
client/scripts/soak-native.sh --duration-sec 120 --sample-sec 2 --log-ms 1000 --restart-server-every-sec 30 --restart-server-max-count 2 --out-dir client/build/soak-m0-20260227 -- --ws-url=ws://127.0.0.1:8080/ws --api-key=prod_key_1
```

## Baseline Results
- Stack compose healthy com `processor` em 2 replicas.
- `smoke` aprovado antes e depois de fault injection.
- Native conectou com sucesso e recebeu ACK de subscribe para `binance`, `bybit`, `okx`.
- Web validado via Playwright MCP:
  - conexão WS estabelecida em `http://127.0.0.1:8090/`;
  - ACKs de subscribe recebidos;
  - após restart do server houve reconnect + resubscribe.

## Soak Summary (120s, fault injection enabled)
- Result: `PASS`
- Output dir: `client/build/soak-m0-20260227`
- Samples: `60`
- Fault restarts: `2`
- Reconnect attempt logs: `5`
- Reconnect fail logs: `3`
- Reconnect success logs: `2`
- Subscribe ACK logs: `36`
- RSS first/last/max (KB): `1424 / 704 / 1424`
- RSS growth (KB): `-720`
- Threads first/last/max: `0 / 0 / 0` (limitacao de coleta no ambiente atual)
- Last soak line: `conn=Connected ... streams=8 ... drop=0 ... rc=0`

## Evidence Anchors
- Plan: `.context/plans/client-multi-exchange-hardening-execution.md`
- Soak summary: `client/build/soak-m0-20260227/summary.txt`
- Soak metadata: `client/build/soak-m0-20260227/meta.txt`
- Fault log: `client/build/soak-m0-20260227/fault.log`
- App log: `client/build/soak-m0-20260227/app.log`
