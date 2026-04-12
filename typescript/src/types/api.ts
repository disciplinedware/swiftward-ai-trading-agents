// TypeScript types matching Go backend MCP responses.
// All monetary values are decimal strings. Timestamps are RFC3339.

// --- Trading MCP ---

export interface Position {
  pair: string
  side: 'long' | 'short'
  qty: string
  avg_price: string
  current_price?: string
  value: string
  unrealized_pnl?: string
  unrealized_pnl_pct?: string
  stop_loss?: string
  take_profit?: string
  concentration_pct?: string
  strategy?: string
}

// Nested portfolio sub-object (used in multiple responses).
export interface PortfolioNested {
  value: string
  cash: string
  peak: string
  initial_value?: string
}

export interface Portfolio {
  portfolio: PortfolioNested
  positions: Position[]
  fill_count: number
  reject_count: number
  halted: boolean
  total_fees?: string
}

export interface Trade {
  timestamp: string
  pair: string
  side: 'buy' | 'sell'
  status: 'fill' | 'reject'
  decision_hash?: string
  // Present when status === 'fill'
  fill?: {
    id: string
    price: string
    qty: string
    value: string
    fee?: string
    fee_asset?: string
    fee_value?: string
  }
  pnl_value?: string
  portfolio?: { value_after: string }
  // Present when status === 'reject'
  reject?: {
    source: string
    reason: string
  }
  // Metadata from params (when available)
  metadata?: {
    strategy?: string
    trigger_reason?: string
    confidence?: number
    reasoning?: string
  }
}

export interface TradeHistory {
  trades: Trade[]
  count: number
}

export interface Limits {
  portfolio: PortfolioNested
  fill_count: number
  reject_count: number
  largest_position_pct: number
  largest_position_pair: string
  halted: boolean
  total_fees?: string
}

export interface EquityPoint {
  timestamp: string
  portfolio: { value: string }
  pair: string
  side: 'buy' | 'sell'
}

export interface EquityCurve {
  equity_curve: EquityPoint[]
  count: number
}

export interface Estimate {
  pair: string
  side: 'buy' | 'sell'
  value: string
  price: string
  qty: string
  portfolio: PortfolioNested
  fill_count: number
  reject_count: number
  halted: boolean
  position_pct_after?: number
  warning?: string
}

export interface Heartbeat {
  portfolio: PortfolioNested
  fill_count: number
  reject_count: number
  halted: boolean
  timestamp: string
}

export interface SubmitOrderFill {
  status: 'fill'
  fill: {
    id: string
    pair: string
    side: 'buy' | 'sell'
    price: string
    qty: string
    value: string
    tx_hash?: string
  }
  decision_hash?: string
  prev_hash?: string
  chain_success?: boolean
  chain_stubbed?: boolean
  persist_error?: string
}

export interface SubmitOrderReject {
  status: 'reject'
  reject: {
    source: string
    reason: string
    verdict?: string
    exec_id?: string
  }
  persist_error?: string
}

export type SubmitOrderResult = SubmitOrderFill | SubmitOrderReject

// --- Risk MCP ---

export interface AgentSummary {
  agent_id: string
  name: string
  portfolio?: { value: string; initial_value?: string }
  fill_count?: number
  halted?: boolean
  last_seen_at?: string
}

export interface AgentList {
  agents: AgentSummary[]
  count: number
}

export interface AgentStatus {
  agent_id: string
  name: string
  halted?: boolean
  portfolio?: {
    value: string
    cash: string
    peak: string
    positions: Position[]
  }
  fill_count?: number
  reject_count?: number
}

export interface HaltResumeResult {
  status: 'halted' | 'resumed'
  agent_id: string
}

// --- Market Data MCP ---

export interface PriceTick {
  market: string
  bid: string
  ask: string
  last: string
  volume_24h: string
  change_24h_pct: string
  high_24h: string
  low_24h: string
  timestamp: string
}

export interface PricesResponse {
  prices: PriceTick[]
  source: string
}

export interface Candle {
  t: string
  o: string
  h: string
  l: string
  c: string
  v: string
  [key: string]: unknown // indicator columns
}

export interface CandlesResponse {
  market: string
  interval: string
  count: number
  source: string
  indicators_computed?: string[]
  candles: Candle[]
}

export interface MarketInfo {
  pair: string
  base: string
  quote: string
  last_price: string
  volume_24h: string
  change_24h_pct: string
  tradeable: boolean
}

export interface MarketsResponse {
  markets: MarketInfo[]
  count: number
  source: string
}

export interface OrderbookResponse {
  market: string
  bids: [string, string][]
  asks: [string, string][]
  bid_total: string
  ask_total: string
  spread: string
  spread_pct: string
  imbalance: string
  source: string
  timestamp: string
}

export interface FundingResponse {
  market: string
  current_rate?: string
  annualized_pct?: string
  next_funding_time?: string
  signal?: 'neutral' | 'bullish_crowd' | 'bearish_crowd' | 'extreme_bullish' | 'extreme_bearish'
  history?: { timestamp: string; rate: string }[]
  source: string
  error?: string
}

export interface OpenInterestResponse {
  market: string
  open_interest?: string
  oi_change_1h_pct?: string
  oi_change_4h_pct?: string
  oi_change_24h_pct?: string
  long_short_ratio?: string
  source: string
  error?: string
}

// --- News MCP ---

export interface NewsArticle {
  title: string
  source: string
  url: string
  published_at: string
  summary?: string
  sentiment?: 'positive' | 'negative' | 'neutral'
  markets?: string[]
  kind?: string
}

export interface NewsResponse {
  articles: NewsArticle[]
  count: number
  source: string
}

export interface SentimentResponse {
  query: string
  sentiment: 'positive' | 'negative' | 'neutral'
  score: number
  article_count: number
  key_themes?: string[]
  period: '1h' | '4h' | '24h' | '7d'
  source: string
}

export interface NewsEvent {
  title: string
  type: 'fork' | 'upgrade' | 'regulation' | 'hack' | 'listing' | 'unlock' | 'macro'
  date: string
  impact_level: 'high' | 'medium' | 'low'
  details?: string
  market?: string
}

export interface EventsResponse {
  events: NewsEvent[]
  count: number
  source: string
}

// --- Evidence API ---

// Envelope returned by GET /v1/evidence/{hash}
export interface EvidenceTrace {
  hash: string
  prev_hash: string
  agent_id: string
  created_at: string
  data: {
    intent?: { pair?: string; side?: string; value?: string; params?: Record<string, unknown> }
    swiftward?: { event_id?: string; verdict?: string; response?: unknown }
    risk_router?: { tx_hash?: string; intent_hash?: string }
    fill?: { id?: string; price?: string; qty?: string; fee?: string; fee_asset?: string }
    [key: string]: unknown
  }
}
