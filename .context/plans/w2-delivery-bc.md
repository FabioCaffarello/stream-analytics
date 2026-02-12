# W2 Delivery BC - Router + Session (InMemory) Plan

> Implementar o bounded context Delivery com Subject VO, RouterActor + SessionActor, transporte WebSocket e roteamento por InMemoryBus, sem NATS real, sem novas bibliotecas.

## Task Snapshot
- **Primary goal:** Entregar o fluxo client WS -> subscribe/unsubscribe/getrange -> RouterActor -> broadcast determinístico por Subject.
- **Success signal:** Testes unitários e de atores cobrindo invariantes de roteamento/refcount/lifecycle, com `go test ./...` e `go test -race ./...` verdes.
- **Key references:**
  - `/Volumes/OWC Express 1M2/Develop/market-raccoon/docs/rfcs/RFC-0001-robustness-roadmap.md`
  - `/Volumes/OWC Express 1M2/Develop/market-raccoon/docs/adrs/ADR-0007-delivery-ws-sessions.md`
  - `/Volumes/OWC Express 1M2/Develop/market-raccoon/docs/contracts/event-bus.md`

## Codebase Context
- `internal/core/delivery` já possui skeleton de sessão/subscription.
- `internal/actors/runtime` (Guardian) já está hardened para shutdown/retry semantics.
- `internal/adapters/bus/inmemory.go` já entrega fanout in-memory de `envelope.Envelope`.
- `internal/interfaces/http` já expõe runtime endpoints; Delivery deve entrar via interface WS dedicada.

## Scope (W2)
1. **Core Delivery (`internal/core/delivery`)**
- Criar `domain.Subject` como value object canônico (`streamType/eventType/venue/symbol/timeframe`).
- Ajustar sessão para subscrever por `Subject` (não string solta).
- Criar app layer para comandos `Subscribe`, `Unsubscribe`, `GetRange` e contratos de portas.

2. **Actors Delivery (`internal/actors/delivery/runtime`)**
- `RouterActor`:
  - mantém `map[Subject]PIDSet`.
  - mantém refcount por subject.
  - recebe envelopes de bus por sentinel message e faz broadcast para sessões.
- `SessionActor`:
  - controla lifecycle da conexão.
  - parseia mensagens do cliente (`subscribe`, `unsubscribe`, `getrange`).
  - envia comandos para `RouterActor`.
  - em desconexão, executa cleanup e unsubscribe-all.

3. **Interfaces (`internal/interfaces/ws`)**
- `net/http` + `gorilla/websocket` (já disponível no repo).
- servidor WS apenas faz upgrade e delega sessão para actor.
- sem regra de negócio no transporte.

4. **Adapters (`internal/adapters/bus`)**
- reusar `InMemoryBus` como fonte de envelopes.
- opcional W2: `TopicBus` leve para helper de match por subject.

5. **Wiring (`cmd/server`)**
- manter `cmd/*` apenas como composição de dependências.
- plugar factory de delivery no Guardian sem mover regra para `cmd`.

## Out of Scope (W2)
- JetStream/NATS real, durable consumer e ack policy.
- autenticação/autorização real.
- persistência histórica real para `getrange` (apenas stub/in-memory port).
- mudanças em `internal/core/*` de outros BCs.

## Contracts / Ports to Add
- `internal/core/delivery/ports/ports.go`
- Interfaces previstas:
  - `RangeStore` para query `GetRange`.
  - `ClientSink` para escrita em sessão (ack/error/data).
  - `EnvelopeSource` (ou equivalente no actor config) para consumo in-memory desacoplado.

## Risks and Decisions
### Decisões
- Boundary WS em `internal/interfaces/ws` para isolar transporte de HTTP runtime.
- Estado mutável apenas dentro de mailbox de actor.
- Erros retornam `problem.Problem` ou erro tratado no handler; sem panic.

### Riscos
- **Leak de goroutine de leitura WS**
  - Mitigação: goroutine só envia sentinelas para mailbox; encerramento por contexto + close conn.
- **Broadcast para sessão lenta degradar router**
  - Mitigação: router apenas `Send`; backpressure local no SessionActor.
- **Refcount inconsistente em unsubscribe concorrente**
  - Mitigação: todas mutações seriadas pelo RouterActor.
- **Shutdown com mensagens tardias do bus**
  - Mitigação: sentinelas ignoradas após estado de parada.

## Acceptance Criteria (W2)
- subscribe/unsubscribe mantém refcount correto por subject.
- último unsubscribe remove subject do mapa e cleanup associado.
- broadcast entrega somente para sessões inscritas.
- parse de mensagens inválidas gera erro protocolado (sem panic).
- shutdown limpa sessão/router sem reinício indevido.
- testes determinísticos cobrindo unit + actor tests + `-race` verde.

## Test Plan and Invariants
1. **Domain tests**
- `Subject.Parse/Normalize`:
  - invariante: representação canônica estável e válida.
- `Session.Subscribe/Unsubscribe`:
  - invariante: unicidade por subject; unsubscribe inexistente -> `NOT_FOUND`.

2. **App tests**
- comandos subscribe/unsubscribe/getrange:
  - invariante: validações retornam `problem` previsível.
  - invariante: nenhum comando causa panic.

3. **Router actor tests**
- subscribe refcount:
  - invariante: primeira inscrição cria entrada; demais incrementam.
- unsubscribe refcount:
  - invariante: último remove subject e set de sessões.
- broadcast:
  - invariante: apenas inscritos recebem payload.
- shutdown:
  - invariante: sentinelas tardias não mutam estado parado.

4. **Session actor tests**
- parse de `subscribe/unsubscribe/getrange`:
  - invariante: comando válido vira mensagem correta para router.
- parse inválido:
  - invariante: erro de protocolo enviado ao cliente.
- disconnect:
  - invariante: cleanup e unsubscribe-all garantidos.

5. **Interface WS tests**
- upgrade + spawn session.
- malformed JSON retorna erro protocolado.

## Working Phases (PREVC)
### Phase 1 — Planning (P)
1. Definir contratos e árvore final de arquivos para core/actors/interfaces/adapters.
2. Registrar RFC-lite de W2 (`docs/rfcs/RFC-0003-W2-DELIVERY-BC.md`).
3. Validar critérios de aceite e invariantes de teste com time.

### Phase 2 — Review (R)
1. Revisar riscos (lifecycle, refcount, shutdown, backpressure).
2. Confirmar exclusões de escopo (sem NATS real, sem auth real).
3. Aprovar plano para execução.

### Phase 3 — Execution (E)
1. Implementar `core/delivery` (VO + app + ports).
2. Implementar `actors/delivery/runtime` (RouterActor, SessionActor, subsystem).
3. Implementar `interfaces/ws` com upgrade e handoff para sessão actor.
4. Conectar com `InMemoryBus`/TopicBus adapter e wiring em `cmd/server`.
5. Escrever/ajustar testes determinísticos.

### Phase 4 — Verification (V)
1. Rodar `go test ./...`.
2. Rodar `go test -race ./...`.
3. Smoke test WS local (subscribe/unsubscribe/getrange + broadcast + shutdown).
4. Revisão final de fronteiras arquiteturais.

## RFC-lite Deliverable (W2)
Criar `/Volumes/OWC Express 1M2/Develop/market-raccoon/docs/rfcs/RFC-0003-W2-DELIVERY-BC.md` com:
1. Visão geral do BC Delivery.
2. Modelo de mensagens client->server (JSON) e server->client.
3. Modelo de Subject.
4. Tabela de responsabilidades (`interfaces` vs `actors` vs `core` vs `adapters`).
5. Plano de migração para W3 (JetStream).

## Rollback Plan
- Se regressão no runtime/shutdown:
  - desabilitar wiring de delivery no `cmd/server` e manter placeholders.
- Se regressão no protocolo WS:
  - manter endpoint WS fora da rota principal e isolar em feature flag de config.
- Se falha de estabilidade em actor tests:
  - reverter apenas pacote `internal/actors/delivery/runtime` sem tocar core.

## Evidence & Follow-up
- Artefatos esperados:
  - PR com mudanças em `internal/core/delivery`, `internal/actors/delivery/runtime`, `internal/interfaces/ws`, `internal/adapters/bus`.
  - RFC-lite em `docs/rfcs/RFC-0003-W2-DELIVERY-BC.md`.
  - saída de testes (`go test ./...` e `go test -race ./...`).
- Follow-up para W3:
  - substituir fonte in-memory por JetStream sem quebrar contratos de core/delivery.

## Confirmation (C) — W2 Checkpoint

- **Checkpoint timestamp (UTC):** 2026-02-12T03:21:15Z
- **Release/Checkpoint:** pending commit (base HEAD: `3773953`)
- **Manual completion marker:** `status: completed-manual`

### Test Evidence
- Workspace module sweep (`go test ./...`, skipping `cmd/store` because no packages): **pass**.
- Workspace module sweep (`go test -race ./...`, skipping `cmd/store` because no packages): **pass**.
- Notes:
  - Direct root `go test ./...` is not valid in this workspace layout (`go.work` with module-only dirs).
  - During race sweep, sandbox denied default Go build cache path; rerun with `GOCACHE=/tmp/go-build-cache` succeeded.

### Docs Updated
- RFC-lite implementado em `/Volumes/OWC Express 1M2/Develop/market-raccoon/docs/rfcs/RFC-0003-W2-DELIVERY-BC.md`.
- Plano W2 atualizado em `/Volumes/OWC Express 1M2/Develop/market-raccoon/.context/plans/w2-delivery-bc.md`.

### Known Limits (W2)
- Sem NATS/JetStream real (fonte in-memory).
- Sem auth real/autz de sessão.
- `getrange` depende de `RangeStore`; quando indisponível retorna `Problem` explícito.

### Next Step (W3)
- Substituir fonte in-memory por integração real JetStream mantendo contratos de `core/delivery` e runtime actors estáveis.

## Workflow Backend Inconsistency

- **Observed at:** 2026-02-12T03:21:15Z
- `workflow-status` mostrou `isComplete=false` com fases `P/R/E/V` em `filled`.
- `workflow-advance` respondeu `Workflow completed!` com `isComplete=true`.
- Ação adotada: registrar este marcador manual de conclusão no plano para rastreabilidade em Git, sem bloquear W3.
