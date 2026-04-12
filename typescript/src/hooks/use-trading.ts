import { useQuery } from '@tanstack/react-query'
import * as tradingApi from '@/api/trading'

export function usePortfolio(agentId: string) {
  return useQuery({
    queryKey: ['portfolio', agentId],
    queryFn: () => tradingApi.getPortfolio(agentId),
    refetchInterval: 5_000,
    enabled: !!agentId,
  })
}

export function useTradeHistory(agentId: string, limit = 50) {
  return useQuery({
    queryKey: ['tradeHistory', agentId, limit],
    queryFn: () => tradingApi.getTradeHistory(agentId, limit),
    refetchInterval: 3_000,
    enabled: !!agentId,
  })
}

export function usePortfolioHistory(agentId: string) {
  return useQuery({
    queryKey: ['portfolioHistory', agentId],
    queryFn: () => tradingApi.getPortfolioHistory(agentId),
    refetchInterval: 10_000,
    enabled: !!agentId,
  })
}

export function useLimits(agentId: string) {
  return useQuery({
    queryKey: ['limits', agentId],
    queryFn: () => tradingApi.getLimits(agentId),
    refetchInterval: 5_000,
    enabled: !!agentId,
  })
}

export function useHeartbeat(agentId: string) {
  return useQuery({
    queryKey: ['heartbeat', agentId],
    queryFn: () => tradingApi.heartbeat(agentId),
    refetchInterval: 5_000,
    enabled: !!agentId,
  })
}
