DROP INDEX IF EXISTS idx_alerts_group;

ALTER TABLE alerts DROP COLUMN IF EXISTS executed_at;
ALTER TABLE alerts DROP COLUMN IF EXISTS notify_on_execution;
ALTER TABLE alerts DROP COLUMN IF EXISTS cancel_group_on_trigger;
ALTER TABLE alerts DROP COLUMN IF EXISTS group_id;
ALTER TABLE alerts DROP COLUMN IF EXISTS exchange_order_id;
ALTER TABLE alerts DROP COLUMN IF EXISTS execution_type;
ALTER TABLE alerts DROP COLUMN IF EXISTS trade_value;
ALTER TABLE alerts DROP COLUMN IF EXISTS trade_side;
ALTER TABLE alerts DROP COLUMN IF EXISTS action;

ALTER TABLE trades DROP COLUMN IF EXISTS params;
