import { useMemo } from 'react'
import { Link } from 'react-router-dom'
import { Fingerprint, ExternalLink } from 'lucide-react'
import { useAgents } from '@/hooks/use-risk'
import { useAllTrades, type TradeEvent } from '@/hooks/use-all-trades'
import { CompactTimeline } from '@/components/evidence/HashChain'
import { ValidationLog } from '@/components/evidence/ValidationLog'

interface AgentTrustRow {
  agentId: string
  total: number
  fills: number
  rejects: number
  hashed: number
}

function computeAgentRows(trades: TradeEvent[], agentIds: string[]): AgentTrustRow[] {
  const map = new Map<string, AgentTrustRow>()
  for (const id of agentIds) {
    map.set(id, { agentId: id, total: 0, fills: 0, rejects: 0, hashed: 0 })
  }
  for (const { trade, agentId } of trades) {
    let row = map.get(agentId)
    if (!row) {
      row = { agentId, total: 0, fills: 0, rejects: 0, hashed: 0 }
      map.set(agentId, row)
    }
    row.total++
    if (trade.status === 'fill') row.fills++
    if (trade.status === 'reject') row.rejects++
    if (trade.decision_hash) row.hashed++
  }
  return Array.from(map.values()).sort((a, b) => b.total - a.total)
}

export function Evidence() {
  const { data: agentList } = useAgents()
  const agentIds = useMemo(
    () => (agentList?.agents ?? []).map((a) => a.agent_id),
    [agentList],
  )

  const { trades, isLoading } = useAllTrades(agentIds, 200)

  const agentRows = useMemo(
    () => computeAgentRows(trades, agentIds),
    [trades, agentIds],
  )

  const totalHashed = trades.filter((t) => t.trade.decision_hash).length

  return (
    <div className="space-y-6">
      {/* Page header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Fingerprint size={20} className="text-accent" />
          <h1 className="text-xl font-semibold text-text-primary">Trust Overview</h1>
        </div>
        <div className="flex items-center gap-4">
          <span className="text-xs text-text-secondary">
            {agentIds.length} agents / {trades.length} decisions / {totalHashed} with hash proof
          </span>
        </div>
      </div>

      {/* Loading state */}
      {isLoading && agentIds.length === 0 && (
        <div className="rounded-lg border border-surface-border bg-surface-card p-8 text-center">
          <p className="text-sm text-text-muted">Loading trust data...</p>
        </div>
      )}

      {/* Cross-agent timeline */}
      <CompactTimeline trades={trades} />

      {/* Agent Trust Table */}
      <div className="rounded-lg border border-surface-border bg-surface-card overflow-hidden">
        <div className="flex items-center gap-2 px-5 py-4 border-b border-surface-border">
          <Fingerprint size={16} className="text-accent" />
          <h2 className="text-sm font-medium text-text-primary">Agent Trust Summary</h2>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-surface-border">
                <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Agent</th>
                <th className="px-4 py-2.5 text-right text-xs font-medium text-text-muted">Decisions</th>
                <th className="px-4 py-2.5 text-right text-xs font-medium text-text-muted">Fills</th>
                <th className="px-4 py-2.5 text-right text-xs font-medium text-text-muted">Rejects</th>
                <th className="px-4 py-2.5 text-right text-xs font-medium text-text-muted">Hash Proofs</th>
                <th className="px-4 py-2.5 w-8"></th>
              </tr>
            </thead>
            <tbody>
              {agentRows.map((row) => (
                <tr key={row.agentId} className="border-b border-surface-border hover:bg-surface-hover transition-colors">
                  <td className="px-4 py-3">
                    <Link
                      to={`/agents/${row.agentId}`}
                      className="text-accent hover:text-accent-hover font-mono text-xs transition-colors"
                    >
                      {row.agentId}
                    </Link>
                  </td>
                  <td className="px-4 py-3 text-right text-xs font-mono text-text-primary">{row.total}</td>
                  <td className="px-4 py-3 text-right text-xs font-mono text-profit">{row.fills}</td>
                  <td className="px-4 py-3 text-right text-xs font-mono text-loss">{row.rejects}</td>
                  <td className="px-4 py-3 text-right text-xs font-mono text-text-secondary">{row.hashed}</td>
                  <td className="px-4 py-3">
                    <Link
                      to={`/agents/${row.agentId}`}
                      className="text-text-muted hover:text-accent transition-colors"
                    >
                      <ExternalLink size={12} />
                    </Link>
                  </td>
                </tr>
              ))}
              {agentRows.length === 0 && (
                <tr>
                  <td colSpan={6} className="px-4 py-8 text-center text-xs text-text-muted">
                    No agents found.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Validation Log (cross-agent) */}
      <ValidationLog trades={trades} />
    </div>
  )
}
