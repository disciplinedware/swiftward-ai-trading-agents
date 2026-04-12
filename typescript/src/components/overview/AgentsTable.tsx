import { useMemo, useState, useSyncExternalStore } from 'react'
import { useNavigate } from 'react-router-dom'
import { clsx } from 'clsx'
import { ArrowUpDown, Pause, Play, ExternalLink } from 'lucide-react'
import { formatCurrency, formatPercent, parseDecimal } from '@/lib/format'
import type { AgentSummary, Limits } from '@/types/api'

type SortField = 'equity' | 'pnl' | 'drawdown' | 'trades'
type SortDir = 'asc' | 'desc'

interface AgentsTableProps {
  agents: AgentSummary[]
  limitsMap: Record<string, Limits>
  onHalt: (agentId: string) => void
  onResume: (agentId: string) => void
  initialCapital?: number
}

const ACTIVE_THRESHOLD_MS = 5 * 60 * 1000 // 5 minutes

// Treat wall-clock time as an external store (React 19 pattern for impure reads)
let _cachedNow = Date.now()
const timeStore = {
  subscribe(cb: () => void) {
    const id = setInterval(() => { _cachedNow = Date.now(); cb() }, 30_000)
    return () => clearInterval(id)
  },
  getSnapshot() { return _cachedNow },
}

function StatusDot({ halted, lastSeenAt }: { halted: boolean; lastSeenAt?: string }) {
  const now = useSyncExternalStore(timeStore.subscribe, timeStore.getSnapshot)
  const isActive = lastSeenAt
    ? now - new Date(lastSeenAt).getTime() < ACTIVE_THRESHOLD_MS
    : false

  let colorClass: string
  let title: string
  if (halted) {
    colorClass = 'bg-loss animate-pulse'
    title = 'Halted'
  } else if (isActive) {
    colorClass = 'bg-profit'
    title = 'Active'
  } else {
    colorClass = 'bg-gray-500'
    title = lastSeenAt
      ? `Inactive since ${new Date(lastSeenAt).toLocaleTimeString()}`
      : 'Never seen'
  }

  return (
    <span
      className={clsx('inline-block h-2.5 w-2.5 rounded-full', colorClass)}
      title={title}
    />
  )
}

export function AgentsTable({
  agents,
  limitsMap,
  onHalt,
  onResume,
  initialCapital = 10000,
}: AgentsTableProps) {
  const navigate = useNavigate()
  const [sortField, setSortField] = useState<SortField>('equity')
  const [sortDir, setSortDir] = useState<SortDir>('desc')
  const [confirmHaltId, setConfirmHaltId] = useState<string | null>(null)

  const toggleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortField(field)
      setSortDir('desc')
    }
  }

  const sortedAgents = useMemo(() => {
    const list = [...agents]
    list.sort((a, b) => {
      const la = limitsMap[a.agent_id]
      const lb = limitsMap[b.agent_id]
      let va = 0
      let vb = 0
      switch (sortField) {
        case 'equity':
          va = parseDecimal(la?.portfolio?.value)
          vb = parseDecimal(lb?.portfolio?.value)
          break
        case 'pnl':
          va = parseDecimal(la?.portfolio?.value) - (parseDecimal(la?.portfolio?.initial_value) || initialCapital)
          vb = parseDecimal(lb?.portfolio?.value) - (parseDecimal(lb?.portfolio?.initial_value) || initialCapital)
          break
        case 'drawdown': {
          const peakA = parseDecimal(la?.portfolio?.peak)
          const valA = parseDecimal(la?.portfolio?.value)
          va = peakA > 0 ? ((valA - peakA) / peakA) * 100 : 0
          const peakB = parseDecimal(lb?.portfolio?.peak)
          const valB = parseDecimal(lb?.portfolio?.value)
          vb = peakB > 0 ? ((valB - peakB) / peakB) * 100 : 0
          break
        }
        case 'trades':
          va = la?.fill_count ?? 0
          vb = lb?.fill_count ?? 0
          break
      }
      return sortDir === 'asc' ? va - vb : vb - va
    })
    return list
  }, [agents, limitsMap, sortField, sortDir, initialCapital])

  const headers: { label: string; field?: SortField; className?: string }[] = [
    { label: '', className: 'w-8' },
    { label: 'Agent ID' },
    { label: 'Name' },
    { label: 'Equity', field: 'equity' },
    { label: 'P&L', field: 'pnl' },
    { label: 'Drawdown', field: 'drawdown' },
    { label: 'Trades', field: 'trades' },
    { label: 'Rejected' },
    { label: 'Largest Pos' },
    { label: 'Actions', className: 'text-right' },
  ]

  return (
    <div className="rounded-lg border border-surface-border bg-surface-card overflow-hidden mb-6">
      <div className="px-6 py-4 border-b border-surface-border">
        <h2 className="text-sm font-medium text-text-secondary">Agents</h2>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-surface-border">
              {headers.map((h) => (
                <th
                  key={h.label || 'status'}
                  className={clsx(
                    'px-4 py-3 text-left text-xs font-medium text-text-muted uppercase tracking-wide',
                    h.className,
                    h.field && 'cursor-pointer select-none hover:text-text-secondary',
                  )}
                  onClick={h.field ? () => toggleSort(h.field!) : undefined}
                >
                  <span className="inline-flex items-center gap-1">
                    {h.label}
                    {h.field && (
                      <ArrowUpDown
                        size={12}
                        className={clsx(
                          sortField === h.field ? 'text-accent' : 'text-text-muted',
                        )}
                      />
                    )}
                  </span>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {sortedAgents.length === 0 && (
              <tr>
                <td
                  colSpan={10}
                  className="px-4 py-8 text-center text-text-muted"
                >
                  No agents connected. Start agents to see data here.
                </td>
              </tr>
            )}
            {sortedAgents.map((agent) => {
              const limits = limitsMap[agent.agent_id]
              const equity = parseDecimal(limits?.portfolio?.value ?? agent.portfolio?.value)
              const agentInitial = parseDecimal(limits?.portfolio?.initial_value ?? agent.portfolio?.initial_value) || initialCapital
              const pnl = equity - agentInitial
              const pnlPct = agentInitial > 0 ? (pnl / agentInitial) * 100 : 0
              const isHalted = limits?.halted ?? agent.halted ?? false
              const rejectedCount = limits?.reject_count ?? 0
              const tradeCount = limits?.fill_count ?? agent.fill_count ?? 0
              const totalIntents = tradeCount + rejectedCount
              const rejPct = totalIntents > 0 ? (rejectedCount / totalIntents) * 100 : 0

              return (
                <tr
                  key={agent.agent_id}
                  className="border-b border-surface-border last:border-0 hover:bg-surface-hover transition-colors cursor-pointer"
                  onClick={() => navigate(`/agents/${agent.agent_id}`)}
                >
                  <td className="px-4 py-3">
                    <StatusDot halted={isHalted} lastSeenAt={agent.last_seen_at} />
                  </td>
                  <td className="px-4 py-3 text-accent font-mono text-xs">
                    {agent.agent_id}
                  </td>
                  <td className="px-4 py-3 text-text-primary font-medium">
                    {agent.name || agent.agent_id}
                  </td>
                  <td className="px-4 py-3 text-text-primary font-mono">
                    {formatCurrency(equity)}
                  </td>
                  <td
                    className={clsx(
                      'px-4 py-3 font-mono',
                      pnl >= 0 ? 'text-profit' : 'text-loss',
                    )}
                  >
                    {pnl >= 0 ? '+' : ''}
                    {formatCurrency(pnl)} ({pnlPct >= 0 ? '+' : ''}
                    {pnlPct.toFixed(1)}%)
                  </td>
                  <td
                    className={clsx(
                      'px-4 py-3 font-mono',
                      (() => {
                        const peak = parseDecimal(limits?.portfolio?.peak)
                        const val = parseDecimal(limits?.portfolio?.value)
                        const dd = peak > 0 ? ((val - peak) / peak) * 100 : 0
                        return dd < -5 ? 'text-loss' : 'text-text-secondary'
                      })(),
                    )}
                  >
                    {limits ? (() => {
                      const peak = parseDecimal(limits.portfolio?.peak)
                      const val = parseDecimal(limits.portfolio?.value)
                      return peak > 0 ? formatPercent(((val - peak) / peak) * 100) : '0%'
                    })() : '-'}
                  </td>
                  <td className="px-4 py-3 text-text-primary font-mono">
                    {tradeCount}
                  </td>
                  <td
                    className={clsx(
                      'px-4 py-3 font-mono',
                      rejPct > 25
                        ? 'text-loss'
                        : rejPct > 10
                          ? 'text-warning'
                          : 'text-text-secondary',
                    )}
                  >
                    {rejectedCount > 0
                      ? `${rejectedCount} (${rejPct.toFixed(0)}%)`
                      : '0'}
                  </td>
                  <td className="px-4 py-3 text-text-secondary text-xs">
                    {limits
                      ? `${limits.largest_position_pct.toFixed(1)}% ${limits.largest_position_pair}`
                      : '-'}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <div
                      className="inline-flex gap-1"
                      onClick={(e) => e.stopPropagation()}
                    >
                      {isHalted ? (
                        <button
                          className="inline-flex items-center gap-1 rounded px-2 py-1 text-xs bg-profit/10 text-profit hover:bg-profit/20 transition-colors"
                          onClick={() => onResume(agent.agent_id)}
                        >
                          <Play size={12} />
                          Resume
                        </button>
                      ) : confirmHaltId === agent.agent_id ? (
                        <>
                          <button
                            className="inline-flex items-center gap-1 rounded px-2 py-1 text-xs bg-loss text-white hover:bg-loss/80 transition-colors"
                            onClick={() => {
                              onHalt(agent.agent_id)
                              setConfirmHaltId(null)
                            }}
                          >
                            Confirm
                          </button>
                          <button
                            className="inline-flex items-center gap-1 rounded px-2 py-1 text-xs bg-surface-hover text-text-secondary hover:text-text-primary transition-colors"
                            onClick={() => setConfirmHaltId(null)}
                          >
                            Cancel
                          </button>
                        </>
                      ) : (
                        <button
                          className="inline-flex items-center gap-1 rounded px-2 py-1 text-xs bg-loss/10 text-loss hover:bg-loss/20 transition-colors"
                          onClick={() => setConfirmHaltId(agent.agent_id)}
                        >
                          <Pause size={12} />
                          Halt
                        </button>
                      )}
                      <button
                        className="inline-flex items-center gap-1 rounded px-2 py-1 text-xs bg-surface-hover text-text-secondary hover:text-text-primary transition-colors"
                        onClick={() => navigate(`/agents/${agent.agent_id}`)}
                      >
                        <ExternalLink size={12} />
                      </button>
                    </div>
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
