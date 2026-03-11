CREATE TABLE IF NOT EXISTS marketmonkey.candles (
    unix DateTime64(6),
    open Float64,
    close Float64,
    high Float64,
    low Float64,
    vbuy Float64,
    vsell Float64,
    tbuy Float64,
    tsell Float64,
    final Bool,
    exchange LowCardinality(String),
    symbol LowCardinality(String)
) ENGINE = MergeTree()
ORDER BY (unix, exchange, symbol);