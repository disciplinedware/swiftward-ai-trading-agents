-- Retroactively mark filled trades with no (or null) attestation record as
-- 'disabled'. These accumulated while ValidationRegistry was not configured
-- (validation addr commented out in .env). The recovery loop must not retry
-- them on restart, so we write an explicit 'disabled' status.
--
-- Match three forms of "no attestation":
--   1. evidence IS NULL
--   2. evidence has no 'attestation' key at all
--   3. evidence.attestation is explicit JSON null, or evidence.attestation.status is null
-- The third case is subtle: `evidence ? 'attestation'` returns true for an
-- explicit null value, so we need a second check via ->>'status' IS NULL to
-- catch it. Without this, rows like {"attestation": null} would escape the
-- migration and still be picked up by the recovery loop.
UPDATE trades
SET evidence = COALESCE(evidence, '{}'::jsonb) || jsonb_build_object(
    'attestation',
    jsonb_build_object(
        'status', 'disabled',
        'reason', 'retroactive_migration_validation_was_disabled'
    )
)
WHERE status = 'fill'
  AND (evidence IS NULL
       OR NOT evidence ? 'attestation'
       OR evidence->'attestation'->>'status' IS NULL);

-- Partial index for the attestation recovery query. Matches three recoverable
-- states: NULL (crash-in-window, or write-pending DB failure), 'pending', and
-- 'waiting_for_gas'. Using OR instead of IN because Postgres's partial-index
-- predicate matcher does not reliably recognize equivalence between an IN list
-- in the query and an IN list in the index predicate.
CREATE INDEX IF NOT EXISTS idx_trades_attestation_recoverable
    ON trades (agent_id, id)
    WHERE status = 'fill'
      AND (evidence->'attestation'->>'status' IS NULL
           OR evidence->'attestation'->>'status' = 'pending'
           OR evidence->'attestation'->>'status' = 'waiting_for_gas');
