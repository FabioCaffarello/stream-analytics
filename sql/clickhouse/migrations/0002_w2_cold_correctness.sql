-- +goose NO TRANSACTION
-- +goose Up
-- W2 cold-path correctness hardening.
-- Keep deterministic upsert key anchored on canonical subject + venue + instrument + seq.
-- source_idempotency_key is optional and used for forensic traceability.
CREATE TABLE IF NOT EXISTS aggregation_snapshots_v2
(
  subject                LowCardinality(String),
  venue                  LowCardinality(String),
  instrument             LowCardinality(String),
  seq                    UInt64,
  source_idempotency_key String,
  payload_hash           String,
  bids_json              String,
  asks_json              String
)
ENGINE = ReplacingMergeTree
ORDER BY (subject, venue, instrument, seq, source_idempotency_key);

-- +goose Down
DROP TABLE IF EXISTS aggregation_snapshots_v2;
