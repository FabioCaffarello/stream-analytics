# ADR-0010 — Config Loading and Startup Validation

**Status**: Accepted
**Date**: 2026-02-11
**Deciders**: Chief Architect
**Relates to**: ADR-0009 (Config JSONC Determinism), RFC-0002 (W1 plan)

---

## Contexto

O sistema possui múltiplos pontos de entrada (`cmd/server`, `cmd/consumer`, `cmd/processor`, `cmd/store`) cada um com configuração própria. Atualmente a configuração é feita exclusivamente via `flag` CLI, sem:

- Arquivo de configuração persistível (versionável em git)
- Validação antes do spawn de actors
- Separação clara entre defaults, ambiente e overrides
- Documentação embutida dos campos (o que JSONC permite via comentários)

ADR-0009 estabeleceu o princípio de "config determinístico" mas não definiu a mecânica de carregamento.

---

## Decisão

### 1. Formato: JSONC

Usar JSONC (JSON with Comments) como formato de arquivo de configuração. Justificativas:

- Suportado nativamente após strip de comentários com `encoding/json` padrão
- Comentários permitem documentar cada campo inline (essencial para operadores)
- Sem dependência de biblioteca externa (evita supply chain risk)
- Familiar para desenvolvedores (JSON é universal)

**Alternativas descartadas**:

- YAML: mais complexo, indentação-sensível, armadilhas de parsing (eg. `yes`/`no` como bool)
- TOML: bom mas menos familiar; biblioteca extra necessária
- HCL: muito ligado ao ecossistema HashiCorp
- env-only: sem hierarquia, difícil de documentar, ruim para configurações complexas

### 2. Pacote: `internal/shared/config`

Config vive em `shared` porque é utilizada por todos os `cmd/*`. Não vive em `core/*` porque config de infraestrutura não é lógica de negócio.

O pacote exporta:

- `AppConfig` — envelope raiz com todos os subsistemas
- `Load(path string) (AppConfig, *problem.Problem)` — carrega e faz defaults
- `(AppConfig) Validate() *problem.Problem` — valida regras de negócio de config

### 3. Precedência de configuração

```
Defaults hardcoded
    ↓ sobrescritos por
Arquivo JSONC (path via flag -config)
    ↓ sobrescritos por (fase futura — não implementar agora)
Variáveis de ambiente
    ↓ sobrescritos por
Flags CLI
```

Na fase atual (W1), apenas Defaults + Arquivo JSONC + Flags CLI são implementados.

### 4. Fail-fast no startup

Cada `cmd/*/main.go` deve:

1. Chamar `config.Load(path)` — falha se arquivo existe mas não é parseável
2. Chamar `cfg.Validate()` — falha se campos obrigatórios inválidos
3. Chamar `os.Exit(1)` com log estruturado em caso de falha
4. Só então criar actors e spawnar o engine

Nenhuma validação de config pode ocorrer dentro de actors. Isso garante que se o processo inicia, a config é válida.

### 5. Config não é passada para actors

Actors recebem apenas os valores específicos de que precisam (eg. `ws.ManagerConfig`, `app.IngestMarketData`), não a `AppConfig` inteira. Isso:

- Mantém actors testáveis de forma isolada
- Evita acoplamento entre infra e domínio
- Facilita testes com valores arbitrários

A montagem `AppConfig → structs específicos` ocorre no `cmd/*/main.go` (wiring).

---

## Consequências

### Positivas

- Configuração versionável em git (JSONC com comentários)
- Startup falha antes de spawnar qualquer goroutine se config inválida
- Desenvolvedores veem defaults documentados no arquivo `.jsonc` de exemplo
- Testes podem usar `config.Load("")` para obter defaults sem arquivo

### Negativas / Tradeoffs

- Strip de comentários é código extra a manter (state machine simples, ~50 linhas)
- Dois mecanismos de configuração (flags + arquivo) requerem merge explícito em `main.go`
- Sem hot-reload de config (intencional — reload passa pelo mecanismo `POST /runtime/reload`)

### Neutras

- Fase futura: variáveis de ambiente podem ser adicionadas sem quebrar API (seriam aplicadas antes da validação)
- Fase futura: config pode ser dividida por arquivo (eg. `consumer.jsonc`, `processor.jsonc`) sem alterar schema

---

## Alternativas consideradas

### A: config via variáveis de ambiente apenas

- Descartado: sem hierarquia, sem documentação inline, difícil de versionar

### B: biblioteca de config (viper, koanf)

- Descartado: adiciona dependência externa; JSONC + flags cobre 100% dos casos de uso atuais

### C: config embutida em código (structs com tags de default)

- Descartado: sem possibilidade de override sem recompilar

---

## Implementação

Ver RFC-0002 (W1 plano executável) para lista de tasks e critérios de aceite.

Arquivos a criar:

- `internal/shared/config/schema.go`
- `internal/shared/config/loader.go`
- `internal/shared/config/loader_test.go`
- `cmd/server/config.jsonc`
- `cmd/consumer/config.jsonc`
- `cmd/processor/config.jsonc`

Arquivos a alterar:

- `cmd/server/main.go`
- `cmd/consumer/main.go`
- `cmd/processor/main.go`
