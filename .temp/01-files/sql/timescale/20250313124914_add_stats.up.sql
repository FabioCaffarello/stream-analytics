-- Create regular table first
CREATE TABLE IF NOT EXISTS stats (
    unix BIGINT NOT NULL,
    exchange TEXT NOT NULL,
    symbol TEXT NOT NULL,
    timeframe BIGINT NOT NULL,
    liq_vsell DOUBLE PRECISION NOT NULL,
    liq_vbuy DOUBLE PRECISION NOT NULL,
    mark_price DOUBLE PRECISION NOT NULL,
    funding DOUBLE PRECISION NOT NULL,
    tbuy BIGINT NOT NULL,
    tsell BIGINT NOT NULL,
    final BOOLEAN NOT NULL,
    PRIMARY KEY (unix, exchange, symbol, timeframe)
);

-- Convert to hypertable
SELECT create_hypertable('stats', 'unix', 
    chunk_time_interval => 86400,  -- 1 day in seconds
    if_not_exists => TRUE
);

-- Create index on common query patterns
CREATE INDEX IF NOT EXISTS idx_stats_lookup 
ON stats (exchange, symbol, timeframe, unix DESC);