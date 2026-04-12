import { clsx } from 'clsx'
import { TrendingUp, TrendingDown } from 'lucide-react'
import { formatCurrency, parseDecimal } from '@/lib/format'
import type { PriceTick } from '@/types/api'

interface MarketPricesTableProps {
  prices: PriceTick[]
  source: string
}

function formatVolume(value: string | undefined): string {
  if (!value) return '-'
  const num = parseFloat(value)
  if (isNaN(num)) return '-'
  if (num >= 1_000_000_000) return '$' + (num / 1_000_000_000).toFixed(2) + 'B'
  if (num >= 1_000_000) return '$' + (num / 1_000_000).toFixed(1) + 'M'
  if (num >= 1_000) return '$' + (num / 1_000).toFixed(1) + 'K'
  return '$' + num.toFixed(0)
}

function computeSpread(bid: string, ask: string, last: string): string {
  const b = parseFloat(bid)
  const a = parseFloat(ask)
  const l = parseFloat(last)
  if (isNaN(b) || isNaN(a) || isNaN(l) || l === 0) return '-'
  const spreadPct = ((a - b) / l) * 100
  return spreadPct.toFixed(3) + '%'
}

export function MarketPricesTable({ prices, source }: MarketPricesTableProps) {
  const headers = [
    'Market',
    'Last Price',
    '24h Change',
    '24h High',
    '24h Low',
    'Volume 24h',
    'Spread',
    'Source',
  ]

  return (
    <div className="rounded-lg border border-surface-border bg-surface-card overflow-hidden">
      <div className="px-6 py-4 border-b border-surface-border flex items-center justify-between">
        <h2 className="text-sm font-medium text-text-secondary">
          Market Prices
        </h2>
        <span className="text-xs text-text-muted">
          Auto-refresh: 10s
        </span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-surface-border">
              {headers.map((h) => (
                <th
                  key={h}
                  className="px-4 py-3 text-left text-xs font-medium text-text-muted uppercase tracking-wide"
                >
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {prices.length === 0 && (
              <tr>
                <td
                  colSpan={8}
                  className="px-4 py-8 text-center text-text-muted"
                >
                  No market data available. Waiting for Market Data MCP connection.
                </td>
              </tr>
            )}
            {prices.map((tick) => {
              const changePct = parseDecimal(tick.change_24h_pct)
              const isPositive = changePct >= 0

              return (
                <tr
                  key={tick.market}
                  className="border-b border-surface-border last:border-0 hover:bg-surface-hover transition-colors"
                >
                  <td className="px-4 py-3 text-text-primary font-medium">
                    {tick.market}
                  </td>
                  <td className="px-4 py-3 text-text-primary font-mono">
                    {formatCurrency(tick.last)}
                  </td>
                  <td className="px-4 py-3">
                    <span
                      className={clsx(
                        'inline-flex items-center gap-1 font-mono',
                        isPositive ? 'text-profit' : 'text-loss',
                      )}
                    >
                      {isPositive ? (
                        <TrendingUp size={14} />
                      ) : (
                        <TrendingDown size={14} />
                      )}
                      {isPositive ? '+' : ''}
                      {changePct.toFixed(2)}%
                    </span>
                  </td>
                  <td className="px-4 py-3 text-text-secondary font-mono">
                    {formatCurrency(tick.high_24h)}
                  </td>
                  <td className="px-4 py-3 text-text-secondary font-mono">
                    {formatCurrency(tick.low_24h)}
                  </td>
                  <td className="px-4 py-3 text-text-secondary font-mono">
                    {formatVolume(tick.volume_24h)}
                  </td>
                  <td className="px-4 py-3 text-text-muted font-mono text-xs">
                    {computeSpread(tick.bid, tick.ask, tick.last)}
                  </td>
                  <td className="px-4 py-3 text-text-muted text-xs">
                    {source}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}
