import { useMemo } from 'react'
import { useQueries } from '@tanstack/react-query'
import * as tradingApi from '@/api/trading'
import type { Trade } from '@/types/api'

export interface RejectionEvent {
  trade: Trade
  agentId: string
}

// Fetch trade history for all agents, filter to rejections, merge and sort
export function useAllRejections(agentIds: string[], limit = 200) {
  const results = useQueries({
    queries: agentIds.map((id) => ({
      queryKey: ['tradeHistory', id, limit],
      queryFn: () => tradingApi.getTradeHistory(id, limit),
      refetchInterval: 5_000,
      enabled: !!id,
    })),
  })

  const rejections = useMemo(() => {
    const all: RejectionEvent[] = []
    for (let i = 0; i < agentIds.length; i++) {
      const data = results[i]?.data
      if (data?.trades) {
        for (const trade of data.trades) {
          if (trade.status === 'reject') {
            all.push({ trade, agentId: agentIds[i] })
          }
        }
      }
    }
    all.sort(
      (a, b) =>
        new Date(b.trade.timestamp).getTime() -
        new Date(a.trade.timestamp).getTime(),
    )
    return all
  }, [agentIds, results])

  return { rejections, isLoading: results.some((r) => r.isLoading) }
}
