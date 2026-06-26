-- +goose Up
-- M2 hot-path retention guardrails for candle/stats.
-- Retention is enforced by an operator-scheduled cleanup function:
--   - sub-minute (`1s`,`5s`): 14 days
--   - other timeframes: 90 days

CREATE INDEX IF NOT EXISTS idx_aggregation_candle_timeframe_created_at
    ON aggregation_candle (timeframe, created_at);

CREATE INDEX IF NOT EXISTS idx_aggregation_stats_timeframe_created_at
    ON aggregation_stats (timeframe, created_at);

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
    WHERE
        (timeframe IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '14 days')
        OR
        (timeframe NOT IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '90 days');
    GET DIAGNOSTICS candle_rows = ROW_COUNT;

    DELETE FROM aggregation_stats
    WHERE
        (timeframe IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '14 days')
        OR
        (timeframe NOT IN ('1s', '5s') AND created_at < ref_ts - INTERVAL '90 days');
    GET DIAGNOSTICS stats_rows = ROW_COUNT;

    RETURN QUERY SELECT candle_rows, stats_rows;
END;
$$;
-- +goose StatementEnd

COMMENT ON FUNCTION cleanup_aggregation_hot_retention(TIMESTAMPTZ)
IS 'Operator-scheduled cleanup for aggregation hot tables with sub-minute retention guardrail.';

-- +goose Down
DROP FUNCTION IF EXISTS cleanup_aggregation_hot_retention(TIMESTAMPTZ);

DROP INDEX IF EXISTS idx_aggregation_stats_timeframe_created_at;
DROP INDEX IF EXISTS idx_aggregation_candle_timeframe_created_at;
