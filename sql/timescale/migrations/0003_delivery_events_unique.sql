-- +goose Up
-- Add UNIQUE constraint on (subject, seq) required by PgRangeStore ON CONFLICT upsert.
CREATE UNIQUE INDEX IF NOT EXISTS uq_delivery_events_subject_seq
    ON delivery_events (subject, seq);

-- +goose Down
DROP INDEX IF EXISTS uq_delivery_events_subject_seq;
