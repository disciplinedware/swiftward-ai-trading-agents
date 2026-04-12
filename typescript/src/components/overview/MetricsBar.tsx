import { clsx } from 'clsx'
import {
  Users,
  DollarSign,
  TrendingDown,
  Activity,
  ShieldAlert,
  Shield,
} from 'lucide-react'
import { formatCurrency, formatPercent } from '@/lib/format'
import { V1_RULES } from '@/lib/policy-rules'
import type { Limits } from '@/types/api'

interface MetricsBarProps {
  agentCount: number
  haltedCount: number
  allLimits: (Limits | undefined)[]
  agentIds: string[]
}

interface MetricCardProps {
  label: string
  value: string
  subtitle?: string
  icon: React.ReactNode
  color?: 'default' | 'profit' | 'loss' | 'warning'
}

function MetricCard({ label, value, subtitle, icon, color = 'default' }: MetricCardProps) {
  const valueColor = {
    default: 'text-text-primary',
    profit: 'text-profit',
    loss: 'text-loss',
    warning: 'text-warning',
  }[color]

  return (
    <div className="rounded-lg border border-surface-border bg-surface-card p-4">
      <div className="flex items-center justify-between mb-2">
        <p className="text-xs text-text-secondary uppercase tracking-wide">{label}</p>
        <div className="text-text-muted">{icon}</div>
      </div>
      <p className={clsx('text-lg font-semibold', valueColor)}>{value}</p>
      {subtitle && (
        <p className="text-xs text-text-muted mt-0.5">{subtitle}</p>
      )}
    </div>
  )
}

export function MetricsBar({ agentCount, haltedCount, allLimits, agentIds }: MetricsBarProps) {
  const activeLimits = allLimits.filter((l): l is Limits => l !== undefined)

  const totalEquity = activeLimits.reduce(
    (sum, l) => sum + parseFloat(l.portfolio?.value || '0'),
    0,
  )

  const totalTrades = activeLimits.reduce((sum, l) => sum + (l.fill_count || 0), 0)
  const totalRejected = activeLimits.reduce((sum, l) => sum + (l.reject_count || 0), 0)
  const rejectionRate = totalTrades + totalRejected > 0
    ? (totalRejected / (totalTrades + totalRejected)) * 100
    : 0

  // Worst drawdown across agents (computed from portfolio value/peak)
  let worstDrawdown = 0
  let worstDrawdownAgent = ''
  for (let i = 0; i < allLimits.length; i++) {
    const l = allLimits[i]
    if (l) {
      const val = parseFloat(l.portfolio?.value || '0')
      const peak = parseFloat(l.portfolio?.peak || '0')
      const dd = peak > 0 ? ((val - peak) / peak) * 100 : 0
      if (dd < worstDrawdown) {
        worstDrawdown = dd
        worstDrawdownAgent = agentIds[i] ? ` (${agentIds[i]})` : ''
      }
    }
  }

  const anyHalted = haltedCount > 0
  const activeCount = agentCount - haltedCount

  const rejectionColor: 'profit' | 'warning' | 'loss' =
    rejectionRate < 10 ? 'profit' : rejectionRate < 25 ? 'warning' : 'loss'

  return (
    <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4 mb-6">
      <MetricCard
        label="Agents Active"
        value={String(activeCount)}
        subtitle={haltedCount > 0 ? `${haltedCount} halted` : 'none halted'}
        icon={<Users size={16} />}
        color={anyHalted ? 'warning' : 'default'}
      />
      <MetricCard
        label="Total Equity"
        value={formatCurrency(totalEquity, true)}
        icon={<DollarSign size={16} />}
      />
      <MetricCard
        label="Total Drawdown"
        value={worstDrawdown < 0 ? formatPercent(worstDrawdown) : '0%'}
        subtitle={worstDrawdownAgent ? `worst agent${worstDrawdownAgent}` : 'from ATH'}
        icon={<TrendingDown size={16} />}
        color={worstDrawdown < -5 ? 'loss' : worstDrawdown < -2 ? 'warning' : 'default'}
      />
      <MetricCard
        label="Trades"
        value={String(totalTrades)}
        icon={<Activity size={16} />}
      />
      <MetricCard
        label="Rejected"
        value={totalRejected > 0 ? `${totalRejected} (${rejectionRate.toFixed(0)}%)` : '0'}
        icon={<ShieldAlert size={16} />}
        color={rejectionColor}
      />
      <MetricCard
        label="Policy Status"
        value={anyHalted ? 'CIRCUIT BREAKER' : `${V1_RULES.length} RULES`}
        subtitle={anyHalted ? `${haltedCount} agent(s) halted` : 'ACTIVE'}
        icon={<Shield size={16} />}
        color={anyHalted ? 'loss' : 'profit'}
      />
    </div>
  )
}
