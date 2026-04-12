import { useState } from 'react'
import { clsx } from 'clsx'
import {
  AlertTriangle,
  ShieldOff,
  ShieldCheck,
  Pause,
  Play,
  Clock,
  Activity,
  TrendingDown,
  BarChart3,
  Zap,
  Target,
} from 'lucide-react'
import toast from 'react-hot-toast'
import { useLimits, useTradeHistory } from '@/hooks/use-trading'
import { useAgentStatus, useHaltAgent, useResumeAgent } from '@/hooks/use-risk'
import { formatPercent } from '@/lib/format'

interface LimitsTabProps {
  agentId: string
}

// --- Policy limits (from rules.yaml) ---

const POSITION_LIMIT_PCT = 15
const CONCENTRATION_LIMIT_PCT = 50
const DAILY_DRAWDOWN_LIMIT = 5
const PEAK_DRAWDOWN_LIMIT = 10

function countRecentTrades(trades: { timestamp: string }[] | undefined, windowMinutes: number): number {
  if (!trades?.length) return 0
  const cutoff = Date.now() - windowMinutes * 60_000
  return trades.filter((t) => new Date(t.timestamp).getTime() >= cutoff).length
}

// --- RiskGauge component ---

interface GaugeConfig {
  icon: React.ComponentType<{ size: number; className?: string }>
  label: string
  currentValue: number
  limitValue: number
  unit: string
  detail?: string
  invert?: boolean // true = higher is worse (drawdown)
}

function gaugeColor(currentValue: number, limitValue: number): string {
  const ratio = Math.abs(currentValue) / limitValue
  if (ratio >= 1) return 'text-loss'
  if (ratio >= 0.8) return 'text-loss'
  if (ratio >= 0.5) return 'text-warning'
  return 'text-profit'
}

function gaugeBarColor(currentValue: number, limitValue: number): string {
  const ratio = Math.abs(currentValue) / limitValue
  if (ratio >= 1) return 'bg-loss'
  if (ratio >= 0.8) return 'bg-loss/80'
  if (ratio >= 0.5) return 'bg-warning/80'
  return 'bg-profit/60'
}

function gaugeStatusLabel(currentValue: number, limitValue: number): string {
  const ratio = Math.abs(currentValue) / limitValue
  if (ratio >= 1) return 'BREACHED'
  if (ratio >= 0.8) return 'DANGER'
  if (ratio >= 0.5) return 'CAUTION'
  return 'OK'
}

function gaugeStatusColor(currentValue: number, limitValue: number): string {
  const ratio = Math.abs(currentValue) / limitValue
  if (ratio >= 1) return 'bg-loss/15 text-loss'
  if (ratio >= 0.8) return 'bg-loss/15 text-loss'
  if (ratio >= 0.5) return 'bg-warning/15 text-warning'
  return 'bg-profit/15 text-profit'
}

function RiskGauge({ icon: Icon, label, currentValue, limitValue, unit, detail, invert }: GaugeConfig) {
  const displayValue = invert ? Math.abs(currentValue) : currentValue
  const ratio = Math.min(displayValue / limitValue, 1.2) // cap at 120% for bar
  const barWidth = Math.min(ratio * 100, 100)
  const isBreached = displayValue >= limitValue

  return (
    <div className={clsx(
      'rounded-lg border bg-surface-card p-4',
      isBreached ? 'border-loss/40' : 'border-surface-border',
    )}>
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <Icon size={14} className="text-text-muted" />
          <span className="text-xs font-medium text-text-muted uppercase tracking-wide">{label}</span>
        </div>
        <span className={clsx(
          'inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium',
          gaugeStatusColor(displayValue, limitValue),
        )}>
          {gaugeStatusLabel(displayValue, limitValue)}
        </span>
      </div>

      <div className="flex items-baseline gap-1 mb-1">
        <span className={clsx('text-xl font-semibold font-mono', gaugeColor(displayValue, limitValue))}>
          {invert ? `-${displayValue.toFixed(1)}` : displayValue.toFixed(1)}{unit}
        </span>
        <span className="text-xs text-text-muted">
          / {limitValue}{unit} max
        </span>
      </div>

      {detail && (
        <div className="text-xs text-text-muted mb-2">{detail}</div>
      )}

      {/* Gauge bar */}
      <div className="relative h-2 bg-surface-hover rounded-full overflow-hidden">
        <div
          className={clsx(
            'absolute left-0 top-0 h-full rounded-full transition-all duration-700',
            gaugeBarColor(displayValue, limitValue),
            isBreached && 'animate-pulse',
          )}
          style={{ width: `${barWidth}%` }}
        />
      </div>
    </div>
  )
}

// --- CircuitBreakerPanel ---

interface CircuitBreakerPanelProps {
  isHalted: boolean
  drawdownPct: number
}

function CircuitBreakerPanel({ isHalted, drawdownPct }: CircuitBreakerPanelProps) {
  // We don't have circuit breaker cooldown timing from the API, so we show status based on halt state
  const isActive = isHalted && Math.abs(drawdownPct) >= DAILY_DRAWDOWN_LIMIT

  return (
    <div className={clsx(
      'rounded-lg border bg-surface-card p-5',
      isActive ? 'border-warning/40' : 'border-surface-border',
    )}>
      <div className="flex items-center gap-2 mb-4">
        {isActive ? (
          <AlertTriangle size={16} className="text-warning animate-pulse" />
        ) : (
          <ShieldCheck size={16} className="text-profit" />
        )}
        <h3 className="text-sm font-medium text-text-primary">Circuit Breaker</h3>
      </div>

      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <span className="text-xs text-text-secondary">Status</span>
          <span className={clsx(
            'inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium',
            isActive ? 'bg-warning/15 text-warning' : 'bg-profit/15 text-profit',
          )}>
            {isActive ? (
              <>
                <Activity size={10} className="animate-pulse" />
                ACTIVE - COOLDOWN
              </>
            ) : (
              'INACTIVE'
            )}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-xs text-text-secondary">Trigger threshold</span>
          <span className="text-xs font-mono text-text-primary">Daily loss &gt; {DAILY_DRAWDOWN_LIMIT}%</span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-xs text-text-secondary">Current daily drawdown</span>
          <span className={clsx(
            'text-xs font-mono',
            Math.abs(drawdownPct) >= DAILY_DRAWDOWN_LIMIT ? 'text-loss' : 'text-text-primary',
          )}>
            {formatPercent(-Math.abs(drawdownPct))}
          </span>
        </div>

        {isActive && (
          <div className="mt-2 p-3 rounded-md bg-warning/5 border border-warning/20">
            <div className="flex items-center gap-1.5 text-xs text-warning">
              <Clock size={12} />
              <span>Circuit breaker triggered due to daily drawdown exceeding {DAILY_DRAWDOWN_LIMIT}% threshold. Trading is paused.</span>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

// --- HaltControl ---

interface HaltControlProps {
  agentId: string
  isHalted: boolean
}

function HaltControl({ agentId, isHalted }: HaltControlProps) {
  const [showConfirm, setShowConfirm] = useState(false)
  const haltMutation = useHaltAgent()
  const resumeMutation = useResumeAgent()

  const handleHalt = () => {
    haltMutation.mutate(agentId, {
      onSuccess: () => {
        toast.success(`Agent ${agentId} halted`)
        setShowConfirm(false)
      },
      onError: (err) => toast.error(`Failed to halt: ${err.message}`),
    })
  }

  const handleResume = () => {
    resumeMutation.mutate(agentId, {
      onSuccess: () => toast.success(`Agent ${agentId} resumed`),
      onError: (err) => toast.error(`Failed to resume: ${err.message}`),
    })
  }

  return (
    <div className="rounded-lg border border-surface-border bg-surface-card p-5">
      <div className="flex items-center gap-2 mb-4">
        {isHalted ? (
          <ShieldOff size={16} className="text-loss" />
        ) : (
          <ShieldCheck size={16} className="text-profit" />
        )}
        <h3 className="text-sm font-medium text-text-primary">Agent Halt Control</h3>
      </div>

      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <span className="text-xs text-text-secondary">Current State</span>
          <span className={clsx(
            'inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium',
            isHalted ? 'bg-loss/15 text-loss' : 'bg-profit/15 text-profit',
          )}>
            <span className={clsx(
              'inline-block h-2 w-2 rounded-full',
              isHalted ? 'bg-loss animate-pulse' : 'bg-profit',
            )} />
            {isHalted ? 'HALTED' : 'ACTIVE'}
          </span>
        </div>

        <div className="pt-2">
          {isHalted ? (
            <button
              className="w-full inline-flex items-center justify-center gap-2 rounded-md bg-profit/15 px-4 py-2.5 text-sm font-medium text-profit hover:bg-profit/25 transition-colors disabled:opacity-50"
              onClick={handleResume}
              disabled={resumeMutation.isPending}
            >
              <Play size={14} />
              Resume Trading
            </button>
          ) : showConfirm ? (
            <div className="space-y-2">
              <div className="p-3 rounded-md bg-loss/5 border border-loss/20">
                <p className="text-xs text-loss">
                  Are you sure? This will immediately stop all trading for agent {agentId}.
                </p>
              </div>
              <div className="flex gap-2">
                <button
                  className="flex-1 inline-flex items-center justify-center gap-1.5 rounded-md bg-loss/15 px-3 py-2 text-sm font-medium text-loss hover:bg-loss/25 transition-colors disabled:opacity-50"
                  onClick={handleHalt}
                  disabled={haltMutation.isPending}
                >
                  <Pause size={14} />
                  Confirm Halt
                </button>
                <button
                  className="flex-1 rounded-md bg-surface-hover px-3 py-2 text-sm font-medium text-text-secondary hover:text-text-primary transition-colors"
                  onClick={() => setShowConfirm(false)}
                >
                  Cancel
                </button>
              </div>
            </div>
          ) : (
            <button
              className="w-full inline-flex items-center justify-center gap-2 rounded-md bg-loss/15 px-4 py-2.5 text-sm font-medium text-loss hover:bg-loss/25 transition-colors"
              onClick={() => setShowConfirm(true)}
            >
              <Pause size={14} />
              Halt Agent
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

// --- HaltHistory ---

interface HaltHistoryProps {
  agentId: string
}

function HaltHistory({ agentId }: HaltHistoryProps) {
  const { data: trades } = useTradeHistory(agentId, 500)

  // Extract halt-related events from trade history
  const haltEvents = (trades?.trades ?? [])
    .filter((t) => t.status === 'reject')
    .slice(0, 10)

  return (
    <div className="rounded-lg border border-surface-border bg-surface-card p-5">
      <h3 className="text-sm font-medium text-text-primary mb-3">Halt History</h3>
      {haltEvents.length === 0 ? (
        <p className="text-xs text-text-muted">No halt events recorded.</p>
      ) : (
        <div className="space-y-2">
          {haltEvents.map((event, i) => (
            <div
              key={`${event.timestamp}-${i}`}
              className="flex items-center gap-3 py-2 border-b border-surface-border last:border-0"
            >
              <span className="text-xs text-text-muted font-mono w-20">
                {new Date(event.timestamp).toLocaleTimeString('en-US', {
                  hour: '2-digit',
                  minute: '2-digit',
                  hour12: false,
                  timeZone: 'UTC',
                })} UTC
              </span>
              <span className="inline-flex items-center gap-1 text-xs text-warning">
                <AlertTriangle size={10} />
                HALTED
              </span>
              <span className="text-xs text-text-secondary">
                {event.pair} {event.side} - rejected
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// --- Main LimitsTab ---

export function LimitsTab({ agentId }: LimitsTabProps) {
  const { data: limits } = useLimits(agentId)
  const { data: status } = useAgentStatus(agentId)
  const { data: trades } = useTradeHistory(agentId, 200)

  const isHalted = limits?.halted ?? status?.halted ?? false
  // Compute drawdown from portfolio values (no drawdown_pct field)
  const portfolioValue = parseFloat(limits?.portfolio?.value || '0')
  const portfolioPeak = parseFloat(limits?.portfolio?.peak || '0')
  const drawdownPct = portfolioPeak > 0 ? ((portfolioValue - portfolioPeak) / portfolioPeak) * 100 : 0
  const positionPct = limits?.largest_position_pct ?? 0
  const positionPair = limits?.largest_position_pair ?? '-'
  // Use largest position % - matches the per-asset concentration rule
  const concentrationPct = positionPct

  // Velocity: count trades in the last 10 minutes (matches policy rule: 50/10min)
  const recentTradeCount = countRecentTrades(trades?.trades, 10)

  const gauges: GaugeConfig[] = [
    {
      icon: Target,
      label: 'Position Limit',
      currentValue: positionPct,
      limitValue: POSITION_LIMIT_PCT,
      unit: '%',
      detail: positionPair !== '-' ? positionPair : undefined,
    },
    {
      icon: BarChart3,
      label: 'Concentration',
      currentValue: concentrationPct,
      limitValue: CONCENTRATION_LIMIT_PCT,
      unit: '%',
      detail: positionPair !== '-' ? `Largest position: ${positionPair}` : 'Largest single-asset exposure',
    },
    {
      icon: Zap,
      label: 'Velocity (10min)',
      currentValue: recentTradeCount,
      limitValue: 50,
      unit: '',
      detail: `${recentTradeCount} trades in last 10 min (limit: 50)`,
    },
    {
      icon: TrendingDown,
      label: 'Peak Drawdown',
      currentValue: Math.abs(drawdownPct),
      limitValue: PEAK_DRAWDOWN_LIMIT,
      unit: '%',
      detail: 'From all-time high',
      invert: true,
    },
  ]

  return (
    <div className="space-y-6">
      {/* Risk Gauges Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {gauges.map((gauge) => (
          <RiskGauge key={gauge.label} {...gauge} />
        ))}
      </div>

      {/* Circuit Breaker + Halt Control */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <CircuitBreakerPanel isHalted={isHalted} drawdownPct={drawdownPct} />
        <HaltControl agentId={agentId} isHalted={isHalted} />
      </div>

      {/* Halt History */}
      <HaltHistory agentId={agentId} />
    </div>
  )
}
