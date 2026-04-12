import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { clsx } from 'clsx'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { vscDarkPlus } from 'react-syntax-highlighter/dist/esm/styles/prism'
import {
  Link2,
  ChevronDown,
  ChevronRight,
  CheckCircle2,
  XCircle,
  Copy,
  ExternalLink,
  Hash,
} from 'lucide-react'
import { formatTime, formatQty } from '@/lib/format'
import { getEvidence } from '@/api/evidence'
import type { TradeEvent } from '@/hooks/use-all-trades'

function truncateHash(hash: string): string {
  if (hash.length <= 12) return hash
  return hash.slice(0, 8) + '...' + hash.slice(-4)
}

function copyToClipboard(text: string) {
  navigator.clipboard.writeText(text)
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

function statusBg(status: string): string {
  switch (status) {
    case 'fill':
      return 'bg-profit/15 border-profit/30'
    case 'reject':
      return 'bg-loss/15 border-loss/30'
    default:
      return 'bg-surface-hover border-surface-border'
  }
}

function StatusIcon({ status }: { status: string }) {
  switch (status) {
    case 'fill':
      return <CheckCircle2 size={14} className="text-profit" />
    case 'reject':
      return <XCircle size={14} className="text-loss" />
    default:
      return <Hash size={14} className="text-text-muted" />
  }
}

// --- Vertical Hash Chain ---

function ChainNode({
  event,
  index,
  isLast,
  isExpanded,
  onToggle,
}: {
  event: TradeEvent
  index: number
  isLast: boolean
  isExpanded: boolean
  onToggle: () => void
}) {
  const { trade, agentId } = event
  const { data: trace, isLoading: traceLoading, isError: traceError } = useQuery({
    queryKey: ['evidence-trace', trade.decision_hash],
    queryFn: () => getEvidence(trade.decision_hash!),
    enabled: isExpanded && !!trade.decision_hash,
    staleTime: Infinity,
  })

  return (
    <div className="flex gap-4">
      {/* Vertical line + node dot */}
      <div className="flex flex-col items-center">
        <div
          className={clsx(
            'w-8 h-8 rounded-full border-2 flex items-center justify-center shrink-0',
            statusBg(trade.status),
          )}
        >
          <StatusIcon status={trade.status} />
        </div>
        {!isLast && (
          <div className="w-0.5 flex-1 min-h-[24px] bg-surface-border" />
        )}
      </div>

      {/* Node content */}
      <div className="flex-1 pb-6">
        <button
          className={clsx(
            'w-full text-left rounded-lg border p-3 transition-colors',
            isExpanded
              ? 'bg-surface-card border-accent/30'
              : 'bg-surface-card border-surface-border hover:border-surface-border/80',
          )}
          onClick={onToggle}
        >
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <span className="text-xs font-mono text-text-muted">#{index + 1}</span>
              <span className={clsx('text-xs font-medium uppercase', statusColor(trade.status))}>
                {trade.status}
              </span>
              <span className="text-xs text-text-secondary">
                {trade.side.toUpperCase()} {trade.pair}
              </span>
              {trade.fill?.qty && (
                <span className="text-xs text-text-muted">{formatQty(trade.fill.qty)}</span>
              )}
            </div>
            <div className="flex items-center gap-2">
              <span className="text-xs text-text-muted">{formatTime(trade.timestamp)}</span>
              {isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
            </div>
          </div>

          {trade.decision_hash && (
            <div className="flex items-center gap-1.5 mt-1.5">
              <Link2 size={10} className="text-text-muted" />
              <span className="text-[10px] font-mono text-text-muted">
                {truncateHash(trade.decision_hash)}
              </span>
            </div>
          )}
        </button>

        {/* Expanded detail */}
        {isExpanded && (
          <div className="mt-2 rounded-lg border border-surface-border bg-surface-base p-4 text-xs space-y-3">
            {/* Summary row */}
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <span className="text-text-muted">Agent</span>
                <span className="font-mono text-text-primary">{agentId}</span>
              </div>
              <div className="flex items-center gap-2">
                {trade.decision_hash && (
                  <button
                    className="text-text-muted hover:text-accent transition-colors"
                    onClick={(e) => {
                      e.stopPropagation()
                      copyToClipboard(trade.decision_hash!)
                    }}
                    title="Copy hash"
                  >
                    <Copy size={10} />
                  </button>
                )}
                {trade.decision_hash && (
                  <a
                    href={`/v1/evidence/${trade.decision_hash}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1 text-accent hover:text-accent-hover transition-colors"
                    onClick={(e) => e.stopPropagation()}
                  >
                    Raw JSON
                    <ExternalLink size={10} />
                  </a>
                )}
              </div>
            </div>

            {/* Full evidence trace */}
            {traceLoading && (
              <div className="text-text-muted text-center py-2">Loading evidence...</div>
            )}
            {trace && (
              <SyntaxHighlighter
                language="json"
                style={vscDarkPlus}
                customStyle={{
                  background: 'transparent',
                  margin: 0,
                  padding: '0.75rem',
                  fontSize: '0.7rem',
                  lineHeight: '1.5',
                  border: '1px solid var(--color-surface-border)',
                  borderRadius: '0.375rem',
                  maxHeight: '400px',
                }}
                wrapLongLines
              >
                {JSON.stringify(trace, null, 2)}
              </SyntaxHighlighter>
            )}
            {traceError && (
              <div className="text-loss text-center py-2">Failed to load evidence</div>
            )}
            {!trace && !traceLoading && !traceError && !trade.decision_hash && (
              <div className="text-text-muted">
                {trade.status === 'reject' ? 'No evidence hash - rejects are not hash-chained' : 'No evidence hash'}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}

// --- Compact Timeline (horizontal dots) ---

export function CompactTimeline({ trades }: { trades: TradeEvent[] }) {
  if (trades.length === 0) {
    return (
      <div className="rounded-lg border border-surface-border bg-surface-card p-6">
        <div className="flex items-center gap-2 mb-4">
          <Hash size={16} className="text-info" />
          <h2 className="text-sm font-medium text-text-primary">Decision Timeline</h2>
        </div>
        <p className="text-xs text-text-muted">No trades recorded yet.</p>
      </div>
    )
  }

  return (
    <div className="rounded-lg border border-surface-border bg-surface-card p-6">
      <div className="flex items-center gap-2 mb-4">
        <Hash size={16} className="text-info" />
        <h2 className="text-sm font-medium text-text-primary">Decision Timeline</h2>
        <span className="ml-auto text-xs text-text-muted">{trades.length} decisions</span>
      </div>

      <div className="flex items-center gap-1 overflow-x-auto pb-2">
        {trades.map((event, i) => (
          <div
            key={`${event.trade.timestamp}-${i}`}
            className="group relative flex-shrink-0"
          >
            <div
              className={clsx(
                'w-3 h-3 rounded-full border transition-transform group-hover:scale-150',
                event.trade.status === 'fill'
                  ? 'bg-profit border-profit/50'
                  : 'bg-loss border-loss/50',
              )}
            />
            {/* Tooltip on hover */}
            <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 hidden group-hover:block z-10">
              <div className="bg-surface-card border border-surface-border rounded-md px-2 py-1 shadow-lg whitespace-nowrap">
                <div className="text-[10px] text-text-primary">
                  {event.trade.side.toUpperCase()} {event.trade.pair}
                </div>
                <div className={clsx('text-[10px] font-medium', statusColor(event.trade.status))}>
                  {event.trade.status.toUpperCase()}
                </div>
                <div className="text-[10px] text-text-muted">{formatTime(event.trade.timestamp)}</div>
              </div>
            </div>
          </div>
        ))}
      </div>

      {/* Legend */}
      <div className="flex items-center gap-4 mt-3 pt-3 border-t border-surface-border">
        <div className="flex items-center gap-1.5">
          <div className="w-2 h-2 rounded-full bg-profit" />
          <span className="text-[10px] text-text-muted">Fill</span>
        </div>
        <div className="flex items-center gap-1.5">
          <div className="w-2 h-2 rounded-full bg-loss" />
          <span className="text-[10px] text-text-muted">Reject</span>
        </div>
      </div>
    </div>
  )
}

// --- Main Hash Chain Component ---

export function HashChain({ trades }: { trades: TradeEvent[] }) {
  const [expandedIndex, setExpandedIndex] = useState<number | null>(null)

  // Show most recent first in the chain view
  const reversedTrades = [...trades].reverse()

  if (reversedTrades.length === 0) {
    return (
      <div className="rounded-lg border border-surface-border bg-surface-card p-6">
        <div className="flex items-center gap-2 mb-4">
          <Link2 size={16} className="text-accent" />
          <h2 className="text-sm font-medium text-text-primary">Decision Hash Chain</h2>
        </div>
        <p className="text-xs text-text-muted">No decisions recorded yet. Trades will appear here as agents execute.</p>
      </div>
    )
  }

  return (
    <div className="rounded-lg border border-surface-border bg-surface-card p-6">
      <div className="flex items-center gap-2 mb-6">
        <Link2 size={16} className="text-accent" />
        <h2 className="text-sm font-medium text-text-primary">Decision Hash Chain</h2>
        <span className="ml-auto text-xs text-text-muted">{reversedTrades.length} decisions</span>
      </div>

      {/* Trade nodes (newest first) */}
      <div className="max-h-[600px] overflow-y-auto pr-2">
        {reversedTrades.map((event, i) => (
          <ChainNode
            key={`${event.trade.timestamp}-${i}`}
            event={event}
            index={trades.length - 1 - i}
            isLast={i === reversedTrades.length - 1}
            isExpanded={expandedIndex === i}
            onToggle={() => setExpandedIndex(expandedIndex === i ? null : i)}
          />
        ))}
      </div>

      {/* Genesis node (bottom of chain) */}
      <div className="flex gap-4 mt-0">
        <div className="flex flex-col items-center">
          <div className="w-8 h-8 rounded-full border-2 border-accent/30 bg-accent/10 flex items-center justify-center shrink-0">
            <Hash size={14} className="text-accent" />
          </div>
        </div>
        <div>
          <div className="rounded-lg border border-accent/20 bg-surface-base px-3 py-2">
            <span className="text-xs font-mono text-accent">GENESIS</span>
            <div className="text-[10px] font-mono text-text-muted mt-0.5">0x000...000</div>
          </div>
        </div>
      </div>
    </div>
  )
}
