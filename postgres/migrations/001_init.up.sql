CREATE TABLE IF NOT EXISTS trades (
    id              BIGSERIAL PRIMARY KEY,
    agent_id        TEXT NOT NULL,
    timestamp       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    pair            TEXT NOT NULL,
    side            TEXT NOT NULL,
    qty             NUMERIC(20,8) NOT NULL,
    price           NUMERIC(20,8),
    value           NUMERIC(20,8),
    pnl             NUMERIC(20,8) DEFAULT 0,
    status          TEXT NOT NULL,
    value_after     NUMERIC(20,8),
    decision_hash   TEXT,
    fill_id         TEXT,
    tx_hash         TEXT,
    reason          TEXT
);

CREATE INDEX IF NOT EXISTS idx_trades_agent_id ON trades(agent_id);
CREATE INDEX IF NOT EXISTS idx_trades_agent_ts ON trades(agent_id, timestamp DESC, id DESC);

CREATE TABLE IF NOT EXISTS agent_state (
    agent_id        TEXT PRIMARY KEY,
    cash            NUMERIC(20,8) NOT NULL,
    initial_value   NUMERIC(20,8) NOT NULL,
    peak_value      NUMERIC(20,8) NOT NULL,
    fill_count      INT NOT NULL DEFAULT 0,
    reject_count    INT NOT NULL DEFAULT 0,
    chain_nonce     BIGINT NOT NULL DEFAULT 0,
    halted          BOOLEAN NOT NULL DEFAULT false,
    CONSTRAINT agent_state_cash_non_negative CHECK (cash >= 0)
);

CREATE TABLE IF NOT EXISTS decision_traces (
    decision_hash   TEXT PRIMARY KEY,
    agent_id        TEXT NOT NULL,
    prev_hash       TEXT NOT NULL,
    trace_json      JSONB NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    seq             BIGSERIAL NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_traces_agent_seq ON decision_traces(agent_id, seq DESC);
