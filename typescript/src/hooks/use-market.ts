import { useQuery } from '@tanstack/react-query'
import * as marketApi from '@/api/market'

export function usePrices(markets: string[]) {
  return useQuery({
    queryKey: ['prices', markets],
    queryFn: () => marketApi.getPrices(markets),
    refetchInterval: 10_000,
    enabled: markets.length > 0,
  })
}

export function useCandles(market: string, interval: string, limit?: number) {
  return useQuery({
    queryKey: ['candles', market, interval, limit],
    queryFn: () => marketApi.getCandles(market, interval, limit),
    refetchInterval: 30_000,
    enabled: !!market,
  })
}

export function useOrderbook(market: string) {
  return useQuery({
    queryKey: ['orderbook', market],
    queryFn: () => marketApi.getOrderbook(market),
    refetchInterval: 10_000,
    enabled: !!market,
  })
}

export function useMarkets() {
  return useQuery({
    queryKey: ['markets'],
    queryFn: marketApi.listMarkets,
    refetchInterval: 60_000,
  })
}

export function useFunding(market: string) {
  return useQuery({
    queryKey: ['funding', market],
    queryFn: () => marketApi.getFunding(market),
    refetchInterval: 60_000,
    enabled: !!market,
  })
}

export function useOpenInterest(market: string) {
  return useQuery({
    queryKey: ['openInterest', market],
    queryFn: () => marketApi.getOpenInterest(market),
    refetchInterval: 30_000,
    enabled: !!market,
  })
}
