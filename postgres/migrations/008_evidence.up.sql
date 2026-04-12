-- Evidence: full pipeline audit trail for each trade.
-- Accumulates at each pipeline step (swiftward, risk_router, fill, hash_chain, attestation).
-- Used for: crash recovery, on-chain attestation notes, hash chain verification.
ALTER TABLE trades ADD COLUMN evidence JSONB DEFAULT '{}';
