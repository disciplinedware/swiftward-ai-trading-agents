-- Revert column renames back to original names.

DO $$
BEGIN
    -- trades table
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='trades' AND column_name='pair') THEN
        ALTER TABLE trades RENAME COLUMN pair TO market;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='trades' AND column_name='value') THEN
        ALTER TABLE trades RENAME COLUMN value TO cost_usd;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='trades' AND column_name='status') THEN
        ALTER TABLE trades RENAME COLUMN status TO verdict;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='trades' AND column_name='value_after') THEN
        ALTER TABLE trades RENAME COLUMN value_after TO equity_after;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='trades' AND column_name='fill_id') THEN
        ALTER TABLE trades RENAME COLUMN fill_id TO order_id;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='trades' AND column_name='reason') THEN
        ALTER TABLE trades RENAME COLUMN reason TO reasoning;
    END IF;

    -- agent_state table
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='agent_state' AND column_name='initial_value') THEN
        ALTER TABLE agent_state RENAME COLUMN initial_value TO initial_cash;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='agent_state' AND column_name='fill_count') THEN
        ALTER TABLE agent_state RENAME COLUMN fill_count TO trade_count;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='agent_state' AND column_name='reject_count') THEN
        ALTER TABLE agent_state RENAME COLUMN reject_count TO rejected_count;
    END IF;
END $$;
