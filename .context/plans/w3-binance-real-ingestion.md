---
status: filled
generated: 2026-02-12
agents:
  - type: "feature-developer"
    role: "Executar remoção do fake mode e consolidar wiring real Binance"
  - type: "bug-fixer"
    role: "Fechar regressões de runtime/config e erros de integração"
  - type: "test-writer"
    role: "Cobrir parser/endpoint/runtime com testes unitários e smoke"
  - type: "code-reviewer"
    role: "Validar riscos de regressão, robustez e observabilidade"
  - type: "documentation-writer"
    role: "Atualizar docs/configs e exemplos operacionais"
docs:
  - "project-overview.md"
  - "testing-strategy.md"
  - "tooling.md"
phases:
  - id: "phase-1"
    name: "Discovery & Alignment"
    prevc: "P"
  - id: "phase-2"
    name: "Implementation & Iteration"
    prevc: "E"
  - id: "phase-3"
    name: "Validation & Handoff"
    prevc: "V"
---

# W3 Binance Real Ingestion Plan

> Hard cut do fake feeder no consumer e consolidação do modo real Binance via `ws.Manager -> ws.Consumer -> Parse -> Ingest`, com resiliência e observabilidade.

## Task Snapshot
- **Primary goal:** Consumer operar somente em modo real Binance, removendo fake mode, flags/configs relacionadas e branches de runtime não produtivos.
- **Success signal:** `cmd/consumer` sobe com `exchange=binance`, conecta WS real via endpoint combinado, parseia `aggTrade/depthUpdate`, ingere sem erros críticos e com logs operacionais mínimos.
- **Non-goals:** construir novo feeder offline no binário do consumer.
- **Key references:**
  - `cmd/consumer/main.go`
  - `internal/adapters/exchange/binance/parser.go`
  - `internal/adapters/exchange/binance/endpoint.go`
  - `internal/shared/config/schema.go`
  - `internal/shared/config/loader.go`
  - `internal/shared/config/loader_test.go`
  - `deploy/configs/consumer.jsonc`
  - `internal/actors/marketdata/runtime/subsystem.go`
  - `internal/actors/marketdata/runtime/subsystem_test.go`

## Architecture Target
1. Consumer sem `fake`, `fake_rate_ms`, `binance_real` e sem `runFakeFeeder`.
2. `buildParseFuncAndManagerCfg` sempre em real-mode Binance.
3. `ParseMessage` sempre delegado para `binance.ParseMessage`.
4. `EndpointBuilder` sempre delegado para `binance.BuildEndpoint`.
5. `ManagerConfig` válido para combined stream (com `SubscriptionBuilder` nulo/no-op, conforme contrato do manager).

## Scope Breakdown
### W3.1 — Hard Cut (remoção de fake mode)
1. Remover branch de fake no runtime:
- Eliminar `realMode` branch em `cmd/consumer/main.go`.
- Eliminar `mdruntime.MakeRawParseFunc` do wiring do consumer.
- Remover `runFakeFeeder` e chamadas associadas.

2. Simplificar configuração:
- Remover campos `Fake`, `BinanceReal`, `FakeRateMs` de `internal/shared/config/schema.go`.
- Atualizar validações/defaults em `internal/shared/config/loader.go`.
- Atualizar `deploy/configs/consumer.jsonc` removendo chaves legadas.
- Manter `exchange`, `tickers`, `binance_ws_base_url`, knobs de streams/ws.

3. Limpar superfície de CLI/UX dev:
- Remover logs e mensagens referentes a fake/real toggle.
- Garantir erro explícito para `exchange != binance` neste estágio do W3 (se for requisito de rollout atual).

4. Atualizar testes afetados:
- Corrigir/reescrever cenários em `internal/shared/config/loader_test.go` removendo expectativas de fake/binance_real.
- Ajustar testes de runtime que dependam de `MakeRawParseFunc` no caminho do consumer.

### W3.2 — Robust Real Mode (resiliência + observabilidade + smoke)
1. Robustez WS/runtime:
- Verificar parâmetros de reconnect/backoff já expostos no config e validar limites.
- Garantir logs de conexão por bucket/endpoint no fluxo manager/consumer.
- Garantir tratamento consistente de parse error (`problem.Code`) com skip seguro.

2. Normalização de ticker/instrumento:
- Confirmar endpoint em lowercase sem separadores (`btcusdt`).
- Confirmar instrumento de domínio canonicalizado no parser (`naming.CanonicalInstrument`).

3. Cobertura de parsing Binance:
- Consolidar testes para wrapper combined stream `{stream,data}`.
- Cobrir `aggTrade` e `depthUpdate` válidos/inválidos.
- Cobrir mensagens não suportadas com `skip=true`.

4. Smoke local:
- Executar consumer com `deploy/configs/consumer.jsonc` atualizado.
- Confirmar ingest contínuo e ausência de crash-loop.
- Capturar evidência de logs com conexão WS, parse/ingest e métricas básicas de volume/skips.

## Acceptance Criteria
1. Não existe mais referência a `fake_rate_ms`, `binance_real`, `runFakeFeeder` no código de produção.
2. Consumer inicia usando apenas pipeline real (`ws.Manager -> ParseMessage(binance) -> Ingest`).
3. `deploy/configs/consumer.jsonc` não possui chaves de fake mode.
4. Testes unitários de parser/endpoint e testes impactados de config/runtime passam.
5. Smoke local documentado com evidência de conexão WS e ingest real.

## Risks & Mitigations
- **Risco:** quebra de fluxo local de desenvolvimento sem fake.
- **Mitigação:** deixar claro que modo offline, se necessário, deve virar ferramenta separada (`cmd/devfeeder`) fora do consumer.

- **Risco:** regressão de parsing para payloads inesperados.
- **Mitigação:** expandir matriz de testes de parser e manter skip seguro para mensagens desconhecidas.

- **Risco:** ruído de logs sem sinal operacional.
- **Mitigação:** padronizar logs de endpoint/bucket, contagem de processadas/skipped e parse failures por código.

## Working Phases
### Phase 1 — Discovery & Alignment
1. Confirmar contrato final: consumer somente Binance real.
2. Inventariar pontos de remoção no config/runtime/tests.
3. Congelar critérios de aceite e smoke.

**Output:** checklist de arquivos impactados e critérios fechados.

### Phase 2 — Implementation & Iteration
1. Executar W3.1 (hard cut) e estabilizar build/test.
2. Executar W3.2 (robustez + observabilidade).
3. Atualizar testes e docs correlatas.

**Output:** código consolidado + suíte verde no escopo alterado.

### Phase 3 — Validation & Handoff
1. Rodar validação local final (build/test/smoke).
2. Registrar evidências (logs/comandos/resultados).
3. Preparar handoff com riscos residuais e próximos passos.

**Output:** pacote de validação pronto para revisão/merge.

## Validation Plan
1. `go test ./internal/adapters/exchange/binance/...`
2. `go test ./internal/shared/config/...`
3. `go test ./internal/actors/marketdata/runtime/...`
4. `go test ./cmd/consumer/...` (se houver testes)
5. Smoke via compose/config local com consumer apontando para Binance WS.

## Rollback Plan
1. Reverter commits de W3.1/W3.2 se houver instabilidade crítica em runtime.
2. Restaurar temporariamente config/branch anterior do consumer em branch de hotfix (sem reintroduzir mudanças parciais inconsistentes).
3. Reabrir rollout com feature branch menor e validação incremental.

## Evidence & Follow-up
- Evidências esperadas:
  - diff com remoção de fake mode;
  - saída de testes do escopo;
  - logs de conexão WS real + ingest.
- Follow-up opcional:
  - criar `cmd/devfeeder` separado para cenários offline de QA.
