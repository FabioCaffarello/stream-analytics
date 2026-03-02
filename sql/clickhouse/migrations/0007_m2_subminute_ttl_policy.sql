-- +goose NO TRANSACTION
-- +goose Up
-- M2 sub-minute retention policy for candle/stats cold artifacts.
-- Keep sub-minute data for shorter horizon to control storage cost.

ALTER TABLE aggregation_candle_cold
MODIFY TTL if(
    timeframe IN ('1s', '5s'),
    toDateTime(created_at) + INTERVAL 14 DAY,
    toDateTime(created_at) + INTERVAL 90 DAY
);

ALTER TABLE aggregation_stats_cold
MODIFY TTL if(
    timeframe IN ('1s', '5s'),
    toDateTime(created_at) + INTERVAL 14 DAY,
    toDateTime(created_at) + INTERVAL 90 DAY
);

-- +goose Down
ALTER TABLE aggregation_stats_cold
MODIFY TTL toDateTime(created_at) + INTERVAL 90 DAY;

ALTER TABLE aggregation_candle_cold
MODIFY TTL toDateTime(created_at) + INTERVAL 90 DAY;
