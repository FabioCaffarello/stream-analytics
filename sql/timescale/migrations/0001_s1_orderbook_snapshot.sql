-- +goose Up
-- S1 orderbook snapshot hot-path schema.

CREATE TABLE IF NOT EXISTS aggregation_orderbook_snapshot (
    venue       TEXT NOT NULL,
    instrument  TEXT NOT NULL,
    seq         BIGINT NOT NULL,
    bids_json   JSONB NOT NULL,
    asks_json   JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue, instrument, seq)
);
