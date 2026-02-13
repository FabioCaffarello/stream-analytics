# RFC-0002 — W1: Config & Shutdown & Runtime Hardening

**Status:** Accepted
**Date**: 2026-02-11
**Author**: Chief Architect
**Workflow**: W1 do RFC-0001
**Relates to**: ADR-0009 (Config JSONC), ADR-0003 (Actor Runtime), ADR-0010 (a criar)

---

## 1. Contexto

O market-raccoon possui fundação sólida (Guardian, subsystem actors, core usecases) mas ainda carece de:

1. **Config estruturado** — cada `cmd/*/main.go` usa `flag` diretamente sem validação, sem arquivo de config, sem documentação dos campos.
2. **Graceful shutdown coordenado** — `PoisonCtx` é chamado sem garantia que os filhos drenaram; pode haver log spam de mensagens após shutdown.
3. **Readiness probe** — `/healthz` serve tanto para liveness quanto readiness, o que impede orquestradores (k8s) de distinguir "processo vivo" de "sistema pronto para tráfego".
4. **Estado `ready` no Guardian** — Guardian não rastreia se os subsystems esperados já iniciaram ao menos uma vez.

---

## 2. Decisão

### 2.1 Config JSONC

Implementar um pacote `internal/shared/config` com:

- **JSONC loader**: lê arquivo `.jsonc`, faz strip de comentários (`// ...` e `/* ... */`), faz decode via `encoding/json`
- **AppConfig struct**: composição de configs por subsistema
- **Validação fail-fast**: chamada explícita `config.Validate()` retorna `*problem.Problem` (nunca panic)
- **Precedência**: flags CLI > variáveis de ambiente > arquivo JSONC > defaults embutidos

### 2.2 Graceful Shutdown

O Guardian recebe uma mensagem `Stop` e deve:

1. Setar `shuttingDown = true` para bloquear novos restarts
2. Cancelar todos os `scheduledRetry` pendentes
3. Enviar `Poison` para todos os filhos ativos (já feito por `stopAll`)

**Coordenação Hollywood-native**: `engine.PoisonCtx(ctx, guardianPID)` em `cmd/main.go` já bloqueia até que Guardian *e toda sua árvore de filhos* parem. Hollywood propaga o stop para todos os filhos antes de entregar `actor.Stopped` ao Guardian. Portanto **não é necessário** um `pendingStop` manual — a espera já é garantida pelo engine.

Problema atual: `ChildFailed` pode chegar durante shutdown e agendar retry desnecessário. Solução: verificar `shuttingDown` no `handleChildFailed` e no `retrySubsystem`.

### 2.3 Readiness Probe

- `/healthz` (liveness) — retorna 200 se o processo consegue processar requisições HTTP; nunca bloqueia.
- `/readyz` (readiness) — consulta Guardian via `engine.Request(Snapshot)` e valida se todos `ExpectedSubsystems` estão `Running && !Degraded`.

### 2.4 Guardian `ExpectedSubsystems`

`GuardianConfig` recebe `ExpectedSubsystems []Subsystem`. Se nil, o Guardian é liberal (nenhum subsystem é "esperado" para readiness). Subsystems com Factory presente são automaticamente incluídos em ExpectedSubsystems se o campo for omitido.

---

## 3. Plano Executável

### 3.1 Task List

| # | Task | Arquivo(s) | Tipo |
|---|---|---|---|
| T1 | Pacote `config` — structs + JSONC loader | `internal/shared/config/loader.go` | CRIAR |
| T2 | Structs de config por subsistema | `internal/shared/config/schema.go` | CRIAR |
| T3 | Testes do loader | `internal/shared/config/loader_test.go` | CRIAR |
| T4 | Templates de config por cmd | `cmd/{server,consumer,processor}/config.jsonc` | CRIAR |
| T5 | Guardian: `ExpectedSubsystems` + readiness state | `internal/actors/runtime/guardian.go` | ALTERAR |
| T6 | Protocol: mensagem `selfPoisonMsg` + `StopAck` | `internal/actors/runtime/protocol.go` | ALTERAR |
| T7 | Guardian: shutdown coordenado via `pendingStop` | `internal/actors/runtime/guardian.go` | ALTERAR |
| T8 | Testes do shutdown coordenado | `internal/actors/runtime/guardian_test.go` | ALTERAR |
| T9 | HTTP server: `/readyz` endpoint | `internal/interfaces/http/server.go` | ALTERAR |
| T10 | HTTP server: testes de readiness | `internal/interfaces/http/server_test.go` | ALTERAR |
| T11 | `cmd/server`: carregar config, wiring readiness | `cmd/server/main.go` | ALTERAR |
| T12 | `cmd/consumer`: carregar config | `cmd/consumer/main.go` | ALTERAR |
| T13 | `cmd/processor`: carregar config | `cmd/processor/main.go` | ALTERAR |
| T14 | ADR-0010 formalizar decisão de config | `docs/adrs/ADR-0010-config-loading.md` | CRIAR |

### 3.2 Ordem de implementação

```
T1 → T2 → T3           (config package — sem dependências externas)
T6 → T5 → T7 → T8      (Guardian hardening — depende de T6 para novos msgs)
T9 → T10                (HTTP server readiness — depende de T5 para snapshot with ready flag)
T4 → T11 → T12 → T13   (cmd wiring — depende de T1-T3 e T5-T9)
T14                     (ADR — pode ser escrita em paralelo)
```

---

## 4. Especificação detalhada por arquivo

### T1 + T2: `internal/shared/config/`

**Arquivo: `schema.go`**

```
package config

// AppConfig é o envelope raiz do arquivo JSONC.
// Todos os campos são opcionais; ausência usa defaults seguros.
type AppConfig struct {
    Log      LogConfig      `json:"log"`
    HTTP     HTTPConfig     `json:"http"`
    Consumer ConsumerConfig `json:"consumer"`
    Processor ProcessorConfig `json:"processor"`
}

type LogConfig struct {
    Level  string `json:"level"`   // "debug"|"info"|"warn"|"error"; default "info"
    Format string `json:"format"`  // "text"|"json"; default "text"
}

type HTTPConfig struct {
    Addr           string `json:"addr"`             // default ":8080"
    ReadTimeout    string `json:"read_timeout"`     // duration string, default "10s"
    WriteTimeout   string `json:"write_timeout"`    // default "15s"
    IdleTimeout    string `json:"idle_timeout"`     // default "60s"
    ShutdownTimeout string `json:"shutdown_timeout"` // default "10s"
}

type ConsumerConfig struct {
    Exchange    string   `json:"exchange"`      // default "binance"
    Tickers     []string `json:"tickers"`       // default ["BTC-USDT","ETH-USDT"]
    Fake        bool     `json:"fake"`          // default true
    FakeRateMs  int      `json:"fake_rate_ms"`  // default 500
}

type ProcessorConfig struct {
    BusCapacity int `json:"bus_capacity"` // channel buffer; default 1024
}
```

**Validação**: `func (a AppConfig) Validate() *problem.Problem`

- Log.Level deve ser um de: debug|info|warn|error
- HTTPConfig.Addr deve ser não-vazio
- ConsumerConfig.Tickers deve ter ao menos 1 item se `!Fake`
- Todos os campos duration string devem parsear com `time.ParseDuration`

**Arquivo: `loader.go`**

```
// Load carrega config de um arquivo JSONC.
// Se path == "", retorna AppConfig com defaults.
func Load(path string) (AppConfig, *problem.Problem)

// stripComments remove comentários // e /* */ do JSONC.
// Implementado como state machine simples (sem regex).
// Preserva newlines para que line numbers em erros de JSON sejam corretos.
func stripComments(src []byte) []byte

// applyDefaults preenche campos zero com valores defaults.
func applyDefaults(c *AppConfig)
```

**Notas de implementação**:

- `stripComments` deve respeitar strings: `"url": "https://..."` não deve ter `//` removido
- State machine: estados `Normal`, `InString`, `InLineComment`, `InBlockComment`, `Escape`
- Usar `encoding/json` padrão após strip; não usar biblioteca externa

---

### T6: `internal/actors/runtime/protocol.go` — alterações

Adicionar mensagens para readiness query (shutdown não requer novas msgs — usa Hollywood nativo):

```go
// ReadyQuery consulta se o Guardian está em estado ready.
// Usado pelo /readyz endpoint.
type ReadyQuery struct {
    ReplyTo *actor.PID // se nil, resposta vai para c.Sender()
}

// ReadyResponse é a resposta ao ReadyQuery.
type ReadyResponse struct {
    Ready   bool
    Pending []Subsystem // subsystems esperados ainda não iniciados
}
```

---

### T5 + T7: `internal/actors/runtime/guardian.go` — alterações

**Adições ao `GuardianConfig`**:

```go
// ExpectedSubsystems lista os subsistemas que devem estar Running para o
// Guardian reportar Ready==true. Se nil, infere a partir de Factories.
ExpectedSubsystems []Subsystem
```

**Novo state no Guardian**:

```go
// readySystems rastreia quais subsystems iniciaram ao menos uma vez com sucesso.
readySystems map[Subsystem]bool
// shuttingDown é true após receber Stop; impede novos restarts.
shuttingDown bool
```

**Novo handler: `ReadyQuery`**:

```go
case ReadyQuery:
    ready, pending := g.computeReady()
    replyTo := msg.ReplyTo
    if replyTo == nil { replyTo = c.Sender() }
    c.Send(replyTo, ReadyResponse{Ready: ready, Pending: pending})
```

**Mudança no handler `Stop`**:

```go
case Stop:
    g.shuttingDown = true
    g.stopAll(c)   // já implementado: cancela retries + poisona filhos
    // NÃO é necessário esperar aqui: engine.PoisonCtx no cmd/main.go
    // bloqueia até que Guardian + toda sua árvore parem (Hollywood-native).
```

**Mudança no `handleChildFailed`**:

```go
// Adicionado no início:
if g.shuttingDown {
    return // não reiniciar durante shutdown
}
```

**Mudança no `retrySubsystem`**:

```go
// Adicionado no início:
if g.shuttingDown {
    return // geração stale; ignorar
}
```

**Atualização de `computeReady()`**:

- Compara `readySystems` contra `expectedSubsystems`
- Retorna `ready=true` apenas quando todos esperados estão em `readySystems`

**Atualização de `startSubsystem` — readySystems tracking (v1 otimista)**:

- Ao final de `startSubsystem`, se spawn foi bem sucedido: `g.readySystems[sub] = true`
- Isso é "readiness otimista" (assume pronto ao spawnar) — alinhado com o comentário existente no protocol.go: *"Runtime v1 emits Recovered after a successful spawn (assumed recovery). A future handshake (e.g. ChildReady) should confirm operational readiness."*
- Para W2+, um `ChildReady` message pode substituir essa marcação otimista por confirmação real do subsystem actor.

---

### T9: `internal/interfaces/http/server.go` — alterações

**Novo endpoint `/readyz`**:

```go
mux.HandleFunc("GET /readyz", s.handleReadyz)

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), s.snapshotTimeout)
    defer cancel()

    res, err := engine.RequestWithContext(ctx, s.guardianPID, runtime.ReadyQuery{})
    if err != nil {
        w.WriteHeader(http.StatusGatewayTimeout)
        jsonError(w, "guardian_timeout")
        return
    }
    resp := res.(runtime.ReadyResponse)
    if !resp.Ready {
        w.WriteHeader(http.StatusServiceUnavailable)
        json.NewEncoder(w).Encode(map[string]any{
            "ready": false,
            "pending": resp.Pending,
        })
        return
    }
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]bool{"ready": true})
}
```

**`/healthz` permanece inalterado** — retorna 200 imediatamente (liveness only).

---

### T4: Templates de config JSONC

**`cmd/server/config.jsonc`**:

```jsonc
{
  // Configuração do server (modo observador)
  "log": {
    "level": "info",       // debug | info | warn | error
    "format": "text"       // text | json
  },
  "http": {
    "addr": ":8080",
    "read_timeout": "10s",
    "write_timeout": "15s",
    "idle_timeout": "60s",
    "shutdown_timeout": "10s"
  }
}
```

**`cmd/consumer/config.jsonc`**:

```jsonc
{
  "log": { "level": "info", "format": "text" },
  "http": { "addr": ":8081" },
  "consumer": {
    "exchange": "binance",
    "tickers": ["BTC-USDT", "ETH-USDT"],
    "fake": true,
    "fake_rate_ms": 500
  }
}
```

**`cmd/processor/config.jsonc`**:

```jsonc
{
  "log": { "level": "info", "format": "text" },
  "http": { "addr": ":8082" },
  "processor": {
    "bus_capacity": 1024
  }
}
```

---

### T11–T13: `cmd/*/main.go` — padrão de carregamento

Cada `main.go` deve seguir este padrão:

```go
func main() {
    configPath := flag.String("config", "config.jsonc", "path to JSONC config file")
    flag.Parse()

    cfg, prob := config.Load(*configPath)
    if prob != nil {
        slog.Error("config load failed", "error", prob)
        os.Exit(1)
    }
    if prob = cfg.Validate(); prob != nil {
        slog.Error("config validation failed", "error", prob)
        os.Exit(1)
    }

    // configurar logger com cfg.Log.Level / cfg.Log.Format
    logger := buildLogger(cfg.Log)
    slog.SetDefault(logger)

    // ... wiring ...

    engine := actor.NewEngine(actor.NewEngineConfig())
    guardianPID := engine.Spawn(runtime.NewGuardian(runtime.GuardianConfig{
        // ExpectedSubsystems inferido de Factories se omitido
        Factories: factories,
        Clock:     clock.System(),
    }), "guardian")

    // HTTP server com guardianPID
    srv := httpserver.New(httpserver.Config{
        Addr:        cfg.HTTP.Addr,
        GuardianPID: guardianPID,
        Engine:      engine,
    })
    go srv.Start()

    // graceful shutdown
    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()
    <-ctx.Done()

    logger.Info("shutting down")
    shutCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.shutdownTimeout())
    defer cancel()
    srv.Shutdown(shutCtx)
    engine.Send(guardianPID, runtime.Stop{})
    // Aguardar Guardian se poisonar (engine.WaitTillShutdown tem timeout)
    // 1. Sinaliza Guardian para parar (seta shuttingDown=true, cancela retries pendentes)
    e.Send(guardianPID, actorruntime.Stop{})
    // 2. Aguarda Guardian + toda árvore de filhos pararem (Hollywood-native).
    //    e.Poison(pid).Done() retorna canal que fecha quando o actor para.
    //    O Stop acima garante que nenhum filho é reiniciado durante o shutdown.
    select {
    case <-e.Poison(guardianPID).Done():
        logger.Info("shutdown complete")
    case <-shutCtx.Done():
        logger.Warn("guardian did not stop in time; forcing exit")
    }
}
```

> **Nota**: `engine.PoisonCtx` é chamado como fallback; o caminho normal é Guardian se auto-poisonar após `Stop`. O `PoisonCtx` com timeout garante que o processo termina mesmo se algo travar.

---

## 5. Critérios de aceite (checklist para PR)

### Config

- [ ] `config.Load("")` retorna AppConfig com defaults (sem erro)
- [ ] `config.Load("nonexistent.jsonc")` retorna `*problem.Problem` com code `config.not_found`
- [ ] `config.Load("valid.jsonc")` parseia campos e strips comentários corretamente
- [ ] `config.Load("invalid_json.jsonc")` retorna `*problem.Problem` com code `config.parse_error`
- [ ] `AppConfig{Log:{Level:"INVALID"}}.Validate()` retorna `*problem.Problem`
- [ ] `AppConfig{}` com defaults aplicados passa `Validate()`
- [ ] Comentário `// ...` dentro de uma string JSON não é removido

### Shutdown

- [ ] `Stop{}` enviado ao Guardian → todos os filhos recebem `Poison` → Guardian se auto-poisona
- [ ] Teste: Guardian com 3 subsystems, Stop enviado, verifica que Guardian só se poisona após os 3 filhos pararem
- [ ] `ChildFailed` recebido durante shutdown é ignorado (não agenda restart)
- [ ] `go test -race` passa para `guardian_test.go`

### Readiness

- [ ] `GET /readyz` retorna 503 enquanto Guardian não recebeu `Recovered` de todos ExpectedSubsystems
- [ ] `GET /readyz` retorna 200 com `{"ready":true}` após todos ExpectedSubsystems iniciarem
- [ ] `GET /healthz` retorna 200 independente do estado de readiness
- [ ] Timeout de `/readyz` retorna 504 (não 500)

### Integração

- [ ] `cmd/server -config=config.jsonc` inicia, responde `/healthz` 200, `/readyz` 200 (nenhum expected subsystem em observer mode)
- [ ] Config com campo inválido causa `os.Exit(1)` antes de spawnar qualquer actor
- [ ] `SIGTERM` encerra o processo em ≤ 10s com log "shutting down"

---

## 6. Riscos e mitigações detalhados

### R1: stripComments e strings JSON

**Risco**: `"url": "https://example.com"` tem `//` dentro da string.
**Mitigação**: state machine com estado `InString`; ao entrar em string (`"`), ignorar todos `//` e `/*` até o `"` de fechamento. Escapamento (`\"`) tratado com estado `Escape`.
**Teste**: caso específico com URL em string + comentário inline.

### R2: ~~actor.Stopped para tracking de filhos~~ (RESOLVIDO — não necessário)

**Resolução**: `engine.PoisonCtx(ctx, guardianPID)` do Hollywood bloqueia até que Guardian e *toda a sua árvore de filhos* parem. Não é necessário rastrear individualmente quando cada filho parou. O Guardian apenas precisa do flag `shuttingDown` para não reiniciar filhos que falham durante o processo de shutdown.

### R3: Race entre Stop e ChildFailed

**Risco**: Um filho falha exatamente quando o Guardian recebe `Stop`. `ChildFailed` chega ao mailbox antes de `Stop` ser processado.
**Mitigação**: `ChildFailed` é processado primeiro (FIFO), agenda restart. Depois `Stop` processa, `shuttingDown=true`, cancela restarts pendidos via `generation` (já implementado no Guardian). Sem race.

### R4: PoisonCtx como fallback pode matar Guardian antes de filhos drenarem

**Risco**: `engine.PoisonCtx(shutCtx, guardianPID)` pode forçar poison antes que o loop `pendingStop` complete.
**Mitigação**: O timeout do `shutCtx` deve ser maior que o tempo máximo de drenagem esperado (tipicamente ≤ 2s para subsystems atuais). Documentar isso no config como `shutdown_timeout`. Valor recomendado: 10s.

---

## 7. Estimativa de complexidade

| Task | Complexidade | Observação |
|---|---|---|
| T1-T3 (config package) | Baixa | State machine de strip é a parte mais cuidadosa |
| T4 (config.jsonc templates) | Trivial | |
| T5-T8 (Guardian hardening) | Média | Requer verificar API Hollywood para actor.Stopped |
| T9-T10 (HTTP /readyz) | Baixa | Padrão já existe para /snapshot |
| T11-T13 (cmd wiring) | Baixa | Refactoring de boilerplate |
| T14 (ADR-0010) | Trivial | Documentação |

---

## 8. Próximos passos após W1

Ao completar W1:

1. Atualizar `MEMORY.md` com padrão de config loading
2. Criar RFC-0003 para W2 (Delivery BC)
3. Marcar W1 como concluído no RFC-0001

## Changelog

- 2026-02-13:
  - normalizado status para taxonomia RFC (`Draft|Accepted`);
  - mantida rastreabilidade histórica do conteúdo original.
