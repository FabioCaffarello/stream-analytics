-- Kafka source: normalized trade events from the consumer analytics publisher.
CREATE TABLE IF NOT EXISTS kafka_trades (
    venue           STRING,
    symbol          STRING,
    trade_id        STRING,
    price           DOUBLE,
    quantity        DOUBLE,
    side            STRING,
    ts_exchange_ms  BIGINT,
    ts_ingest_ms    BIGINT,
    event_time      AS TO_TIMESTAMP_LTZ(ts_exchange_ms, 3),
    WATERMARK FOR event_time AS event_time - INTERVAL '5' SECOND
) WITH (
    'connector'                     = 'kafka',
    'topic'                         = 'market.trades',
    'properties.bootstrap.servers'  = 'kafka:9092',
    'properties.group.id'           = 'flink-market-trades',
    'scan.startup.mode'             = 'group-offsets',
    'format'                        = 'json',
    'json.fail-on-missing-field'    = 'false',
    'json.ignore-parse-errors'      = 'true'
);
