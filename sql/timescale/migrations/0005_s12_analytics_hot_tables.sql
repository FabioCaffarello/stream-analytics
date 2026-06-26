-- +goose Up
-- S12 hot read model tables for tape, OI, delta volume, CVD, bar stats.

-- Tape trade-flow aggregation
CREATE TABLE IF NOT EXISTS aggregation_tape (
    venue              TEXT NOT NULL,
    instrument         TEXT NOT NULL,
    timeframe          TEXT NOT NULL,
    window_start       BIGINT NOT NULL,
    window_end         BIGINT NOT NULL,
    trade_count        BIGINT NOT NULL DEFAULT 0,
    buy_count          BIGINT NOT NULL DEFAULT 0,
    sell_count         BIGINT NOT NULL DEFAULT 0,
    buy_volume         DOUBLE PRECISION NOT NULL DEFAULT 0,
    sell_volume        DOUBLE PRECISION NOT NULL DEFAULT 0,
    total_volume       DOUBLE PRECISION NOT NULL DEFAULT 0,
    buy_notional       DOUBLE PRECISION NOT NULL DEFAULT 0,
    sell_notional      DOUBLE PRECISION NOT NULL DEFAULT 0,
    vwap_price         DOUBLE PRECISION NOT NULL DEFAULT 0,
    max_price          DOUBLE PRECISION NOT NULL DEFAULT 0,
    min_price          DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_price         DOUBLE PRECISION NOT NULL DEFAULT 0,
    max_trade_size     DOUBLE PRECISION NOT NULL DEFAULT 0,
    rate_trades_per_sec DOUBLE PRECISION NOT NULL DEFAULT 0,
    volume_imbalance   DOUBLE PRECISION NOT NULL DEFAULT 0,
    is_burst           BOOLEAN NOT NULL DEFAULT FALSE,
    seq_last           BIGINT NOT NULL,
    idempotency_key    TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue, instrument, timeframe, window_start)
);

-- Open-interest aggregation
CREATE TABLE IF NOT EXISTS aggregation_oi (
    venue           TEXT NOT NULL,
    instrument      TEXT NOT NULL,
    timeframe       TEXT NOT NULL,
    window_start    BIGINT NOT NULL,
    window_end      BIGINT NOT NULL,
    open_interest   DOUBLE PRECISION NOT NULL DEFAULT 0,
    delta           DOUBLE PRECISION NOT NULL DEFAULT 0,
    delta_pct       DOUBLE PRECISION NOT NULL DEFAULT 0,
    seq             BIGINT NOT NULL,
    ts_ingest       BIGINT NOT NULL,
    idempotency_key TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue, instrument, timeframe, window_start)
);

-- Delta-volume aggregation
CREATE TABLE IF NOT EXISTS aggregation_delta_volume (
    venue           TEXT NOT NULL,
    instrument      TEXT NOT NULL,
    timeframe       TEXT NOT NULL,
    window_start    BIGINT NOT NULL,
    window_end      BIGINT NOT NULL,
    buy_volume      DOUBLE PRECISION NOT NULL DEFAULT 0,
    sell_volume     DOUBLE PRECISION NOT NULL DEFAULT 0,
    delta_volume    DOUBLE PRECISION NOT NULL DEFAULT 0,
    seq             BIGINT NOT NULL,
    ts_ingest       BIGINT NOT NULL,
    idempotency_key TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue, instrument, timeframe, window_start)
);

-- Cumulative volume delta aggregation
CREATE TABLE IF NOT EXISTS aggregation_cvd (
    venue           TEXT NOT NULL,
    instrument      TEXT NOT NULL,
    timeframe       TEXT NOT NULL,
    window_start    BIGINT NOT NULL,
    window_end      BIGINT NOT NULL,
    delta_volume    DOUBLE PRECISION NOT NULL DEFAULT 0,
    cvd             DOUBLE PRECISION NOT NULL DEFAULT 0,
    seq             BIGINT NOT NULL,
    ts_ingest       BIGINT NOT NULL,
    idempotency_key TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue, instrument, timeframe, window_start)
);

-- Bar-statistics aggregation
CREATE TABLE IF NOT EXISTS aggregation_bar_stats (
    venue           TEXT NOT NULL,
    instrument      TEXT NOT NULL,
    timeframe       TEXT NOT NULL,
    window_start    BIGINT NOT NULL,
    window_end      BIGINT NOT NULL,
    trade_count     BIGINT NOT NULL DEFAULT 0,
    buy_count       BIGINT NOT NULL DEFAULT 0,
    sell_count      BIGINT NOT NULL DEFAULT 0,
    total_volume    DOUBLE PRECISION NOT NULL DEFAULT 0,
    buy_volume      DOUBLE PRECISION NOT NULL DEFAULT 0,
    sell_volume     DOUBLE PRECISION NOT NULL DEFAULT 0,
    vwap_price      DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_price      DOUBLE PRECISION NOT NULL DEFAULT 0,
    max_price       DOUBLE PRECISION NOT NULL DEFAULT 0,
    min_price       DOUBLE PRECISION NOT NULL DEFAULT 0,
    imbalance       DOUBLE PRECISION NOT NULL DEFAULT 0,
    is_burst        BOOLEAN NOT NULL DEFAULT FALSE,
    seq             BIGINT NOT NULL,
    ts_ingest       BIGINT NOT NULL,
    idempotency_key TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue, instrument, timeframe, window_start)
);

-- +goose Down
DROP TABLE IF EXISTS aggregation_bar_stats;
DROP TABLE IF EXISTS aggregation_cvd;
DROP TABLE IF EXISTS aggregation_delta_volume;
DROP TABLE IF EXISTS aggregation_oi;
DROP TABLE IF EXISTS aggregation_tape;
