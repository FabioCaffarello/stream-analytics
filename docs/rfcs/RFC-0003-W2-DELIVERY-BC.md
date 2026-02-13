# RFC-0003 — W2 Delivery BC (Router + Session, InMemory)

**Status:** Accepted
**Date**: 2026-02-12
**Relates to**: ADR-0007, RFC-0001, RFC-0002

## 1. Visão geral

W2 introduz o bounded context Delivery com fronteiras explícitas:
- `internal/core/delivery`: VO `Subject`, estado de sessão, usecase `GetRange`, ports.
- `internal/actors/delivery/runtime`: `RouterActor`, `SessionActor`, `SubsystemActor`.
- `internal/interfaces/ws`: upgrade WebSocket e handoff para `SessionActor`.
- `internal/adapters/bus`: `InMemoryBus` como fonte de envelopes (sem NATS).

Objetivo: suportar subscribe/unsubscribe/getrange com roteamento por Subject e broadcast para sessões inscritas, mantendo shutdown limpo e sem panics.

## 2. Modelo de mensagens client -> server (JSON)

Formato de comando:

```json
{
  "op": "subscribe|unsubscribe|getrange",
  "subject": "marketdata.trade/binance/BTC-USDT/raw",
  "request_id": "optional-id",
  "params": {
    "from_ms": 0,
    "to_ms": 0,
    "limit": 100
  }
}
```

Regras:
- `op` obrigatório.
- `subject` obrigatório para todas as operações.
- `params` usado por `getrange`.
- payload inválido retorna resposta `type=error` com `problem`.

## 3. Modelo de mensagens server -> client (payload)

Ack:

```json
{
  "type": "ack",
  "op": "subscribe",
  "request_id": "r1",
  "subject": "marketdata.trade/binance/BTC-USDT/raw"
}
```

Erro:

```json
{
  "type": "error",
  "op": "unsubscribe",
  "request_id": "r2",
  "problem": {
    "code": "VAL_VALIDATION_FAILED",
    "message": "..."
  }
}
```

Evento broadcast:

```json
{
  "type": "event",
  "subject": "marketdata.trade/binance/BTC-USDT/raw",
  "seq": 123,
  "ts_ingest": 1710000005,
  "payload": "..."
}
```

Resposta `getrange`:

```json
{
  "type": "range",
  "op": "getrange",
  "request_id": "r3",
  "subject": "marketdata.trade/binance/BTC-USDT/raw",
  "items": []
}
```

## 4. Modelo de Subject

`Subject` é value object de Delivery:
- `StreamType`
- `Venue`
- `Symbol`
- `Timeframe`

Formato canônico:

```text
<stream_type>/<venue>/<symbol>/<timeframe>
```

Exemplo:

```text
marketdata.trade/binance/BTC-USDT/raw
```

Observações:
- `stream_type` preserva namespace de tipo do envelope (ex.: `marketdata.trade`).
- normalização: stream/venue/timeframe em lowercase, symbol em uppercase.

## 5. Tabela de responsabilidades

| Camada | Responsabilidade | Não faz |
|---|---|---|
| `internal/interfaces/ws` | Upgrade WS, spawn de sessão actor | regra de negócio de subscription |
| `internal/actors/delivery/runtime` | lifecycle de sessão, parse de comandos, roteamento actor->actor | regras de domínio fora do core |
| `internal/core/delivery` | VO Subject, estado de Session, contratos de range | transporte e infraestrutura |
| `internal/adapters/bus` | fonte de envelopes em memória | lógica de sessão |
| `cmd/server` | wiring de dependencies e rotas | lógica de negócio |

## 6. Plano de migração para W3 (JetStream)

W3 substitui somente a origem dos envelopes no router:
1. trocar `InMemoryBus.Subscribe()` por consumer JetStream.
2. manter `RouterActor`/`SessionActor` e protocolo WS estáveis.
3. introduzir consumer registry por subject com refcount no adapter JetStream.
4. manter `core/delivery` inalterado (ports e VO permanecem).

## 7. Critérios de aceite aplicados no W2

- subscribe/unsubscribe com refcount por subject no router.
- broadcast apenas para sessões inscritas.
- parse de session commands com erro estruturado (`problem`) e sem panic.
- lifecycle de sessão com `UnregisterSession` em disconnect.
- testes verdes para:
  - `internal/core/delivery`
  - `internal/actors`
  - `internal/interfaces`
  - `cmd/server`

## Changelog

- 2026-02-13:
  - normalizado status para taxonomia RFC (`Draft|Accepted`);
  - mantido escopo histórico da entrega W2.
