# AUDIT-PACK-W11 Finalization

## 1) System Snapshot (20 linhas)
- Baseline auditado: código em `internal/`, `cmd/`, `scripts/`, `docs/` na branch atual.
- Escopo desta evidência: PRD-0001 + ADR/RFC citados abaixo, apenas com anchors verificáveis.
- Arquitetura: supervisão por atores com `Guardian` e subsistemas em `internal/actors/runtime/guardian.go` (`startAll`, `stopAll`, `handleChildFailed`).
- Arquitetura: consumo/publicação JetStream encapsulado em adapter (`internal/adapters/jetstream/consumer.go`, `publisher.go`), sem vazar para `internal/core/*`.
- Arquitetura: replay determinístico offline em `internal/shared/replay/` (`player.go`, `recorder.go`, `reader.go`, `writer.go`, `sequencer.go`, `canon.go`).
- Arquitetura: boundary de contratos em `internal/shared/contracts/*`; geração proto em `internal/shared/proto/gen/*` como camada de borda.
- Arquitetura: runtime multi-exchange por configuração em `cmd/consumer/main.go` (`configuredExchanges`, `buildExchangeRuntimes`, `buildBybitRuntime`).
- Arquitetura: estado crítico bounded via `internal/shared/ds/boundedmap.go` aplicado em ingest/aggregation (`internal/core/marketdata/app/ingest.go`, `internal/core/aggregation/app/update_orderbook.go`).
-- Invariante hard (Domain Isolation): guard de imports protobuf fora de `internal/shared/*` em `scripts/ci/check-domain-isolation.sh` + `internal/shared/contracts/import_guard_test.go:TestImportGuard_ProtoImportsStayInSharedBoundary`.
- Invariante hard (Determinismo): encoding canônico e byte-stable em `internal/shared/replay/canon.go` + `internal/shared/replay/replay_test.go:TestDeterministicEncodingStable`.
- Invariante hard (Golden replay): comparação byte-for-byte em `internal/shared/replay/golden_test.go:TestGoldenReplay` e `TestGoldenReplayByteStable50Runs`.
- Invariante hard (Taxonomia de subject): validação estrita em `internal/adapters/jetstream/subject_validation.go` + `internal/adapters/jetstream/subject_validation_test.go`.
- Invariante hard (ACK/NAK/TERM): decisão e aplicação em `internal/adapters/jetstream/consumer.go:ackWithDisposition` + `internal/adapters/jetstream/ingest_conformance_test.go`.
- Invariante hard (test hooks fail-closed): `cmd/consumer/e2e_testhook.go:newE2ERuntime` e `cmd/processor/e2e_testhook.go:newE2ERuntime` validados por `*_e2e_testhook_test.go`.
- Guardrail CI: `Makefile:invariants-check` é pré-requisito de `lint`, `test-workspace`, `test-workspace-race`.
-- Guardrail CI: `scripts/ci/check-domain-isolation.sh` falha em `google.golang.org/protobuf`/`github.com/golang/protobuf` dentro de `internal/core|actors|interfaces`.
-- Guardrail CI: `scripts/ci/check-domain-isolation.sh` falha em `time.Now()` dentro de `internal/core` (exceto `_test.go`).
-- Guardrail CI: `scripts/ci/check-domain-isolation.sh` falha se `internal/shared/replay` importar `github.com/nats-io/nats.go`.
- Guardrail de testes: `internal/shared/metrics/metrics_test.go` protege cardinalidade/labels (inclui assert de ausência de label `instrument` em métricas de outcome).
- Guardrail operacional: `internal/interfaces/http/server.go` expõe `/debug/pprof/*` somente quando `enablePprof` e via `localhostOnly`; caso contrário rota não é registrada.

## 2) Invariant Coverage Matrix

| Invariant | Code anchor (file:symbol) | Test anchor (file:test) | Doc anchor (ADR/RFC/PRD) |
|---|---|---|---|
| Domain isolation (core/actors/interfaces protobuf-free) | `scripts/ci/check-domain-isolation.sh:scan_with_rg` | `internal/shared/contracts/import_guard_test.go:TestImportGuard_ProtoImportsStayInSharedBoundary` | `docs/adrs/ADR-0001-bounded-contexts-and-boundaries.md`, `docs/adrs/ADR-0016-protobuf-contract-layer.md`, `docs/prd/PRD-0001-extreme-runtime.md` |
| Determinism / byte-stable / golden | `internal/shared/replay/canon.go:canonicalLineBytes`; `internal/shared/replay/player.go:Replay` | `internal/shared/replay/replay_test.go:TestDeterministicEncodingStable`; `internal/shared/replay/golden_test.go:TestGoldenReplayByteStable50Runs` | `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md`; `docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md` |
| Subject taxonomy strict | `internal/adapters/jetstream/subject_validation.go:ValidateSubjectTaxonomy` | `internal/adapters/jetstream/subject_validation_test.go:TestValidateSubjectTaxonomy_Invalid`; `TestValidateSubjectPattern_InvalidRootFailsFast` | `docs/adrs/ADR-0014-stream-partitioning-strategy.md`; `docs/rfcs/RFC-0008-W7-nats-jetstream-integration.md` |
| Stream partitioning key | `internal/core/marketdata/domain/instrument_stream.go:StreamID.SequencerInstrumentKey`; `internal/core/marketdata/app/ingest.go:Execute` | `internal/core/marketdata/domain/instrument_stream_test.go:TestInstrumentStream_withMarketType`; `internal/core/marketdata/app/ingest_test.go:TestIngest_StreamIdentityIncludesMarketType` | `docs/adrs/ADR-0014-stream-partitioning-strategy.md`; `docs/prd/PRD-0001-extreme-runtime.md` |
| JetStream semantics ACK/NAK/TERM | `internal/adapters/jetstream/consumer.go:ackWithDisposition`; `internal/adapters/jetstream/ingest_policy.go:ClassifyIngestError` | `internal/adapters/jetstream/ingest_conformance_test.go:TestIngestConformance_AckNakTermGoldenTable`; `internal/adapters/jetstream/consumer_test.go` | `docs/adrs/ADR-0004-bus-nats-jetstream.md`; `docs/rfcs/RFC-0008-W7-nats-jetstream-integration.md` |
| Memory boundedness (`max_instruments` + bounded maps) | `internal/shared/ds/boundedmap.go:NewBoundedMap`; `internal/core/marketdata/app/ingest.go:NewIngestMarketDataWithConfig`; `internal/core/aggregation/app/update_orderbook.go:NewUpdateOrderBookFromEventsWithConfig`; `internal/shared/config/schema.go:MarketDataConfig.MaxInstruments` | `internal/core/marketdata/app/ingest_test.go:TestIngest_boundedStreamsEvictionDeterministicVictim`; `internal/core/aggregation/app/update_orderbook_test.go:TestUpdateOrderBook_boundedBooksEvictionDeterministicVictim`; `internal/shared/config/loader_test.go` | `docs/rfcs/RFC-0006-W5-memory-lifecycle-hardening.md`; `docs/prd/PRD-0001-extreme-runtime.md` |
| Lifecycle bounded (startup/shutdown deterministic) | `internal/actors/runtime/guardian.go:startAll`; `internal/actors/runtime/guardian.go:stopAll`; `cmd/consumer/main.go` shutdown timeout | `internal/actors/runtime/guardian_test.go:TestGuardian_StartStopDeterministicOrder`; `TestGuardian_StopAll_CancelsAndClearsScheduledRetries`; `internal/actors/marketdata/ws/consumer_test.go:TestConsumer_ConnectDisconnectCycle_NoGoroutineLeak` | `docs/adrs/ADR-0012-lifecycle-invariants-leak-prevention.md`; `docs/rfcs/RFC-0006-W5-memory-lifecycle-hardening.md` |
| Test hooks fail-closed | `cmd/consumer/e2e_testhook.go:newE2ERuntime`; `cmd/processor/e2e_testhook.go:newE2ERuntime` | `cmd/consumer/e2e_testhook_test.go:TestNewE2ERuntime_RequiresExplicitTestPosture`; `cmd/processor/e2e_testhook_test.go:TestNewE2ERuntime_RequiresExplicitTestPosture`; `cmd/consumer/e2e_consumer_integration_test.go:TestE2EConsumerFailClosedWithoutTestRunMode` | `docs/prd/PRD-0001-extreme-runtime.md`; `docs/rfcs/RFC-0010-W9-multi-exchange-readiness.md` |

## 3) Risk Register (Top 10)

| # | Sintoma sob escala | Root cause provável | Evidência no repo | Mitigação mínima (sem redesign) | Regressão a monitorar |
|---|---|---|---|---|---|
| 1 | Golden muda sem mudança funcional | Uso indevido de `-update-golden` fora de fluxo controlado | `internal/shared/replay/golden_test.go:shouldUpdateGolden`; `testdata/golden/*` | Rodar CI sem `-update-golden`; exigir diff explícito de `testdata/golden/*` | Divergência frequente em `testdata/golden/` |
| 2 | Replay deixa de ser offline | Introdução de dependência NATS no pacote replay | `scripts/ci/check-domain-isolation.sh` (bloco `replay_nats_violations`) | Manter `invariants-check` obrigatório em `lint/test` | Falha do guardrail ou import novo em `internal/shared/replay/*` |
| 3 | Crescimento de heap em cardinalidade alta | `max_instruments` configurado alto em runtime | `internal/shared/config/schema.go`; `internal/core/marketdata/app/ingest.go`; `internal/core/aggregation/app/update_orderbook.go` | Fixar limites por ambiente em config e monitorar evictions | `ingest_streams_active`/`aggregation_books_active` subindo sem estabilizar |
| 4 | Cardinalidade de métricas explode | Alteração de labels para incluir IDs dinâmicos | `internal/shared/metrics/metrics.go`; `internal/shared/metrics/metrics_test.go` | Preservar testes de labels + sanitização | Crescimento de séries em `ingest_*`/`insights_*` |
| 5 | pprof exposto indevidamente | `enablePprof=true` com rede/proxy mal configurados | `internal/interfaces/http/server.go:registerPprofRoutes/localhostOnly`; `server_test.go` | Manter default desabilitado e bind loopback | Requisições remotas em `/debug/pprof/*` retornando 200 |
| 6 | Replay aborta em fixture heterogêneo | `content_type` desconhecido falha fechado | `internal/shared/replay/replay_test.go:TestFixtureUnknownContentTypeFailsDeterministically` | Pré-validação de fixture antes de execuções longas | Falhas recorrentes de validação no início do replay |
| 7 | Starvation após sequência de falhas | Limiter global de restart entra em cooldown | `internal/actors/runtime/guardian.go:allowGlobalRestart`; `guardian_test.go` | Ajuste fino de janela/limite no config + alerta | `guardian_rate_limited_total` crescente com subsistema parado |
| 8 | Mensagens válidas rejeitadas por taxonomy | Subject fora do padrão formal | `internal/adapters/jetstream/subject_validation.go`; `subject_validation_test.go` | Validar subject no produtor antes de publish | aumento de `ingest_term_total{reason="validation_failed"}` |
| 9 | Ambiente não-teste sobe com hook de teste | Env leak (`E2E_TEST_MODE=1`) em runtime | `cmd/consumer/e2e_testhook.go:newE2ERuntime`; `cmd/processor/e2e_testhook.go:newE2ERuntime` | Sanitizar env em deploy e manter fail-closed | startup fail com mensagem de postura inválida |
| 10 | Auditoria/execução bloqueada por drift documental | Checklists PRD/RFC desatualizados vs código | `docs/prd/PRD-0001-extreme-runtime.md` (itens `[ ]`); `docs/rfcs/EXECUTION-SEQUENCE.md` | Atualizar somente status/evidências sem alterar design | divergência contínua entre docs e testes verdes |

## 4) ADR/RFC Status & Drift

### ADRs 0014-0018 (status atual + impacto)
| ADR | Status no arquivo | Impacto observado no código | Evidência objetiva |
|---|---|---|---|
| `ADR-0014` Stream Partitioning | Proposed | Taxonomia e validação de pattern já aplicadas no adapter JetStream | `internal/adapters/jetstream/subject_validation.go`; `internal/adapters/jetstream/subject_validation_test.go` |
| `ADR-0015` Deterministic Replay | Proposed | Replay/record/golden implementados com FakeClock + sequencer | `internal/shared/replay/*`; `cmd/consumer/main.go:runConsumerReplay`; `cmd/consumer/replay_test.go:TestReplayIngestGolden1000` |
| `ADR-0016` Protobuf Contract Layer | Proposed | Fundação W6-1 ativa (contracts + proto gen), migração runtime ainda parcial | `internal/shared/contracts/*`; `internal/shared/proto/gen/*`; `docs/adrs/ADR-0016-protobuf-contract-layer.md` (amendment W6-1) |
| `ADR-0017` Multi-Exchange | Proposed | Runtime binance+bybit e testes multi-exchange existentes | `cmd/consumer/main.go:buildExchangeRuntimes`; `cmd/consumer/e2e_consumer_integration_test.go:TestE2EConsumerMultiExchange` |
| `ADR-0018` Actor Topology | Proposed | Guardião com ordem determinística/start-stop e chaves dinâmicas de subsistema | `internal/actors/runtime/guardian.go`; `internal/actors/runtime/guardian_test.go` |

### Drift: proposed vs implemented
- **Proposed mas já implementado (evidência forte):** ADR-0014, ADR-0015, ADR-0017.
- **Proposed e parcialmente implementado:** ADR-0016 (foundation pronta, runtime wire migration ainda não completa), ADR-0018 (mecânica principal presente; documento permanece Proposed).

### Drift: accepted vs não implementado (ou sem evidência operacional clara)
- **ADR-0006 (Accepted) com lacuna material:** não há adapter de cold-path implementado em `internal/adapters/db/` (diretório vazio); apenas hot-path está explícito em `internal/core/aggregation/ports/ports.go`.
- **ADR-0004 (Accepted) implementado com evidência forte:** `internal/adapters/jetstream/*` + testes de integração (`consumer_integration_test.go`, `publisher_integration_test.go`).

### Recomendação de patch documental
- **Patch de texto ADR (não apenas status):** ADR-0016 e ADR-0018 (parcialidade explícita em escopo/limites atuais).
- **Marcar como Accepted (com changelog curto):** ADR-0014, ADR-0015, ADR-0017, condicionando a manter os testes/guards já existentes.
- **Abrir ADR patch de gap:** ADR-0006 para explicitar que cold-path segue pendente de implementação (não tratado neste patch).

## 5) PRD Gaps Checklist (somente verificável)

| Item PRD-0001 ainda lacunado | O que falta (teste/métrica/doc) | Onde adicionar (arquivo alvo) | Teste/cheque esperado |
|---|---|---|---|
| `B.5` regressão de performance `<5%` (`docs/prd/PRD-0001-extreme-runtime.md:448`) | Falta benchmark de ingest/aggregation no código (não há `BenchmarkIngest/BenchmarkApplyDelta`) | `internal/core/marketdata/app/ingest_bench_test.go`; `internal/core/aggregation/domain/orderbook_bench_test.go` | `go test -run '^$' -bench BenchmarkIngest ./internal/core/marketdata/app` |
| `B.6` soak 30min como gate (`docs/prd/PRD-0001-extreme-runtime.md:487`) | Há script (`scripts/test/soak/soak-test.sh`), mas sem integração em gate padrão | `Makefile` (alvo opcional `soak-check`) + `docs/rfcs/EXECUTION-SEQUENCE.md` | `bash scripts/test/soak/soak-test.sh` com critérios documentados |
| Checkpoint PRD `go test -race ./...` (`docs/prd/PRD-0001-extreme-runtime.md:447`) | Doc conflita com estratégia workspace-safe (`make test-root` evita root `go test ./...`) | `docs/prd/PRD-0001-extreme-runtime.md` seção de validação | `make test-workspace-race` como comando canônico |
| Checklist W8 no PRD (`docs/prd/PRD-0001-extreme-runtime.md:616-620`) | Código/testes existem, mas checklist ainda não reflete evidência | `docs/prd/PRD-0001-extreme-runtime.md` | Referenciar `cmd/consumer/replay_test.go:TestReplayIngestGolden1000` e `internal/shared/replay/golden_test.go` |
| Checklist W4/W10 (`docs/prd/PRD-0001-extreme-runtime.md:444-446`) | Endpoints/testes existem; lacuna é documentação de aceite atualizada | `docs/prd/PRD-0001-extreme-runtime.md` | Referenciar `internal/interfaces/http/server_test.go` (`metrics`, `pprof disabled=404`, `enabled=200`) |
| EXECUTION-SEQUENCE W6/W7/W8/W9 mantém vários `[ ]` sem espelho no estado real | Falta reconciliar checklist com evidências de testes já presentes | `docs/rfcs/EXECUTION-SEQUENCE.md` | Cada item com link para teste correspondente no repo |

## 6) Go/No-Go Criteria (curto)

| Critério binário | Comando de validação |
|---|---|
| 1. Domain isolation/proto-free + determinism guards ativos | `make invariants-check` |
| 2. Replay fixture IO determinístico (linhas inválidas/ctype desconhecido falham fechado) | `go test ./internal/shared/replay -run 'TestFixtureReaderInvalidLineFailsDeterministically|TestFixtureUnknownContentTypeFailsDeterministically' -count=1` |
| 3. Golden replay byte-for-byte estável | `go test ./internal/shared/replay -run 'TestGoldenReplay|TestGoldenReplayByteStable50Runs' -count=1` |
| 4. Golden ingest 1000 envelopes estável | `go test ./cmd/consumer -run TestReplayIngestGolden1000 -count=1` |
| 5. ACK/NAK/TERM semântica conformance | `go test ./internal/adapters/jetstream -run TestIngestConformance_AckNakTermGoldenTable -count=1` |
| 6. pprof fail-closed e localhost-only | `go test ./internal/interfaces/http -run 'TestServer_Pprof_DisabledReturns404|TestServer_Pprof_EnabledLocalhostAllowed|TestServer_Pprof_EnabledRemoteForbidden' -count=1` |
| 7. /metrics responde e labels críticos sem instrument | `go test ./internal/interfaces/http -run TestServer_Metrics_ExposesPrometheusFormat -count=1 && go test ./internal/shared/metrics -run 'TestIngestOutcomeMetrics_ReasonOnlyNoInstrumentLabel|TestBusDropSubscriberLabelBounded' -count=1` |
| 8. Bounded maps com eviction determinística | `go test ./internal/core/marketdata/app -run TestIngest_boundedStreamsEvictionDeterministicVictim -count=1 && go test ./internal/core/aggregation/app -run TestUpdateOrderBook_boundedBooksEvictionDeterministicVictim -count=1` |

## Open TODOs

- Comando executado: `rg "TODO|FIXME" -n docs internal cmd scripts`
- Resultado: nenhum `TODO`/`FIXME` encontrado nesses diretórios no estado atual.
