-- OHLCV candle aggregation: 1-minute tumbling windows.
-- open/high/low/close use backtick quoting (reserved keywords in Flink SQL).
INSERT INTO pg_fact_candles
SELECT
    venue AS exchange_name,
    symbol,
    '1m' AS timeframe,
    UNIX_TIMESTAMP(DATE_FORMAT(TUMBLE_START(event_time, INTERVAL '1' MINUTE), 'yyyy-MM-dd HH:mm:ss')) * 1000 AS open_time_ms,
    FIRST_VALUE(price) AS `open`,
    MAX(price)         AS `high`,
    MIN(price)         AS `low`,
    LAST_VALUE(price)  AS `close`,
    SUM(quantity)      AS volume,
    COUNT(*)           AS trade_count
FROM kafka_trades
GROUP BY venue, symbol, TUMBLE(event_time, INTERVAL '1' MINUTE);

-- OHLCV candle aggregation: 5-minute tumbling windows.
INSERT INTO pg_fact_candles
SELECT
    venue AS exchange_name,
    symbol,
    '5m' AS timeframe,
    UNIX_TIMESTAMP(DATE_FORMAT(TUMBLE_START(event_time, INTERVAL '5' MINUTE), 'yyyy-MM-dd HH:mm:ss')) * 1000 AS open_time_ms,
    FIRST_VALUE(price) AS `open`,
    MAX(price)         AS `high`,
    MIN(price)         AS `low`,
    LAST_VALUE(price)  AS `close`,
    SUM(quantity)      AS volume,
    COUNT(*)           AS trade_count
FROM kafka_trades
GROUP BY venue, symbol, TUMBLE(event_time, INTERVAL '5' MINUTE);

-- OHLCV candle aggregation: 15-minute tumbling windows.
INSERT INTO pg_fact_candles
SELECT
    venue AS exchange_name,
    symbol,
    '15m' AS timeframe,
    UNIX_TIMESTAMP(DATE_FORMAT(TUMBLE_START(event_time, INTERVAL '15' MINUTE), 'yyyy-MM-dd HH:mm:ss')) * 1000 AS open_time_ms,
    FIRST_VALUE(price) AS `open`,
    MAX(price)         AS `high`,
    MIN(price)         AS `low`,
    LAST_VALUE(price)  AS `close`,
    SUM(quantity)      AS volume,
    COUNT(*)           AS trade_count
FROM kafka_trades
GROUP BY venue, symbol, TUMBLE(event_time, INTERVAL '15' MINUTE);

-- OHLCV candle aggregation: 1-hour tumbling windows.
INSERT INTO pg_fact_candles
SELECT
    venue AS exchange_name,
    symbol,
    '1h' AS timeframe,
    UNIX_TIMESTAMP(DATE_FORMAT(TUMBLE_START(event_time, INTERVAL '1' HOUR), 'yyyy-MM-dd HH:mm:ss')) * 1000 AS open_time_ms,
    FIRST_VALUE(price) AS `open`,
    MAX(price)         AS `high`,
    MIN(price)         AS `low`,
    LAST_VALUE(price)  AS `close`,
    SUM(quantity)      AS volume,
    COUNT(*)           AS trade_count
FROM kafka_trades
GROUP BY venue, symbol, TUMBLE(event_time, INTERVAL '1' HOUR);
