import { useMemo } from 'react'
import { useQueries } from '@tanstack/react-query'
import * as tradingApi from '@/api/trading'
import type { Trade } from '@/types/api'

export interface TradeEvent {
  trade: Trade
  agentId: string
}

// Fetch trade history for all agents, merge and sort by timestamp (oldest first for chain order)
export function useAllTrades(agentIds: string[], limit = 200) {
  const results = useQueries({
    queries: agentIds.map((id) => ({
      queryKey: ['tradeHistory', id, limit],
      queryFn: () => tradingApi.getTradeHistory(id, limit),
      refetchInterval: 10_000,
      enabled: !!id,
    })),
  })

  const trades = useMemo(() => {
    const all: TradeEvent[] = []
    for (let i = 0; i < agentIds.length; i++) {
      const data = results[i]?.data
      if (data?.trades) {
        for (const trade of data.trades) {
          all.push({ trade, agentId: agentIds[i] })
        }
      }
    }
    // Sort oldest first (chain order)
    all.sort(
      (a, b) =>
        new Date(a.trade.timestamp).getTime() -
        new Date(b.trade.timestamp).getTime(),
    )
    return all
  }, [agentIds, results])

  return { trades, isLoading: results.some((r) => r.isLoading) }
}
