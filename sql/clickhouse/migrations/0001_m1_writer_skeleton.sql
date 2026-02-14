-- M1 cold-path schema contract for deterministic idempotent upsert key.
-- Storage key invariant: (venue, instrument, seq).
CREATE TABLE IF NOT EXISTS aggregation_snapshots_v1
(
  venue      LowCardinality(String),
  instrument LowCardinality(String),
  seq        UInt64,
  bids_json  String,
  asks_json  String
)
ENGINE = ReplacingMergeTree
ORDER BY (venue, instrument, seq);
