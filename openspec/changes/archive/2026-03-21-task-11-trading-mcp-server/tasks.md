## 1. PriceClient (infra layer)

- [ ] 1.1 Create `src/trading_mcp/infra/price_client.py` with `PriceClient` class using `httpx.AsyncClient`; implement `get_prices_latest(assets) -> dict[str, Decimal]` and `get_price(asset) -> Decimal`; raise `MCPError` on JSON-RPC error response
- [ ] 1.2 Create `tests/trading_mcp/test_price_client.py` with parametrized tests using `respx` mock: successful multi-asset fetch, single-asset `get_price`, upstream JSON-RPC error raises `MCPError`

## 2. LiveEngine skeleton

- [ ] 2.1 Create `src/trading_mcp/engine/live.py` with `LiveEngine` class; implement `execute_swap(intent, current_price)` that checks `risk_router_address` and raises `RuntimeError` if empty/placeholder; add `_build_eip712_payload(intent)` that constructs domain + struct dicts with `chainId` from config
- [ ] 2.2 Add 60-second confirmation poll loop in `LiveEngine` using `w3.eth.wait_for_transaction_receipt(tx_hash, timeout=60)` (via `asyncio.to_thread`); raise `TimeoutError` on expiry
- [ ] 2.3 Create `tests/trading_mcp/test_live_engine.py` with tests: unconfigured Risk Router raises `RuntimeError`; EIP-712 payload contains correct `chainId`; confirmation timeout raises `TimeoutError` (mock `w3`)

## 3. TradingService (orchestration layer)

- [ ] 3.1 Create `src/trading_mcp/service/__init__.py` (empty)
- [ ] 3.2 Create `src/trading_mcp/service/trading_service.py` with `TradingService` class; inject `engine` (PaperEngine or LiveEngine), `portfolio_service: PortfolioService`, `price_client: PriceClient`, `registry: ERC8004Registry`, `session_factory`
- [ ] 3.3 Implement `TradingService.execute_swap(intent: TradeIntent) -> ExecutionResult`: fetch price via `price_client.get_price(intent.asset)`, call `engine.execute_swap(intent, price)`, on LONG success query position_id and fire `create_task(registry.submit_validation(id))`, on FLAT success query position_id and fire `create_task(registry.submit_reputation(id))`
- [ ] 3.4 Implement `TradingService.get_portfolio(assets: list[str]) -> PortfolioSummary`: fetch prices for all assets, delegate to `portfolio_service.get_portfolio(prices)`
- [ ] 3.5 Implement `TradingService.get_positions(assets: list[str]) -> list[PositionView]`: fetch prices, delegate to `portfolio_service.get_positions(prices)`
- [ ] 3.6 Implement `TradingService.get_position(asset: str) -> PositionView | None`: fetch single price, delegate to `portfolio_service.get_position(asset, price)`
- [ ] 3.7 Implement `TradingService.get_daily_pnl() -> Decimal`: delegate to `portfolio_service.get_daily_pnl()`

## 4. FastMCP Server

- [ ] 4.1 Create `src/trading_mcp/server.py` with `FastMCP("trading_mcp", stateless_http=True, lifespan=_lifespan, host="0.0.0.0", port=8005)`
- [ ] 4.2 Implement `_lifespan`: run Alembic `upgrade head` via `asyncio.to_thread`; create engine + session factory; instantiate `PortfolioService`, `PriceClient`, select engine (PaperEngine or LiveEngine) based on `config.trading.mode`; instantiate `IpfsProvider` (MockIpfs or PinataIpfs based on `config.erc8004.ipfs_provider`); instantiate `ERC8004Registry`; instantiate `TradingService`; fire `asyncio.create_task(registry.register_identity())`; store in module-level globals; yield; dispose engine
- [ ] 4.3 Implement `GET /health` custom route returning `{"status": "ok"}`
- [ ] 4.4 Implement `@mcp.tool() execute_swap(intent: dict) -> dict`: validate with `TradeIntent.model_validate(intent)`, call `_get_service().execute_swap(intent)`, return `dataclasses.asdict(result)` with Decimal fields as str
- [ ] 4.5 Implement `@mcp.tool() get_portfolio(assets: list[str]) -> dict`: call service, return serialized `PortfolioSummary`
- [ ] 4.6 Implement `@mcp.tool() get_positions(assets: list[str]) -> list[dict]`: call service, return list of serialized `PositionView`
- [ ] 4.7 Implement `@mcp.tool() get_position(asset: str) -> dict | None`: call service, return serialized `PositionView` or None
- [ ] 4.8 Implement `@mcp.tool() get_daily_pnl() -> str`: call service, return Decimal as string
- [ ] 4.9 Add `if __name__ == "__main__": mcp.run(transport="streamable-http")` entrypoint

## 5. Integration Tests

- [ ] 5.1 Create `tests/trading_mcp/test_server.py`; set up fixtures: SQLite in-memory DB + session factory, respx mock for `price_feed_mcp` returning static prices, `TradingService` with PaperEngine wired to SQLite, MockIpfs registry
- [ ] 5.2 Test `execute_swap LONG → portfolio state`: call service `execute_swap` with LONG intent, assert `status="executed"`, call `get_portfolio`, assert `open_position_count=1` and position present
- [ ] 5.3 Test `execute_swap LONG then FLAT → closed position`: open then close, assert portfolio `open_position_count=0`, `realized_pnl_today` non-zero
- [ ] 5.4 Test `execute_swap rejected at max positions`: open 2 positions, 3rd returns `status="rejected"`
- [ ] 5.5 Test `get_daily_pnl` returns `"0"` with no trades today
- [ ] 5.6 Test health endpoint: call `GET /health` on the FastMCP app (using `httpx.AsyncClient` against the ASGI app), assert HTTP 200 and `{"status": "ok"}`
