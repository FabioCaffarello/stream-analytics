#!/usr/bin/env python3
"""
Metabase provisioning script — Stream Analytics Analytics Dashboard.

Sets up a complete Metabase instance with:
  - PostgreSQL (TimescaleDB) data source pointing at the analytics schema
  - A collection of expert-grade BI cards
  - A single multi-section dashboard covering trade flow, price analysis,
    market microstructure (liquidations, OI, CVD, funding) and data quality.

Usage:
    python3 provision.py

Environment variables (all have sane local-dev defaults):
    METABASE_URL              http://localhost:3001
    METABASE_ADMIN_EMAIL      admin@raccoon.local
    METABASE_ADMIN_PASSWORD   raccoon_admin!
    METABASE_SITE_NAME        Stream Analytics Analytics
    TIMESCALE_HOST            timescale   (container-internal name)
    TIMESCALE_PORT            5432
    TIMESCALE_DB              raccoon
    TIMESCALE_USER            raccoon
    TIMESCALE_PASSWORD        raccoon
"""

from __future__ import annotations

import os
import sys
import time
import logging
from typing import Optional

import requests

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(message)s",
    datefmt="%H:%M:%S",
)
log = logging.getLogger("provision")

MB_URL       = os.environ.get("METABASE_URL",             "http://localhost:3001")
MB_EMAIL     = os.environ.get("METABASE_ADMIN_EMAIL",     "admin@raccoon.local")
MB_PASSWORD  = os.environ.get("METABASE_ADMIN_PASSWORD",  "raccoon_admin!")
MB_SITE_NAME = os.environ.get("METABASE_SITE_NAME",       "Stream Analytics Analytics")

PG_HOST = os.environ.get("TIMESCALE_HOST",     "timescale")
PG_PORT = int(os.environ.get("TIMESCALE_PORT", "5432"))
PG_DB   = os.environ.get("TIMESCALE_DB",       "raccoon")
PG_USER = os.environ.get("TIMESCALE_USER",     "raccoon")
PG_PASS = os.environ.get("TIMESCALE_PASSWORD", "raccoon")

DB_NAME = "Stream Analytics — TimescaleDB"
COLLECTION_NAME = "Stream Analytics Analytics"
DASHBOARD_NAME  = "Market Microstructure Dashboard"

# ---------------------------------------------------------------------------
# HTTP client
# ---------------------------------------------------------------------------

class MetabaseClient:
    def __init__(self, base_url: str):
        self.base = base_url.rstrip("/")
        self.s    = requests.Session()
        self.s.headers.update({"Content-Type": "application/json"})
        self._token: Optional[str] = None

    def _set_auth(self):
        if self._token:
            self.s.headers["X-Metabase-Session"] = self._token
        elif "X-Metabase-Session" in self.s.headers:
            del self.s.headers["X-Metabase-Session"]

    def get(self, path: str) -> dict | list:
        self._set_auth()
        r = self.s.get(f"{self.base}/api{path}")
        r.raise_for_status()
        return r.json()

    def post(self, path: str, body: dict) -> dict:
        self._set_auth()
        r = self.s.post(f"{self.base}/api{path}", json=body)
        if r.status_code == 401:
            payload = r.json()
            err = str(payload)
            if "Too many attempts" in err:
                wait = next((w for w in err.split() if w.isdigit()), "unknown")
                raise RuntimeError(
                    f"Metabase rate limiter active — too many failed login attempts.\n"
                    f"  Wait {wait}s OR run: docker restart stream-analytics-metabase\n"
                    f"  Then re-run: make metabase-provision"
                )
        r.raise_for_status()
        return r.json()

    def put(self, path: str, body: dict) -> dict:
        self._set_auth()
        r = self.s.put(f"{self.base}/api{path}", json=body)
        r.raise_for_status()
        return r.json()

    def wait_ready(self, retries: int = 60, delay: float = 5.0):
        for i in range(retries):
            try:
                r = self.s.get(f"{self.base}/api/health", timeout=5)
                if r.status_code == 200 and r.json().get("status") in ("ok", "initialized"):
                    log.info("Metabase ready")
                    return
            except Exception:
                pass
            log.info("Waiting for Metabase (%d/%d)…", i + 1, retries)
            time.sleep(delay)
        raise RuntimeError("Metabase did not become ready in time")

    def setup_if_needed(self):
        props = self.get("/session/properties")
        token = props.get("setup-token")
        if not token or props.get("has-user-setup"):
            log.info("Metabase already configured — skipping setup")
            return
        log.info("Running first-time setup…")
        self.post("/setup", {
            "token":    token,
            "prefs":    {"site_name": MB_SITE_NAME, "allow_tracking": False},
            "user": {
                "first_name": "Market",
                "last_name":  "Raccoon",
                "email":      MB_EMAIL,
                "password":   MB_PASSWORD,
            },
        })
        log.info("Initial setup complete")

    def authenticate(self):
        result = self.post("/session", {"username": MB_EMAIL, "password": MB_PASSWORD})
        self._token = result["id"]
        log.info("Authenticated as %s", MB_EMAIL)

    # ── Resource helpers ────────────────────────────────────────────────────

    def get_or_create_database(self) -> int:
        for db in self.get("/database").get("data", []):
            if db.get("name") == DB_NAME:
                log.info("Database already exists (id=%d)", db["id"])
                return db["id"]
        result = self.post("/database", {
            "engine": "postgres",
            "name":   DB_NAME,
            "details": {
                "host":                  PG_HOST,
                "port":                  PG_PORT,
                "dbname":                PG_DB,
                "user":                  PG_USER,
                "password":              PG_PASS,
                "ssl":                   False,
                "schema-filters-type":   "all",
                "tunnel-enabled":        False,
            },
            "auto_run_queries": True,
            "is_full_sync":     True,
            "is_on_demand":     False,
        })
        log.info("Created database (id=%d)", result["id"])
        return result["id"]

    def get_or_create_collection(self, name: str, description: str = "") -> int:
        for col in self.get("/collection"):
            if col.get("name") == name:
                log.info("Collection '%s' already exists (id=%s)", name, col["id"])
                return col["id"]
        result = self.post("/collection", {
            "name":        name,
            "description": description,
            "color":       "#509EE3",
        })
        log.info("Created collection '%s' (id=%d)", name, result["id"])
        return result["id"]

    def get_or_create_card(
        self,
        name:                    str,
        database_id:             int,
        sql:                     str,
        display:                 str,
        visualization_settings:  dict,
        collection_id:           Optional[int] = None,
        description:             str        = "",
    ) -> int:
        for card in self.get("/card"):
            if card.get("name") == name:
                log.info("Card '%s' already exists (id=%d)", name, card["id"])
                return card["id"]
        body = {
            "name":        name,
            "description": description,
            "display":     display,
            "dataset_query": {
                "type":     "native",
                "native":   {"query": sql, "template-tags": {}},
                "database": database_id,
            },
            "visualization_settings": visualization_settings,
        }
        if collection_id is not None:
            body["collection_id"] = collection_id
        result = self.post("/card", body)
        log.info("Created card '%s' (id=%d)", name, result["id"])
        return result["id"]

    def get_or_create_dashboard(
        self,
        name:          str,
        description:   str,
        collection_id: Optional[int] = None,
    ) -> int:
        for dash in self.get("/dashboard"):
            if dash.get("name") == name:
                log.info("Dashboard '%s' already exists (id=%d)", name, dash["id"])
                return dash["id"]
        body = {"name": name, "description": description}
        if collection_id is not None:
            body["collection_id"] = collection_id
        result = self.post("/dashboard", body)
        log.info("Created dashboard '%s' (id=%d)", name, result["id"])
        return result["id"]

    def populate_dashboard(self, dashboard_id: int, placements: list[dict]):
        dash = self.get(f"/dashboard/{dashboard_id}")
        if dash.get("dashcards"):
            log.info("Dashboard already has cards — skipping layout step")
            return
        dashcards = [
            {
                "id":                    -(i + 1),
                "card_id":               p["card_id"],
                "row":                   p["row"],
                "col":                   p["col"],
                "size_x":                p["size_x"],
                "size_y":                p["size_y"],
                "parameter_mappings":    [],
                "visualization_settings": p.get("visualization_settings", {}),
            }
            for i, p in enumerate(placements)
        ]
        self.put(f"/dashboard/{dashboard_id}/cards", {"cards": dashcards})
        log.info("Added %d cards to dashboard %d", len(dashcards), dashboard_id)


# ---------------------------------------------------------------------------
# SQL card definitions
# ---------------------------------------------------------------------------

def _cards(db_id: int, col_id: int) -> list[dict]:
    """Return a list of card specs: {name, sql, display, vis, description}."""

    def card(name, sql, display="table", vis=None, desc=""):
        return {
            "name":        name,
            "sql":         sql,
            "display":     display,
            "vis":         vis or {},
            "description": desc,
            "database_id": db_id,
            "collection_id": col_id,
        }

    # ── KPI helpers ─────────────────────────────────────────────────────────

    scalar_vis = {"graph.dimensions": [], "graph.metrics": []}

    c: list[dict] = [

        # ── Row 0: KPIs ────────────────────────────────────────────────────
        card(
            "KPI · Total Trades (24h)",
            """
SELECT COUNT(*) AS "Total Trades (24h)"
FROM analytics.fact_trades
WHERE TO_TIMESTAMP(ts_exchange_ms / 1000.0) >= NOW() - INTERVAL '24 hours'
""",
            "scalar", scalar_vis,
            "Aggregate trade count across all exchanges and symbols over the last 24 hours.",
        ),
        card(
            "KPI · Total Volume (24h)",
            """
SELECT ROUND(SUM(quantity)::NUMERIC, 4) AS "Total Volume (24h)"
FROM analytics.fact_trades
WHERE TO_TIMESTAMP(ts_exchange_ms / 1000.0) >= NOW() - INTERVAL '24 hours'
""",
            "scalar", scalar_vis,
            "Total traded quantity (base asset units) across all venues in the last 24 h.",
        ),
        card(
            "KPI · Active Instruments (1h)",
            """
SELECT COUNT(DISTINCT exchange_name || ':' || symbol) AS "Active Instruments (1h)"
FROM analytics.fact_trades
WHERE TO_TIMESTAMP(ts_exchange_ms / 1000.0) >= NOW() - INTERVAL '1 hour'
""",
            "scalar", scalar_vis,
            "Number of exchange:symbol pairs that had at least one trade in the last hour.",
        ),
        card(
            "KPI · Buy/Sell Volume Ratio (24h)",
            """
SELECT ROUND(
    (SUM(CASE WHEN side = 'buy'  THEN quantity ELSE 0 END) /
     NULLIF(SUM(CASE WHEN side = 'sell' THEN quantity ELSE 0 END), 0))::NUMERIC, 3
) AS "Buy/Sell Ratio (24h)"
FROM analytics.fact_trades
WHERE TO_TIMESTAMP(ts_exchange_ms / 1000.0) >= NOW() - INTERVAL '24 hours'
""",
            "scalar", scalar_vis,
            "Ratio > 1 indicates net buying pressure; < 1 indicates net selling pressure.",
        ),
        card(
            "KPI · Avg Ingest Latency p50 (1h, ms)",
            """
SELECT ROUND(
    PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY ts_ingest_ms - ts_exchange_ms)::NUMERIC, 0
) AS "Ingest Latency p50 (ms)"
FROM analytics.fact_trades
WHERE ts_exchange_ms   > 0
  AND ts_ingest_ms    >= ts_exchange_ms
  AND TO_TIMESTAMP(ts_ingest_ms / 1000.0) >= NOW() - INTERVAL '1 hour'
""",
            "scalar", scalar_vis,
            "Median end-to-end latency from exchange timestamp to ingest timestamp.",
        ),

        # ── Row 1: Market Overview ──────────────────────────────────────────
        card(
            "Volume by Exchange (24h)",
            """
SELECT
    exchange_name                           AS "Exchange",
    ROUND(SUM(quantity)::NUMERIC, 4)        AS "Volume (24h)"
FROM analytics.fact_trades
WHERE TO_TIMESTAMP(ts_exchange_ms / 1000.0) >= NOW() - INTERVAL '24 hours'
GROUP BY exchange_name
ORDER BY "Volume (24h)" DESC
""",
            "bar",
            {
                "graph.dimensions": ["Exchange"],
                "graph.metrics":    ["Volume (24h)"],
                "stackable.stack_type": None,
            },
            "Total traded volume per exchange over the last 24 hours.",
        ),
        card(
            "Top Active Instruments (24h)",
            """
SELECT
    exchange_name                                                                        AS "Exchange",
    symbol                                                                               AS "Symbol",
    COUNT(*)                                                                             AS "Trades",
    ROUND(SUM(quantity)::NUMERIC, 4)                                                     AS "Volume",
    ROUND((SUM(CASE WHEN side='buy' THEN quantity ELSE 0 END)
         / NULLIF(SUM(quantity), 0) * 100)::NUMERIC, 1)                                  AS "Buy %",
    ROUND((SUM(price * quantity) / NULLIF(SUM(quantity), 0))::NUMERIC, 4)                AS "VWAP"
FROM analytics.fact_trades
WHERE TO_TIMESTAMP(ts_exchange_ms / 1000.0) >= NOW() - INTERVAL '24 hours'
GROUP BY exchange_name, symbol
ORDER BY "Trades" DESC
LIMIT 25
""",
            "table",
            {"table.pivot": False},
            "Ranked list of most active exchange:symbol pairs by trade count.",
        ),

        # ── Row 2: Trade Flow Analysis ──────────────────────────────────────
        card(
            "Buy vs Sell Volume per 5-min Window",
            """
SELECT
    window_start                        AS "Time",
    exchange_name                       AS "Exchange",
    symbol                              AS "Symbol",
    ROUND(buy_volume::NUMERIC, 4)       AS "Buy Volume",
    ROUND(sell_volume::NUMERIC, 4)      AS "Sell Volume"
FROM analytics.v_volume_stats
WHERE window_start >= NOW() - INTERVAL '6 hours'
  AND window_secs  = 300
ORDER BY window_start, exchange_name, symbol
""",
            "bar",
            {
                "graph.dimensions":       ["Time"],
                "graph.metrics":          ["Buy Volume", "Sell Volume"],
                "stackable.stack_type":   "stacked",
                "series_settings": {
                    "Buy Volume":  {"color": "#51528D"},
                    "Sell Volume": {"color": "#EF8C8C"},
                },
            },
            "Stacked buy/sell volume in 5-minute windows — reveals directional pressure.",
        ),
        card(
            "Volume Delta % (Buying Pressure) — 5-min",
            """
SELECT
    window_start                        AS "Time",
    exchange_name                       AS "Exchange",
    symbol                              AS "Symbol",
    delta_pct                           AS "Delta %"
FROM analytics.v_volume_stats
WHERE window_start >= NOW() - INTERVAL '6 hours'
  AND window_secs  = 300
ORDER BY window_start, exchange_name, symbol
""",
            "line",
            {
                "graph.dimensions": ["Time"],
                "graph.metrics":    ["Delta %"],
            },
            "Percentage difference between buy and sell volume. Positive = buying dominant.",
        ),

        # ── Row 3: Price & OHLCV ───────────────────────────────────────────
        card(
            "Close Price — 1h Candles (last 7 days)",
            """
SELECT
    open_time                           AS "Time",
    exchange_name                       AS "Exchange",
    symbol                              AS "Symbol",
    ROUND(close::NUMERIC, 4)            AS "Close",
    ROUND(high::NUMERIC, 4)             AS "High",
    ROUND(low::NUMERIC, 4)              AS "Low"
FROM analytics.v_candles
WHERE timeframe  = '1h'
  AND open_time >= NOW() - INTERVAL '7 days'
ORDER BY open_time, exchange_name, symbol
""",
            "line",
            {
                "graph.dimensions": ["Time"],
                "graph.metrics":    ["Close"],
            },
            "1-hour close price time series from Flink-computed OHLCV candles.",
        ),
        card(
            "Hourly Volume (last 7 days)",
            """
SELECT
    open_time                           AS "Time",
    exchange_name                       AS "Exchange",
    symbol                              AS "Symbol",
    ROUND(volume::NUMERIC, 4)           AS "Volume",
    trade_count                         AS "Trades"
FROM analytics.v_candles
WHERE timeframe  = '1h'
  AND open_time >= NOW() - INTERVAL '7 days'
ORDER BY open_time, exchange_name, symbol
""",
            "bar",
            {
                "graph.dimensions": ["Time"],
                "graph.metrics":    ["Volume"],
            },
            "Hourly aggregated volume alongside trade count from 1h Flink candles.",
        ),

        # ── Row 4: Derivatives — Liquidations & OI ─────────────────────────
        card(
            "Liquidation Volume by Side — 5m",
            """
SELECT
    window_start                            AS "Time",
    exchange_name                           AS "Exchange",
    symbol                                  AS "Symbol",
    ROUND(liq_buy_volume::NUMERIC, 4)       AS "Liq Buy",
    ROUND(liq_sell_volume::NUMERIC, 4)      AS "Liq Sell"
FROM analytics.v_agg_stats
WHERE timeframe    = '5m'
  AND window_start >= NOW() - INTERVAL '6 hours'
ORDER BY window_start, exchange_name, symbol
""",
            "bar",
            {
                "graph.dimensions":     ["Time"],
                "graph.metrics":        ["Liq Buy", "Liq Sell"],
                "stackable.stack_type": "stacked",
                "series_settings": {
                    "Liq Buy":  {"color": "#84BB4C"},
                    "Liq Sell": {"color": "#F9D45C"},
                },
            },
            "Liquidation buy/sell volume in 5-min buckets — spikes signal forced deleveraging.",
        ),
        card(
            "Open Interest & Delta — 5m",
            """
SELECT
    window_start                                AS "Time",
    exchange_name                               AS "Exchange",
    symbol                                      AS "Symbol",
    ROUND(open_interest::NUMERIC, 2)            AS "Open Interest",
    ROUND(oi_delta::NUMERIC, 4)                 AS "OI Delta",
    ROUND(oi_delta_pct::NUMERIC, 4)             AS "OI Delta %"
FROM analytics.v_agg_oi
WHERE timeframe    = '5m'
  AND window_start >= NOW() - INTERVAL '24 hours'
ORDER BY window_start, exchange_name, symbol
""",
            "line",
            {
                "graph.dimensions": ["Time"],
                "graph.metrics":    ["Open Interest", "OI Delta"],
            },
            "Open interest (absolute) and delta — rising OI with rising price = strong trend.",
        ),

        # ── Row 5: CVD & Funding Rate ───────────────────────────────────────
        card(
            "Cumulative Volume Delta (CVD) — 5-min",
            """
SELECT
    window_start                            AS "Time",
    exchange_name                           AS "Exchange",
    symbol                                  AS "Symbol",
    ROUND(cumulative_delta::NUMERIC, 4)     AS "CVD",
    ROUND(delta_volume::NUMERIC, 4)         AS "Delta Volume"
FROM analytics.v_cvd
WHERE window_start >= NOW() - INTERVAL '24 hours'
ORDER BY window_start, exchange_name, symbol
""",
            "line",
            {
                "graph.dimensions": ["Time"],
                "graph.metrics":    ["CVD"],
            },
            "Running sum of (buy_volume - sell_volume). Divergence from price signals reversal risk.",
        ),
        card(
            "Funding Rate History — 1h",
            """
SELECT
    window_start                                    AS "Time",
    exchange_name                                   AS "Exchange",
    symbol                                          AS "Symbol",
    ROUND(funding_rate_last::NUMERIC, 6)            AS "Funding Rate",
    ROUND(funding_rate_avg::NUMERIC, 6)             AS "Avg Funding Rate"
FROM analytics.v_agg_stats
WHERE timeframe        = '1h'
  AND window_start    >= NOW() - INTERVAL '7 days'
  AND funding_rate_last IS NOT NULL
ORDER BY window_start, exchange_name, symbol
""",
            "line",
            {
                "graph.dimensions": ["Time"],
                "graph.metrics":    ["Funding Rate"],
            },
            "Perpetual funding rate per hour. Positive = longs pay shorts; negative = shorts pay longs.",
        ),

        # ── Row 6: Market Microstructure (Tape) ────────────────────────────
        card(
            "Trade Rate & Burst Detection — 1m",
            """
SELECT
    window_start                                    AS "Time",
    exchange_name                                   AS "Exchange",
    symbol                                          AS "Symbol",
    ROUND(rate_trades_per_sec::NUMERIC, 2)          AS "Trades/sec",
    ROUND(volume_imbalance::NUMERIC, 4)             AS "Volume Imbalance",
    is_burst                                        AS "Burst?"
FROM analytics.v_agg_tape
WHERE timeframe    = '1m'
  AND window_start >= NOW() - INTERVAL '2 hours'
ORDER BY window_start, exchange_name, symbol
""",
            "line",
            {
                "graph.dimensions": ["Time"],
                "graph.metrics":    ["Trades/sec"],
            },
            "Trade velocity (trades/sec) per 1-min bar with burst flag when volume spikes abnormally.",
        ),
        card(
            "VWAP vs Last Price — 1m Tape",
            """
SELECT
    window_start                            AS "Time",
    exchange_name                           AS "Exchange",
    symbol                                  AS "Symbol",
    ROUND(vwap::NUMERIC, 4)                 AS "VWAP",
    ROUND(last_price::NUMERIC, 4)           AS "Last Price",
    ROUND((last_price - vwap)::NUMERIC, 4)  AS "Price vs VWAP"
FROM analytics.v_agg_tape
WHERE timeframe    = '1m'
  AND window_start >= NOW() - INTERVAL '2 hours'
ORDER BY window_start, exchange_name, symbol
""",
            "line",
            {
                "graph.dimensions": ["Time"],
                "graph.metrics":    ["VWAP", "Last Price"],
            },
            "VWAP vs last trade price per bar — spread signals intra-bar directional bias.",
        ),

        # ── Row 7: Data Quality / Observability ────────────────────────────
        card(
            "Ingestion Latency by Exchange (1h)",
            """
SELECT
    exchange_name                                           AS "Exchange",
    ROUND(AVG(latency_ms)::NUMERIC, 0)                     AS "Avg (ms)",
    ROUND(PERCENTILE_CONT(0.5)  WITHIN GROUP
        (ORDER BY latency_ms)::NUMERIC, 0)                 AS "p50 (ms)",
    ROUND(PERCENTILE_CONT(0.95) WITHIN GROUP
        (ORDER BY latency_ms)::NUMERIC, 0)                 AS "p95 (ms)",
    ROUND(PERCENTILE_CONT(0.99) WITHIN GROUP
        (ORDER BY latency_ms)::NUMERIC, 0)                 AS "p99 (ms)",
    COUNT(*)                                               AS "Sample Count"
FROM analytics.v_ingestion_latency
WHERE ingest_time >= NOW() - INTERVAL '1 hour'
GROUP BY exchange_name
ORDER BY "p99 (ms)" DESC
""",
            "table",
            {"table.pivot": False},
            "Percentile breakdown of exchange→ingest latency. p99 outliers indicate connection issues.",
        ),
        card(
            "Data Freshness by Instrument",
            """
SELECT
    exchange_name                                                           AS "Exchange",
    symbol                                                                  AS "Symbol",
    MAX(TO_TIMESTAMP(ts_exchange_ms / 1000.0))                             AS "Last Trade At",
    ROUND(EXTRACT(EPOCH FROM
        (NOW() - MAX(TO_TIMESTAMP(ts_exchange_ms / 1000.0))))::NUMERIC, 0) AS "Seconds Ago",
    COUNT(*) FILTER (
        WHERE TO_TIMESTAMP(ts_exchange_ms / 1000.0) >= NOW() - INTERVAL '1 hour'
    )                                                                       AS "Trades (1h)"
FROM analytics.fact_trades
GROUP BY exchange_name, symbol
ORDER BY "Seconds Ago"
""",
            "table",
            {"table.pivot": False},
            "Staleness detector — instruments not seen recently may have ingestion issues.",
        ),
    ]

    return c


# ---------------------------------------------------------------------------
# Dashboard layout
# ---------------------------------------------------------------------------

def _layout(card_ids: list[int]) -> list[dict]:
    """
    Map card indices to (row, col, size_x, size_y).
    Grid is 24 columns wide. Rows accumulate by section height.

    Index corresponds to _cards() order:
       0-4   KPI scalars
       5-6   Market overview
       7-8   Trade flow
       9-10  Price & OHLCV
       11-12 Derivatives
       13-14 CVD & Funding
       15-16 Microstructure tape
       17-18 Data quality
    """
    placements = [
        # Row 0 — KPIs (h=3)
        {"card_id": card_ids[0],  "row": 0,  "col": 0,  "size_x": 5, "size_y": 3},
        {"card_id": card_ids[1],  "row": 0,  "col": 5,  "size_x": 5, "size_y": 3},
        {"card_id": card_ids[2],  "row": 0,  "col": 10, "size_x": 4, "size_y": 3},
        {"card_id": card_ids[3],  "row": 0,  "col": 14, "size_x": 5, "size_y": 3},
        {"card_id": card_ids[4],  "row": 0,  "col": 19, "size_x": 5, "size_y": 3},
        # Row 3 — Market overview (h=8)
        {"card_id": card_ids[5],  "row": 3,  "col": 0,  "size_x": 12, "size_y": 8},
        {"card_id": card_ids[6],  "row": 3,  "col": 12, "size_x": 12, "size_y": 8},
        # Row 11 — Trade flow (h=8)
        {"card_id": card_ids[7],  "row": 11, "col": 0,  "size_x": 16, "size_y": 8},
        {"card_id": card_ids[8],  "row": 11, "col": 16, "size_x": 8,  "size_y": 8},
        # Row 19 — Price & OHLCV (h=8)
        {"card_id": card_ids[9],  "row": 19, "col": 0,  "size_x": 16, "size_y": 8},
        {"card_id": card_ids[10], "row": 19, "col": 16, "size_x": 8,  "size_y": 8},
        # Row 27 — Derivatives (h=8)
        {"card_id": card_ids[11], "row": 27, "col": 0,  "size_x": 12, "size_y": 8},
        {"card_id": card_ids[12], "row": 27, "col": 12, "size_x": 12, "size_y": 8},
        # Row 35 — CVD & Funding (h=8)
        {"card_id": card_ids[13], "row": 35, "col": 0,  "size_x": 16, "size_y": 8},
        {"card_id": card_ids[14], "row": 35, "col": 16, "size_x": 8,  "size_y": 8},
        # Row 43 — Microstructure tape (h=8)
        {"card_id": card_ids[15], "row": 43, "col": 0,  "size_x": 16, "size_y": 8},
        {"card_id": card_ids[16], "row": 43, "col": 16, "size_x": 8,  "size_y": 8},
        # Row 51 — Data quality (h=8)
        {"card_id": card_ids[17], "row": 51, "col": 0,  "size_x": 12, "size_y": 8},
        {"card_id": card_ids[18], "row": 51, "col": 12, "size_x": 12, "size_y": 8},
    ]
    return placements


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------

def main():
    client = MetabaseClient(MB_URL)

    log.info("Connecting to Metabase at %s", MB_URL)
    client.wait_ready()
    client.setup_if_needed()
    client.authenticate()

    db_id  = client.get_or_create_database()
    col_id = client.get_or_create_collection(
        COLLECTION_NAME,
        "Expert-grade crypto market microstructure dashboards for Stream Analytics.",
    )

    card_specs = _cards(db_id, col_id)
    card_ids   = []
    for spec in card_specs:
        cid = client.get_or_create_card(
            name                   = spec["name"],
            database_id            = spec["database_id"],
            sql                    = spec["sql"],
            display                = spec["display"],
            visualization_settings = spec["vis"],
            collection_id          = spec["collection_id"],
            description            = spec["description"],
        )
        card_ids.append(cid)

    dash_id = client.get_or_create_dashboard(
        DASHBOARD_NAME,
        (
            "Market microstructure analytics: trade flow, OHLCV, CVD, "
            "liquidations, open interest, funding rates, VWAP, burst detection "
            "and data quality monitoring across all tracked exchanges and instruments."
        ),
        col_id,
    )
    client.populate_dashboard(dash_id, _layout(card_ids))

    log.info(
        "Done. Dashboard: %s/dashboard/%d",
        MB_URL,
        dash_id,
    )


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        log.error("Provisioning failed: %s", exc)
        sys.exit(1)
