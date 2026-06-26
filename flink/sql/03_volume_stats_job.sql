-- Volume statistics aggregation: 5-minute tumbling windows.
INSERT INTO pg_fact_volume_stats
SELECT
    venue AS exchange_name,
    symbol,
    UNIX_TIMESTAMP(DATE_FORMAT(TUMBLE_START(event_time, INTERVAL '5' MINUTE), 'yyyy-MM-dd HH:mm:ss')) * 1000 AS window_start_ms,
    300 AS window_secs,
    SUM(quantity)                                                       AS total_volume,
    SUM(CASE WHEN side = 'buy'  THEN quantity ELSE 0.0 END)            AS buy_volume,
    SUM(CASE WHEN side = 'sell' THEN quantity ELSE 0.0 END)            AS sell_volume,
    COUNT(*)                                                            AS trade_count,
    SUM(price * quantity) / NULLIF(SUM(quantity), 0)                   AS vwap
FROM kafka_trades
GROUP BY venue, symbol, TUMBLE(event_time, INTERVAL '5' MINUTE);
