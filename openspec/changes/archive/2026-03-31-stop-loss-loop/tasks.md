## 1. Shared model update

- [x] 1.1 Add `"take_profit"` to `TriggerReason` literal in `src/common/models/trade_intent.py`

## 2. Slim price fetch method

- [x] 2.1 Add `get_prices_latest(assets: list[str]) -> dict[str, Decimal]` to `PriceFeedMCPClient` in `src/agent/infra/price_feed_mcp.py` — single `get_prices_latest` JSON-RPC call, returns empty dict for empty input

## 3. Stop-loss loop implementation

- [x] 3.1 Create `src/agent/loops/__init__.py` (empty)
- [x] 3.2 Create `src/agent/loops/exit_watchdog.py` with `ExitWatchdog` class: constructor takes `trading: TradingMCPClient`, `price_feed: PriceFeedMCPClient`, `gate: CooldownGate`; `run()` loops forever with 120s sleep; `_check()` fetches portfolio, gathers prices concurrently, compares against stop_loss/take_profit, fires FLAT intents
- [x] 3.3 FLAT intent construction: `action="FLAT"`, `asset=pos.asset`, `size_pct=Decimal("0")`, `strategy=pos.strategy`, `trigger_reason="stop_loss"` or `"take_profit"` depending on which level was breached, `reasoning` includes price and breached level

## 4. Wire into main

- [x] 4.1 Import `ExitWatchdog` in `src/agent/main.py`, construct it with `trading`, `price_feed`, `gate`, replace the `_noop_loop()` placeholder for Task 18 with `exit_watchdog.run()`

## 5. Tests

- [x] 5.1 Create `tests/agent/loops/test_exit_watchdog.py` with table-driven tests covering: stop_loss breach fires FLAT with `trigger_reason="stop_loss"`, take_profit breach fires FLAT with `trigger_reason="take_profit"`, no breach takes no action, MCP error skips cycle without crash, cooldown recorded after exit, gate check NOT called before exit
