-- +goose Up
-- Analytics DW schema: dimension and fact tables consumed by Flink → Metabase.
-- Uses a dedicated `analytics` schema to avoid collisions with existing tables.

CREATE SCHEMA IF NOT EXISTS analytics;

CREATE TABLE analytics.dim_exchange (
    exchange_key  SERIAL PRIMARY KEY,
    exchange_name TEXT NOT NULL UNIQUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE analytics.dim_symbol (
    symbol_key    SERIAL PRIMARY KEY,
    exchange_name TEXT NOT NULL,
    symbol        TEXT NOT NULL,
    base_asset    TEXT,
    quote_asset   TEXT,
    market_type   TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (exchange_name, symbol)
);

-- Fact: append-only trade tape written directly by Flink from market.trades.
CREATE TABLE analytics.fact_trades (
    exchange_name  TEXT NOT NULL,
    symbol         TEXT NOT NULL,
    trade_id       TEXT NOT NULL,
    price          DOUBLE PRECISION NOT NULL,
    quantity       DOUBLE PRECISION NOT NULL,
    side           TEXT NOT NULL CHECK (side IN ('buy','sell')),
    ts_exchange_ms BIGINT NOT NULL,
    ts_ingest_ms   BIGINT NOT NULL,
    ingested_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (exchange_name, symbol, trade_id)
);

CREATE INDEX idx_fact_trades_ts ON analytics.fact_trades (ts_exchange_ms DESC);

-- Fact: OHLCV candles computed by Flink tumbling-window jobs.
CREATE TABLE analytics.fact_candles (
    exchange_name TEXT NOT NULL,
    symbol        TEXT NOT NULL,
    timeframe     TEXT NOT NULL CHECK (timeframe IN ('1m','5m','15m','1h')),
    open_time_ms  BIGINT NOT NULL,
    open          DOUBLE PRECISION NOT NULL,
    high          DOUBLE PRECISION NOT NULL,
    low           DOUBLE PRECISION NOT NULL,
    close         DOUBLE PRECISION NOT NULL,
    volume        DOUBLE PRECISION NOT NULL,
    trade_count   BIGINT NOT NULL,
    computed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (exchange_name, symbol, timeframe, open_time_ms)
);

CREATE INDEX idx_fact_candles_ts ON analytics.fact_candles (open_time_ms DESC);

-- Fact: volume statistics aggregated per window by Flink.
CREATE TABLE analytics.fact_volume_stats (
    exchange_name    TEXT NOT NULL,
    symbol           TEXT NOT NULL,
    window_start_ms  BIGINT NOT NULL,
    window_secs      INT NOT NULL,
    total_volume     DOUBLE PRECISION NOT NULL,
    buy_volume       DOUBLE PRECISION NOT NULL,
    sell_volume      DOUBLE PRECISION NOT NULL,
    trade_count      BIGINT NOT NULL,
    vwap             DOUBLE PRECISION,
    computed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (exchange_name, symbol, window_secs, window_start_ms)
);

-- +goose Down
DROP TABLE IF EXISTS analytics.fact_volume_stats;
DROP TABLE IF EXISTS analytics.fact_candles;
DROP TABLE IF EXISTS analytics.fact_trades;
DROP TABLE IF EXISTS analytics.dim_symbol;
DROP TABLE IF EXISTS analytics.dim_exchange;
DROP SCHEMA IF EXISTS analytics;
