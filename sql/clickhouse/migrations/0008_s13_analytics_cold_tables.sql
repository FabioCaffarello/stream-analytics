-- +goose NO TRANSACTION
-- +goose Up
-- S13 cold analytics tables for tape, OI, delta volume, CVD, bar stats.
-- Mirrors the Timescale hot tables (0005_s12) in ClickHouse for long-term retention.

-- Tape trade-flow cold storage
CREATE TABLE IF NOT EXISTS aggregation_tape_cold
(
    venue              LowCardinality(String),
    instrument         LowCardinality(String),
    timeframe          LowCardinality(String),
    window_start       Int64,
    window_end         Int64,
    trade_count        Int64,
    buy_count          Int64,
    sell_count         Int64,
    buy_volume         Float64,
    sell_volume        Float64,
    total_volume       Float64,
    buy_notional       Float64,
    sell_notional      Float64,
    vwap_price         Float64,
    max_price          Float64,
    min_price          Float64,
    last_price         Float64,
    max_trade_size     Float64,
    rate_trades_per_sec Float64,
    volume_imbalance   Float64,
    is_burst           UInt8,
    seq_last           Int64,
    idempotency_key    String,
    created_at         DateTime64(3) DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (venue, instrument, timeframe, window_start, idempotency_key)
TTL toDateTime(created_at) + INTERVAL 90 DAY;

-- Open-interest cold storage
CREATE TABLE IF NOT EXISTS aggregation_oi_cold
(
    venue           LowCardinality(String),
    instrument      LowCardinality(String),
    timeframe       LowCardinality(String),
    window_start    Int64,
    window_end      Int64,
    open_interest   Float64,
    delta           Float64,
    delta_pct       Float64,
    seq             Int64,
    ts_ingest       Int64,
    idempotency_key String,
    created_at      DateTime64(3) DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (venue, instrument, timeframe, window_start, idempotency_key)
TTL toDateTime(created_at) + INTERVAL 90 DAY;

-- Delta-volume cold storage
CREATE TABLE IF NOT EXISTS aggregation_delta_volume_cold
(
    venue           LowCardinality(String),
    instrument      LowCardinality(String),
    timeframe       LowCardinality(String),
    window_start    Int64,
    window_end      Int64,
    buy_volume      Float64,
    sell_volume     Float64,
    delta_volume    Float64,
    seq             Int64,
    ts_ingest       Int64,
    idempotency_key String,
    created_at      DateTime64(3) DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (venue, instrument, timeframe, window_start, idempotency_key)
TTL toDateTime(created_at) + INTERVAL 90 DAY;

-- Cumulative volume delta cold storage
CREATE TABLE IF NOT EXISTS aggregation_cvd_cold
(
    venue           LowCardinality(String),
    instrument      LowCardinality(String),
    timeframe       LowCardinality(String),
    window_start    Int64,
    window_end      Int64,
    delta_volume    Float64,
    cvd             Float64,
    seq             Int64,
    ts_ingest       Int64,
    idempotency_key String,
    created_at      DateTime64(3) DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (venue, instrument, timeframe, window_start, idempotency_key)
TTL toDateTime(created_at) + INTERVAL 90 DAY;

-- Bar-statistics cold storage
CREATE TABLE IF NOT EXISTS aggregation_bar_stats_cold
(
    venue           LowCardinality(String),
    instrument      LowCardinality(String),
    timeframe       LowCardinality(String),
    window_start    Int64,
    window_end      Int64,
    trade_count     Int64,
    buy_count       Int64,
    sell_count      Int64,
    total_volume    Float64,
    buy_volume      Float64,
    sell_volume     Float64,
    vwap_price      Float64,
    last_price      Float64,
    max_price       Float64,
    min_price       Float64,
    imbalance       Float64,
    is_burst        UInt8,
    seq             Int64,
    ts_ingest       Int64,
    idempotency_key String,
    created_at      DateTime64(3) DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (venue, instrument, timeframe, window_start, idempotency_key)
TTL toDateTime(created_at) + INTERVAL 90 DAY;

-- +goose Down
DROP TABLE IF EXISTS aggregation_bar_stats_cold;
DROP TABLE IF EXISTS aggregation_cvd_cold;
DROP TABLE IF EXISTS aggregation_delta_volume_cold;
DROP TABLE IF EXISTS aggregation_oi_cold;
DROP TABLE IF EXISTS aggregation_tape_cold;
