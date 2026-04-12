import { mcpCall } from './mcp-client'
import type { NewsResponse, SentimentResponse, EventsResponse } from '@/types/api'

const ENDPOINT = '/mcp/news'

export function getLatestNews(limit?: number, market?: string): Promise<NewsResponse> {
  const params: Record<string, unknown> = {}
  if (limit !== undefined) params.limit = limit
  if (market) params.market = market
  return mcpCall<NewsResponse>(ENDPOINT, 'news/get_latest', params)
}

export function searchNews(query: string, limit?: number): Promise<NewsResponse> {
  const params: Record<string, unknown> = { query }
  if (limit !== undefined) params.limit = limit
  return mcpCall<NewsResponse>(ENDPOINT, 'news/search', params)
}

export function getSentiment(query: string, period?: string): Promise<SentimentResponse> {
  const params: Record<string, unknown> = { query }
  if (period) params.period = period
  return mcpCall<SentimentResponse>(ENDPOINT, 'news/get_sentiment', params)
}

export function getEvents(limit?: number): Promise<EventsResponse> {
  const params: Record<string, unknown> = {}
  if (limit !== undefined) params.limit = limit
  return mcpCall<EventsResponse>(ENDPOINT, 'news/get_events', params)
}
