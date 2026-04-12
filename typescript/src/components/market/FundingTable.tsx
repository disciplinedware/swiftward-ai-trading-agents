import { clsx } from 'clsx'
import type { FundingResponse, OpenInterestResponse } from '@/types/api'

interface FundingTableProps {
  markets: string[]
  fundingMap: Record<string, FundingResponse>
  oiMap: Record<string, OpenInterestResponse>
}

function formatRate(value: string | undefined): string {
  if (!value) return '-'
  const num = parseFloat(value)
  if (isNaN(num)) return '-'
  return (num * 100).toFixed(4) + '%'
}

function formatAnnualized(value: string | undefined): string {
  if (!value) return '-'
  const num = parseFloat(value)
  if (isNaN(num)) return '-'
  return num.toFixed(2) + '%'
}

function formatOI(value: string | undefined): string {
  if (!value) return '-'
  const num = parseFloat(value)
  if (isNaN(num)) return '-'
  if (num >= 1_000_000_000) return '$' + (num / 1_000_000_000).toFixed(2) + 'B'
  if (num >= 1_000_000) return '$' + (num / 1_000_000).toFixed(1) + 'M'
  if (num >= 1_000) return '$' + (num / 1_000).toFixed(1) + 'K'
  return '$' + num.toFixed(0)
}

function formatChangePct(value: string | undefined): string {
  if (!value) return '-'
  const num = parseFloat(value)
  if (isNaN(num)) return '-'
  const sign = num >= 0 ? '+' : ''
  return sign + num.toFixed(2) + '%'
}

function signalLabel(signal: FundingResponse['signal']): string {
  switch (signal) {
    case 'bullish_crowd':
      return 'Bullish'
    case 'bearish_crowd':
      return 'Bearish'
    case 'extreme_bullish':
      return 'Ext. Bullish'
    case 'extreme_bearish':
      return 'Ext. Bearish'
    case 'neutral':
      return 'Neutral'
    default:
      return '-'
  }
}

function signalColor(signal: FundingResponse['signal']): string {
  switch (signal) {
    case 'bullish_crowd':
    case 'extreme_bullish':
      return 'text-profit'
    case 'bearish_crowd':
    case 'extreme_bearish':
      return 'text-loss'
    case 'neutral':
      return 'text-text-muted'
    default:
      return 'text-text-muted'
  }
}

export function FundingTable({ markets, fundingMap, oiMap }: FundingTableProps) {
  const hasData = markets.some(
    (m) => fundingMap[m]?.current_rate || oiMap[m]?.open_interest,
  )

  const headers = [
    'Market',
    'Funding Rate',
    'Annualized',
    'Signal',
    'Open Interest',
    'OI Change 24h',
    'L/S Ratio',
    'Source',
  ]

  return (
    <div className="rounded-lg border border-surface-border bg-surface-card overflow-hidden">
      <div className="px-6 py-4 border-b border-surface-border flex items-center justify-between">
        <h2 className="text-sm font-medium text-text-secondary">
          Funding & Open Interest
        </h2>
        <span className="text-xs text-text-muted">
          Auto-refresh: 30-60s
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
            {!hasData && (
              <tr>
                <td
                  colSpan={8}
                  className="px-4 py-8 text-center text-text-muted"
                >
                  Funding and open interest data not available. Requires futures data source (Bybit).
                </td>
              </tr>
            )}
            {hasData &&
              markets.map((market) => {
                const funding = fundingMap[market]
                const oi = oiMap[market]
                const hasRow = funding?.current_rate || oi?.open_interest

                if (!hasRow) return null

                const oiChangePct = oi?.oi_change_24h_pct
                const oiChangeNum = oiChangePct ? parseFloat(oiChangePct) : NaN

                return (
                  <tr
                    key={market}
                    className="border-b border-surface-border last:border-0 hover:bg-surface-hover transition-colors"
                  >
                    <td className="px-4 py-3 text-text-primary font-medium">
                      {market}
                    </td>
                    <td className="px-4 py-3 text-text-primary font-mono">
                      {formatRate(funding?.current_rate)}
                    </td>
                    <td className="px-4 py-3 text-text-secondary font-mono">
                      {formatAnnualized(funding?.annualized_pct)}
                    </td>
                    <td className="px-4 py-3">
                      <span
                        className={clsx(
                          'text-xs font-medium',
                          signalColor(funding?.signal),
                        )}
                      >
                        {signalLabel(funding?.signal)}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-text-primary font-mono">
                      {formatOI(oi?.open_interest)}
                    </td>
                    <td className="px-4 py-3">
                      <span
                        className={clsx(
                          'font-mono',
                          !isNaN(oiChangeNum) && oiChangeNum >= 0
                            ? 'text-profit'
                            : 'text-loss',
                        )}
                      >
                        {formatChangePct(oi?.oi_change_24h_pct)}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-text-secondary font-mono">
                      {oi?.long_short_ratio
                        ? parseFloat(oi.long_short_ratio).toFixed(2)
                        : '-'}
                    </td>
                    <td className="px-4 py-3 text-text-muted text-xs">
                      {funding?.source || oi?.source || '-'}
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
