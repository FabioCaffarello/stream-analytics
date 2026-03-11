CREATE TABLE IF NOT EXISTS marketmonkey.volumes (
    exchange LowCardinality(String),
    symbol LowCardinality(String),
    unix DateTime64(6),
    prices Array(Float64),
    buys Array(Float64),
    sells Array(Float64),
    price_group Float64,
    final Bool
) ENGINE = MergeTree()
ORDER BY (unix, exchange, symbol);
