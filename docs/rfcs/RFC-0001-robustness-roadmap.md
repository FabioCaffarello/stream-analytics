# RFC-0001 — Robustness Roadmap: Market-Raccoon v1.0

**Status**: Accepted
**Date**: 2026-02-11
**Author**: Chief Architect
**Relates to**: ADR-0003 (Actor Runtime), ADR-0004 (JetStream), ADR-0007 (Delivery WS), ADR-0009 (Config)

---

## 1. Objetivo

Evoluir o market-raccoon de um protótipo funcional para uma solução de produção em 3 workflows incrementais e testáveis (PREVC). Cada workflow entrega valor independente e mantém `go test -race ./...` verde.

---

## 2. Estado de Partida

| Componente | Estado |
|---|---|
| `internal/shared` (foundation) | ✅ Completo, testado |
| `internal/core/marketdata` | ✅ Completo, testado |
| `internal/core/aggregation` | ✅ Completo, testado |
| `internal/core/delivery` | 🟡 Domain skeleton (Session, Subscription, Topic, Filter) — sem app layer |
| `internal/core/insights` | 🟡 Domain skeleton — sem app layer |
| `internal/actors/runtime` (Guardian) | ✅ Completo, testado |
| `internal/actors/marketdata` (ws + runtime) | ✅ Completo, testado |
| `internal/actors/aggregation/runtime` | ✅ Completo, testado |
| `internal/adapters/bus` | ✅ InMemoryBus + LogPublisher |
| `internal/interfaces/http` | ✅ /healthz /snapshot /reload |
| `cmd/{server,consumer,processor}` | 🟡 Wiring mínimo, sem config estruturado |
| Config carregamento (ADR-0009) | ❌ Não implementado |
| Graceful shutdown coordenado | ❌ Ad-hoc por cmd |
| Readiness probe | ❌ Ausente |
| Delivery BC (Router/Session actor) | ❌ Não implementado |
| NATS JetStream adapter | ❌ Não implementado |

---

## 3. Princípios transversais

1. **Sem lógica de negócio fora de `internal/core`** — atores e interfaces só orquestram.
2. **Sem panics** — falhas fatais propagam via `runtime.ChildFailed` ou poison self.
3. **Sem goroutines nuas** — toda goroutine fora do mailbox usa sentinel message.
4. **Ports first** — qualquer dependência externa passa por interface em `core/*/ports`.
5. **Incrementalidade** — cada PR compila, testa e funciona de forma autônoma.

---

## 4. W1 — Config & Shutdown & Runtime Hardening

### Escopo

- Carregar configuração estruturada (JSONC) por subsistema no startup
- Validação fail-fast de toda config obrigatória antes de spawn do Guardian
- Graceful shutdown coordenado: Guardian espera filhos drenarem antes de se poisonar
- Distinção liveness (`/healthz`) vs readiness (`/readyz`) no HTTP server
- Guardian expõe estado `ready` (todos os subsystems configurados iniciaram pelo menos uma vez)

### Riscos

| Risco | Mitigação |
|---|---|
| Config JSONC pode conter comentários ilegais para encoding/json | Implementar strip de comentários antes do decode |
| PoisonCtx pode ser chamado antes de Guardian processar todos filhos | Guardian responde a `Stop` só após todos filhos receberem Poison |
| Readiness falsa positiva se Guardian não rastrear subsystems esperados | Guardian.Config lista `ExpectedSubsystems []Subsystem` |

### Critérios de aceite

- [ ] `cmd/server -config=config.jsonc` carrega e valida config; startup falha explicitamente se campo obrigatório ausente
- [ ] `SIGTERM` → graceful shutdown completa em ≤ 10s sem goroutine leak
- [ ] `/readyz` retorna 503 enquanto nem todos `ExpectedSubsystems` iniciaram; 200 depois
- [ ] `/healthz` retorna 200 se o processo vive (independente de readiness)
- [ ] `go test -race ./...` verde
- [ ] Nenhuma alteração em `internal/core/*`

### Dependências

Nenhuma. W1 é self-contained e pode ser desenvolvido sem W2/W3.

---

## 5. W2 — Delivery BC: Router + Session com InMemory

### Escopo

- `internal/core/delivery/app`: usecases `Subscribe`, `Unsubscribe`, `GetRange` (ports only para storage)
- `internal/core/delivery/ports`: `SnapshotStore`, `SubscriptionRegistry`, `SessionWriter`
- `internal/actors/delivery/runtime`: `RouterSubsystemActor` — mantém `map[Topic]*pidset`, cria consumers de bus por topic sob demanda (refcount), broadcast para sessions
- `internal/actors/delivery/session`: `SessionActor` — mantém WebSocket conn, parseia msgs do client, delega a usecases
- `internal/adapters/delivery/`: fake in-memory SnapshotStore e SubscriptionRegistry
- `cmd/delivery/main.go`: wiring delivery com InMemoryBus (sem NATS)

### Motivação de design (pattern catalog)

O Router actor do catálogo de referência mantém `map[Subject]*PIDSet` e cria consumers NATS por subject sob demanda. Na nossa versão, o "consumer" é uma goroutine que lê de `InMemoryBus.Subscribe()`. O refcount garante que quando `subCount == 0` para um topic, o canal é fechado e a goroutine encerrada.

### Riscos

| Risco | Mitigação |
|---|---|
| SessionActor pode vazar goroutine se WebSocket fechar abruptamente | Deadline na leitura + poison self ao detectar erro de leitura |
| RouterActor pode criar multiplos consumers para o mesmo topic | Map com topic como chave + mutex dentro do actor (mailbox garante sequencialidade) |
| Broadcast lento bloqueia RouterActor | Send ao SessionActor é non-blocking com `engine.Send`; mailbox do session tem limite |

### Critérios de aceite

- [ ] Cliente WS pode se conectar, enviar `{"action":"subscribe","topic":"marketdata.bookdelta.binance.BTC-USDT"}` e receber envelopes publicados no bus
- [ ] Desconexão do cliente → SessionActor se auto-poisona → Router remove do pidset
- [ ] Quando último subscriber de um topic sai → consumer goroutine encerra (sem leak)
- [ ] `go test -race ./...` verde
- [ ] Nenhum NATS real necessário para compilar ou testar

### Dependências

W1 deve estar completo (config loading permite injetar endereço WS listen e topics).

---

## 6. W3 — NATS JetStream Integration

### Escopo

- `internal/adapters/jetstream/`: adapter implementando `ports.EventPublisher` (produtor) e `ports.EventConsumer` (consumidor)
- Lifecycle "consumer per subject + refcount" conforme ADR-0004: criar durable consumer na primeira subscrição, fechar quando refcount chega a 0
- Idempotency: `envelope.IdempotencyKey` → Msg-ID no header NATS (deduplication window)
- Subject schema: `{bc}.{event_type}.v{version}.{venue}.{instrument}` (ex: `marketdata.bookdelta.v1.binance.BTC-USDT`)
- `cmd/consumer` e `cmd/processor` trocam InMemoryBus por JetStream (one-line swap)
- `cmd/delivery` troca InMemoryBus por JetStream consumer no RouterActor

### Motivação de design (pattern catalog)

O Router actor da referência cria consumers NATS por subject sob demanda e remove quando subCount==0. Na nossa versão isso vive em `adapters/jetstream/consumer_registry.go`: o `ConsumerRegistry` mantém `map[subject]refcount` e os handles de consumer NATS. RouterActor comunica com ConsumerRegistry via método síncrono (não goroutine nua, pois ConsumerRegistry é injetado como dependência).

### Riscos

| Risco | Mitigação |
|---|---|
| NATS indisponível no startup | Adapter retorna `*problem.Problem` retryable; Guardian reinicia subsystem |
| Consumer lag infinito sem backpressure | Configurar `MaxAckPending` e `MaxDeliver` no durable consumer |
| Reordenação de mensagens em replay | Sequence number no envelope + OrderBook reconciliation já implementado |
| Testes dependem de NATS real | Usar testcontainers ou mock do JetStream JS API |

### Critérios de aceite

- [ ] `cmd/consumer` publica para JetStream; `cmd/processor` consome do mesmo stream
- [ ] Parar e reiniciar `cmd/processor` não perde mensagens (durable consumer)
- [ ] Idempotency key previne duplicatas dentro da deduplication window (teste de replay)
- [ ] Criar/remover subscribers não vaza goroutines ou consumers NATS (refcount correto)
- [ ] `go test -race ./...` verde com testcontainers NATS
- [ ] `cmd/consumer` e `cmd/processor` compilam e funcionam com InMemoryBus (sem NATS no env) com flag `-bus=inmemory`

### Dependências

W2 deve estar completo (RouterActor usa ConsumerRegistry que pode ser trocado de InMemory para JetStream).

---

## 7. Sequência de entregas

```
W1 ──────────────────────────────────────────────────────► done
              W2 ─────────────────────────────────────────► done
                             W3 ──────────────────────────► done
```

Cada workflow:

1. **P** — RFC-lite + task list + critérios de aceite
2. **R** — revisar riscos com equipe, ajustar escopo se necessário
3. **E** — implementar em commits pequenos (um subsistema por commit)
4. **V** — validar testes + smoke test manual
5. **C** — registrar checkpoint (atualizar MEMORY.md, criar RFC de checkpoint)

---

## 8. Arquivos de referência cruzada

| Workflow | RFC detalhe | ADR relevante |
|---|---|---|
| W1 | RFC-0002 | ADR-0009 (Config), ADR-0003 (Runtime) |
| W2 | RFC-0003 (a criar) | ADR-0007 (Delivery WS) |
| W3 | RFC-0004 (a criar) | ADR-0004 (JetStream) |
