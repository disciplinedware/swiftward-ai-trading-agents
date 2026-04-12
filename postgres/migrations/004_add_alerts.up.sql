-- Persistent alerts table: shared by Market Data MCP (price alerts),
-- Trading MCP (position alerts), and polled by the Claude Agent runtime.
--
-- Design:
--   - Each alert belongs to one service (market, trading, news)
--   - Condition evaluation happens inside the owning MCP's 10s loop
--   - When triggered: triggered_at set, trigger_count incremented
--   - auto_execute alerts: Trading MCP submits sell immediately on trigger
--   - wake_triage / wake_full alerts: Claude Agent runtime picks them up and
--     injects them into the next session's startup prompt

CREATE TABLE IF NOT EXISTS alerts (
    alert_id        TEXT        NOT NULL PRIMARY KEY,
    agent_id        TEXT        NOT NULL,
    service         TEXT        NOT NULL,           -- 'market' | 'trading' | 'news' | 'time'
    status          TEXT        NOT NULL DEFAULT 'active', -- 'active' | 'triggered' | 'exhausted' | 'cancelled' | 'expired'
    on_trigger      TEXT        NOT NULL DEFAULT 'wake_full', -- 'auto_execute' | 'wake_triage' | 'wake_full'
    max_triggers    INTEGER     NOT NULL DEFAULT 1,  -- 0 = unlimited
    trigger_count   INTEGER     NOT NULL DEFAULT 0,
    params          JSONB       NOT NULL DEFAULT '{}',
    triage_prompt   TEXT,                            -- hint for Haiku triage (wake_triage only)
    note            TEXT,                            -- human-readable label
    expires_at      TIMESTAMPTZ,                     -- NULL = no expiry
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    triggered_at    TIMESTAMPTZ                      -- most recent trigger time
);

CREATE INDEX IF NOT EXISTS idx_alerts_agent_service_status
    ON alerts (agent_id, service, status);

CREATE INDEX IF NOT EXISTS idx_alerts_status_triggered
    ON alerts (status, triggered_at)
    WHERE triggered_at IS NOT NULL;

-- For GetDueTimeAlerts: service='time' AND status='active' without agent_id filter
CREATE INDEX IF NOT EXISTS idx_alerts_service_status
    ON alerts (service, status);

-- For GetTriggeredAlerts: agent_id + triggered_at IS NOT NULL
CREATE INDEX IF NOT EXISTS idx_alerts_agent_triggered
    ON alerts (agent_id, triggered_at)
    WHERE triggered_at IS NOT NULL;
