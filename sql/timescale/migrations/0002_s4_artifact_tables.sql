-- +goose Up
-- S4 artifact storage schema (candle, stats, heatmap, volume profile).

-- Candle OHLCV aggregation
CREATE TABLE IF NOT EXISTS aggregation_candle (
    venue           TEXT NOT NULL,
    instrument      TEXT NOT NULL,
    timeframe       TEXT NOT NULL,
    window_start    BIGINT NOT NULL,
    window_end      BIGINT NOT NULL,
    open_price      DOUBLE PRECISION NOT NULL,
    high_price      DOUBLE PRECISION NOT NULL,
    low_price       DOUBLE PRECISION NOT NULL,
    close_price     DOUBLE PRECISION NOT NULL,
    volume          DOUBLE PRECISION NOT NULL,
    buy_volume      DOUBLE PRECISION NOT NULL,
    sell_volume     DOUBLE PRECISION NOT NULL,
    trade_count     BIGINT NOT NULL,
    seq_first       BIGINT NOT NULL,
    seq_last        BIGINT NOT NULL,
    idempotency_key TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue, instrument, timeframe, window_start)
);

-- Stats aggregation (liquidation volume + markprice + funding rate per timeframe)
CREATE TABLE IF NOT EXISTS aggregation_stats (
    venue             TEXT NOT NULL,
    instrument        TEXT NOT NULL,
    timeframe         TEXT NOT NULL,
    window_start      BIGINT NOT NULL,
    window_end        BIGINT NOT NULL,
    liq_buy_volume    DOUBLE PRECISION NOT NULL DEFAULT 0,
    liq_sell_volume   DOUBLE PRECISION NOT NULL DEFAULT 0,
    liq_total_volume  DOUBLE PRECISION NOT NULL DEFAULT 0,
    liq_count         BIGINT NOT NULL DEFAULT 0,
    markprice_open    DOUBLE PRECISION,
    markprice_high    DOUBLE PRECISION,
    markprice_low     DOUBLE PRECISION,
    markprice_close   DOUBLE PRECISION,
    funding_rate_avg  DOUBLE PRECISION,
    funding_rate_last DOUBLE PRECISION,
    seq_first         BIGINT NOT NULL,
    seq_last          BIGINT NOT NULL,
    idempotency_key   TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue, instrument, timeframe, window_start)
);

-- Heatmap artifact cells
CREATE TABLE IF NOT EXISTS aggregation_heatmap (
    venue                  TEXT NOT NULL,
    instrument             TEXT NOT NULL,
    timeframe              TEXT NOT NULL,
    window_start_ts        BIGINT NOT NULL,
    window_end_ts          BIGINT NOT NULL,
    price_bucket_low       DOUBLE PRECISION NOT NULL,
    price_bucket_high      DOUBLE PRECISION NOT NULL,
    size_bucket            TEXT NOT NULL,
    bid_liquidity          DOUBLE PRECISION NOT NULL,
    ask_liquidity          DOUBLE PRECISION NOT NULL,
    trade_volume           DOUBLE PRECISION NOT NULL,
    seq_min                BIGINT NOT NULL,
    seq_max                BIGINT NOT NULL,
    samples                BIGINT NOT NULL,
    source_idempotency_key TEXT NOT NULL,
    idempotency_key        TEXT NOT NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (
        venue,
        instrument,
        timeframe,
        window_start_ts,
        price_bucket_low,
        price_bucket_high,
        size_bucket,
        source_idempotency_key
    )
);

-- Volume profile bucket aggregates
CREATE TABLE IF NOT EXISTS aggregation_volume_profile (
    venue             TEXT NOT NULL,
    instrument        TEXT NOT NULL,
    timeframe         TEXT NOT NULL,
    window_start_ts   BIGINT NOT NULL,
    bucket_low        DOUBLE PRECISION NOT NULL,
    bucket_high       DOUBLE PRECISION NOT NULL,
    buy_volume        DOUBLE PRECISION NOT NULL DEFAULT 0,
    sell_volume       DOUBLE PRECISION NOT NULL DEFAULT 0,
    total_volume      DOUBLE PRECISION NOT NULL DEFAULT 0,
    seq_min           BIGINT NOT NULL,
    seq_max           BIGINT NOT NULL,
    last_operation_id TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue, instrument, timeframe, window_start_ts, bucket_low, bucket_high)
);

-- VPVR operation dedup log (bounded by retention policy)
CREATE TABLE IF NOT EXISTS aggregation_volume_profile_oplog (
    operation_id    TEXT PRIMARY KEY,
    venue           TEXT NOT NULL,
    instrument      TEXT NOT NULL,
    timeframe       TEXT NOT NULL,
    window_start_ts BIGINT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Delivery events for GetRange queries
CREATE TABLE IF NOT EXISTS delivery_events (
    id         BIGSERIAL PRIMARY KEY,
    subject    TEXT NOT NULL,
    seq        BIGINT NOT NULL,
    ts_ingest  BIGINT NOT NULL,
    payload    BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_delivery_events_subject_ts
    ON delivery_events (subject, ts_ingest, seq);
