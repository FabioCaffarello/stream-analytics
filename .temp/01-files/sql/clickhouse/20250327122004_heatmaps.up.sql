CREATE TABLE IF NOT EXISTS marketmonkey.heatmaps (
    unix DateTime64(6),
    price_group Float64,
    exchange LowCardinality(String),
    symbol LowCardinality(String),
    prices Array(Float64),
    sizes Array(Float64),
    min_price Float64,
    max_price Float64,
    max_size Float64
) ENGINE = MergeTree()
ORDER BY (unix, exchange, symbol);