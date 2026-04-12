## MODIFIED Requirements

### Requirement: execute_swap tool
The server SHALL expose an `execute_swap` MCP tool that accepts a serialized `TradeIntent` dict, validates it, and routes based on `action`:
- `action="LONG"`: fetch current price from `price_feed_mcp`, execute via configured engine, return result
- `action="FLAT"`: close the open position for `intent.asset` via the engine, return result
- `action="FLAT_ALL"`: query all open positions, close each one sequentially via the engine, return aggregated result

For `FLAT_ALL`, if there are no open positions, the tool SHALL return `status="executed"` with `trades=[]` and no error.

#### Scenario: Successful LONG execution in paper mode
- **WHEN** `execute_swap` is called with a valid LONG TradeIntent and the engine is paper
- **THEN** the response contains `status="executed"`, a `tx_hash` starting with `"paper_"`, `executed_price`, and `slippage_pct`

#### Scenario: Rejected execution when at max positions
- **WHEN** `execute_swap` is called with a LONG TradeIntent and open positions are already at the configured maximum
- **THEN** the response contains `status="rejected"` and a non-empty `reason`

#### Scenario: ERC-8004 validation hook fires after LONG
- **WHEN** a LONG trade executes successfully
- **THEN** `submit_validation` is scheduled as a non-blocking async task

#### Scenario: ERC-8004 reputation hook fires after FLAT
- **WHEN** a FLAT trade executes successfully (closing a position)
- **THEN** `submit_reputation` is scheduled as a non-blocking async task

#### Scenario: FLAT_ALL with two open positions closes both
- **WHEN** `execute_swap` is called with `action="FLAT_ALL"` and two positions are open (SOL, AVAX)
- **THEN** both positions are closed, `status="executed"`, and the response lists two closed trades

#### Scenario: FLAT_ALL with no open positions is a no-op
- **WHEN** `execute_swap` is called with `action="FLAT_ALL"` and no positions are open
- **THEN** the response contains `status="executed"` and an empty trades list
