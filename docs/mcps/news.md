# News MCP (6 tools)

Headlines, sentiment analysis, and upcoming events for crypto markets.

**Source**: [CryptoPanic API](https://cryptopanic.com/developers/api/) (free tier).
Sentiment is derived from community vote ratios (positive/liked/important vs negative/disliked/toxic).
Events are classified from important news articles using keyword matching.

**Endpoint**: `POST /mcp/news` (backend on trading-server:8091, gateway on swiftward-server:8095)

**Config**:
- `TRADING__NEWS__SOURCES=cryptopanic` — ordered source list (currently only cryptopanic)
- `TRADING__NEWS__CRYPTOPANIC_TOKEN=<token>` — free API auth token from cryptopanic.com
- `TRADING__LLM_AGENT__NEWS_MCP_URL=http://swiftward-server:8095/mcp/news` — agent connection URL

## `news/search`

Search crypto news articles by keyword with optional filters.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `query` | string | yes | Search keyword (matched against title and currency names) |
| `markets` | string[] | no | Filter by crypto symbols, e.g. `["BTC", "ETH"]` |
| `filter` | string | no | `"rising"`, `"hot"`, `"bullish"`, `"bearish"`, `"important"` |
| `kind` | string | no | `"news"`, `"media"`, `"all"` (default: all) |
| `date_from` | string | no | RFC3339 datetime (e.g. `2026-03-09T00:00:00Z`) — only articles after this time |
| `date_to` | string | no | RFC3339 datetime (e.g. `2026-03-09T23:59:59Z`) — only articles before this time |
| `limit` | int | no | Max articles (default 10, max 50) |

**Result:**
```json
{
  "articles": [{
    "title": "Bitcoin Hits New All-Time High",
    "source": "CoinDesk",
    "url": "https://...",
    "published_at": "2026-03-09T12:00:00Z",
    "sentiment": "positive",
    "markets": ["BTC"],
    "kind": "news"
  }],
  "count": 1,
  "source": "cryptopanic"
}
```

## `news/get_latest`

Get latest crypto news headlines.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `kind` | string | no | `"news"`, `"media"`, `"all"` (default: all) |
| `markets` | string[] | no | Filter by crypto symbols |
| `filter` | string | no | `"rising"`, `"hot"`, `"bullish"`, `"bearish"`, `"important"` |
| `limit` | int | no | Max articles (default 10, max 50) |

**Result:** Same shape as `news/search`.

## `news/get_sentiment`

Get aggregated news sentiment for a crypto asset or topic.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `query` | string | yes | Asset or topic (e.g. "ETH", "Bitcoin", "DeFi") |
| `markets` | string[] | no | Filter by crypto symbols for precision |
| `period` | string | no | `"1h"`, `"4h"`, `"24h"`, `"7d"` (default: 24h) |

**Result:**
```json
{
  "query": "ETH",
  "sentiment": "positive",
  "score": 0.65,
  "article_count": 47,
  "key_themes": ["upgrade", "adoption"],
  "period": "24h",
  "source": "cryptopanic"
}
```

Score range: -1.0 (very bearish) to 1.0 (very bullish).

## `news/get_events`

Get market-moving crypto events (upcoming and recent) classified from important news articles. Events are extracted from articles published within the lookback window - read titles for actual event timing.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `markets` | string[] | no | Filter by crypto symbols |
| `type` | string | no | `"fork"`, `"upgrade"`, `"regulation"`, `"hack"`, `"listing"`, `"unlock"`, `"macro"` |
| `days` | int | no | How many days of recent news to scan for events (default 7, max 30) |

**Result:**
```json
{
  "events": [{
    "title": "Ethereum Pectra Upgrade",
    "type": "upgrade",
    "date": "2026-03-15T00:00:00Z",
    "impact_level": "high",
    "details": "Source: The Block",
    "market": "ETH"
  }],
  "count": 1,
  "source": "cryptopanic"
}
```

Event classification keywords:
- **hack**: "hack", "exploit", "breach", "stolen", "attack", "vulnerability"
- **upgrade**: "upgrade", "hard fork", "pectra", "dencun", "cancun"
- **fork**: "fork"
- **regulation**: "sec", "regulation", "ban", "lawsuit", "court"
- **listing**: "listing", "delist", "launch"
- **unlock**: "unlock", "vesting", "airdrop"
- **macro**: "fed", "fomc", "cpi", "inflation", "interest rate", "gdp"

## `news/set_alert`

Create a news alert that fires when matching articles appear. The poller checks news every ~5 minutes. When triggered, the agent is woken up with the alert context. At least one of `markets` or `categories` must be provided.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `markets` | string[] | conditional | Ticker symbols to monitor, e.g. `["BTC", "ETH"]` |
| `categories` | string[] | conditional | Topic categories to monitor. Enum: `REGULATION`, `TRADING`, `MARKET`, `EXCHANGE`, `BUSINESS`, `TECHNOLOGY`, `BLOCKCHAIN`, `MACROECONOMICS`, `FIAT`, `ICO`, `TOKEN SALE`, `MINING`, `SPONSORED`, `OTHER` |
| `title_contains` | string | no | Client-side case-insensitive substring filter applied after the API result |
| `on_trigger` | string | no | `wake_full` (default, start a full agent session) or `wake_triage` (quick Haiku triage first) |
| `note` | string | no | Free-form note shown in the alert feed |
| `max_triggers` | int | no | Max times this alert can fire before expiring. Default 1. `0` = unlimited. |

**Result:**
```json
{
  "alert_id": "news_alert_abc123",
  "markets": ["BTC"],
  "categories": ["REGULATION"],
  "title_contains": "",
  "on_trigger": "wake_full",
  "status": "active"
}
```

Alerts are scoped per agent via `X-Agent-ID`. At least one of `markets` or `categories` is required (purely title-filter alerts are not supported).

## `news/get_triggered_alerts`

Fetch news alerts that have fired since the last call. Draining this list acknowledges the alerts — they will not be returned again.

No params.

**Result:**
```json
{
  "alerts": [
    {
      "alert_id": "news_alert_abc123",
      "triggered_at": "2026-04-10T12:34:56Z",
      "article": {
        "title": "SEC announces new stablecoin rules",
        "source": "CoinDesk",
        "url": "https://...",
        "published_at": "2026-04-10T12:30:00Z",
        "markets": ["USDC"],
        "categories": ["REGULATION"]
      },
      "on_trigger": "wake_full"
    }
  ]
}
```
