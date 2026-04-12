-- Reverse the retroactive migration. Order matters: reverse the data UPDATE
-- FIRST, then drop the index. If a service restarts between these two
-- statements with the old order (drop-first), it would run recovery queries
-- as a seq scan against rows that still have the 'disabled' state, wasting
-- CPU. With data-first order, the worst case is a brief window where the
-- index exists for now-unindexed rows, which is harmless.

-- 1. Reverse the UPDATE: remove the attestation sub-object, but only for
--    rows whose reason matches the migration's exact string. Trades that
--    had other attestation states written after migration are untouched.
UPDATE trades
SET evidence = evidence - 'attestation'
WHERE status = 'fill'
  AND evidence->'attestation'->>'reason' = 'retroactive_migration_validation_was_disabled';

-- 2. Drop the partial index last.
DROP INDEX IF EXISTS idx_trades_attestation_recoverable;
