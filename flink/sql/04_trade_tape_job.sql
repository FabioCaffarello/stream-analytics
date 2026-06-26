-- Trade tape: append-only insert of every raw trade into the DW.
INSERT INTO pg_fact_trades
SELECT
    venue     AS exchange_name,
    symbol,
    trade_id,
    price,
    quantity,
    side,
    ts_exchange_ms,
    ts_ingest_ms
FROM kafka_trades;
