-- Rename columns to match models.md naming conventions.
-- Safe to run on DBs that already have 001_init applied with old names.
-- Uses IF EXISTS-style approach: DO block checks column existence before renaming.

DO $$
BEGIN
    -- trades table
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='trades' AND column_name='market') THEN
        ALTER TABLE trades RENAME COLUMN market TO pair;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='trades' AND column_name='cost_usd') THEN
        ALTER TABLE trades RENAME COLUMN cost_usd TO value;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='trades' AND column_name='verdict') THEN
        ALTER TABLE trades RENAME COLUMN verdict TO status;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='trades' AND column_name='equity_after') THEN
        ALTER TABLE trades RENAME COLUMN equity_after TO value_after;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='trades' AND column_name='order_id') THEN
        ALTER TABLE trades RENAME COLUMN order_id TO fill_id;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='trades' AND column_name='reasoning') THEN
        ALTER TABLE trades RENAME COLUMN reasoning TO reason;
    END IF;

    -- agent_state table
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='agent_state' AND column_name='initial_cash') THEN
        ALTER TABLE agent_state RENAME COLUMN initial_cash TO initial_value;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='agent_state' AND column_name='trade_count') THEN
        ALTER TABLE agent_state RENAME COLUMN trade_count TO fill_count;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='agent_state' AND column_name='rejected_count') THEN
        ALTER TABLE agent_state RENAME COLUMN rejected_count TO reject_count;
    END IF;
END $$;
