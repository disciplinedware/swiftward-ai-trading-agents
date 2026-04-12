-- Add fee tracking columns to trades.
ALTER TABLE trades ADD COLUMN IF NOT EXISTS fee NUMERIC(20,8) DEFAULT 0;
ALTER TABLE trades ADD COLUMN IF NOT EXISTS fee_asset TEXT;
ALTER TABLE trades ADD COLUMN IF NOT EXISTS fee_value NUMERIC(20,8) DEFAULT 0;

-- Add cumulative fee tracking to agent_state.
ALTER TABLE agent_state ADD COLUMN IF NOT EXISTS total_fees NUMERIC(20,8) NOT NULL DEFAULT 0;
