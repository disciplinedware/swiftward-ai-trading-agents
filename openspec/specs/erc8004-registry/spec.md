## ADDED Requirements

### Requirement: Identity registration is idempotent

The system SHALL check the `agents` table before calling `identityRegistry.register`. If a row already exists, it SHALL skip registration and return without error.

#### Scenario: First startup registers identity
- **WHEN** `register_identity()` is called and no row exists in `agents`
- **THEN** it SHALL upload a registration JSON to IPFS, call `identityRegistry.register(uri, hash)`, and persist the returned `agentId` to the `agents` table

#### Scenario: Subsequent startup skips registration
- **WHEN** `register_identity()` is called and an `agents` row already exists
- **THEN** it SHALL return without calling IPFS or the registry

### Requirement: Validation hook uploads reasoning trace

The system SHALL provide `submit_validation(position_id)` which loads the position, uploads a reasoning trace to IPFS, calls `validationRegistry.validationRequest()`, and updates `positions.validation_uri`.

#### Scenario: Validation submitted for open position
- **WHEN** `submit_validation(position_id)` is called for a valid position
- **THEN** it SHALL upload the reasoning trace to IPFS and call `validationRegistry.validationRequest()` with the agentId, URI, and content hash
- **THEN** it SHALL update `positions.validation_uri` with the returned IPFS URI

### Requirement: Reputation hook converts PnL to score

The system SHALL provide `submit_reputation(position_id)` which converts `realized_pnl_pct` to a 0–100 integer score using `clamp(int((pnl_pct + 1.0) * 50), 0, 100)` and calls `reputationRegistry.giveFeedback()`.

#### Scenario: Positive PnL maps to score above 50
- **WHEN** `realized_pnl_pct` is `0.10` (10% gain)
- **THEN** the score SHALL be `55`

#### Scenario: Negative PnL maps to score below 50
- **WHEN** `realized_pnl_pct` is `-0.20` (20% loss)
- **THEN** the score SHALL be `40`

#### Scenario: Score is clamped to 0–100
- **WHEN** `realized_pnl_pct` is extreme (e.g., `+5.0` or `-2.0`)
- **THEN** the score SHALL be clamped to `100` or `0` respectively

### Requirement: Registry calls are non-blocking

All three registry methods (identity, validation, reputation) SHALL be wrapped in `asyncio.create_task` so callers return immediately without awaiting chain confirmation.

#### Scenario: Caller returns before chain confirmation
- **WHEN** a registry hook is triggered
- **THEN** the calling code SHALL return immediately without blocking on the registry transaction
