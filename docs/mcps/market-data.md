# Market Data MCP (8 core tools + 3 optional PRISM)

Prices, candles (OHLCV) with server-side indicators, orderbook, funding rates, open interest, and price / funding / open-interest alerts. Optional Strykr PRISM tools for fear & greed, technicals, and AI signals when `PRISM_ENABLED=true`.

Endpoint: `POST /mcp/market`

Agents reach this MCP via the MCP Gateway (`swiftward-server:8095/mcp/market`), which proxies
to the backend (`trading-server:8091/mcp/market`). No guardrails on market data tools; note that alert management tools (`market/set_alert`, `market/cancel_alert`) modify server-side alert state.

## `market/get_prices`

| Param | Type | Required |
|-------|------|----------|
| `markets` | string[] | yes |

**Result:**
```json
{
  "prices": [
    {
      "market": "ETH-USDC",
      "bid": "3204.50",
      "ask": "3206.20",
      "last": "3205.50",
      "volume_24h": "1250000000",
      "change_24h_pct": "2.35",
      "high_24h": "3250.00",
      "low_24h": "3120.00",
      "timestamp": "2026-03-10T13:00:00Z"
    }
  ]
}
```

## `market/get_candles`

| Param | Type | Required | Notes |
|-------|------|----------|-------|
| `market` | string | yes | |
| `interval` | string | yes | 1m, 5m, 15m, 1h, 4h, 1d |
| `limit` | int | yes | 1-720 (hard cap set by Kraken's per-request max) |
| `end_time` | string | no | ISO8601 |
| `format` | string | no | `"json"` (default) or `"csv"`. Ignored when `save_to_file=true`. |
| `save_to_file` | bool | no | If true, writes CSV to `market/{market}_{interval}.csv` in the agent's workspace and returns path metadata. Use before `code/execute` - keeps data out of LLM context. |
| `indicators` | string[] | no | Server-side indicators, e.g. ["rsi_14", "ema_21", "macd", "bbands", "atr_14", "vwap"] |

Only closed candles are returned (current incomplete candle excluded).

**Supported indicators:**
- `rsi_<period>` - RSI (Wilder's smoothing), e.g. `rsi_14`
- `ema_<period>` - Exponential Moving Average, e.g. `ema_21`
- `sma_<period>` - Simple Moving Average, e.g. `sma_20`
- `macd` or `macd_<fast>_<slow>_<signal>` - MACD (default 12/26/9). Columns: macd, macd_signal, macd_hist
- `bbands` or `bbands_<period>_<stddev>` - Bollinger Bands (default 20/2). Columns: bb_upper, bb_middle, bb_lower
- `atr_<period>` - Average True Range, e.g. `atr_14`
- `vwap` - Volume-weighted average price (cumulative across all requested candles)

Indicator values are null during the warm-up period (not enough candles yet).

**Result (`format="json"`, default):**
```json
{
  "market": "ETH-USDC",
  "interval": "1h",
  "count": 100,
  "candles": [
    {
      "t": "2026-03-10T13:00:00Z",
      "o": "3180.00",
      "h": "3210.00",
      "l": "3175.00",
      "c": "3205.00",
      "v": "45230000",
      "rsi_14": 62.3,
      "ema_21": 3195.4
    }
  ]
}
```

**Result (`format="csv"`):** Returns CSV text with full column names (pandas-compatible). Only use for small datasets - the data goes through LLM context.

**Result (`save_to_file=true`) - writes to shared volume, returns path only:**
```json
{
  "saved_to": "/workspace/market/ETH-USDC_1h.csv",
  "market": "ETH-USDC",
  "interval": "1h",
  "rows": 500,
  "columns": ["timestamp", "open", "high", "low", "close", "volume", "rsi_14"],
  "updated_at": "2026-03-10T14:30:00Z"
}
```

The file is written to `{workspace}/{agent-id}/market/{market}_{interval}.csv` on the shared volume. The sandbox sees it at `/workspace/market/{market}_{interval}.csv`. File is overwritten on each call.

**Workflow - Python analysis without polluting LLM context:**
```
market/get_candles(market="ETH-USDC", interval="1h", limit=500, indicators=["rsi_14"], save_to_file=true)
  -> writes /data/workspace/{agent-id}/market/ETH-USDC_1h.csv
  -> returns {saved_to: "/workspace/market/ETH-USDC_1h.csv", rows: 500}  <- tiny response

code/execute("import pandas as pd; df = pd.read_csv('/workspace/market/ETH-USDC_1h.csv'); print(df.tail())")
  -> Python reads directly from volume
  -> returns analysis result only
```

## `market/get_orderbook`

| Param | Type | Required |
|-------|------|----------|
| `market` | string | yes |
| `depth` | int | no (default 20) |

**Result:**
```json
{
  "market": "ETH-USDC",
  "bids": [[3204.50, 12.5]],
  "asks": [[3206.20, 10.1]],
  "bid_total": 450.2,
  "ask_total": 380.5,
  "spread": 1.70,
  "spread_pct": 0.053,
  "imbalance": 0.54
}
```

`imbalance` is bid_total / (bid_total + ask_total). Above 0.5 = more buy pressure.

## `market/list_markets`

| Param | Type | Required |
|-------|------|----------|
| `quote` | string | no |
| `sort_by` | string | no | "volume" (default), "name", "change" |

**Result:**
```json
{
  "markets": [
    {
      "pair": "ETH-USDC",
      "base": "ETH",
      "quote": "USDC",
      "last_price": 3205.50,
      "volume_24h": 1250000000,
      "change_24h_pct": 2.35,
      "tradeable": true
    }
  ]
}
```

## `market/get_funding`

Perpetual contract funding rates.

| Param | Type | Required |
|-------|------|----------|
| `market` | string | yes |
| `limit` | int | no (default 10) |

**Result:**
```json
{
  "market": "ETH-USDC",
  "current_rate": 0.00125,
  "annualized_pct": 10.95,
  "next_funding_time": "2026-03-10T16:00:00Z",
  "history": [{"timestamp": "2026-03-10T08:00:00Z", "rate": 0.00125}],
  "signal": "neutral"
}
```

`signal` is one of: `"extreme_bullish"` (rate > 0.1%), `"bullish_crowd"` (0.01% < rate <= 0.1%), `"bearish_crowd"` (-0.1% <= rate < -0.01%), `"extreme_bearish"` (rate < -0.1%), `"neutral"` (|rate| <= 0.01%).

## `market/get_open_interest`

| Param | Type | Required |
|-------|------|----------|
| `market` | string | yes |

**Result:**
```json
{
  "market": "ETH-USDC",
  "open_interest": 1250000000,
  "oi_change_1h_pct": 0.5,
  "oi_change_4h_pct": 2.1,
  "oi_change_24h_pct": 3.2,
  "long_short_ratio": 1.15
}
```

## `market/set_alert`

| Param | Type | Required | Notes |
|-------|------|----------|-------|
| `market` | string | yes | |
| `condition` | string | yes | `above`, `below`, `change_pct`, `volume_spike`, `funding_threshold`, `oi_change_pct` |
| `value` | float | yes | Price level, % magnitude, or rate depending on condition |
| `window` | string | no | For `change_pct`: `5m`, `1h`. Default `5m`. |
| `note` | string | no | |

**Conditions**:

- `above` / `below` - fire when price crosses the threshold (value = price level)
- `change_pct` - fire when price moves |X|% from the reference price at creation (value = % magnitude, `window` sets the reference window)
- `volume_spike` - fire when |24h change %| exceeds value (proxy for unusual activity)
- `funding_threshold` - fire when |funding rate| >= value (e.g. `0.001` = 0.1%)
- `oi_change_pct` - fire when |1h open-interest change %| exceeds value

**Result:** `{"alert_id": "alert_abc123", "status": "active"}`

Alerts are per-agent (scoped by `X-Agent-ID` header). The background poller checks every ~10 seconds. Alert IDs are deterministic from `(agent_id, market, condition, value, window)` so duplicate creation is idempotent. When an alert fires it is marked `triggered` and surfaced to the agent via `alert/triggered` on the Trading MCP.

## `market/cancel_alert`

| Param | Type | Required |
|-------|------|----------|
| `alert_id` | string | yes |

**Result:** `{"success": true}`

## Optional PRISM tools (when `PRISM_ENABLED=true`)

Three extra tools backed by the Strykr PRISM API surface additional market intelligence:

- `market/get_fear_greed` - Fear & Greed index (0-100) with historical context
- `market/get_technicals` - daily RSI / MACD / SMA / EMA / Bollinger / Stochastic / ATR / ADX panel
- `market/get_signals` - AI signal summary per asset

These tools are only registered when `PRISM_ENABLED=true` in config (see `golang/internal/mcps/marketdata/service.go`).
