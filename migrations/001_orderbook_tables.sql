-- S1 orderbook snapshot storage schema.
-- Applied externally by deployment tooling.

-- Timescale (hot path)
CREATE TABLE IF NOT EXISTS aggregation_orderbook_snapshot (
    venue       TEXT NOT NULL,
    instrument  TEXT NOT NULL,
    seq         BIGINT NOT NULL,
    bids_json   JSONB NOT NULL,
    asks_json   JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue, instrument, seq)
);

-- ClickHouse (cold path)
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
