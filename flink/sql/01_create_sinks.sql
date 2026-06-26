-- PostgreSQL sink: fact_trades (append-only trade tape).
CREATE TABLE IF NOT EXISTS pg_fact_trades (
    exchange_name   STRING,
    symbol          STRING,
    trade_id        STRING,
    price           DOUBLE,
    quantity        DOUBLE,
    side            STRING,
    ts_exchange_ms  BIGINT,
    ts_ingest_ms    BIGINT,
    PRIMARY KEY (exchange_name, symbol, trade_id) NOT ENFORCED
) WITH (
    'connector'                   = 'jdbc',
    'url'                         = 'jdbc:postgresql://timescale:5432/raccoon?currentSchema=analytics',
    'table-name'                  = 'fact_trades',
    'username'                    = '${TIMESCALE_USER}',
    'password'                    = '${TIMESCALE_PASSWORD}',
    'sink.buffer-flush.max-rows'  = '500',
    'sink.buffer-flush.interval'  = '2s'
);

-- PostgreSQL sink: fact_candles (OHLCV aggregations).
-- open/high/low/close are reserved words in Flink SQL — use backtick quoting.
CREATE TABLE IF NOT EXISTS pg_fact_candles (
    exchange_name   STRING,
    symbol          STRING,
    timeframe       STRING,
    open_time_ms    BIGINT,
    `open`          DOUBLE,
    `high`          DOUBLE,
    `low`           DOUBLE,
    `close`         DOUBLE,
    volume          DOUBLE,
    trade_count     BIGINT,
    PRIMARY KEY (exchange_name, symbol, timeframe, open_time_ms) NOT ENFORCED
) WITH (
    'connector'                   = 'jdbc',
    'url'                         = 'jdbc:postgresql://timescale:5432/raccoon?currentSchema=analytics',
    'table-name'                  = 'fact_candles',
    'username'                    = '${TIMESCALE_USER}',
    'password'                    = '${TIMESCALE_PASSWORD}',
    'sink.buffer-flush.max-rows'  = '200',
    'sink.buffer-flush.interval'  = '5s'
);

-- PostgreSQL sink: fact_volume_stats (volume window aggregations).
CREATE TABLE IF NOT EXISTS pg_fact_volume_stats (
    exchange_name    STRING,
    symbol           STRING,
    window_start_ms  BIGINT,
    window_secs      INT,
    total_volume     DOUBLE,
    buy_volume       DOUBLE,
    sell_volume      DOUBLE,
    trade_count      BIGINT,
    vwap             DOUBLE,
    PRIMARY KEY (exchange_name, symbol, window_secs, window_start_ms) NOT ENFORCED
) WITH (
    'connector'                   = 'jdbc',
    'url'                         = 'jdbc:postgresql://timescale:5432/raccoon?currentSchema=analytics',
    'table-name'                  = 'fact_volume_stats',
    'username'                    = '${TIMESCALE_USER}',
    'password'                    = '${TIMESCALE_PASSWORD}',
    'sink.buffer-flush.max-rows'  = '200',
    'sink.buffer-flush.interval'  = '5s'
);
