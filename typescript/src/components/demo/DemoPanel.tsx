import { clsx } from 'clsx'
import {
  Shield,
  ShieldOff,
  TrendingDown,
  Ban,
  AlertTriangle,
  Activity,
} from 'lucide-react'
import { formatCurrency, formatPercent, parseDecimal } from '@/lib/format'
import type { Limits, Trade } from '@/types/api'

interface DemoPanelProps {
  variant: 'with' | 'without'
  agentId: string | null
  limits: Limits | undefined
  trades: Trade[]
  isLoading: boolean
}

interface GuardrailSave {
  label: string
  count: number
}

function computeGuardrailSaves(trades: Trade[]): GuardrailSave[] {
  const reasons: Record<string, number> = {}
  for (const t of trades) {
    if (t.status === 'reject') {
      const reason = t.reject?.reason?.toLowerCase().includes('halt') ? 'Agent halted' : 'Policy rejection'
      reasons[reason] = (reasons[reason] || 0) + 1
    }
  }
  return Object.entries(reasons)
    .map(([label, count]) => ({ label, count }))
    .sort((a, b) => b.count - a.count)
}

interface LossEvent {
  label: string
  amount: string
}

function computeUnprotectedLosses(trades: Trade[]): LossEvent[] {
  const losses: LossEvent[] = []
  for (const t of trades) {
    if (t.status === 'fill' && t.pnl_value) {
      const pnl = parseDecimal(t.pnl_value)
      if (pnl < 0) {
        losses.push({
          label: `${t.side.toUpperCase()} ${t.pair}`,
          amount: formatCurrency(Math.abs(pnl)),
        })
      }
    }
  }
  return losses.slice(0, 8)
}

export function DemoPanel({
  variant,
  agentId,
  limits,
  trades,
  isLoading,
}: DemoPanelProps) {
  const isWith = variant === 'with'
  const borderColor = isWith ? 'border-profit/30' : 'border-loss/30'
  const dotColor = isWith ? 'bg-profit' : 'bg-loss'
  const textColor = isWith ? 'text-profit' : 'text-loss'
  const Icon = isWith ? Shield : ShieldOff

  const equity = limits ? parseDecimal(limits.portfolio?.value) : 0
  const peak = limits ? parseDecimal(limits.portfolio?.peak) : 0
  const initialCapital = 10_000
  const pnl = equity - initialCapital
  const pnlPct = initialCapital > 0 ? (pnl / initialCapital) * 100 : 0
  const drawdownAbs = peak > 0 ? Math.abs(((equity - peak) / peak) * 100) : 0
  const tradeCount = limits?.fill_count ?? 0
  const rejectedCount = limits?.reject_count ?? 0
  const totalIntents = tradeCount + rejectedCount
  const halted = limits?.halted ?? false

  const guardrailSaves = isWith ? computeGuardrailSaves(trades) : []
  const unprotectedLosses = !isWith ? computeUnprotectedLosses(trades) : []

  if (!agentId) {
    return (
      <div className={clsx('rounded-lg border-2 bg-surface-card p-6', borderColor)}>
        <div className="flex items-center gap-2 mb-4">
          <Icon className={clsx('h-4 w-4', textColor)} />
          <h2 className={clsx('text-sm font-semibold uppercase', textColor)}>
            {isWith ? 'With Guardrails' : 'Without Guardrails'}
          </h2>
        </div>
        <p className="text-xs text-text-secondary mb-4">
          {isWith ? 'MCP Gateway (policy enforced)' : 'Direct (no policy)'}
        </p>
        <p className="text-text-muted text-sm">
          Select an agent above to compare.
        </p>
      </div>
    )
  }

  return (
    <div className={clsx('rounded-lg border-2 bg-surface-card p-6', borderColor)}>
      {/* Header */}
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-2">
          <div className={clsx('h-3 w-3 rounded-full', dotColor)} />
          <Icon className={clsx('h-4 w-4', textColor)} />
          <h2 className={clsx('text-sm font-semibold uppercase', textColor)}>
            {isWith ? 'With Guardrails' : 'Without Guardrails'}
          </h2>
        </div>
        <span className="text-xs text-text-muted font-mono">{agentId}</span>
      </div>

      <p className="text-xs text-text-secondary mb-5">
        {isWith ? 'MCP Gateway (policy enforced)' : 'Direct (no policy)'}
      </p>

      {isLoading ? (
        <p className="text-text-muted text-sm">Loading agent data...</p>
      ) : (
        <>
          {/* Metrics grid */}
          <div className="grid grid-cols-2 gap-3 mb-5">
            <MetricCard
              label="Equity"
              value={formatCurrency(equity)}
              sub={`${pnl >= 0 ? '+' : ''}${formatCurrency(pnl)} (${formatPercent(pnlPct)})`}
              positive={pnl >= 0}
            />
            <MetricCard
              label="Peak"
              value={formatCurrency(peak > 0 ? peak : equity)}
              sub={limits ? `Drawdown: ${formatPercent(-drawdownAbs)}` : '-'}
            />
            <MetricCard
              label="Drawdown"
              value={formatPercent(-drawdownAbs)}
              sub="from peak"
              positive={drawdownAbs < 5}
            />
            <MetricCard
              label="Trades"
              value={String(tradeCount)}
              sub={`${rejectedCount} rejected (${totalIntents > 0 ? Math.round((rejectedCount / totalIntents) * 100) : 0}%)`}
            />
          </div>

          {/* Status */}
          <div className="mb-5">
            {halted ? (
              <div className="flex items-center gap-2 px-3 py-2 rounded-md bg-loss/10 border border-loss/20">
                <Ban className="h-3.5 w-3.5 text-loss" />
                <span className="text-xs font-medium text-loss">HALTED</span>
              </div>
            ) : drawdownAbs > 5 ? (
              <div className="flex items-center gap-2 px-3 py-2 rounded-md bg-warning/10 border border-warning/20">
                <AlertTriangle className="h-3.5 w-3.5 text-warning" />
                <span className="text-xs font-medium text-warning">HIGH DRAWDOWN</span>
              </div>
            ) : (
              <div className="flex items-center gap-2 px-3 py-2 rounded-md bg-profit/10 border border-profit/20">
                <Activity className="h-3.5 w-3.5 text-profit" />
                <span className="text-xs font-medium text-profit">ACTIVE</span>
              </div>
            )}
          </div>

          {/* Guardrail Saves (with) or Unprotected Losses (without) */}
          {isWith && (
            <div>
              <h3 className="text-xs font-medium text-text-secondary mb-2 flex items-center gap-1.5">
                <Shield className="h-3 w-3 text-profit" />
                Guardrail Saves
              </h3>
              {guardrailSaves.length > 0 ? (
                <ul className="space-y-1.5">
                  {guardrailSaves.map((s) => (
                    <li
                      key={s.label}
                      className="flex items-center justify-between text-xs"
                    >
                      <span className="text-text-secondary">{s.label}</span>
                      <span className="text-profit font-mono">{s.count}x</span>
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="text-xs text-text-muted">No rejections yet</p>
              )}
            </div>
          )}

          {!isWith && (
            <div>
              <h3 className="text-xs font-medium text-text-secondary mb-2 flex items-center gap-1.5">
                <TrendingDown className="h-3 w-3 text-loss" />
                Recent Losses
              </h3>
              {unprotectedLosses.length > 0 ? (
                <ul className="space-y-1.5">
                  {unprotectedLosses.map((l, i) => (
                    <li
                      key={i}
                      className="flex items-center justify-between text-xs"
                    >
                      <span className="text-text-secondary">{l.label}</span>
                      <span className="text-loss font-mono">-{l.amount}</span>
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="text-xs text-text-muted">No losses recorded yet</p>
              )}
            </div>
          )}
        </>
      )}
    </div>
  )
}

function MetricCard({
  label,
  value,
  sub,
  positive,
}: {
  label: string
  value: string
  sub?: string
  positive?: boolean
}) {
  return (
    <div className="rounded-md bg-surface-base/50 p-3">
      <p className="text-[10px] uppercase tracking-wider text-text-muted mb-1">
        {label}
      </p>
      <p className="text-sm font-semibold text-text-primary">{value}</p>
      {sub && (
        <p
          className={clsx(
            'text-[10px] mt-0.5',
            positive === true
              ? 'text-profit'
              : positive === false
                ? 'text-loss'
                : 'text-text-muted',
          )}
        >
          {sub}
        </p>
      )}
    </div>
  )
}
