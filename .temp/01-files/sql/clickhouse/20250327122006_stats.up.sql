CREATE TABLE IF NOT EXISTS marketmonkey.stats (
    unix DateTime64(6),
    exchange LowCardinality(String),
    symbol LowCardinality(String),
    timeframe Int64,
    liq_vsell Float64,
    liq_vbuy Float64,
    mark_price Float64,
    funding Float64,
    tbuy Int64,
    tsell Int64,
    final Bool
) ENGINE = MergeTree()
ORDER BY (unix, exchange, symbol, timeframe);