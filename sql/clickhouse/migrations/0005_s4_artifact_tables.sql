-- +goose NO TRANSACTION
-- +goose Up
-- S4 artifact cold-path schema (candle, stats).

-- Candle OHLCV cold storage
CREATE TABLE IF NOT EXISTS aggregation_candle_cold
(
    venue           LowCardinality(String),
    instrument      LowCardinality(String),
    timeframe       LowCardinality(String),
    window_start    Int64,
    window_end      Int64,
    open_price      Float64,
    high_price      Float64,
    low_price       Float64,
    close_price     Float64,
    volume          Float64,
    buy_volume      Float64,
    sell_volume     Float64,
    trade_count     Int64,
    seq_first       Int64,
    seq_last        Int64,
    idempotency_key String,
    created_at      DateTime64(3) DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (venue, instrument, timeframe, window_start, idempotency_key)
TTL toDateTime(created_at) + INTERVAL 90 DAY;

-- Stats aggregation cold storage
CREATE TABLE IF NOT EXISTS aggregation_stats_cold
(
    venue             LowCardinality(String),
    instrument        LowCardinality(String),
    timeframe         LowCardinality(String),
    window_start      Int64,
    window_end        Int64,
    liq_buy_volume    Float64,
    liq_sell_volume   Float64,
    liq_total_volume  Float64,
    liq_count         Int64,
    markprice_open    Nullable(Float64),
    markprice_high    Nullable(Float64),
    markprice_low     Nullable(Float64),
    markprice_close   Nullable(Float64),
    funding_rate_avg  Nullable(Float64),
    funding_rate_last Nullable(Float64),
    seq_first         Int64,
    seq_last          Int64,
    idempotency_key   String,
    created_at        DateTime64(3) DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (venue, instrument, timeframe, window_start, idempotency_key)
TTL toDateTime(created_at) + INTERVAL 90 DAY;

-- +goose Down
DROP TABLE IF EXISTS aggregation_stats_cold;
DROP TABLE IF EXISTS aggregation_candle_cold;
