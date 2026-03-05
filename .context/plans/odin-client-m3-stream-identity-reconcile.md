---
status: completed
progress: 100
generated: 2026-03-05
title: Odin Client M3 - Stream Identity & Reconcile
owner: client-platform
workflow: PREVC
phase: C
---

# Odin Client M3 - Stream Identity & Reconcile

> Remover duplicação de streams e reduzir churn de subscribe/unsubscribe/ack.

## Scope
- Canonicalizar identidade de stream por mercado (`venue+symbol`).
- Endurecer `resolve_market_id` e controlar fallback para `channel_sid`.
- Refatorar diff de reconciliação para idempotência e menor churn.

## Tasks
1. owner: `client-platform`
   target: `client/src/core/layers/data_source.odin`
   acceptance: fallback explícito, observável e minimizado
   verify: `make -C client check-core`
2. owner: `client-platform`
   target: `client/src/core/app/reconcile.odin`, `client/src/core/app/layer_marketdata.odin`
   acceptance: sem crescimento de stream_count em troca de TF
   verify: `make -C client check-widgets-online`
3. owner: `qa-automation`
   target: `tests/playwright/scripts/m1-baseline-probes.mjs`
   acceptance: comparação de churn antes/depois disponível
   verify: `node tests/playwright/scripts/m1-baseline-probes.mjs`

## Acceptance Criteria
1. `stream_count` estável em TF switches sem mudança de mercado.
2. `subscribe_ack_count` por TF switch reduzido para budget definido.
3. Invariantes de slot/stream sem duplicação funcional de mercado.

## Dependencies
| Dependency | Type | Status |
|-----------|------|--------|
| `odin-client-m1-observability-baseline.md` | informs | done |
| `odin-client-m2-deterministic-interaction.md` | informs | done |

## Execution Status (2026-03-05)
- [x] Endurecimento de identidade no datasource:
  - `client/src/core/layers/data_source.odin`: `data_source_seed_market_id(...)` + `mid_cache_insert` com update in-place.
- [x] Pre-seed da relação `channel_sid -> market_id` durante reconciliação:
  - `client/src/core/app/reconcile.odin`: chamada de `layers.data_source_seed_market_id(...)` antes do subscribe.
- [x] Evidência M1 atualizada sem crescimento de stream:
  - `click_tf_switch.stream_count_delta = 0`
  - `warnings = []`
- [x] Bug de handshake WS no cliente nativo identificado e corrigido:
  - `client/src/platform/native/ws_client.odin`: correção de socket sombreado no loop de dial (`conn` zero-value causava `Invalid_Argument` no `send_tcp`).
  - Soak nativo passou a atingir `conn=Connected` com HELLO/ACK.
- [x] Gate de soak online estabilizado:
  - `make -C client check-widgets-online`: PASS.
  - `SOAK_MULTI=1 make -C client check-widgets-online`: PASS.
  - Gate agora emite `NOTE` para `stats/heatmap/vpvr` quando zerados em perfil local opcional.
  - Evidência: `.context/evidence/m3-online-soak-playwright-cacheless-2026-03-05.md`.
- [x] Revalidação de invariantes após M5:
  - comparação congelada de baseline M1 sem regressão (`stream_count_delta=0`, `ack_delta` estável).
  - Evidência: `.context/evidence/m5-probes-delta-vs-frozen-baseline-2026-03-05.md`.

## Risks
| Risk | Mitigation |
|------|-----------|
| Mudança de identidade afetar roteamento antigo | rollout por flag + evidência comparativa M1 |
