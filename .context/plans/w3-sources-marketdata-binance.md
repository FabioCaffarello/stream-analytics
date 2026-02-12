# W3 Sources/MarketData v1 - Binance Plan

> Implementar adapter Binance (WS) em `internal/adapters/exchange/binance`, contratos doc-first de W3, wiring do `cmd/consumer` para modos `fake` e `binance real`, com robustez e testes determinísticos.

## Task Snapshot
- **Primary goal:** entregar ingestão real de Binance (`aggTrade` e `depth`) para o BC `marketdata`, sem contaminar `core` com regras de exchange.
- **Success signal:** `cmd/consumer` roda em modo fake e modo real, parser Binance converte mensagens para `app.IngestRequest`, testes unitários + integração leve verdes com `-race` nos módulos tocados.
- **Key references:**
  - `/Volumes/OWC Express 1M2/Develop/market-raccoon/docs/rfcs/RFC-0001-robustness-roadmap.md`
  - `/Volumes/OWC Express 1M2/Develop/market-raccoon/docs/adrs/ADR-0004-bus-nats-jetstream.md`
  - `/Volumes/OWC Express 1M2/Develop/market-raccoon/docs/contracts/event-bus.md`

## Scope (W3)
1. **Adapter Binance** (`internal/adapters/exchange/binance`)
- Endpoint builder para streams por ticker.
- Parser WS para `aggTrade` e `depth`.
- Normalização de venue/instrument conforme canonical interno.
- Conversão para `app.IngestRequest` com payloads do `core/marketdata/domain`.

2. **Doc-first contracts**
- RFC-lite W3 em `docs/rfcs/RFC-0004-W3-SOURCES-MARKETDATA-BINANCE.md`.
- ADR com decisão estrutural de canonical symbol/event mapping.

3. **Wiring no consumer**
- Config JSONC/flags para habilitar modo real Binance.
- Conectar `ws.Manager -> mdruntime.SubsystemActor -> BinanceParseFunc -> app.IngestMarketData`.
- Preservar modo fake existente.

4. **Robustez/observabilidade**
- Parse failure: log + skip (sem derrubar subsystem).
- Ws errors com `Kind` estável já propagados como `ChildFailed`.
- Sem goroutine sem cancelamento.

## Out of Scope
- NATS/JetStream real obrigatório em runtime (fica preparado para W4).
- Estratégias avançadas de replay/backfill.
- Auth/autz de fonte.

## Decisions to Record
- Canonical instrument v1: `BTCUSDT` (sem separador), alinhado ao `naming.CanonicalInstrument`.
- Event mapping v1:
  - Binance `aggTrade` -> `marketdata.trade` v1 (`TradeTickV1`)
  - Binance `depthUpdate` -> `marketdata.bookdelta` v1 (`BookDeltaV1`)

## Risks and Mitigations
- **Schema drift Binance**: parser tolerante com skip+log para campos ausentes/invalid.
- **Spam de log em parse errors**: manter logs em `Warn` com contexto mínimo e sem stack.
- **Acoplamento indevido**: adapters dependem de core app/domain; core não depende de adapters.

## Acceptance Criteria
- Adapter Binance parseia com sucesso payloads reais de `aggTrade` e `depthUpdate`.
- Consumer seleciona parse/wiring por config (`fake=true` continua default seguro; modo real habilitável).
- Pipeline segue vivo em parse inválido (skip).
- Testes verdes (incluindo `-race`) em módulos tocados.
- RFC/ADR de W3 versionados.

## Working Phases (PREVC)
### Phase P — Plan
1. Definir contratos de parser, canonicalização e mapeamento de eventos.
2. Criar/ligar plano W3 no workflow MCP.

### Phase R — Review
1. Revisar riscos de schema drift e semântica de idempotência/ordering.
2. Aprovar escopo final para implementação.

### Phase E — Execute
1. Implementar adapter Binance + testes unitários.
2. Ajustar config schema/loader/tests para modo real.
3. Wiring em `cmd/consumer` + testes de integração leve do actor.
4. Publicar RFC-0004 e ADR da decisão estrutural.

### Phase V — Verify
1. Rodar `go test ./...` e `go test -race ./...` nos módulos tocados.
2. Verificar build do consumer e comportamento de modo fake/real por configuração.
3. Registrar evidências no checkpoint do plano.

### Phase C — Confirm
1. Registrar checkpoint final (commit/hash, evidências, limites conhecidos, próximo passo W4).
2. Avançar workflow para conclusão.

## Rollback Plan
- Reverter apenas `internal/adapters/exchange/binance` + wiring do `cmd/consumer` se regressão.
- Manter fallback em fake mode como caminho seguro.

## Evidence & Follow-up
- Artefatos esperados:
  - `internal/adapters/exchange/binance/*`
  - `docs/rfcs/RFC-0004-W3-SOURCES-MARKETDATA-BINANCE.md`
  - `docs/adrs/ADR-0011-*.md` (decisão estrutural)
  - evidências de `go test` e `go test -race`.
- Follow-up W4:
  - substituir publisher/source in-memory por JetStream mantendo contratos atuais.

## Validation Checkpoint (V)

- **Timestamp (UTC):** 2026-02-12T03:46:53Z
- **Base HEAD:** `3773953`
- **Checkpoint:** pending commit

### Test Evidence

Commands executed on touched modules:
1. `go test ./...` in `internal/adapters` -> pass
2. `go test -race ./...` in `internal/adapters` -> pass
3. `go test ./...` in `internal/shared` -> pass
4. `go test -race ./...` in `internal/shared` -> pass
5. `go test ./...` in `internal/actors` -> pass
6. `go test -race ./...` in `internal/actors` -> pass
7. `go test ./...` in `cmd/consumer` -> pass (`[no test files]`)
8. `go test -race ./...` in `cmd/consumer` -> pass (`[no test files]`)

### Docs Updated

- RFC: `/Volumes/OWC Express 1M2/Develop/market-raccoon/docs/rfcs/RFC-0004-W3-SOURCES-MARKETDATA-BINANCE.md`
- ADR: `/Volumes/OWC Express 1M2/Develop/market-raccoon/docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md`

### Known Limits (W3)

- Sem JetStream/NATS real obrigatório.
- Parser Binance cobre `aggTrade` e `depthUpdate` v1; demais eventos são `skip`.
- `cmd/consumer` mantém sequencer in-memory (não distribuído).

## Confirmation (C)

- W3 entregue com adapter Binance separado em `internal/adapters/exchange/binance`.
- `cmd/consumer` suporta modo fake e modo binance real por configuração/flag.
- Próximo passo recomendado: W4 (JetStream integration real + msg-id/dedup no broker).
