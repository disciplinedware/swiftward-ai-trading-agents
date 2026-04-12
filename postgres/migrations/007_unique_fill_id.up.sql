-- Prevent duplicate exchange fills per agent.
-- Partial unique index: only enforced when fill_id is non-null and non-empty.
CREATE UNIQUE INDEX IF NOT EXISTS idx_trades_agent_fill_id
    ON trades (agent_id, fill_id)
    WHERE fill_id IS NOT NULL AND fill_id != '';
