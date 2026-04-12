import { useState, useMemo } from 'react'
import { clsx } from 'clsx'
import {
  Award,
  TrendingUp,
  Target,
  ShieldCheck,
  BarChart3,
  ArrowDownRight,
  ExternalLink,
} from 'lucide-react'
import type { TradeEvent } from '@/hooks/use-all-trades'
import { parseDecimal } from '@/lib/format'

interface ReputationMetric {
  label: string
  value: string
  description: string
  icon: React.ReactNode
  color: string
}

function computeReputationMetrics(
  trades: TradeEvent[],
  agentId: string,
): ReputationMetric[] {
  const agentTrades = trades.filter((t) => t.agentId === agentId)
  const approved = agentTrades.filter((t) => t.trade.status === 'fill')
  const totalCount = agentTrades.length

  // Win rate: approved trades with positive PnL / approved trades with PnL
  const tradesWithPnl = approved.filter((t) => t.trade.pnl_value !== undefined)
  const wins = tradesWithPnl.filter((t) => parseDecimal(t.trade.pnl_value) > 0)
  const winRate = tradesWithPnl.length > 0
    ? (wins.length / tradesWithPnl.length) * 100
    : 0

  // Total return: sum of all PnL / initial equity (estimate from first trade)
  const totalPnl = approved.reduce((sum, t) => sum + parseDecimal(t.trade.pnl_value), 0)
  const firstEquity = approved.length > 0
    ? parseDecimal(approved[0].trade.portfolio?.value_after) - parseDecimal(approved[0].trade.pnl_value)
    : 10000
  const returnPct = firstEquity > 0 ? (totalPnl / firstEquity) * 100 : 0

  // Compliance: approved / total intents
  const compliancePct = totalCount > 0
    ? (approved.length / totalCount) * 100
    : 100

  // Max drawdown: track peak equity and worst drawdown
  let peak = 0
  let maxDrawdown = 0
  for (const t of approved) {
    const eq = parseDecimal(t.trade.portfolio?.value_after)
    if (eq > peak) peak = eq
    if (peak > 0) {
      const dd = ((peak - eq) / peak) * 100
      if (dd > maxDrawdown) maxDrawdown = dd
    }
  }

  // Sharpe estimate: mean return / std deviation of per-trade percentage returns.
  // Not annualized because returns are per-trade (not per-day), and trade frequency varies.
  const returns = tradesWithPnl.map((t) => {
    const pnl = parseDecimal(t.trade.pnl_value)
    const equityBefore = parseDecimal(t.trade.portfolio?.value_after) - pnl
    return equityBefore > 0 ? pnl / equityBefore : 0
  })
  const meanReturn = returns.length > 0
    ? returns.reduce((a, b) => a + b, 0) / returns.length
    : 0
  const variance = returns.length > 1
    ? returns.reduce((sum, r) => sum + (r - meanReturn) ** 2, 0) / (returns.length - 1)
    : 1
  const stdDev = Math.sqrt(variance)
  const sharpe = stdDev > 0 ? meanReturn / stdDev : 0

  // Guardrail saves: rejected trades count
  const guardrailSaves = agentTrades.filter(
    (t) => t.trade.status === 'reject',
  ).length

  return [
    {
      label: 'Sharpe Ratio',
      value: sharpe.toFixed(2),
      description: 'Risk-adjusted return (per-trade)',
      icon: <BarChart3 size={14} />,
      color: parseFloat(sharpe.toFixed(2)) >= 1 ? 'text-profit' : 'text-text-primary',
    },
    {
      label: 'Total Return',
      value: `${returnPct >= 0 ? '+' : ''}${returnPct.toFixed(1)}%`,
      description: 'Cumulative P&L %',
      icon: <TrendingUp size={14} />,
      color: returnPct >= 0 ? 'text-profit' : 'text-loss',
    },
    {
      label: 'Win Rate',
      value: `${winRate.toFixed(1)}%`,
      description: `${wins.length}W / ${tradesWithPnl.length - wins.length}L`,
      icon: <Target size={14} />,
      color: winRate >= 50 ? 'text-profit' : 'text-loss',
    },
    {
      label: 'Max Drawdown',
      value: `-${maxDrawdown.toFixed(1)}%`,
      description: 'Worst peak-to-trough',
      icon: <ArrowDownRight size={14} />,
      color: maxDrawdown < 5 ? 'text-profit' : maxDrawdown < 10 ? 'text-warning' : 'text-loss',
    },
    {
      label: 'Compliance',
      value: `${compliancePct.toFixed(1)}%`,
      description: `${approved.length} / ${totalCount} intents approved`,
      icon: <ShieldCheck size={14} />,
      color: compliancePct >= 80 ? 'text-profit' : 'text-warning',
    },
    {
      label: 'Guardrail Saves',
      value: `${guardrailSaves}`,
      description: 'Blocked trades',
      icon: <ShieldCheck size={14} />,
      color: guardrailSaves > 0 ? 'text-info' : 'text-text-muted',
    },
  ]
}

export function ReputationScores({
  trades,
  agentIds,
}: {
  trades: TradeEvent[]
  agentIds: string[]
}) {
  const [selectedAgent, setSelectedAgent] = useState<string>(agentIds[0] || '')

  // Update selected agent when agentIds change
  const effectiveAgent = agentIds.includes(selectedAgent)
    ? selectedAgent
    : agentIds[0] || ''

  const metrics = useMemo(
    () => computeReputationMetrics(trades, effectiveAgent),
    [trades, effectiveAgent],
  )

  if (agentIds.length === 0) {
    return (
      <div className="rounded-lg border border-surface-border bg-surface-card p-6">
        <div className="flex items-center gap-2 mb-4">
          <Award size={16} className="text-warning" />
          <h2 className="text-sm font-medium text-text-primary">On-Chain Reputation Scores</h2>
        </div>
        <p className="text-xs text-text-muted">No agents found.</p>
      </div>
    )
  }

  return (
    <div className="rounded-lg border border-surface-border bg-surface-card p-6">
      <div className="flex items-center justify-between mb-5">
        <div className="flex items-center gap-2">
          <Award size={16} className="text-warning" />
          <h2 className="text-sm font-medium text-text-primary">On-Chain Reputation Scores</h2>
          <span className="inline-flex items-center rounded-full bg-warning/15 px-2 py-0.5 text-[10px] font-medium text-warning">
            ERC-8004
          </span>
        </div>

        {/* Agent selector (hidden when single agent) */}
        {agentIds.length > 1 && (
          <select
            className="bg-surface-card border border-surface-border rounded-md px-2 py-1 text-xs text-text-primary"
            value={effectiveAgent}
            onChange={(e) => setSelectedAgent(e.target.value)}
          >
            {agentIds.map((id) => (
              <option key={id} value={id}>{id}</option>
            ))}
          </select>
        )}
      </div>

      {/* Metrics table */}
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-surface-border">
              <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Metric</th>
              <th className="px-4 py-2.5 text-right text-xs font-medium text-text-muted">Score</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Description</th>
            </tr>
          </thead>
          <tbody>
            {metrics.map((m) => (
              <tr
                key={m.label}
                className="border-b border-surface-border last:border-0 hover:bg-surface-hover transition-colors"
              >
                <td className="px-4 py-3">
                  <div className="flex items-center gap-2">
                    <span className="text-text-muted">{m.icon}</span>
                    <span className="text-text-primary font-medium">{m.label}</span>
                  </div>
                </td>
                <td className="px-4 py-3 text-right">
                  <span className={clsx('font-mono font-semibold', m.color)}>{m.value}</span>
                </td>
                <td className="px-4 py-3 text-xs text-text-muted">{m.description}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Actions */}
      <div className="flex items-center justify-between pt-4 mt-4 border-t border-surface-border">
        <span className="text-xs text-text-muted">
          Computed from trade history. On-chain scores update every 30 min.
        </span>
        <a
          href="https://sepolia.etherscan.io"
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 text-xs text-accent hover:text-accent-hover transition-colors"
        >
          View on Sepolia
          <ExternalLink size={10} />
        </a>
      </div>
    </div>
  )
}
