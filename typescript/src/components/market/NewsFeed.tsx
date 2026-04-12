import { useState } from 'react'
import { clsx } from 'clsx'
import { ExternalLink, Newspaper } from 'lucide-react'
import type { NewsArticle } from '@/types/api'

interface NewsFeedProps {
  articles: NewsArticle[]
}

function sentimentLabel(sentiment: NewsArticle['sentiment']): string {
  switch (sentiment) {
    case 'positive':
      return 'Bullish'
    case 'negative':
      return 'Bearish'
    case 'neutral':
      return 'Neutral'
    default:
      return 'Neutral'
  }
}

function sentimentBadgeClass(sentiment: NewsArticle['sentiment']): string {
  switch (sentiment) {
    case 'positive':
      return 'bg-profit/10 text-profit'
    case 'negative':
      return 'bg-loss/10 text-loss'
    case 'neutral':
    default:
      return 'bg-surface-hover text-text-muted'
  }
}

function formatNewsTime(timestamp: string): string {
  const d = new Date(timestamp)
  if (isNaN(d.getTime())) return ''
  return d.toLocaleTimeString('en-US', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: 'UTC',
  })
}

export function NewsFeed({ articles }: NewsFeedProps) {
  const [filter, setFilter] = useState('')

  const filtered = filter
    ? articles.filter((a) => {
        const lf = filter.toLowerCase()
        return (
          a.title.toLowerCase().includes(lf) ||
          a.markets?.some((m) => m.toLowerCase().includes(lf)) ||
          a.source.toLowerCase().includes(lf)
        )
      })
    : articles

  return (
    <div className="rounded-lg border border-surface-border bg-surface-card overflow-hidden">
      <div className="px-6 py-4 border-b border-surface-border flex items-center justify-between">
        <h2 className="text-sm font-medium text-text-secondary">News Feed</h2>
        <div className="flex items-center gap-3">
          <input
            type="text"
            placeholder="Filter by keyword..."
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="px-3 py-1.5 text-xs rounded border border-surface-border bg-surface-base text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent w-48"
          />
          <span className="text-xs text-text-muted">Auto-refresh: 30s</span>
        </div>
      </div>

      <div className="divide-y divide-surface-border max-h-[500px] overflow-y-auto">
        {filtered.length === 0 && (
          <div className="px-6 py-8 text-center text-text-muted text-sm">
            {articles.length === 0
              ? 'No news available. Waiting for News MCP connection.'
              : 'No articles match the filter.'}
          </div>
        )}
        {filtered.map((article, idx) => (
          <div
            key={`${article.published_at}-${idx}`}
            className="px-6 py-3 hover:bg-surface-hover transition-colors flex items-start gap-3"
          >
            <div className="text-text-muted text-xs font-mono w-12 shrink-0 pt-0.5">
              {formatNewsTime(article.published_at)}
            </div>

            <span
              className={clsx(
                'inline-flex items-center px-2 py-0.5 rounded text-xs font-medium shrink-0',
                sentimentBadgeClass(article.sentiment),
              )}
            >
              {sentimentLabel(article.sentiment)}
            </span>

            <div className="flex-1 min-w-0">
              <div className="flex items-start gap-2">
                <span className="text-sm text-text-primary leading-snug">
                  {article.title}
                </span>
                {article.url && (
                  <a
                    href={article.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-text-muted hover:text-accent shrink-0 mt-0.5"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <ExternalLink size={12} />
                  </a>
                )}
              </div>
              <div className="flex items-center gap-2 mt-1">
                <span className="text-xs text-text-muted">{article.source}</span>
                {article.markets && article.markets.length > 0 && (
                  <>
                    <span className="text-text-muted">-</span>
                    {article.markets.map((m) => (
                      <span
                        key={m}
                        className="text-xs px-1.5 py-0.5 rounded bg-surface-hover text-text-secondary"
                      >
                        {m}
                      </span>
                    ))}
                  </>
                )}
              </div>
            </div>
          </div>
        ))}
      </div>

      {articles.length > 0 && (
        <div className="px-6 py-3 border-t border-surface-border flex items-center gap-2 text-text-muted text-xs">
          <Newspaper size={12} />
          <span>
            Showing {filtered.length} of {articles.length} articles
          </span>
        </div>
      )}
    </div>
  )
}
