-- M8 heatmap cold-path schema.

CREATE TABLE IF NOT EXISTS aggregation_heatmap_cold
(
    venue                  LowCardinality(String),
    instrument             LowCardinality(String),
    timeframe              LowCardinality(String),
    window_start           Int64,
    window_end             Int64,
    price_bucket_low       Float64,
    price_bucket_high      Float64,
    size_bucket            LowCardinality(String),
    bid_liquidity          Float64,
    ask_liquidity          Float64,
    trade_volume           Float64,
    seq_min                Int64,
    seq_max                Int64,
    samples                Int64,
    source_idempotency_key String,
    idempotency_key        String,
    created_at             DateTime64(3) DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (venue, instrument, timeframe, window_start, price_bucket_low, price_bucket_high, size_bucket, source_idempotency_key)
TTL toDateTime(created_at) + INTERVAL 90 DAY;
