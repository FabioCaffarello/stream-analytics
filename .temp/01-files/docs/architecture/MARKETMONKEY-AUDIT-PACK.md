# MARKETMONKEY Architecture Audit Pack

Audit target: `MARKETMONKEY` repository  
Audit mode: static code audit (no production runtime access)

---

## 1) Repo Map

### VERIFIED

### 1.1 Executáveis / serviços (`cmd/*`)

| Executável | Papel principal | Dependências diretas |
|---|---|---|
| `cmd/consumer/main.go` | Sobe consumidor por exchange (Binance, BinanceF, Bybit, Coinbase, Hyperliquid). | `actor/consumer/*`, `config`, `hollywood/actor` |
| `cmd/processor/main.go` | Sobe processor por exchange, com restart ilimitado. | `actor/processor`, `hollywood/actor` |
| `cmd/store/main.go` | Sobe ator de persistência (store). | `actor/store`, `pkg/db`, `hollywood/actor` |
| `cmd/server/main.go` | Sobe servidor WS/API e autenticação. | `actor/server`, `pkg/db`, `echo`, `websocket` |
| `cmd/backfill/main.go` | Reprocessamento histórico offline (sem consumir NATS no processor). | `actor/processor`, `actor/store`, `pkg/history/binancef` |
| `cmd/history/main.go` | Ferramenta operacional de histórico/check gaps. | `pkg/db/*`, `config`, APIs históricas |
| `cmd/scripts/main.go` | Script auxiliar para extrair symbols/tickSize. | API Binance |
| `cmd/test/main.go` | Harness de ticker local. | `pkg/tick` |

**Evidence**

```go
// cmd/consumer/main.go:42-56
var pid *actor.PID
switch *exchange {
case config.Binancef:
    pid = e.Spawn(binancef.New(), "consumer", actor.WithID(config.Binancef))
...
default:
    log.Fatalf("invalid or unsupported exchange: %s", *exchange)
}
```

```go
// cmd/processor/main.go:39-42
pid := e.Spawn(processor.New(*exchange), id,
    actor.WithID(id),
    actor.WithMaxRestarts(math.MaxInt),
)
```

```go
// cmd/store/main.go:35-36
pid := e.Spawn(store.New(dbClient), "store", actor.WithID("1"))
cmd.WaitTillShutdown(e, pid)
```

```go
// cmd/server/main.go:38-40
pid := e.Spawn(server.New(httpServerAddr, dbClient), "server")
cmd.WaitTillShutdown(e, pid)
```

### 1.2 Atores principais (`actor/*`)

| Ator | Responsabilidade |
|---|---|
| `actor/processor` | Consome streams de ingest por exchange, roteia por símbolo, cria `symbol` children. |
| `actor/symbol` | Multiplexa mensagens do símbolo para `trade/stat/orderbook/volume`. |
| `actor/trade` | Amostragem de candles multi-timeframe e publish realtime/store. |
| `actor/stat` | Agregação de stats/liquidações/trades por timeframe e publish. |
| `actor/volume` | Agregação volumétrica por bins de preço e publish periódico. |
| `actor/orderbook` | Estado de livro (bids/asks), heatmap e orderbook realtime/store. |
| `actor/store` | Consome streams `store_*` e grava DB. |
| `actor/server` | Entrada HTTP/WS, auth, spawn de sessões. |
| `actor/server_router` | Multiplexa assinatura NATS por subject e fanout para sessões. |
| `actor/server_session` | Sessão WS cliente; subscribe/unsubscribe/getrange. |
| `actor/consumer/ws` | Runtime WS upstream (manager + consumer fail-fast). |

**Evidence**

```go
// actor/processor/processor.go:19-37
func consumerRegistry(p *Processor) []event.Registry {
    return []event.Registry{
        { Stream: nats.StreamTypeTrade, Fn: event.CreateStreamHandler(p.handleTrade) },
        { Stream: nats.StreamTypeBookUpdate, Fn: event.CreateStreamHandler(p.handleOrderbook) },
        { Stream: nats.StreamTypePreStat, Fn: event.CreateStreamHandler(p.handlePreStat) },
        { Stream: nats.StreamTypeLiquidation, Fn: event.CreateStreamHandler(p.handleLiquidation) },
    }
}
```

```go
// actor/symbol/symbol.go:41-52
case *event.Trade:
    c.Forward(s.bookPID)
    c.Forward(s.statPID)
    c.Forward(s.tradePID)
    c.Forward(s.volumePID)
case *event.Stat:
    c.Forward(s.statPID)
case *event.BookUpdate:
    c.Forward(s.bookPID)
```

### 1.3 Runtime de streams (`pkg/nats/*`)

| Arquivo | Papel |
|---|---|
| `pkg/nats/streams.go` | Taxonomia de stream/subject, classes de storage, limits. |
| `pkg/nats/producer.go` | Publicação JetStream com `WithMsgID`. |
| `pkg/nats/consumer.go` | Consumo JetStream, ACK/NAK, criação durable/ephemeral. |

### 1.4 Wire (`event/*`) e Storage (`pkg/db/*`, `sql/*`)

- `event/event.go`: contratos de evento + keys dedup.  
- `event/encoding.go`: decode CBOR de mensagens NATS.  
- `pkg/db/timescale/*`, `pkg/db/clickhouse/*`: implementação de persistência.  
- `sql/timescale/*`, `sql/clickhouse/*`: schema e constraints.

### INFERRED

- Arquitetura operacional principal é multi-processo (consumer/processor/store/server separados), com NATS como backbone de desacoplamento entre ingest/process/serve/store.
- `actor/combined` parece caminho legado/experimental (não aparece no wiring de `cmd/*`).

### UNKNOWN

- Topologia real de produção (replicas, sizing, affinity) não está no código; `docker-compose.yml` é referência de self-hosting, não prova de produção.

---

## 2) Runtime Mental Model (E2E)

### VERIFIED

### 2.1 Pipeline E2E

1. **Consumer WS upstream -> ingest streams**
- Consumers parseiam payloads da exchange e publicam em `trades/bookupdates/prestats/liquidations` via `BaseConsumer.PublishMessage`.

```go
// actor/consumer/base/consumer.go:75-83
msg := nats.PublishParams{
    Subject: nats.Subject{ StreamType: p.Stream, Exchange: b.Exchange, Symbol: p.Symbol }.PubString(),
    Msg:   data,
    MsgID: p.Key,
}
```

2. **Processor consome ingest streams e cria malha por símbolo**

```go
// actor/processor/processor.go:139-143
for _, sym := range market.Symbols {
    pair := event.NewPair(p.exchange, sym.Ticker)
    pid := c.SpawnChild(symbol.New(pair), "symbol", actor.WithID(pair.Symbol), actor.WithContext(ctx))
    p.symbols[strings.ToUpper(pair.Symbol)] = pid
}
```

3. **Roteamento por símbolo**

```go
// actor/processor/processor.go:89-100
case *event.Trade:
    pid := p.symbols[strings.ToUpper(msg.Pair.Symbol)]
    c.Forward(pid)
case *event.BookUpdate:
    pid := p.symbols[strings.ToUpper(msg.Pair.Symbol)]
    c.Forward(pid)
```

4. **Symbol actor fanout para subatores stateful**

```go
// actor/symbol/symbol.go:64-69
s.statPID = c.SpawnChild(stat.New(s.pair), "stat", actor.WithID(s.pair.Symbol), actor.WithContext(c.Context()))
s.bookPID = c.SpawnChild(orderbook.New(s.pair), "book", actor.WithID(s.pair.Symbol), actor.WithContext(c.Context()))
s.tradePID = c.SpawnChild(trade.New(s.pair), "trade", actor.WithID(s.pair.Symbol), actor.WithContext(c.Context()))
s.volumePID = c.SpawnChild(volume.New(s.pair), "volume", actor.WithID(s.pair.Symbol), actor.WithContext(c.Context()))
```

5. **Processors publicam outputs realtime/store**

```go
// actor/processor/processor.go:41-50
var producerStreams = []nats.StreamType{
    nats.StreamTypeRealTimeCandle,
    nats.StreamTypeRealTimeHeatmap,
    nats.StreamTypeRealTimeStat,
    nats.StreamTypeRealTimeVolume,
    nats.StreamTypeRealTimeOrderbook,
    nats.StreamTypeStoreCandle,
    nats.StreamTypeStoreHeatmap,
    nats.StreamTypeStoreStat,
    nats.StreamTypeStoreVolume,
}
```

6. **Store consome `store_*` e grava DB**

```go
// actor/store/store.go:121-128
for _, reg := range streams(s) {
    subject := nats.Subject{StreamType: reg.Stream}
    _, err := s.consumer.NewConsumer(nats.ConsumerParams{
        Subject: subject,
        Durable: reg.Stream.Durable("store"),
        Handler: reg.Fn,
    })
}
```

7. **ServerRouter cria consumer por assinatura e faz fanout**

```go
// actor/server_router/server_router.go:100-103
if _, ok := s.subscriptions[subject]; !ok {
    err := s.createConsumer(subject)
    if err != nil { return }
}
```

```go
// actor/server_router/server_router.go:87-91
subs, ok := s.subscriptions[subject]
if ok {
    subs.Pids.ForEach(func(i int, pid *actor.PID) {
        s.ctx.Send(pid, msg)
    })
}
```

8. **ServerSession envia payload para cliente**

```go
// actor/server_session/server_session.go:242-249
b, err := json.Marshal(p)
if err != nil { return }
s.conn.WriteMessage(websocket.BinaryMessage, b)
metrics.ReportServerWSMessage(s.id.String())
```

### 2.2 Diagrama ASCII

```text
[Exchange WS]
   |
   v
[consumer-<exchange>] --CBOR+MsgID--> [JetStream ingest streams]
   |                                      (trades/bookupdates/prestats/liquidations)
   |                                              |
   |                                              v
   |                                      [processor-<exchange>]
   |                                              |
   |                                  route by symbol (map[SYMBOL]PID)
   |                                              v
   |                                        [symbol:<SYM>]
   |                                   /       |       |       \
   |                                  v        v       v        v
   |                               [trade] [orderbook] [stat] [volume]
   |                                  |        |        |       |
   |                                  +--> realtime_* streams <--+
   |                                  +----> store_* streams  <--+
   |                                              |
   |                                              v
   |                                          [store]
   |                                              |
   |                                              v
   |                                     [Timescale/ClickHouse]
   |
   +------------------------------------------> [server_router] <--- subscribe/unsubscribe --- [server_session]
                                                              |
                                                              v
                                                         [WS clients]
```

### INFERRED

- Ordenação forte é local ao ator (mailbox serial), não global cross-stream.
- Fronteira de causalidade dominante é `exchange + symbol`.

### UNKNOWN

- Garantias exatas de ordering JetStream por subject em produção dependem de config runtime e padrão de publish concorrente por exchange.

---

## 3) Data Planes & JetStream Classes

### VERIFIED

### 3.1 Classes de stream e limites

```go
// pkg/nats/streams.go:43-52 (realtime)
return jetstream.StreamConfig{
    MaxBytes: 1024 * 1024 * 128, // 128MB
    Storage:  jetstream.MemoryStorage,
    MaxAge:   time.Minute * 5,
}
```

```go
// pkg/nats/streams.go:58-66 (store)
return jetstream.StreamConfig{
    MaxBytes: 1024 * 1024 * 1024 * 2, // 2GB
    Storage:  jetstream.FileStorage,
    MaxAge:   time.Hour * 12,
}
```

```go
// pkg/nats/streams.go:71-79 (ingest)
return jetstream.StreamConfig{
    MaxBytes: 1024 * 1024 * 1024 * 4, // 4GB
    Storage:  jetstream.FileStorage,
    MaxAge:   time.Hour * 12,
}
```

```go
// pkg/nats/streams.go:194-203
return jetstream.StreamConfig{
    Name:       s.Name(),
    Subjects:   []string{Subject{StreamType: s}.SubString()},
    MaxAge:     config.MaxAge,
    MaxBytes:   config.MaxBytes,
    Storage:    config.Storage,
    MaxMsgSize: 1024 * 1024 * 10, // 10MB
}
```

### 3.2 Tabela de planos

| Plane | Streams | SLA primário | Retention/Storage | Replay | Failure semantics |
|---|---|---|---|---|---|
| Ephemeral truth | `rt_*` | latência para cliente | `Memory`, `5m`, `128MB` | baixo | perda aceitável sob restart/eviction da janela |
| Transport truth (ingest) | `trades/bookupdates/prestats/liquidations` | desacoplamento ingest->process | `File`, `12h`, `4GB` | sim (janela) | suporte a replay curto após falha de processor |
| Transport truth (store) | `store_*` | buffer process->persist | `File`, `12h`, `2GB` | sim (janela) | store pode recuperar backlog dentro da retenção |
| Canonical truth | DB (`candles/volumes/heatmaps/stats`) | consulta histórica | persistente | sim (DB) | depende de idempotência/constraints do engine |

### INFERRED

- O design assume 3 verdades com custos diferentes: tempo real efêmero, transporte replayável de curto prazo e canônico persistente.

### UNKNOWN

- Configurações adicionais de stream (retention policy/discard/duplicate window) não aparecem explicitamente no código; podem estar em defaults JetStream.

---

## 4) Wire & Contracts

### VERIFIED

### 4.1 Contrato interno: CBOR + tags compactas

```go
// event/event.go:31-37
type Trade struct {
    ID    string  `cbor:"0" json:"id,omitempty"`
    Pair  *Pair   `cbor:"1" json:"pair,omitempty"`
    Unix  int64   `cbor:"2" json:"unix,omitempty"`
    Price float64 `cbor:"3" json:"price,omitempty"`
    Qty   float64 `cbor:"4" json:"qty,omitempty"`
    IsBuy bool    `cbor:"5" json:"isBuy,omitempty"`
}
```

```go
// event/encoding.go:21-27
if err := cbor.Unmarshal(msg, v); err != nil {
    reportDecode(st, string(meta.Stream), err)
    return err
}
reportDecode(st, string(meta.Stream), nil)
return fn(v, meta)
```

### 4.2 MsgID / dedup

```go
// pkg/nats/producer.go:132-137
if params.MsgID == "" {
    params.MsgID = uuid.NewString()
}
_, err := p.js.Publish(ctx, params.Subject, params.Msg, jetstream.WithMsgID(params.MsgID))
```

```go
// event/event.go:163-165
func (t *Trade) Key() string {
    return fmt.Sprintf("trade:%s:%s:%s", t.Pair.Exchange, t.Pair.Symbol, t.ID)
}
```

```go
// actor/trade/trade.go:79-85
key := candles.Key()
if stream == nats.StreamTypeRealTimeCandle {
    key = uuid.NewString()
}
```

### 4.3 Contrato REAL cliente (on-the-wire)

- Server serializa evento para JSON e embala em `WSPayload` JSON com `Data []byte`.
- Em JSON, `[]byte` vira string base64.
- Cliente decodifica base64 e faz `json.unmarshal` do payload interno.

```go
// actor/server_session/server_session.go:65-71
data, _ := json.Marshal(msg)
payload := types.WSPayload{
    Pair:      event.NewPair(msg.Pair.Exchange, msg.Pair.Symbol),
    Stream:    event.StreamTrades,
    Timeframe: 0,
    Data:      data,
}
```

```go
// types/types.go:17-22
type WSPayload struct {
    Pair      *event.Pair  `json:"pair"`
    Stream    event.Stream `json:"stream"`
    Timeframe int64        `json:"timeframe"`
    Data      []byte       `json:"data"`
}
```

```odin
// client/src/app.odin:188-190
handle_ws_payload :: proc(payload: Payload) -> HandleWSError {
    decoded := base64.decode(payload.data, allocator = context.temp_allocator) or_return
```

```odin
// client/src/types.odin:56-60
Payload :: struct {
    pair:      Pair `json:"Pair"`,
    stream:    StreamType `json:"Stream"`,
    timeframe: i64 `json:"Timeframe"`,
    data:      string `json:"Data"`,
}
```

### INFERRED

- O protocolo cliente efetivo é `JSON envelope + base64(JSON data)`, não CBOR end-to-end.

### UNKNOWN

- Estratégia formal de evolução de schema (version envelope per-event, compatibility matrix) não está implementada explicitamente.

---

## 5) ACK, Durability & Failure Boundaries

### VERIFIED

### 5.1 Ponto exato de ACK

```go
// pkg/nats/consumer.go:165-175
cctx, err := consumer.Consume(func(msg jetstream.Msg) {
    meta, _ := msg.Metadata()
    if err := params.Handler(msg.Data(), meta); err != nil {
        ...
        msg.Nak()
        return
    }
    msg.Ack()
})
```

### 5.2 ACK significa o quê?

No path de store, handler retorna `nil` após enqueue no ator, não após commit DB:

```go
// actor/store/store.go:136-139
func (s *Store) handleCandles(candles *event.Candles, _ *jetstream.MsgMetadata) error {
    s.ctx.Send(s.ctx.PID(), candles)
    return nil
}
```

Commit DB acontece no receive loop depois:

```go
// actor/store/store.go:72-76
if err := s.client.InsertCandles(c.Context(), msg.Pair, msg.Values); err != nil {
    metrics.ReportStoreInsertError("candles", "insert_error")
    break
}
metrics.ReportStoreInsertion("candles", msg.Pair.Exchange, msg.Pair.Symbol, st)
```

**Conclusão:** boundary atual é `ack-on-enqueue`, não `ack-on-commit`.

### 5.3 Failure modes concretos

1. **Decode error -> NAK**

```go
// event/encoding.go:21-24
if err := cbor.Unmarshal(msg, v); err != nil {
    reportDecode(st, string(meta.Stream), err)
    return err
}
```

```go
// pkg/nats/consumer.go:167-172
if err := params.Handler(msg.Data(), meta); err != nil {
    ...
    msg.Nak()
    return
}
```

2. **Consumer closed bug reconhecido no código**

```go
// pkg/nats/consumer.go:194-199
// TODO:
// ... if there is a nats error, the consumer will not be restarted ...
// ... we will not know about it.
```

3. **Publish errors silenciosos no path processor output**

```go
// actor/orderbook/orderbook.go:260-268
o.producer.Publish(c.Context(), nats.PublishParams{
    Subject: ...,
    Msg:   b,
    MsgID: uuid.NewString(),
})
```

4. **Store insert error não reprocessa localmente**

```go
// actor/store/store.go:87-94
if err := s.client.InsertVolumes(c.Context(), msg.Values); err != nil {
    metrics.ReportStoreInsertError("volumes", "insert_error")
}
...
if err := s.client.InsertStats(c.Context(), msg); err != nil {
    metrics.ReportStoreInsertError("stats", "insert_error")
}
```

5. **WS fail-fast por panic**

```go
// actor/consumer/ws/ws.go:68-72
case WebsocketError:
    slog.Error("websocket error", "error", msg.Err)
    c.Stop()
    panic(msg.Err)
```

### INFERRED

- Existe risco de perda na janela entre ACK e persistência (crash no store actor/processo).
- Poison message policy não está formalizada (sem DLQ explícita no createConsumer).

### UNKNOWN

- Política JetStream de redelivery limite (`MaxDeliver`) efetiva em produção (defaults podem variar).  
How to verify: inspecionar `ConsumerInfo.Config.MaxDeliver` via `nats consumer info <stream> <durable>`.

---

## 6) Hidden Invariants (INV-MM-01..INV-MM-15)

### VERIFIED

| INV | Descrição | Onde aparece | Risco se quebrar | Como detectar |
|---|---|---|---|---|
| INV-MM-01 | Roteamento por símbolo no processor (single path por símbolo). | `actor/processor/processor.go:90-100` | estado de símbolo corrompido por multi-writer | latência por processor + testes de race |
| INV-MM-02 | `Symbol` fanout fixo para quatro domínios (`trade/stat/book/volume`). | `actor/symbol/symbol.go:41-45` | drift entre domínios derivados | divergência de métricas de output por stream |
| INV-MM-03 | Durable naming por `stream:exchange`. | `pkg/nats/streams.go:188-190`, `actor/processor/processor.go:181` | consumidores concorrentes inesperados | `nats consumer ls` por stream |
| INV-MM-04 | Store durable fixo `stream:STORE`. | `actor/store/store.go:126` | conflitos de consumo store | monitorar lag do durable store |
| INV-MM-05 | Streams realtime são memory/short retention. | `pkg/nats/streams.go:49-52` | tentar usar realtime como fonte histórica | perda de replay esperado |
| INV-MM-06 | Streams store/ingest usam file com replay 12h. | `pkg/nats/streams.go:63-66`, `pkg/nats/streams.go:76-79` | backlog maior que janela perde dados | lag + age de mensagens |
| INV-MM-07 | Candles canônicos: apenas `Final && timeframe==60`. | `actor/trade/trade.go:65-67` | canônico inconsistente por granularidade | auditoria DB contra stream |
| INV-MM-08 | Stats canônicos: apenas 1m final; realtime não recebe esse branch. | `actor/stat/stat.go:73-78` | clientes não verem fechamento final no realtime | comparação realtime vs getrange |
| INV-MM-09 | Volumes canônicos: apenas 1m final. | `actor/volume/volume.go:113-115` | explosão cardinalidade DB | validação schema + contagem writes |
| INV-MM-10 | Heatmap store é clock-driven por tick de 60s. | `actor/orderbook/orderbook.go:88-92`, `actor/symbol/symbol.go:58-60` | ausência de snapshot canônico por minuto | gap checker de heatmaps |
| INV-MM-11 | Orderbook mantém faixa de preço limitada (±10%). | `actor/orderbook/orderbook.go:113-115` | crescimento de memória/latência | métrica custom de níveis do livro |
| INV-MM-12 | Orderbook só processa updates úteis após `lastPrice` inicial. | `actor/orderbook/orderbook.go:83-85` | startup sem livro válido até trade | alarmes de livro vazio por símbolo |
| INV-MM-13 | MsgID determinístico em ingest/store e rand em realtime. | `event/event.go:163`, `actor/trade/trade.go:83-85` | dedup indevido ou explosão de duplicatas | contadores de duplicate ack no broker |
| INV-MM-14 | Router mantém 1 consumer por subject + refcount de sessões. | `actor/server_router/server_router.go:100-103`, `actor/server_router/server_router.go:143-147` | churn / leak de consumer | `server_router_active_subscriptions` + `nats consumer ls` |
| INV-MM-15 | ACK boundary do store é enqueue no ator, não commit DB. | `pkg/nats/consumer.go:174`, `actor/store/store.go:136-139` | perda sob crash entre enqueue e insert | teste de crash injection + reconciliação |

### INFERRED

- O sistema assume, operacionalmente, um processor ativo por exchange (também refletido no compose com um serviço por exchange).
- O contrato canônico é modelado como "verdade agregada por minuto" em vez de tick truth persistida.

### UNKNOWN

- Invariantes operacionais de produção (ex.: "nunca rodar 2 processors no mesmo exchange") não estão enforceados no código.

---

## 7) Scale (10x) & Backpressure

### VERIFIED

### 7.1 Pontos de bound existentes

1. **NATS stream bounds**: `MaxBytes`, `MaxAge`, `MaxMsgSize` por classe.
2. **WS manager capacity**: `MaxStreamsPerWebsocket`, `MaxWebsockets`, bucketização.
3. **Orderbook boundedness**: prune por faixa e output depth fixo.

```go
// actor/consumer/ws/manager.go:225-227
if totalStreams > maxTotalStreams {
    return nil, fmt.Errorf("total streams (%d) exceed maximum capacity (%d)", totalStreams, maxTotalStreams)
}
```

```go
// actor/orderbook/orderbook.go:223-224
depth := 2048
orderbook := event.Orderbook{ ... }
```

### 7.2 Lacunas de backpressure visíveis

1. **Consumer config sem `MaxAckPending`, `AckWait`, `MaxDeliver`, DLQ binding explícitos**.

```go
// pkg/nats/consumer.go:216-221
return s.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
    Durable:           params.Durable,
    FilterSubjects:    []string{params.Subject.SubString()},
    AckPolicy:         jetstream.AckExplicitPolicy,
    InactiveThreshold: time.Hour * 24,
})
```

2. **Fanout por subscriber sem shedding explícito**.

```go
// actor/server_router/server_router.go:89-91
subs.Pids.ForEach(func(i int, pid *actor.PID) {
    s.ctx.Send(pid, msg)
})
```

3. **Write WS sem controle explícito de fila/slow consumer**.

```go
// actor/server_session/server_session.go:248
s.conn.WriteMessage(websocket.BinaryMessage, b)
```

4. **Reconnect sem jitter (risco herd)**.

```go
// pkg/nats/consumer.go:72-74
delay := baseDelay * time.Duration(1<<attempts)
time.Sleep(delay)
```

### 7.3 Bend vs Break sob 10x

- **Bend (degrada mas funciona):** churn de assinatura no router, backlog no store streams, aumento de latência de encode/decode.
- **Break (risco real):** crash entre ACK e commit, reconnect cascades sem jitter, sessão WS lenta acumulando backlog sem política de drop.

### 7.4 Guardrails recomendados

1. Definir `MaxAckPending/AckWait/MaxDeliver` + DLQ stream para poison.
2. Introduzir jitter no backoff de reconnect.
3. Inserir fila bounded por sessão WS com política explícita de shedding.
4. Expor métricas de lag por durable e queue depth por sessão/ator.
5. Tornar boundary ACK configurável por plano (`enqueue` vs `commit`).

### INFERRED

- O runtime já trata memória como restrição em streams e orderbook, mas não fecha o loop de backpressure no fanout WS.

### UNKNOWN

- Capacidade real dos mailboxes dos atores no engine em runtime (não configurada explicitamente no repositório).

---

## 8) Store & Canonical Truth + Observability

### VERIFIED

### 8.1 Granularidade canônica

- Candles/Volumes/Heatmaps canônicos convergem para 1m final no path de store.
- Schema reforça isso (candles/volumes/heatmaps sem campo timeframe no schema base).

```sql
-- sql/timescale/20250313124730_init.up.sql:3-17
CREATE TABLE IF NOT EXISTS candles (
    unix BIGINT NOT NULL,
    ...
    exchange TEXT NOT NULL,
    symbol TEXT NOT NULL,
    PRIMARY KEY (unix, exchange, symbol),
    UNIQUE (unix, exchange, symbol)
);
```

```go
// actor/trade/trade.go:65-67
if candle.Final && timeframe == 60 && candle.Unix > 0 {
    t.publish(candles, nats.StreamTypeStoreCandle)
}
```

### 8.2 Timescale vs ClickHouse

**Timescale (mais idempotente no write path):**

```go
// pkg/db/timescale/timescale.go:152-156
const query = `
   INSERT INTO candles (...) VALUES (...)
    ON CONFLICT (unix, exchange, symbol) DO NOTHING`
```

**ClickHouse (risco de duplicata/erro silencioso de batch flush):**

```go
// pkg/db/clickhouse/clickhouse.go:204-210
batch, err := s.conn.PrepareBatch(ctx, query)
if err != nil {
    return err
}
defer batch.Send()

for _, candle := range candles {
```

```go
// pkg/db/clickhouse/clickhouse.go:230
return nil
```

### 8.3 Stubs/lacunas de persistência

```go
// pkg/db/timescale/timescale.go:59-61
func (s *client) GetStats(pair *event.Pair, from, to, timeframe int64) ([]*event.Stat, error) {
    return nil, nil
}
```

### 8.4 Superfície de observabilidade

Métricas existentes por domínio:

- consumer: publish duration/errors/count
- processor: process duration por tipo
- store: insertion duration/errors/count
- server: auth/connections/ws messages/subscriptions
- nats decode: decode duration/errors

```go
// pkg/metrics/metrics.go:79-85
StoreInsertionDuration = prometheus.NewHistogramVec(
    prometheus.HistogramOpts{
        Name: "store_insertion_duration_ms",
        Help: "Time taken to insert data into the database in milliseconds",
        Buckets: prometheus.ExponentialBuckets(1, 2, 16),
```

```go
// pkg/metrics/report.go:99-101
StoreInsertionDuration.
    WithLabelValues(stream).
    Observe(float64(time.Since(start).Microseconds()))
```

**Conflito de unidade identificado:** métrica nomeada como `ms`, observada em `Microseconds()`.

### 8.5 Lacunas de observability

- Sem métrica explícita de consumer lag por durable.
- Sem métrica de queue depth / mailbox depth.
- Sem contadores de drop/shed por sessão WS.
- `server_ws_messages_sent_total{session_id}` tem cardinalidade potencialmente alta por label de sessão.

### INFERRED

- Timescale tende a comportamento mais seguro contra duplicatas no write path que ClickHouse nesta implementação específica.

### UNKNOWN

- Qual engine roda em produção e com quais SLAs reais.

---

## 9) MarketMonkey -> Market-Raccoon Alignment Pack

### 9.1 Lessons worth stealing (10)

| # | Padrão no MarketMonkey | Evidência MM | Recomendação concreta para Market-Raccoon |
|---|---|---|---|
| 1 | Separar planos por SLA (`rt`, `ingest`, `store`, `canonical`) | `pkg/nats/streams.go:37-79` | Definir classes de stream no ADR de taxonomia de subjects. |
| 2 | MsgID determinístico para ingest/store | `event/event.go:163-230` | Padronizar builders determinísticos por tipo e validar colisão em CI. |
| 3 | MsgID randômico para realtime update streams | `actor/trade/trade.go:83-85` | Evitar dedup indevido em streams de delta contínuo. |
| 4 | Particionamento por símbolo com actor local stateful | `actor/processor/processor.go:90-100` | Garantir single-writer por partição lógica (`exchange+symbol`). |
| 5 | Durable naming estável por domínio | `pkg/nats/streams.go:188-190` | Convenção de durable naming no bootstrap de consumers. |
| 6 | Bucketização de upstream WS por capacidade | `actor/consumer/ws/manager.go:217-273` | Implementar budget de conexões/streams por connector. |
| 7 | Realtime + store dual publish no mesmo processor | `actor/processor/processor.go:41-50` | Separar derivação realtime da persistência sem duplicar pipeline. |
| 8 | Bounded orderbook (prune + depth fixo) | `actor/orderbook/orderbook.go:113-140`, `223` | BoundedMap + output cap para estabilidade de heap/payload. |
| 9 | Roteador de assinatura por subject com refcount | `actor/server_router/server_router.go:100-147` | Multiplexar assinaturas para reduzir consumers redundantes. |
| 10 | Métricas por fronteira arquitetural | `pkg/metrics/metrics.go:180-212` | Telemetria por domínio (ingest/process/store/serve), não só por serviço. |

### 9.2 Do-not-copy (8)

| # | Não copiar | Evidência MM | Ajuste recomendado no MR |
|---|---|---|---|
| 1 | ACK-before-commit para caminho canônico | `pkg/nats/consumer.go:174`, `actor/store/store.go:136-139` | Implementar ack policy explícita por plano com opção ack-on-commit. |
| 2 | Ausência de DLQ/poison policy explícita | `pkg/nats/consumer.go:206-221` | Configurar `MaxDeliver` + DLQ e alarme de poison rate. |
| 3 | Reconnect sem jitter | `pkg/nats/consumer.go:72-74`, `pkg/nats/producer.go:102-104` | Backoff exponencial com jitter para evitar herd. |
| 4 | Erros de publish ignorados em processors | `actor/orderbook/orderbook.go:260-268` | Exigir checagem de erro e contadores de falha por stream. |
| 5 | Dedup key fraca para liquidations (`pair+unix`) | `event/event.go:192-194` | Incluir ID de evento da exchange quando disponível. |
| 6 | Batch flush de ClickHouse sem checagem explícita de erro | `pkg/db/clickhouse/clickhouse.go:208`, `230` | Tornar flush síncrono com erro propagado e retry policy. |
| 7 | Inconsistência de unidade de métrica | `pkg/metrics/metrics.go:81`, `pkg/metrics/report.go:101` | Padronizar unidade no nome e no observe. |
| 8 | Lacunas de API canônica em engine alternativo | `pkg/db/timescale/timescale.go:59-61` | Definir parity matrix obrigatória entre backends suportados. |

### 9.3 Roadmap P0/P1/P2 (com critérios de aceite)

| Prioridade | Mudança | Critério de aceite testável |
|---|---|---|
| P0 | Política de ACK por plano (`enqueue` vs `commit`) | Teste de caos: crash entre enqueue/commit; verificar perda/dup conforme política definida. |
| P0 | DLQ policy (`MaxDeliver`, dead-letter stream, alarmes) | Injetar mensagem inválida; deve sair da fila principal para DLQ após N tentativas. |
| P0 | Jitter em reconnect de NATS/WS | Teste de desconexão em massa: histogram de reconnect não deve concentrar no mesmo segundo. |
| P0 | Checagem obrigatória de erros de publish e flush DB | Testes unit/integration falham se publish/flush error for ignorado. |
| P1 | Envelope de versão para wire/eventos | Testes de compatibilidade backward/forward com fixtures versionadas. |
| P1 | Métricas de lag/queue depth/drop por sessão | Dashboard com lag por durable e drops por sessão; alertas acionáveis. |
| P1 | Contrato explícito de dedup key por tipo | Fuzz de keys sem colisão acima de threshold definido por tipo de evento. |
| P2 | Golden replay E2E (ingest->process->canonical) | Replay determinístico deve reproduzir snapshot canônico esperado. |
| P2 | Testes de carga com fanout extremo de subscribers | 10x subscribers mantendo limites de memória e SLO de latência. |

### INFERRED

- O MM já demonstra entendimento de runtime em múltiplos planos de verdade, mas precisa formalizar fronteiras de durabilidade para subir de maturidade operacional.

### UNKNOWN

- Quais contratos já existem no `Market-Raccoon` (ADR/RFC IDs reais) para mapear 1:1 com essas recomendações.

---

# Appendix

## A) VERIFIED Evidence Index

| Claim | path:line | snippet curto |
|---|---|---|
| Consumer sobe por exchange | `cmd/consumer/main.go:42-56` | `switch *exchange { ... e.Spawn(... ) }` |
| Processor restart ilimitado | `cmd/processor/main.go:39-42` | `actor.WithMaxRestarts(math.MaxInt)` |
| Store serviço dedicado | `cmd/store/main.go:35-36` | `e.Spawn(store.New(dbClient), "store"...)` |
| Server serviço dedicado | `cmd/server/main.go:38-40` | `e.Spawn(server.New(httpServerAddr, dbClient), "server")` |
| Processor registra 4 ingest streams | `actor/processor/processor.go:19-37` | `StreamTypeTrade/BookUpdate/PreStat/Liquidation` |
| Processor cria symbol actors por market config | `actor/processor/processor.go:139-143` | `c.SpawnChild(symbol.New(pair), ...)` |
| Roteamento por símbolo | `actor/processor/processor.go:89-100` | `pid := p.symbols[strings.ToUpper(msg.Pair.Symbol)]` |
| Symbol fanout para 4 atores | `actor/symbol/symbol.go:41-45` | `Forward(book/stat/trade/volume)` |
| Tick de 1m para orderbook/store heatmap | `actor/symbol/symbol.go:58-60` | `Send(s.bookPID, types.Tick{Value: 60...})` |
| Streams de output do processor | `actor/processor/processor.go:41-50` | `rt_* + store_*` |
| Store consome `store_*` durable | `actor/store/store.go:121-128` | `Durable: reg.Stream.Durable("store")` |
| Realtime memory class | `pkg/nats/streams.go:49-52` | `Storage: MemoryStorage, MaxAge: 5m` |
| Store file class | `pkg/nats/streams.go:63-66` | `Storage: FileStorage, MaxAge: 12h` |
| Ingest file class | `pkg/nats/streams.go:76-79` | `Storage: FileStorage, MaxAge: 12h` |
| MaxMsgSize stream | `pkg/nats/streams.go:202` | `MaxMsgSize: 10MB` |
| Consumer ACK/NAK | `pkg/nats/consumer.go:167-175` | `if err -> Nak(); else Ack()` |
| Durable consumer config | `pkg/nats/consumer.go:216-221` | `AckPolicy + InactiveThreshold` |
| TODO bug close consumer | `pkg/nats/consumer.go:194-199` | `consumer will not be restarted` |
| Producer usa MsgID | `pkg/nats/producer.go:136` | `jetstream.WithMsgID(params.MsgID)` |
| Trade key determinística | `event/event.go:163-165` | `trade:%s:%s:%s` |
| Liquidation key fraca | `event/event.go:192-194` | `liquidation:%s:%s:%d` |
| Realtime candle MsgID randômica | `actor/trade/trade.go:83-85` | `if realtime { key = uuid.NewString() }` |
| Candle canônico só 1m final | `actor/trade/trade.go:65-67` | `if candle.Final && timeframe == 60` |
| Stat canônico 1m final | `actor/stat/stat.go:73-75` | `if msg.Final && tf == 60` |
| Volume canônico 1m final | `actor/volume/volume.go:113-115` | `if volume.Final && timeframe == 60` |
| Orderbook prune +/-10% | `actor/orderbook/orderbook.go:113-115` | `lowerBound = lastPrice*0.9; upper=1.1` |
| Orderbook publish depth 2048 | `actor/orderbook/orderbook.go:223` | `depth := 2048` |
| Router cria consumer por subject | `actor/server_router/server_router.go:100-103` | `if !exists -> createConsumer(subject)` |
| Router remove consumer no refcount 0 | `actor/server_router/server_router.go:143-147` | `if subCount == 0 { RemoveConsumer; delete }` |
| Router fanout por PIDSet | `actor/server_router/server_router.go:89-91` | `subs.Pids.ForEach(... Send(pid,msg))` |
| Server spawn router child | `actor/server/server.go:93-98` | `ctx.SpawnChild(serverrouter.New(), "router"...)` |
| Server spawn session por WS | `actor/server/server.go:139-141` | `SpawnChild(serversession.New(...))` |
| Session encode payload JSON | `actor/server_session/server_session.go:65-71` | `data,_:=json.Marshal(msg); payload.Data=data` |
| Session escreve BinaryMessage | `actor/server_session/server_session.go:248` | `WriteMessage(websocket.BinaryMessage, b)` |
| Cliente decodifica base64 payload | `client/src/app.odin:189` | `decoded := base64.decode(payload.data...)` |
| Payload cliente define Data string | `client/src/types.odin:60` | `data: string` |
| README afirma CBOR no WS data | `README.md:22` | `data over websockets encoded with CBOR` |
| Timescale ON CONFLICT candles | `pkg/db/timescale/timescale.go:155` | `ON CONFLICT ... DO NOTHING` |
| Timescale ON CONFLICT stats | `pkg/db/timescale/timescale.go:273` | `ON CONFLICT ... DO NOTHING` |
| ClickHouse MergeTree sem unique contract | `sql/clickhouse/20250327122003_candles.up.sql:14` | `ENGINE = MergeTree()` |
| ClickHouse defer batch.Send | `pkg/db/clickhouse/clickhouse.go:208` | `defer batch.Send()` |
| ClickHouse insert retorna nil | `pkg/db/clickhouse/clickhouse.go:230` | `return nil` |
| Timescale GetStats stub | `pkg/db/timescale/timescale.go:59-61` | `return nil, nil` |
| Métrica store nomeada ms | `pkg/metrics/metrics.go:81` | `store_insertion_duration_ms` |
| Métrica store observa us | `pkg/metrics/report.go:101` | `time.Since(start).Microseconds()` |
| WS manager capacidade streams | `actor/consumer/ws/manager.go:225-227` | `if totalStreams > maxTotalStreams { error }` |
| WS consumer fail-fast panic | `actor/consumer/ws/ws.go:68-72` | `case WebsocketError ... panic(msg.Err)` |

## B) INFERRED (com confiança)

| Inferência | Confiança |
|---|---|
| Arquitetura em 3 planos de verdade (ephemeral/transport/canonical). | High |
| Fronteira de ordenação forte é local ao ator por símbolo. | High |
| Boundary de durabilidade efetivo do store é ack-on-enqueue. | High |
| Risco de reconnect herd por ausência de jitter. | High |
| Risco de perda silenciosa por publish errors ignorados em outputs do processor. | High |
| `actor/combined` é caminho legado/fora do runtime principal. | Medium |
| Produção provavelmente opera com 1 processor por exchange. | Medium |
| Contrato realtime não garante entrega de evento final 1m em stats. | Medium |

## C) UNKNOWN + How to verify

| UNKNOWN | Como verificar objetivamente |
|---|---|
| Config JetStream efetiva de `MaxDeliver/AckWait/MaxAckPending` em produção. | `nats consumer info <stream> <durable>` e comparar com baseline ADR. |
| Política de DLQ real (se existe fora do código). | Inspecionar stream/consumer de DLQ e rotas de redelivery no ambiente. |
| Topologia real de produção (replicas, autoscaling, anti-affinity). | Exportar manifests deploy/prod; validar com `kubectl get deploy -o yaml` (ou equivalente). |
| Limites reais de mailbox/queue no runtime de atores. | Instrumentar depth por mailbox em runtime + stress test com subscribers lentos. |
| Engine DB efetivo em produção e estratégia de failover. | Inventário de env/provisioning e logs de bootstrap (`DATABASE_ENGINE`). |
| Taxa de colisão real das keys de liquidation em bursts. | Rodar replay de fluxo de liquidações e medir duplicate key ratio por janela de 1ms. |
| Impacto operacional do bug TODO de consumer close sem restart. | Chaos test: forçar desconexão NATS e monitorar se consumers retomam sem intervenção. |

## D) Risk Register (probabilidade x impacto)

| ID | Risco | Probabilidade | Impacto | Evidência | Mitigação |
|---|---|---|---|---|---|
| R1 | Perda entre ACK e commit no store | Alta | Alto | `pkg/nats/consumer.go:174`, `actor/store/store.go:136-139` | Política de ack-on-commit para stream canônico ou transactional outbox. |
| R2 | Poison loop sem DLQ explícita | Média | Alto | `pkg/nats/consumer.go:171`, `206-221` | `MaxDeliver + DLQ + alertas`. |
| R3 | Reconnect herd/cascata | Alta | Médio-Alto | `pkg/nats/consumer.go:72-74` | Exponential backoff com jitter. |
| R4 | Erro de publish não observado | Alta | Médio | `actor/orderbook/orderbook.go:260-268` | Checagem obrigatória de erro + métricas de falha. |
| R5 | Duplicatas em ClickHouse | Média | Alto | `sql/clickhouse/* MergeTree`, `pkg/db/clickhouse/clickhouse.go:208` | Dedup contract forte (Replacing/keys) + validação pós-write. |
| R6 | Churn de consumer no router com explosão de subjects | Média | Médio-Alto | `actor/server_router/server_router.go:100-147` | Cache LRU de subjects + hysteresis/TTL em unsubscribe. |
| R7 | Slow consumers WS sem shedding | Média | Alto | `actor/server_session/server_session.go:248` | Outbound queue bounded + drop policy e métricas de drop. |
| R8 | Inconsistência de unidades em métricas | Alta | Médio | `pkg/metrics/metrics.go:81`, `pkg/metrics/report.go:101` | Normalizar naming e unidade, com lint de métricas. |

## E) Suggested Test Plan (smoke + chaos + load)

### Smoke

1. **E2E ingest->process->serve**
- Subir `consumer + processor + server + nats`.
- Assinar stream de trades e confirmar recebimento contínuo por símbolo.

2. **E2E store path**
- Subir `store` e validar inserção de `store_candles/store_stats/store_volumes/store_heatmaps` no DB.

3. **Wire contract client**
- Capturar frame WS e validar: JSON envelope, campo `data` base64, decode JSON interno.

### Chaos

1. **Crash between enqueue/commit (ACK boundary test)**
- Injetar kill no processo `store` imediatamente após handler enqueue e antes de insert.
- Critério: medir perda/dup e confirmar semântica atual vs alvo.

2. **NATS disconnect / consumer closed**
- Cortar conectividade NATS e restaurar.
- Critério: consumers retomam automaticamente; nenhum durable fica estagnado.

3. **Poison message**
- Publicar payload inválido CBOR no stream.
- Critério: mensagem deve seguir política esperada (DLQ ou limite de redelivery) sem loop infinito.

4. **Reconnect storm**
- Derrubar múltiplos upstreams simultaneamente.
- Critério: reconexões distribuídas no tempo (com jitter), sem spike sincronizado.

### Load

1. **10x subscribers fanout**
- Simular explosão de assinantes por `exchange/symbol/timeframe`.
- Critério: latência p99 sob limite, memória estável, sem OOM.

2. **10x ingest burst**
- Replay acelerado em `trades/bookupdates`.
- Critério: backlog recuperável dentro da janela de retenção.

3. **Replay/golden deterministic test**
- Reprocessar janela histórica fixa.
- Critério: snapshot canônico (candles/stats/volumes/heatmaps) idêntico ao golden.

