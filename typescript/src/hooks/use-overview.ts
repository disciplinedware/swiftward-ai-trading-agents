import { useMemo } from 'react'
import { useQueries } from '@tanstack/react-query'
import * as tradingApi from '@/api/trading'
import type { Limits, EquityCurve, TradeHistory, Trade } from '@/types/api'

// Fetch limits for all agents in parallel
export function useAllAgentLimits(agentIds: string[]) {
  const results = useQueries({
    queries: agentIds.map((id) => ({
      queryKey: ['limits', id],
      queryFn: () => tradingApi.getLimits(id),
      refetchInterval: 5_000,
      enabled: !!id,
    })),
  })

  const limitsMap = useMemo(() => {
    const map: Record<string, Limits> = {}
    for (let i = 0; i < agentIds.length; i++) {
      const data = results[i]?.data
      if (data) map[agentIds[i]] = data
    }
    return map
  }, [agentIds, results])

  const allLimits = useMemo(
    () => agentIds.map((_, i) => results[i]?.data),
    [agentIds, results],
  )

  return { limitsMap, allLimits, isLoading: results.some((r) => r.isLoading) }
}

// Fetch equity curves for all agents
export function useAllEquityCurves(agentIds: string[]) {
  const results = useQueries({
    queries: agentIds.map((id) => ({
      queryKey: ['portfolioHistory', id],
      queryFn: () => tradingApi.getPortfolioHistory(id),
      refetchInterval: 10_000,
      enabled: !!id,
    })),
  })

  const curves = useMemo(() => {
    const map: Record<string, EquityCurve> = {}
    for (let i = 0; i < agentIds.length; i++) {
      const data = results[i]?.data
      if (data) map[agentIds[i]] = data
    }
    return map
  }, [agentIds, results])

  return { curves, isLoading: results.some((r) => r.isLoading) }
}

// Fetch recent trades for all agents, merge and sort
export function useRecentTrades(agentIds: string[], limit = 10) {
  const results = useQueries({
    queries: agentIds.map((id) => ({
      queryKey: ['tradeHistory', id, limit],
      queryFn: () => tradingApi.getTradeHistory(id, limit),
      refetchInterval: 3_000,
      enabled: !!id,
    })),
  })

  const events = useMemo(() => {
    const all: { trade: Trade; agentId: string }[] = []
    for (let i = 0; i < agentIds.length; i++) {
      const data: TradeHistory | undefined = results[i]?.data
      if (data?.trades) {
        for (const trade of data.trades) {
          all.push({ trade, agentId: agentIds[i] })
        }
      }
    }
    // Sort newest first
    all.sort(
      (a, b) =>
        new Date(b.trade.timestamp).getTime() -
        new Date(a.trade.timestamp).getTime(),
    )
    return all.slice(0, limit)
  }, [agentIds, results, limit])

  return { events, isLoading: results.some((r) => r.isLoading) }
}
