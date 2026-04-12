import { mcpCall } from './mcp-client'
import type { Portfolio, TradeHistory, EquityCurve, Limits, Heartbeat, Estimate } from '@/types/api'

const ENDPOINT = '/mcp/trading'

export function getPortfolio(agentId: string): Promise<Portfolio> {
  return mcpCall<Portfolio>(ENDPOINT, 'trade/get_portfolio', {}, agentId)
}

export function getTradeHistory(agentId: string, limit = 50): Promise<TradeHistory> {
  return mcpCall<TradeHistory>(ENDPOINT, 'trade/get_history', { limit }, agentId)
}

export function getPortfolioHistory(agentId: string): Promise<EquityCurve> {
  return mcpCall<EquityCurve>(ENDPOINT, 'trade/get_portfolio_history', {}, agentId)
}

export function getLimits(agentId: string): Promise<Limits> {
  return mcpCall<Limits>(ENDPOINT, 'trade/get_limits', {}, agentId)
}

export function heartbeat(agentId: string): Promise<Heartbeat> {
  return mcpCall<Heartbeat>(ENDPOINT, 'trade/heartbeat', {}, agentId)
}

export function estimateOrder(
  agentId: string,
  pair: string,
  side: 'buy' | 'sell',
  value: number,
): Promise<Estimate> {
  return mcpCall<Estimate>(ENDPOINT, 'trade/estimate_order', { pair, side, value }, agentId)
}
