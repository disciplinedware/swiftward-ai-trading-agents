## 1. Package scaffold

- [x] 1.1 Create `src/onchain_data_mcp/__init__.py`, `infra/__init__.py`, `service/__init__.py`

## 2. Infra layer — BinanceFuturesClient

- [x] 2.1 Implement `src/onchain_data_mcp/infra/binance_futures.py` with `BinanceFuturesClient`: async httpx client, `connect()`/`close()`, base URL `https://fapi.binance.com`
- [x] 2.2 Add `get_premium_index(symbol: str) -> dict` — calls `GET /fapi/v1/premiumIndex`
- [x] 2.3 Add `get_open_interest_hist(symbol: str, period: str, limit: int) -> list[dict]` — calls `GET /fapi/v1/openInterestHist`
- [x] 2.4 Add `get_force_orders(symbol: str, start_ms: int, end_ms: int) -> list[dict]` — calls `GET /fapi/v1/allForceOrders`

## 3. Service layer — OnchainDataService

- [x] 3.1 Implement `src/onchain_data_mcp/service/onchain_data.py` with `OnchainDataService(client, cache)`
- [x] 3.2 Implement `get_funding_rate(assets)`: fetch `premiumIndex` per asset, compute `annualized_pct`, cache 5 min at `"onchain:funding:{asset}"`
- [x] 3.3 Implement `get_open_interest(assets)`: fetch 2 daily OI snapshots, compute `change_pct_24h`, cache 5 min at `"onchain:oi:{asset}"`
- [x] 3.4 Implement `get_liquidations(assets)`: fetch `allForceOrders` for `[now-15min, now]`, aggregate long/short USD, cache 5 min at `"onchain:liq:{asset}"`
- [x] 3.5 Implement `get_netflow()`: return hardcoded neutral dict for BTC/ETH with TODO comment pointing to CryptoQuant endpoint

## 4. Server

- [x] 4.1 Implement `src/onchain_data_mcp/server.py`: FastMCP on port 8003, lifespan wires `BinanceFuturesClient` + `RedisCache` into `OnchainDataService`, `/health` route, four `@mcp.tool()` one-liners

## 5. Update OnchainData model

- [x] 5.1 Replace the `OnchainData(BaseModel): pass` stub in `src/common/models/signal_bundle.py` with the full field set (all `str | None`, defaulting to `None`)

## 6. Tests

- [x] 6.1 Test `get_funding_rate`: positive rate, negative rate, annualized_pct math (mock `BinanceFuturesClient`)
- [x] 6.2 Test `get_open_interest`: OI increasing (change_pct positive), OI decreasing (change_pct negative)
- [x] 6.3 Test `get_liquidations`: mixed longs/shorts aggregation, empty orders list → all zeros
- [x] 6.4 Test `get_netflow`: returns neutral for BTC and ETH, no HTTP call made
- [x] 6.5 Test `OnchainData` model: full fields round-trip, empty construction (all None)
