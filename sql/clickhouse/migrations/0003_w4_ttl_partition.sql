-- +goose NO TRANSACTION
-- +goose Up
-- W4 cold-path TTL + monthly partitioning.
-- Adds ts column for time-based partition and automatic expiry.
-- TTL 90 days: snapshots older than 90 days are dropped automatically.
-- PARTITION BY toYYYYMM(ts): monthly partitions for efficient drops and queries.
CREATE TABLE IF NOT EXISTS aggregation_snapshots_v3
(
  subject                LowCardinality(String),
  venue                  LowCardinality(String),
  instrument             LowCardinality(String),
  seq                    UInt64,
  source_idempotency_key String,
  payload_hash           String,
  bids_json              String,
  asks_json              String,
  ts                     DateTime64(3) DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(ts)
ORDER BY (subject, venue, instrument, seq, source_idempotency_key)
TTL toDateTime(ts) + INTERVAL 90 DAY;

-- +goose Down
DROP TABLE IF EXISTS aggregation_snapshots_v3;
