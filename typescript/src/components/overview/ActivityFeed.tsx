import { clsx } from 'clsx'
import { formatTimeShort, formatCurrency, formatQty } from '@/lib/format'
import type { Trade } from '@/types/api'

interface ActivityEvent {
  trade: Trade
  agentId: string
}

interface ActivityFeedProps {
  events: ActivityEvent[]
}

function statusLabel(status: string): string {
  switch (status) {
    case 'fill':
      return 'FILL'
    case 'reject':
      return 'REJECT'
    default:
      return status.toUpperCase()
  }
}

function statusColor(status: string): string {
  switch (status) {
    case 'fill':
      return 'text-profit'
    case 'reject':
      return 'text-loss'
    default:
      return 'text-text-muted'
  }
}

export function ActivityFeed({ events }: ActivityFeedProps) {
  return (
    <div className="rounded-lg border border-surface-border bg-surface-card p-6 h-full">
      <h2 className="text-sm font-medium text-text-secondary mb-4">
        Recent Activity
      </h2>
      {events.length === 0 ? (
        <p className="text-text-muted text-sm">
          No trading activity yet.
        </p>
      ) : (
        <div className="space-y-2">
          {events.map((ev, idx) => {
            const { trade, agentId } = ev
            const isFill = trade.status === 'fill'

            return (
              <div
                key={`${trade.timestamp}-${idx}`}
                className="flex items-start gap-2 text-xs py-1.5 border-b border-surface-border last:border-0"
              >
                <span className="text-text-muted shrink-0 font-mono w-11">
                  {formatTimeShort(trade.timestamp)}
                </span>
                <span className="text-accent shrink-0 font-mono w-16 truncate">
                  {agentId}
                </span>
                <span
                  className={clsx(
                    'shrink-0 font-medium w-18',
                    statusColor(trade.status),
                  )}
                >
                  {statusLabel(trade.status)}
                </span>
                <span className="text-text-secondary truncate flex-1 min-w-0">
                  {trade.side} {trade.pair}{' '}
                  {isFill && trade.fill?.qty && (
                    <>
                      {formatQty(trade.fill.qty)}
                      {trade.fill?.price && <> @ {formatCurrency(trade.fill.price)}</>}
                    </>
                  )}
                </span>
                {isFill && trade.pnl_value && (
                  <span
                    className={clsx(
                      'shrink-0 font-mono',
                      parseFloat(trade.pnl_value) >= 0 ? 'text-profit' : 'text-loss',
                    )}
                  >
                    {parseFloat(trade.pnl_value) >= 0 ? '+' : ''}
                    {formatCurrency(trade.pnl_value)}
                  </span>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
