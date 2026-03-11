-- Create your UP migration here
-- Create candles table
CREATE TABLE IF NOT EXISTS candles (
    unix        BIGINT       		    NOT NULL,
    open        DOUBLE PRECISION  	NOT NULL,
    close       DOUBLE PRECISION  	NOT NULL,
    high        DOUBLE PRECISION  	NOT NULL,
    low         DOUBLE PRECISION  	NOT NULL,
    vbuy        DOUBLE PRECISION  	NOT NULL,
    vsell       DOUBLE PRECISION  	NOT NULL,
    tbuy        DOUBLE PRECISION  	NOT NULL,
    tsell       DOUBLE PRECISION  	NOT NULL,
    final       BOOLEAN           	NOT NULL,
    exchange    TEXT              	NOT NULL,
    symbol      TEXT              	NOT NULL,
    PRIMARY KEY (unix, exchange, symbol),
    UNIQUE (unix, exchange, symbol)
);

-- Create hypertable with 1-day chunks (good balance for 1m data)
SELECT create_hypertable(
  'candles',
  'unix',
  chunk_time_interval => 86400,  -- 1 day in seconds
  if_not_exists => true
);

-- Add composite index for fast filtering
CREATE INDEX IF NOT EXISTS idx_candles_pair_time ON candles (exchange, symbol, unix);

-- Create heatmaps table
CREATE TABLE IF NOT EXISTS heatmaps (
    unix        BIGINT            NOT NULL,
    price_group DOUBLE PRECISION  NOT NULL,
    exchange    TEXT              NOT NULL,
    symbol      TEXT              NOT NULL,
    prices      DOUBLE PRECISION[] NOT NULL,
    sizes       DOUBLE PRECISION[] NOT NULL,
    min_price   DOUBLE PRECISION NOT NULL,
    max_price   DOUBLE PRECISION NOT NULL,
    max_size    DOUBLE PRECISION NOT NULL,
    PRIMARY KEY (unix, exchange, symbol),
    UNIQUE (unix, exchange, symbol)
);

-- Create hypertable with 1-day chunks (consistent with candles table)
SELECT create_hypertable(
  'heatmaps',
  'unix',
  chunk_time_interval => 86400,  -- 1 day in seconds
  if_not_exists => true
);

-- Add composite index for fast filtering (same pattern as candles table)
CREATE INDEX IF NOT EXISTS idx_heatmaps_pair_time ON heatmaps (exchange, symbol, unix);

-- Create volumes table
CREATE TABLE IF NOT EXISTS volumes (
    exchange    TEXT              NOT NULL,
    symbol      TEXT              NOT NULL,
    unix        BIGINT            NOT NULL,
    prices      DOUBLE PRECISION[] NOT NULL,
    buys        DOUBLE PRECISION[] NOT NULL,
    sells       DOUBLE PRECISION[] NOT NULL,
    price_group DOUBLE PRECISION  NOT NULL,
    final       BOOLEAN           NOT NULL,
    PRIMARY KEY (unix, exchange, symbol, price_group),
    UNIQUE (unix, exchange, symbol)
);

-- Create hypertable with 1-day chunks
SELECT create_hypertable(
  'volumes',
  'unix',
  chunk_time_interval => 86400,  -- 1 day in seconds
  if_not_exists => true
);

-- Add composite index for fast filtering
CREATE INDEX IF NOT EXISTS idx_volume_pair_time ON volumes (exchange, symbol, unix);
