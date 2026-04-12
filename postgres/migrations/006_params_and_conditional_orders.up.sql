-- trades: store full params bag from submit_order as JSONB
-- Contains: stop_loss, take_profit, strategy, reasoning, trigger_reason,
-- confidence, max_slippage_bps, order_type, and any other agent-provided fields.
ALTER TABLE trades ADD COLUMN params JSONB DEFAULT NULL;

-- alerts: extend for conditional orders model
-- Existing alerts keep working (defaults match current behavior).
-- New columns enable: OCO groups, platform/native execution types,
-- trade-attached conditional orders (SL/TP auto-created from submit_order).
ALTER TABLE alerts ADD COLUMN action TEXT NOT NULL DEFAULT 'execute_trade';       -- 'execute_trade' | 'wake_agent'
ALTER TABLE alerts ADD COLUMN trade_side TEXT;                                     -- 'buy' | 'sell' (only when action='execute_trade')
ALTER TABLE alerts ADD COLUMN trade_value NUMERIC(20,8);                           -- quote currency amount; 0 = close full position
ALTER TABLE alerts ADD COLUMN execution_type TEXT NOT NULL DEFAULT 'platform';     -- 'platform' (10s polling) | 'native' (exchange stop order)
ALTER TABLE alerts ADD COLUMN exchange_order_id TEXT;                              -- tracks native exchange stop order ID
ALTER TABLE alerts ADD COLUMN group_id TEXT;                                       -- links OCO orders (SL + TP cancel each other)
ALTER TABLE alerts ADD COLUMN cancel_group_on_trigger BOOLEAN NOT NULL DEFAULT TRUE;  -- OCO behavior
ALTER TABLE alerts ADD COLUMN notify_on_execution BOOLEAN NOT NULL DEFAULT TRUE;      -- wake agent after conditional order executes
ALTER TABLE alerts ADD COLUMN executed_at TIMESTAMPTZ;                             -- when the conditional order was executed

-- Index for OCO group lookups (cancel sibling orders on trigger)
CREATE INDEX IF NOT EXISTS idx_alerts_group ON alerts (group_id) WHERE group_id IS NOT NULL;
