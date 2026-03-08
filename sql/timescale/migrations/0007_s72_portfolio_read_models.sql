-- +goose Up
-- S72: Portfolio read model tables for persistence & materialization.

CREATE TABLE portfolio_state (
    state_id           TEXT        NOT NULL,
    scope              TEXT        NOT NULL,
    account_id         TEXT        NOT NULL,
    venue              TEXT        NOT NULL DEFAULT '',
    projected_at_ms    BIGINT      NOT NULL,
    equity_usd         DOUBLE PRECISION NOT NULL,
    realized_pnl_usd   DOUBLE PRECISION NOT NULL,
    unrealized_pnl_usd DOUBLE PRECISION NOT NULL,
    balances           JSONB       NOT NULL DEFAULT '[]',
    positions          JSONB       NOT NULL DEFAULT '[]',
    exposures          JSONB       NOT NULL DEFAULT '[]',
    risk               JSONB       NOT NULL DEFAULT '{}',
    fill_summary       JSONB       NOT NULL DEFAULT '{}',
    provenance         JSONB       NOT NULL DEFAULT '{}',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, venue, state_id)
);

CREATE INDEX idx_portfolio_state_projected
    ON portfolio_state (account_id, venue, projected_at_ms DESC);

CREATE TABLE portfolio_account_snapshot (
    snapshot_id        TEXT        NOT NULL,
    account_id         TEXT        NOT NULL,
    projected_at_ms    BIGINT      NOT NULL,
    total_equity_usd   DOUBLE PRECISION NOT NULL,
    total_realized_usd DOUBLE PRECISION NOT NULL,
    total_unrealized   DOUBLE PRECISION NOT NULL,
    total_margin_used  DOUBLE PRECISION NOT NULL,
    total_leverage     DOUBLE PRECISION NOT NULL,
    venues             JSONB       NOT NULL DEFAULT '[]',
    fill_summary       JSONB       NOT NULL DEFAULT '{}',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, snapshot_id)
);

CREATE INDEX idx_portfolio_account_snapshot_projected
    ON portfolio_account_snapshot (account_id, projected_at_ms DESC);

CREATE TABLE portfolio_summary (
    summary_id           TEXT        NOT NULL,
    projected_at_ms      BIGINT      NOT NULL,
    global_equity_usd    DOUBLE PRECISION NOT NULL,
    global_realized_usd  DOUBLE PRECISION NOT NULL,
    global_unrealized    DOUBLE PRECISION NOT NULL,
    global_margin_used   DOUBLE PRECISION NOT NULL,
    global_leverage      DOUBLE PRECISION NOT NULL,
    total_position_count INT         NOT NULL DEFAULT 0,
    total_open_orders    INT         NOT NULL DEFAULT 0,
    accounts             JSONB       NOT NULL DEFAULT '[]',
    fill_summary         JSONB       NOT NULL DEFAULT '{}',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (summary_id)
);

CREATE INDEX idx_portfolio_summary_projected
    ON portfolio_summary (projected_at_ms DESC);

-- +goose Down
DROP TABLE IF EXISTS portfolio_summary;
DROP TABLE IF EXISTS portfolio_account_snapshot;
DROP TABLE IF EXISTS portfolio_state;
