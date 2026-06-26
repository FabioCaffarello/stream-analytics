-- OHLCV candle aggregation — tumbling event-time windows.
-- open/high/low/close use backtick quoting (reserved keywords in Flink SQL).
--
-- Determinism of FIRST_VALUE/LAST_VALUE:
-- The Kafka producer keys every trade message as "venue:instrument"
-- (internal/adapters/kafka/market_publisher.go). Kafka routes all messages
-- with the same key to a single partition, and Flink's keyed state assigns
-- all records for a given (venue, symbol) to a single sub-task. Records are
-- therefore processed in Kafka-offset order within each symbol, making
-- FIRST_VALUE the earliest-sent trade (open) and LAST_VALUE the
-- latest-sent trade (close) for each window. This guarantee breaks if the
-- producer key or topic partition count changes without a consumer-group reset.
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
