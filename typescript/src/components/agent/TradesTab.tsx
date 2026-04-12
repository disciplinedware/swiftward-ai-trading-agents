import { useState, useMemo } from 'react'
import { clsx } from 'clsx'
import { ChevronDown, ChevronRight, Copy, ExternalLink, Search } from 'lucide-react'
import toast from 'react-hot-toast'
import { useTradeHistory } from '@/hooks/use-trading'
import { formatCurrency, formatQty, formatTime, parseDecimal } from '@/lib/format'
import type { Trade } from '@/types/api'

interface TradesTabProps {
  agentId: string
}

type VerdictFilter = 'all' | 'fill' | 'reject'
type SideFilter = 'all' | 'buy' | 'sell'
type TimeFilter = '1h' | '6h' | '24h' | 'all'

function timeCutoff(filter: TimeFilter): number {
  if (filter === 'all') return 0
  const hours = filter === '1h' ? 1 : filter === '6h' ? 6 : 24
  return Date.now() - hours * 3600_000
}

const EVIDENCE_API_BASE = '/v1/evidence'

function StatusBadge({ status }: { status: Trade['status'] }) {
  return (
    <span
      className={clsx(
        'inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium',
        status === 'fill' && 'bg-profit/15 text-profit',
        status === 'reject' && 'bg-loss/15 text-loss',
      )}
    >
      {status.toUpperCase()}
    </span>
  )
}

function TradeDetail({ trade }: { trade: Trade }) {
  const isRejected = trade.status !== 'fill'

  const copyHash = () => {
    if (trade.decision_hash) {
      navigator.clipboard.writeText(trade.decision_hash)
      toast.success('Hash copied')
    }
  }

  if (isRejected) {
    return (
      <div className="bg-surface-hover/50 rounded-md p-4 text-sm space-y-2">
        <div className="flex items-center justify-between">
          <span className="text-text-muted text-xs uppercase tracking-wide">Trade Detail (Rejected)</span>
        </div>
        {trade.decision_hash && (
          <div className="flex items-center gap-2">
            <span className="text-text-muted">Decision Hash:</span>
            <span className="text-text-primary font-mono text-xs">{trade.decision_hash}</span>
            <button onClick={copyHash} className="text-text-muted hover:text-text-primary transition-colors">
              <Copy size={12} />
            </button>
          </div>
        )}
        <div className="flex items-center gap-2">
          <span className="text-text-muted">Status:</span>
          <StatusBadge status={trade.status} />
        </div>
        {trade.reject && (
          <div className="text-text-secondary text-xs">
            {trade.reject.source}: {trade.reject.reason}
          </div>
        )}
      </div>
    )
  }

  return (
    <div className="bg-surface-hover/50 rounded-md p-4 text-sm space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-text-muted text-xs uppercase tracking-wide">Trade Detail</span>
      </div>
      {trade.decision_hash && (
        <div className="flex items-center gap-2">
          <span className="text-text-muted">Decision Hash:</span>
          <span className="text-text-primary font-mono text-xs">{trade.decision_hash}</span>
          <button onClick={copyHash} className="text-text-muted hover:text-text-primary transition-colors">
            <Copy size={12} />
          </button>
          <a
            href={`${EVIDENCE_API_BASE}/${trade.decision_hash}`}
            target="_blank"
            rel="noopener noreferrer"
            className="text-accent hover:text-accent/80 transition-colors"
          >
            <ExternalLink size={12} />
          </a>
        </div>
      )}
      <div className="flex items-center gap-2">
        <span className="text-text-muted">Status:</span>
        <StatusBadge status={trade.status} />
      </div>
      {trade.fill?.id && (
        <div className="flex items-center gap-2">
          <span className="text-text-muted">Fill ID:</span>
          <span className="text-text-primary font-mono text-xs">{trade.fill.id}</span>
        </div>
      )}
      {trade.fill?.fee && (
        <div className="flex items-center gap-2">
          <span className="text-text-muted">Fee:</span>
          <span className="text-text-primary font-mono text-xs">
            {trade.fill.fee} {trade.fill.fee_asset}
          </span>
          {trade.fill.fee_value && (
            <span className="text-text-muted text-xs">({formatCurrency(trade.fill.fee_value)})</span>
          )}
        </div>
      )}
      {trade.decision_hash && (
        <div className="flex items-center gap-2">
          <span className="text-text-muted">Evidence:</span>
          <a
            href={`${EVIDENCE_API_BASE}/${trade.decision_hash}`}
            target="_blank"
            rel="noopener noreferrer"
            className="text-accent hover:underline text-xs"
          >
            {EVIDENCE_API_BASE}/{trade.decision_hash.substring(0, 16)}...
          </a>
        </div>
      )}
      {trade.metadata && (
        <div className="flex flex-col gap-2 pt-1 border-t border-surface-border/50">
          <div className="flex flex-wrap items-center gap-3">
            {trade.metadata.strategy && (
              <span className="inline-flex items-center rounded-full bg-accent/10 text-accent px-2 py-0.5 text-xs font-medium">
                {trade.metadata.strategy}
              </span>
            )}
            {trade.metadata.trigger_reason && (
              <span className="text-text-muted text-xs">trigger: {trade.metadata.trigger_reason}</span>
            )}
            {trade.metadata.confidence != null && (
              <span className="text-text-muted text-xs">confidence: {(trade.metadata.confidence * 100).toFixed(0)}%</span>
            )}
          </div>
          {trade.metadata.reasoning && (
            <div className="text-xs text-text-secondary leading-relaxed">
              <span className="text-text-muted">Reasoning:</span> {trade.metadata.reasoning}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

export function TradesTab({ agentId }: TradesTabProps) {
  const { data: tradeHistory } = useTradeHistory(agentId, 500)
  const [marketFilter, setMarketFilter] = useState('all')
  const [verdictFilter, setVerdictFilter] = useState<VerdictFilter>('all')
  const [sideFilter, setSideFilter] = useState<SideFilter>('all')
  const [timeFilter, setTimeFilter] = useState<TimeFilter>('all')
  const [hashSearch, setHashSearch] = useState('')
  const [expandedHash, setExpandedHash] = useState<string | null>(null)
  const [page, setPage] = useState(0)
  const pageSize = 50

  // Extract unique markets
  const markets = useMemo(() => {
    if (!tradeHistory?.trades) return []
    const set = new Set(tradeHistory.trades.map((t) => t.pair))
    return Array.from(set).sort()
  }, [tradeHistory])

  // Filtered trades
  const filteredTrades = useMemo(() => {
    if (!tradeHistory?.trades) return []
    let filtered = [...tradeHistory.trades]

    if (marketFilter !== 'all') {
      filtered = filtered.filter((t) => t.pair === marketFilter)
    }
    if (verdictFilter !== 'all') {
      filtered = filtered.filter((t) => t.status === verdictFilter)
    }
    if (sideFilter !== 'all') {
      filtered = filtered.filter((t) => t.side === sideFilter)
    }
    if (timeFilter !== 'all') {
      const cutoff = timeCutoff(timeFilter)
      filtered = filtered.filter((t) => new Date(t.timestamp).getTime() >= cutoff)
    }
    if (hashSearch) {
      const search = hashSearch.toLowerCase()
      filtered = filtered.filter((t) =>
        t.decision_hash?.toLowerCase().includes(search),
      )
    }

    // Sort newest first
    filtered.sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime())
    return filtered
  }, [tradeHistory, marketFilter, verdictFilter, sideFilter, timeFilter, hashSearch])

  const pagedTrades = filteredTrades.slice(0, (page + 1) * pageSize)
  const hasMore = pagedTrades.length < filteredTrades.length

  const toggleExpand = (trade: Trade) => {
    const key = trade.decision_hash || `${trade.timestamp}-${trade.pair}`
    setExpandedHash((prev) => (prev === key ? null : key))
  }

  const getTradeKey = (trade: Trade) =>
    trade.decision_hash || `${trade.timestamp}-${trade.pair}-${trade.side}`

  return (
    <div className="space-y-4">
      {/* Filters Bar */}
      <div className="rounded-lg border border-surface-border bg-surface-card p-4">
        <div className="flex flex-wrap items-center gap-3">
          <select
            className="rounded-md border border-surface-border bg-surface-hover px-3 py-1.5 text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
            value={marketFilter}
            onChange={(e) => { setMarketFilter(e.target.value); setPage(0) }}
          >
            <option value="all">Pair: All</option>
            {markets.map((m) => (
              <option key={m} value={m}>{m}</option>
            ))}
          </select>

          <select
            className="rounded-md border border-surface-border bg-surface-hover px-3 py-1.5 text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
            value={verdictFilter}
            onChange={(e) => { setVerdictFilter(e.target.value as VerdictFilter); setPage(0) }}
          >
            <option value="all">Status: All</option>
            <option value="fill">Fill</option>
            <option value="reject">Reject</option>
          </select>

          <select
            className="rounded-md border border-surface-border bg-surface-hover px-3 py-1.5 text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
            value={sideFilter}
            onChange={(e) => { setSideFilter(e.target.value as SideFilter); setPage(0) }}
          >
            <option value="all">Side: All</option>
            <option value="buy">Buy</option>
            <option value="sell">Sell</option>
          </select>

          <select
            className="rounded-md border border-surface-border bg-surface-hover px-3 py-1.5 text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
            value={timeFilter}
            onChange={(e) => { setTimeFilter(e.target.value as TimeFilter); setPage(0) }}
          >
            <option value="all">Time: All</option>
            <option value="1h">1 Hour</option>
            <option value="6h">6 Hours</option>
            <option value="24h">24 Hours</option>
          </select>

          <div className="relative flex-1 min-w-48">
            <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-text-muted" />
            <input
              type="text"
              placeholder="Search hash..."
              className="w-full rounded-md border border-surface-border bg-surface-hover pl-8 pr-3 py-1.5 text-sm text-text-primary placeholder-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
              value={hashSearch}
              onChange={(e) => { setHashSearch(e.target.value); setPage(0) }}
            />
          </div>

          <span className="text-xs text-text-muted">
            {filteredTrades.length} trades
          </span>
        </div>
      </div>

      {/* Trade History Table */}
      <div className="rounded-lg border border-surface-border bg-surface-card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-surface-border">
                <th className="px-3 py-3 w-6" />
                {['Time', 'Pair', 'Side', 'Qty', 'Price', 'Value', 'Fee', 'P&L', 'Value After', 'Status', 'Hash'].map((h) => (
                  <th
                    key={h}
                    className="px-3 py-3 text-left text-xs font-medium text-text-muted uppercase tracking-wide"
                  >
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {pagedTrades.length === 0 && (
                <tr>
                  <td colSpan={12} className="px-4 py-8 text-center text-text-muted text-sm">
                    No trades found.
                  </td>
                </tr>
              )}
              {pagedTrades.map((trade) => {
                const key = getTradeKey(trade)
                const isExpanded = expandedHash === (trade.decision_hash || `${trade.timestamp}-${trade.pair}`)
                const isRejected = trade.status !== 'fill'
                const pnl = parseDecimal(trade.pnl_value)

                return (
                  <TradeRow
                    key={key}
                    trade={trade}
                    isExpanded={isExpanded}
                    isRejected={isRejected}
                    pnl={pnl}
                    onToggle={() => toggleExpand(trade)}
                  />
                )
              })}
            </tbody>
          </table>
        </div>

        {hasMore && (
          <div className="px-4 py-3 border-t border-surface-border text-center">
            <button
              className="text-sm text-accent hover:text-accent/80 transition-colors"
              onClick={() => setPage((p) => p + 1)}
            >
              Load more ({filteredTrades.length - pagedTrades.length} remaining)
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

function TradeRow({
  trade,
  isExpanded,
  isRejected,
  pnl,
  onToggle,
}: {
  trade: Trade
  isExpanded: boolean
  isRejected: boolean
  pnl: number
  onToggle: () => void
}) {
  return (
    <>
      <tr
        className={clsx(
          'border-b border-surface-border hover:bg-surface-hover transition-colors cursor-pointer',
          isRejected && 'opacity-60',
        )}
        onClick={onToggle}
      >
        <td className="px-3 py-3 text-text-muted">
          {isExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        </td>
        <td className="px-3 py-3 text-text-secondary font-mono text-xs whitespace-nowrap">
          {formatTime(trade.timestamp)}
        </td>
        <td className="px-3 py-3 text-text-primary font-medium">{trade.pair}</td>
        <td className={clsx(
          'px-3 py-3 font-medium uppercase text-xs',
          trade.side === 'buy' ? 'text-profit' : 'text-loss',
        )}>
          {trade.side.toUpperCase()}
        </td>
        <td className="px-3 py-3 text-text-primary font-mono">
          {isRejected ? '-' : formatQty(trade.fill?.qty)}
        </td>
        <td className="px-3 py-3 text-text-primary font-mono">
          {isRejected ? '-' : formatCurrency(trade.fill?.price)}
        </td>
        <td className="px-3 py-3 text-text-primary font-mono">
          {isRejected ? '-' : formatCurrency(trade.fill?.value)}
        </td>
        <td className="px-3 py-3 text-text-muted font-mono text-xs">
          {isRejected ? '-' : trade.fill?.fee_value ? formatCurrency(trade.fill.fee_value) : '-'}
        </td>
        <td className={clsx(
          'px-3 py-3 font-mono',
          isRejected ? 'text-text-muted' : pnl >= 0 ? 'text-profit' : 'text-loss',
        )}>
          {isRejected ? '-' : `${pnl >= 0 ? '+' : ''}${formatCurrency(pnl)}`}
        </td>
        <td className="px-3 py-3 text-text-primary font-mono">
          {isRejected ? '-' : formatCurrency(trade.portfolio?.value_after)}
        </td>
        <td className="px-3 py-3">
          <StatusBadge status={trade.status} />
        </td>
        <td className="px-3 py-3 text-text-muted font-mono text-xs">
          {trade.decision_hash
            ? `${trade.decision_hash.substring(0, 10)}...`
            : '-'}
        </td>
      </tr>
      {isExpanded && (
        <tr>
          <td colSpan={12} className="px-3 py-3 bg-surface-base">
            <TradeDetail trade={trade} />
          </td>
        </tr>
      )}
    </>
  )
}
