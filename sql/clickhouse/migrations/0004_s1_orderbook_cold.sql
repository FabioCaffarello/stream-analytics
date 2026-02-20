-- +goose NO TRANSACTION
-- +goose Up
-- S1 orderbook cold-path schema (aligned with Timescale hot-path).
-- Supersedes v3 with canonical column names matching hot-path schema.
CREATE TABLE IF NOT EXISTS aggregation_orderbook_snapshot_cold
(
    venue                  LowCardinality(String),
    instrument             LowCardinality(String),
    seq                    UInt64,
    bids_json              String,
    asks_json              String,
    source_idempotency_key String,
    created_at             DateTime64(3) DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (venue, instrument, seq, source_idempotency_key)
TTL toDateTime(created_at) + INTERVAL 90 DAY;

-- +goose Down
DROP TABLE IF EXISTS aggregation_orderbook_snapshot_cold;
