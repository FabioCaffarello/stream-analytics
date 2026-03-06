-- +goose NO TRANSACTION
-- +goose Up
-- S13 sub-minute retention policy for 5 new cold analytics tables.
-- Matches the existing 0007 policy: 14d for sub-minute, 90d for minute+.

ALTER TABLE aggregation_tape_cold
MODIFY TTL if(
    timeframe IN ('250ms', '1s', '5s'),
    toDateTime(created_at) + INTERVAL 14 DAY,
    toDateTime(created_at) + INTERVAL 90 DAY
);

ALTER TABLE aggregation_oi_cold
MODIFY TTL if(
    timeframe IN ('1s', '5s'),
    toDateTime(created_at) + INTERVAL 14 DAY,
    toDateTime(created_at) + INTERVAL 90 DAY
);

ALTER TABLE aggregation_delta_volume_cold
MODIFY TTL if(
    timeframe IN ('250ms', '1s', '5s'),
    toDateTime(created_at) + INTERVAL 14 DAY,
    toDateTime(created_at) + INTERVAL 90 DAY
);

ALTER TABLE aggregation_cvd_cold
MODIFY TTL if(
    timeframe IN ('250ms', '1s', '5s'),
    toDateTime(created_at) + INTERVAL 14 DAY,
    toDateTime(created_at) + INTERVAL 90 DAY
);

ALTER TABLE aggregation_bar_stats_cold
MODIFY TTL if(
    timeframe IN ('250ms', '1s', '5s'),
    toDateTime(created_at) + INTERVAL 14 DAY,
    toDateTime(created_at) + INTERVAL 90 DAY
);

-- +goose Down
ALTER TABLE aggregation_bar_stats_cold
MODIFY TTL toDateTime(created_at) + INTERVAL 90 DAY;

ALTER TABLE aggregation_cvd_cold
MODIFY TTL toDateTime(created_at) + INTERVAL 90 DAY;

ALTER TABLE aggregation_delta_volume_cold
MODIFY TTL toDateTime(created_at) + INTERVAL 90 DAY;

ALTER TABLE aggregation_oi_cold
MODIFY TTL toDateTime(created_at) + INTERVAL 90 DAY;

ALTER TABLE aggregation_tape_cold
MODIFY TTL toDateTime(created_at) + INTERVAL 90 DAY;
