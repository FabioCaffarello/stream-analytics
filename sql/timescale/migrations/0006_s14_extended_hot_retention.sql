-- +goose Up
-- S14 extend hot-retention cleanup to cover all 7 artifact tables.
-- Adds indexes and DELETE clauses for tape, OI, delta_volume, CVD, bar_stats.

CREATE INDEX IF NOT EXISTS idx_aggregation_tape_timeframe_created_at
    ON aggregation_tape (timeframe, created_at);

CREATE INDEX IF NOT EXISTS idx_aggregation_oi_timeframe_created_at
    ON aggregation_oi (timeframe, created_at);

CREATE INDEX IF NOT EXISTS idx_aggregation_delta_volume_timeframe_created_at
    ON aggregation_delta_volume (timeframe, created_at);

CREATE INDEX IF NOT EXISTS idx_aggregation_cvd_timeframe_created_at
    ON aggregation_cvd (timeframe, created_at);

CREATE INDEX IF NOT EXISTS idx_aggregation_bar_stats_timeframe_created_at
    ON aggregation_bar_stats (timeframe, created_at);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION cleanup_aggregation_hot_retention(ref_ts TIMESTAMPTZ DEFAULT NOW())
RETURNS TABLE (
    deleted_candles       BIGINT,
    deleted_stats         BIGINT,
    deleted_tape          BIGINT,
    deleted_oi            BIGINT,
    deleted_delta_volume  BIGINT,
    deleted_cvd           BIGINT,
    deleted_bar_stats     BIGINT
)
LANGUAGE plpgsql
AS $$
DECLARE
    candle_rows  BIGINT := 0;
    stats_rows   BIGINT := 0;
    tape_rows    BIGINT := 0;
    oi_rows      BIGINT := 0;
    dv_rows      BIGINT := 0;
    cvd_rows     BIGINT := 0;
    bs_rows      BIGINT := 0;
BEGIN
    DELETE FROM aggregation_candle
    WHERE (timeframe IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '14 days')
       OR (timeframe NOT IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '90 days');
    GET DIAGNOSTICS candle_rows = ROW_COUNT;

    DELETE FROM aggregation_stats
    WHERE (timeframe IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '14 days')
       OR (timeframe NOT IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '90 days');
    GET DIAGNOSTICS stats_rows = ROW_COUNT;

    DELETE FROM aggregation_tape
    WHERE (timeframe IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '14 days')
       OR (timeframe NOT IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '90 days');
    GET DIAGNOSTICS tape_rows = ROW_COUNT;

    DELETE FROM aggregation_oi
    WHERE (timeframe IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '14 days')
       OR (timeframe NOT IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '90 days');
    GET DIAGNOSTICS oi_rows = ROW_COUNT;

    DELETE FROM aggregation_delta_volume
    WHERE (timeframe IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '14 days')
       OR (timeframe NOT IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '90 days');
    GET DIAGNOSTICS dv_rows = ROW_COUNT;

    DELETE FROM aggregation_cvd
    WHERE (timeframe IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '14 days')
       OR (timeframe NOT IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '90 days');
    GET DIAGNOSTICS cvd_rows = ROW_COUNT;

    DELETE FROM aggregation_bar_stats
    WHERE (timeframe IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '14 days')
       OR (timeframe NOT IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '90 days');
    GET DIAGNOSTICS bs_rows = ROW_COUNT;

    RETURN QUERY SELECT candle_rows, stats_rows, tape_rows, oi_rows, dv_rows, cvd_rows, bs_rows;
END;
$$;
-- +goose StatementEnd

COMMENT ON FUNCTION cleanup_aggregation_hot_retention(TIMESTAMPTZ)
IS 'Operator-scheduled cleanup for all 7 aggregation hot tables with sub-minute retention guardrail.';

-- +goose Down
-- Restore the original 2-table version from migration 0004.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION cleanup_aggregation_hot_retention(ref_ts TIMESTAMPTZ DEFAULT NOW())
RETURNS TABLE (deleted_candles BIGINT, deleted_stats BIGINT)
LANGUAGE plpgsql
AS $$
DECLARE
    candle_rows BIGINT := 0;
    stats_rows BIGINT := 0;
BEGIN
    DELETE FROM aggregation_candle
    WHERE (timeframe IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '14 days')
       OR (timeframe NOT IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '90 days');
    GET DIAGNOSTICS candle_rows = ROW_COUNT;

    DELETE FROM aggregation_stats
    WHERE (timeframe IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '14 days')
       OR (timeframe NOT IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '90 days');
    GET DIAGNOSTICS stats_rows = ROW_COUNT;

    RETURN QUERY SELECT candle_rows, stats_rows;
END;
$$;
-- +goose StatementEnd

DROP INDEX IF EXISTS idx_aggregation_bar_stats_timeframe_created_at;
DROP INDEX IF EXISTS idx_aggregation_cvd_timeframe_created_at;
DROP INDEX IF EXISTS idx_aggregation_delta_volume_timeframe_created_at;
DROP INDEX IF EXISTS idx_aggregation_oi_timeframe_created_at;
DROP INDEX IF EXISTS idx_aggregation_tape_timeframe_created_at;
