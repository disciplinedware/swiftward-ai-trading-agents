import { useQuery } from '@tanstack/react-query'
import * as newsApi from '@/api/news'

export function useLatestNews(limit = 20, market?: string) {
  return useQuery({
    queryKey: ['latestNews', limit, market],
    queryFn: () => newsApi.getLatestNews(limit, market),
    refetchInterval: 30_000,
  })
}

export function useSearchNews(query: string, limit?: number) {
  return useQuery({
    queryKey: ['searchNews', query, limit],
    queryFn: () => newsApi.searchNews(query, limit),
    enabled: !!query,
  })
}

export function useSentiment(query: string, period?: string) {
  return useQuery({
    queryKey: ['sentiment', query, period],
    queryFn: () => newsApi.getSentiment(query, period),
    refetchInterval: 60_000,
    enabled: !!query,
  })
}

export function useEvents(limit?: number) {
  return useQuery({
    queryKey: ['events', limit],
    queryFn: () => newsApi.getEvents(limit),
    refetchInterval: 60_000,
  })
}
