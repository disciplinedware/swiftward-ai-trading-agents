import { mcpCall } from './mcp-client'
import type {
  PricesResponse,
  CandlesResponse,
  OrderbookResponse,
  MarketsResponse,
  FundingResponse,
  OpenInterestResponse,
} from '@/types/api'

const ENDPOINT = '/mcp/market'

export function getPrices(markets: string[]): Promise<PricesResponse> {
  return mcpCall<PricesResponse>(ENDPOINT, 'market/get_prices', { markets })
}

export function getCandles(
  market: string,
  interval: string,
  limit?: number,
  indicators?: string[],
): Promise<CandlesResponse> {
  const params: Record<string, unknown> = { market, interval }
  if (limit !== undefined) params.limit = limit
  if (indicators?.length) params.indicators = indicators
  return mcpCall<CandlesResponse>(ENDPOINT, 'market/get_candles', params)
}

export function getOrderbook(market: string, depth?: number): Promise<OrderbookResponse> {
  const params: Record<string, unknown> = { market }
  if (depth !== undefined) params.depth = depth
  return mcpCall<OrderbookResponse>(ENDPOINT, 'market/get_orderbook', params)
}

export function listMarkets(): Promise<MarketsResponse> {
  return mcpCall<MarketsResponse>(ENDPOINT, 'market/list_markets')
}

export function getFunding(market: string): Promise<FundingResponse> {
  return mcpCall<FundingResponse>(ENDPOINT, 'market/get_funding', { market })
}

export function getOpenInterest(market: string): Promise<OpenInterestResponse> {
  return mcpCall<OpenInterestResponse>(ENDPOINT, 'market/get_open_interest', { market })
}
