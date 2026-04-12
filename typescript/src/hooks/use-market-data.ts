import { useMemo } from 'react'
import { useQueries } from '@tanstack/react-query'
import * as marketApi from '@/api/market'
import type { FundingResponse, OpenInterestResponse } from '@/types/api'

// Fetch funding for all markets in parallel
export function useAllFunding(markets: string[]) {
  const results = useQueries({
    queries: markets.map((m) => ({
      queryKey: ['funding', m],
      queryFn: () => marketApi.getFunding(m),
      refetchInterval: 60_000,
      enabled: !!m,
    })),
  })

  const fundingMap = useMemo(() => {
    const map: Record<string, FundingResponse> = {}
    for (let i = 0; i < markets.length; i++) {
      const data = results[i]?.data
      if (data) map[markets[i]] = data
    }
    return map
  }, [markets, results])

  return { fundingMap, isLoading: results.some((r) => r.isLoading) }
}

// Fetch open interest for all markets in parallel
export function useAllOpenInterest(markets: string[]) {
  const results = useQueries({
    queries: markets.map((m) => ({
      queryKey: ['openInterest', m],
      queryFn: () => marketApi.getOpenInterest(m),
      refetchInterval: 30_000,
      enabled: !!m,
    })),
  })

  const oiMap = useMemo(() => {
    const map: Record<string, OpenInterestResponse> = {}
    for (let i = 0; i < markets.length; i++) {
      const data = results[i]?.data
      if (data) map[markets[i]] = data
    }
    return map
  }, [markets, results])

  return { oiMap, isLoading: results.some((r) => r.isLoading) }
}
