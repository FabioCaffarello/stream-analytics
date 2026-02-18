-- S4 artifact storage schema (candle, stats, heatmap, volume profile).
-- Applied externally by deployment tooling.

-- Timescale (hot path): candle
CREATE TABLE IF NOT EXISTS aggregation_candle (
    venue           TEXT NOT NULL,
    instrument      TEXT NOT NULL,
    timeframe       TEXT NOT NULL,
    window_start    BIGINT NOT NULL,
    window_end      BIGINT NOT NULL,
    open_price      DOUBLE PRECISION NOT NULL,
    high_price      DOUBLE PRECISION NOT NULL,
    low_price       DOUBLE PRECISION NOT NULL,
    close_price     DOUBLE PRECISION NOT NULL,
    volume          DOUBLE PRECISION NOT NULL,
    buy_volume      DOUBLE PRECISION NOT NULL,
    sell_volume     DOUBLE PRECISION NOT NULL,
    trade_count     BIGINT NOT NULL,
    seq_first       BIGINT NOT NULL,
    seq_last        BIGINT NOT NULL,
    idempotency_key TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue, instrument, timeframe, window_start)
);

-- Timescale (hot path): stats
CREATE TABLE IF NOT EXISTS aggregation_stats (
    venue             TEXT NOT NULL,
    instrument        TEXT NOT NULL,
    timeframe         TEXT NOT NULL,
    window_start      BIGINT NOT NULL,
    window_end        BIGINT NOT NULL,
    liq_buy_volume    DOUBLE PRECISION NOT NULL DEFAULT 0,
    liq_sell_volume   DOUBLE PRECISION NOT NULL DEFAULT 0,
    liq_total_volume  DOUBLE PRECISION NOT NULL DEFAULT 0,
    liq_count         BIGINT NOT NULL DEFAULT 0,
    markprice_open    DOUBLE PRECISION,
    markprice_high    DOUBLE PRECISION,
    markprice_low     DOUBLE PRECISION,
    markprice_close   DOUBLE PRECISION,
    funding_rate_avg  DOUBLE PRECISION,
    funding_rate_last DOUBLE PRECISION,
    seq_first         BIGINT NOT NULL,
    seq_last          BIGINT NOT NULL,
    idempotency_key   TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue, instrument, timeframe, window_start)
);

-- Timescale (hot path): heatmap artifact cells
CREATE TABLE IF NOT EXISTS aggregation_heatmap (
    venue                  TEXT NOT NULL,
    instrument             TEXT NOT NULL,
    timeframe              TEXT NOT NULL,
    window_start_ts        BIGINT NOT NULL,
    window_end_ts          BIGINT NOT NULL,
    price_bucket_low       DOUBLE PRECISION NOT NULL,
    price_bucket_high      DOUBLE PRECISION NOT NULL,
    size_bucket            TEXT NOT NULL,
    bid_liquidity          DOUBLE PRECISION NOT NULL,
    ask_liquidity          DOUBLE PRECISION NOT NULL,
    trade_volume           DOUBLE PRECISION NOT NULL,
    seq_min                BIGINT NOT NULL,
    seq_max                BIGINT NOT NULL,
    samples                BIGINT NOT NULL,
    source_idempotency_key TEXT NOT NULL,
    idempotency_key        TEXT NOT NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (
        venue,
        instrument,
        timeframe,
        window_start_ts,
        price_bucket_low,
        price_bucket_high,
        size_bucket,
        source_idempotency_key
    )
);

-- Timescale (hot path): volume profile bucket aggregates
CREATE TABLE IF NOT EXISTS aggregation_volume_profile (
    venue             TEXT NOT NULL,
    instrument        TEXT NOT NULL,
    timeframe         TEXT NOT NULL,
    window_start_ts   BIGINT NOT NULL,
    bucket_low        DOUBLE PRECISION NOT NULL,
    bucket_high       DOUBLE PRECISION NOT NULL,
    buy_volume        DOUBLE PRECISION NOT NULL DEFAULT 0,
    sell_volume       DOUBLE PRECISION NOT NULL DEFAULT 0,
    total_volume      DOUBLE PRECISION NOT NULL DEFAULT 0,
    seq_min           BIGINT NOT NULL,
    seq_max           BIGINT NOT NULL,
    last_operation_id TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue, instrument, timeframe, window_start_ts, bucket_low, bucket_high)
);

-- Optional dedup log for VPVR operation ids (bounded by retention policy).
CREATE TABLE IF NOT EXISTS aggregation_volume_profile_oplog (
    operation_id   TEXT PRIMARY KEY,
    venue          TEXT NOT NULL,
    instrument     TEXT NOT NULL,
    timeframe      TEXT NOT NULL,
    window_start_ts BIGINT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ClickHouse (cold path): candle
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

-- ClickHouse (cold path): stats
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
