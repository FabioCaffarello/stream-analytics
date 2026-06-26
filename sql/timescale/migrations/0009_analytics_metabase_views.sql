-- +goose Up
-- Analytics views for Metabase BI dashboard consumption.
-- All BIGINT millisecond timestamps are converted to TIMESTAMPTZ.
-- Operational (public schema) columns venue/instrument aliased to exchange_name/symbol for consistency.

-- 1. Market summary over the last 24 hours (DW layer)
CREATE VIEW analytics.v_market_summary_24h AS
SELECT
    t.exchange_name,
    t.symbol,
    COUNT(*)                                                                                  AS trade_count,
    SUM(t.quantity)                                                                           AS total_volume,
    SUM(CASE WHEN t.side = 'buy'  THEN t.quantity ELSE 0 END)                                AS buy_volume,
    SUM(CASE WHEN t.side = 'sell' THEN t.quantity ELSE 0 END)                                AS sell_volume,
    ROUND(
        (SUM(CASE WHEN t.side = 'buy' THEN t.quantity ELSE 0 END)
         / NULLIF(SUM(t.quantity), 0) * 100)::NUMERIC, 2
    )                                                                                         AS buy_pct,
    MIN(t.price)                                                                              AS low_24h,
    MAX(t.price)                                                                              AS high_24h,
    SUM(t.price * t.quantity) / NULLIF(SUM(t.quantity), 0)                                   AS vwap_24h,
    MAX(TO_TIMESTAMP(t.ts_exchange_ms / 1000.0))                                             AS latest_trade_at
FROM analytics.fact_trades t
WHERE TO_TIMESTAMP(t.ts_exchange_ms / 1000.0) >= NOW() - INTERVAL '24 hours'
GROUP BY t.exchange_name, t.symbol;

-- 2. OHLCV candles with readable timestamps and derived price metrics (DW layer)
CREATE VIEW analytics.v_candles AS
SELECT
    c.exchange_name,
    c.symbol,
    c.timeframe,
    TO_TIMESTAMP(c.open_time_ms / 1000.0)                                                   AS open_time,
    c.open,
    c.high,
    c.low,
    c.close,
    c.volume,
    c.trade_count,
    c.close - c.open                                                                         AS price_change,
    ROUND(((c.close - c.open) / NULLIF(c.open, 0) * 100)::NUMERIC, 4)                      AS price_change_pct
FROM analytics.fact_candles c;

-- 3. Volume statistics with flow metrics and readable timestamps (DW layer)
CREATE VIEW analytics.v_volume_stats AS
SELECT
    vs.exchange_name,
    vs.symbol,
    TO_TIMESTAMP(vs.window_start_ms / 1000.0)                                               AS window_start,
    vs.window_secs,
    vs.total_volume,
    vs.buy_volume,
    vs.sell_volume,
    vs.trade_count,
    vs.vwap,
    ROUND((vs.buy_volume  / NULLIF(vs.total_volume, 0) * 100)::NUMERIC, 2)                  AS buy_pct,
    ROUND((vs.sell_volume / NULLIF(vs.total_volume, 0) * 100)::NUMERIC, 2)                  AS sell_pct,
    vs.buy_volume - vs.sell_volume                                                           AS delta_volume,
    ROUND(
        ((vs.buy_volume - vs.sell_volume) / NULLIF(vs.total_volume, 0) * 100)::NUMERIC, 2
    )                                                                                        AS delta_pct
FROM analytics.fact_volume_stats vs;

-- 4. Cumulative Volume Delta from 5-minute windows (DW layer)
CREATE VIEW analytics.v_cvd AS
SELECT
    vs.exchange_name,
    vs.symbol,
    TO_TIMESTAMP(vs.window_start_ms / 1000.0)                                               AS window_start,
    vs.buy_volume - vs.sell_volume                                                           AS delta_volume,
    SUM(vs.buy_volume - vs.sell_volume) OVER (
        PARTITION BY vs.exchange_name, vs.symbol
        ORDER BY vs.window_start_ms
        ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
    )                                                                                        AS cumulative_delta,
    vs.buy_volume,
    vs.sell_volume,
    vs.total_volume
FROM analytics.fact_volume_stats vs
WHERE vs.window_secs = 300;

-- 5. Ingestion latency per trade (DW layer)
CREATE VIEW analytics.v_ingestion_latency AS
SELECT
    t.exchange_name,
    t.symbol,
    TO_TIMESTAMP(t.ts_ingest_ms / 1000.0)                                                   AS ingest_time,
    t.ts_ingest_ms - t.ts_exchange_ms                                                        AS latency_ms,
    t.side
FROM analytics.fact_trades t
WHERE t.ts_exchange_ms > 0
  AND t.ts_ingest_ms >= t.ts_exchange_ms;

-- 6. Hot-path OHLCV candles with buy/sell attribution (operational)
CREATE VIEW analytics.v_agg_candles AS
SELECT
    ac.venue                                                                                  AS exchange_name,
    ac.instrument                                                                             AS symbol,
    ac.timeframe,
    TO_TIMESTAMP(ac.window_start / 1000.0)                                                   AS window_start,
    ac.open_price                                                                             AS open,
    ac.high_price                                                                             AS high,
    ac.low_price                                                                              AS low,
    ac.close_price                                                                            AS close,
    ac.volume,
    ac.buy_volume,
    ac.sell_volume,
    ac.trade_count,
    ROUND((ac.buy_volume / NULLIF(ac.volume, 0) * 100)::NUMERIC, 2)                         AS buy_pct,
    ac.close_price - ac.open_price                                                           AS price_change
FROM aggregation_candle ac;

-- 7. Liquidations, mark price, and funding rate (operational)
CREATE VIEW analytics.v_agg_stats AS
SELECT
    s.venue                                                                                   AS exchange_name,
    s.instrument                                                                              AS symbol,
    s.timeframe,
    TO_TIMESTAMP(s.window_start / 1000.0)                                                    AS window_start,
    s.liq_buy_volume,
    s.liq_sell_volume,
    s.liq_total_volume,
    s.liq_count,
    s.markprice_close,
    s.markprice_high,
    s.markprice_low,
    s.funding_rate_avg,
    s.funding_rate_last
FROM aggregation_stats s;

-- 8. Open interest with delta (operational)
CREATE VIEW analytics.v_agg_oi AS
SELECT
    oi.venue                                                                                  AS exchange_name,
    oi.instrument                                                                             AS symbol,
    oi.timeframe,
    TO_TIMESTAMP(oi.window_start / 1000.0)                                                   AS window_start,
    oi.open_interest,
    oi.delta                                                                                  AS oi_delta,
    oi.delta_pct                                                                              AS oi_delta_pct
FROM aggregation_oi oi;

-- 9. Cumulative Volume Delta from hot-path (operational)
CREATE VIEW analytics.v_agg_cvd AS
SELECT
    cvd.venue                                                                                 AS exchange_name,
    cvd.instrument                                                                            AS symbol,
    cvd.timeframe,
    TO_TIMESTAMP(cvd.window_start / 1000.0)                                                  AS window_start,
    cvd.delta_volume,
    cvd.cvd
FROM aggregation_cvd cvd;

-- 10. Trade-flow tape with burst flag (operational)
CREATE VIEW analytics.v_agg_tape AS
SELECT
    t.venue                                                                                   AS exchange_name,
    t.instrument                                                                              AS symbol,
    t.timeframe,
    TO_TIMESTAMP(t.window_start / 1000.0)                                                    AS window_start,
    t.trade_count,
    t.buy_count,
    t.sell_count,
    t.buy_volume,
    t.sell_volume,
    t.total_volume,
    t.vwap_price                                                                              AS vwap,
    t.max_trade_size,
    t.rate_trades_per_sec,
    t.volume_imbalance,
    t.is_burst
FROM aggregation_tape t;

-- 11. Delta volume per window with flow ratio (operational)
CREATE VIEW analytics.v_agg_delta_volume AS
SELECT
    dv.venue                                                                                  AS exchange_name,
    dv.instrument                                                                             AS symbol,
    dv.timeframe,
    TO_TIMESTAMP(dv.window_start / 1000.0)                                                   AS window_start,
    dv.buy_volume,
    dv.sell_volume,
    dv.delta_volume,
    ROUND(
        (dv.buy_volume / NULLIF(dv.buy_volume + dv.sell_volume, 0) * 100)::NUMERIC, 2
    )                                                                                        AS buy_pct
FROM aggregation_delta_volume dv;

-- +goose Down
DROP VIEW IF EXISTS analytics.v_agg_delta_volume;
DROP VIEW IF EXISTS analytics.v_agg_tape;
DROP VIEW IF EXISTS analytics.v_agg_cvd;
DROP VIEW IF EXISTS analytics.v_agg_oi;
DROP VIEW IF EXISTS analytics.v_agg_stats;
DROP VIEW IF EXISTS analytics.v_agg_candles;
DROP VIEW IF EXISTS analytics.v_ingestion_latency;
DROP VIEW IF EXISTS analytics.v_cvd;
DROP VIEW IF EXISTS analytics.v_volume_stats;
DROP VIEW IF EXISTS analytics.v_candles;
DROP VIEW IF EXISTS analytics.v_market_summary_24h;
