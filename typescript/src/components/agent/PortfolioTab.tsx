import { useState, useMemo } from 'react'
import {
  Line,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  ReferenceLine,
  Area,
  ComposedChart,
} from 'recharts'
import { clsx } from 'clsx'
import { DollarSign, Wallet, TrendingDown, Target, Receipt } from 'lucide-react'
import { usePortfolio, usePortfolioHistory, useTradeHistory } from '@/hooks/use-trading'
import { formatCurrency, formatPercent, formatPrice, formatQty, parseDecimal } from '@/lib/format'

interface PortfolioTabProps {
  agentId: string
  initialCapital?: number
}

type TimeRange = '1h' | '6h' | '24h' | 'all'

const TIME_RANGES: { label: string; value: TimeRange }[] = [
  { label: '1H', value: '1h' },
  { label: '6H', value: '6h' },
  { label: '24H', value: '24h' },
  { label: 'All', value: 'all' },
]

function MetricCard({
  icon: Icon,
  label,
  value,
  subtitle,
  valueColor,
}: {
  icon: React.ComponentType<{ size: number; className?: string }>
  label: string
  value: string
  subtitle?: string
  valueColor?: string
}) {
  return (
    <div className="rounded-lg border border-surface-border bg-surface-card p-4">
      <div className="flex items-center gap-2 mb-2">
        <Icon size={14} className="text-text-muted" />
        <span className="text-xs font-medium text-text-muted uppercase tracking-wide">{label}</span>
      </div>
      <div className={clsx('text-lg font-semibold font-mono', valueColor || 'text-text-primary')}>
        {value}
      </div>
      {subtitle && (
        <div className="text-xs text-text-muted mt-0.5">{subtitle}</div>
      )}
    </div>
  )
}

function filterByTimeRange<T extends { timestamp: string }>(
  points: T[],
  range: TimeRange,
): T[] {
  if (range === 'all') return points
  const now = Date.now()
  const hours = range === '1h' ? 1 : range === '6h' ? 6 : 24
  const cutoff = now - hours * 3600_000
  return points.filter((p) => new Date(p.timestamp).getTime() >= cutoff)
}

export function PortfolioTab({ agentId, initialCapital = 10000 }: PortfolioTabProps) {
  const { data: portfolio } = usePortfolio(agentId)
  const { data: history } = usePortfolioHistory(agentId)
  const { data: trades } = useTradeHistory(agentId, 500)
  const [timeRange, setTimeRange] = useState<TimeRange>('all')

  // Compute win rate from trade history
  const { wins, losses, winRate } = useMemo(() => {
    if (!trades?.trades) return { wins: 0, losses: 0, winRate: 0 }
    const fills = trades.trades.filter((t) => t.status === 'fill' && t.pnl_value !== undefined)
    const w = fills.filter((t) => parseDecimal(t.pnl_value) > 0).length
    const l = fills.filter((t) => parseDecimal(t.pnl_value) <= 0).length
    return { wins: w, losses: l, winRate: w + l > 0 ? (w / (w + l)) * 100 : 0 }
  }, [trades])

  const equity = parseDecimal(portfolio?.portfolio?.value)
  const cash = parseDecimal(portfolio?.portfolio?.cash)
  const peak = parseDecimal(portfolio?.portfolio?.peak)
  const totalFees = parseDecimal(portfolio?.total_fees)
  const drawdownPct = peak > 0 ? ((equity - peak) / peak) * 100 : 0
  const cashPct = equity > 0 ? (cash / equity) * 100 : 0

  // Chart data
  const chartData = useMemo(() => {
    if (!history?.equity_curve) return []
    const filtered = filterByTimeRange(history.equity_curve, timeRange)

    let runningPeak = initialCapital
    return filtered.map((point) => {
      const eq = parseDecimal(point.portfolio?.value)
      if (eq > runningPeak) runningPeak = eq
      return {
        time: new Date(point.timestamp).getTime(),
        equity: eq,
        peak: runningPeak,
        drawdown: eq < runningPeak ? eq : undefined,
      }
    })
  }, [history, timeRange, initialCapital])

  // Positions with computed portfolio %
  const positions = useMemo(() => {
    if (!portfolio?.positions) return []
    return portfolio.positions.map((pos) => {
      const posValue = parseDecimal(pos.value)
      const pct = equity > 0 ? (posValue / equity) * 100 : 0
      return { ...pos, portfolioPct: pct }
    })
  }, [portfolio, equity])

  // Concentration data
  const concentrationData = useMemo(() => {
    const items = positions.map((pos) => ({
      pair: pos.pair,
      pct: pos.portfolioPct,
    }))
    const totalPositions = items.reduce((sum, item) => sum + item.pct, 0)
    items.push({ pair: 'Cash', pct: Math.max(0, 100 - totalPositions) })
    return items
  }, [positions])

  return (
    <div className="space-y-6">
      {/* Metric Cards */}
      <div className="grid grid-cols-2 lg:grid-cols-5 gap-4">
        <MetricCard
          icon={DollarSign}
          label="Equity"
          value={formatCurrency(equity)}
          subtitle={`peak: ${formatCurrency(peak)}`}
          valueColor={equity >= initialCapital ? 'text-profit' : 'text-loss'}
        />
        <MetricCard
          icon={Wallet}
          label="Cash"
          value={formatCurrency(cash)}
          subtitle={`${cashPct.toFixed(0)}% of equity`}
        />
        <MetricCard
          icon={TrendingDown}
          label="Drawdown"
          value={formatPercent(drawdownPct)}
          subtitle={drawdownPct < 0 ? `${formatCurrency(equity - peak)} from peak` : 'at peak'}
          valueColor={drawdownPct < -5 ? 'text-loss' : drawdownPct < -2 ? 'text-warning' : 'text-text-primary'}
        />
        <MetricCard
          icon={Target}
          label="Win Rate"
          value={`${winRate.toFixed(0)}%`}
          subtitle={`${wins}W / ${losses}L`}
          valueColor={wins + losses === 0 ? undefined : winRate >= 50 ? 'text-profit' : 'text-loss'}
        />
        <MetricCard
          icon={Receipt}
          label="Fees Paid"
          value={formatCurrency(totalFees)}
          subtitle={`${portfolio?.fill_count ?? 0} fills`}
          valueColor="text-text-secondary"
        />
      </div>

      {/* Equity Curve Chart */}
      <div className="rounded-lg border border-surface-border bg-surface-card p-6">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-sm font-medium text-text-secondary">Equity Curve</h2>
          <div className="flex gap-1">
            {TIME_RANGES.map((r) => (
              <button
                key={r.value}
                className={clsx(
                  'rounded px-2.5 py-1 text-xs font-medium transition-colors',
                  timeRange === r.value
                    ? 'bg-accent/15 text-accent'
                    : 'text-text-muted hover:text-text-secondary hover:bg-surface-hover',
                )}
                onClick={() => setTimeRange(r.value)}
              >
                {r.label}
              </button>
            ))}
          </div>
        </div>

        <div className="h-72">
          {chartData.length === 0 ? (
            <div className="h-full flex items-center justify-center text-text-muted text-sm">
              No equity data yet.
            </div>
          ) : (
            <ResponsiveContainer width="100%" height="100%">
              <ComposedChart data={chartData}>
                <XAxis
                  dataKey="time"
                  type="number"
                  domain={['dataMin', 'dataMax']}
                  tickFormatter={(ts: number) => {
                    const d = new Date(ts)
                    return `${d.getUTCHours().toString().padStart(2, '0')}:${d.getUTCMinutes().toString().padStart(2, '0')}`
                  }}
                  tick={{ fill: '#5c5f6e', fontSize: 11 }}
                  axisLine={{ stroke: '#2a2d36' }}
                  tickLine={false}
                />
                <YAxis
                  tickFormatter={(v: number) => formatCurrency(v, true)}
                  tick={{ fill: '#5c5f6e', fontSize: 11 }}
                  axisLine={false}
                  tickLine={false}
                  width={70}
                />
                <Tooltip
                  contentStyle={{
                    backgroundColor: '#1a1d26',
                    border: '1px solid #2a2d36',
                    borderRadius: 8,
                    fontSize: 12,
                  }}
                  labelFormatter={(ts: number) => new Date(ts).toUTCString()}
                  formatter={(value: number, name: string) => {
                    if (name === 'peak') return [formatCurrency(value), 'Peak']
                    if (name === 'drawdown') return [formatCurrency(value), 'Drawdown']
                    return [formatCurrency(value), 'Equity']
                  }}
                />
                <ReferenceLine
                  y={initialCapital}
                  stroke="#2a2d36"
                  strokeDasharray="4 4"
                  label={{
                    value: 'Initial',
                    position: 'right',
                    fill: '#5c5f6e',
                    fontSize: 10,
                  }}
                />
                {/* Drawdown shaded area */}
                <Area
                  type="monotone"
                  dataKey="drawdown"
                  fill="#ef4444"
                  fillOpacity={0.1}
                  stroke="none"
                  connectNulls={false}
                />
                {/* Peak line (dashed) */}
                <Line
                  type="monotone"
                  dataKey="peak"
                  stroke="#2a2d36"
                  strokeWidth={1}
                  strokeDasharray="3 3"
                  dot={false}
                  name="peak"
                />
                {/* Main equity line */}
                <Line
                  type="monotone"
                  dataKey="equity"
                  stroke="#6366f1"
                  strokeWidth={2}
                  dot={false}
                  name="equity"
                />
              </ComposedChart>
            </ResponsiveContainer>
          )}
        </div>
      </div>

      {/* Current Positions */}
      <div className="rounded-lg border border-surface-border bg-surface-card overflow-hidden">
        <div className="px-6 py-4 border-b border-surface-border">
          <h2 className="text-sm font-medium text-text-secondary">Current Positions</h2>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-surface-border">
                {['Pair', 'Side', 'Qty', 'Avg Price', 'Current Price', 'Unr. P&L', 'Mkt Value', 'SL', 'TP', '% of Portfolio'].map((h) => (
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
              {positions.length === 0 && (
                <tr>
                  <td colSpan={10} className="px-4 py-6 text-center text-text-muted text-sm">
                    No open positions.
                  </td>
                </tr>
              )}
              {positions.map((pos) => {
                const hasUnrPnl = pos.unrealized_pnl != null
                const unrealizedPnl = hasUnrPnl ? parseDecimal(pos.unrealized_pnl) : 0
                const unrealizedPnlPct = hasUnrPnl && pos.unrealized_pnl_pct != null ? parseDecimal(pos.unrealized_pnl_pct) : 0
                const curPrice = pos.current_price ? parseDecimal(pos.current_price) : 0
                const slPrice = pos.stop_loss ? parseDecimal(pos.stop_loss) : 0
                const tpPrice = pos.take_profit ? parseDecimal(pos.take_profit) : 0
                const slDist = curPrice > 0 && slPrice > 0 ? ((slPrice - curPrice) / curPrice) * 100 : null
                const tpDist = curPrice > 0 && tpPrice > 0 ? ((tpPrice - curPrice) / curPrice) * 100 : null
                return (
                  <tr
                    key={pos.pair}
                    className="border-b border-surface-border last:border-0"
                  >
                    <td className="px-4 py-3 text-text-primary font-medium">{pos.pair}</td>
                    <td className={clsx(
                      'px-4 py-3 font-medium uppercase text-xs',
                      pos.side === 'long' ? 'text-profit' : 'text-loss',
                    )}>
                      {pos.side === 'long' ? 'Long' : 'Short'}
                    </td>
                    <td className="px-4 py-3 text-text-primary font-mono">{formatQty(pos.qty)}</td>
                    <td className="px-4 py-3 text-text-muted font-mono text-xs">{formatPrice(pos.avg_price)}</td>
                    <td className="px-4 py-3 text-text-primary font-mono">
                      {pos.current_price ? formatPrice(pos.current_price) : '-'}
                    </td>
                    <td className={clsx(
                      'px-4 py-3 font-mono',
                      !hasUnrPnl ? 'text-text-muted' : unrealizedPnl >= 0 ? 'text-profit' : 'text-loss',
                    )}>
                      {!hasUnrPnl ? '-' : (
                        <>
                          {unrealizedPnl >= 0 ? '+' : ''}{formatCurrency(unrealizedPnl)} ({unrealizedPnlPct >= 0 ? '+' : ''}{unrealizedPnlPct.toFixed(1)}%)
                        </>
                      )}
                    </td>
                    <td className="px-4 py-3 text-text-primary font-mono">{formatCurrency(pos.value)}</td>
                    <td className="px-4 py-3 text-text-muted font-mono text-xs">
                      {slPrice > 0 ? (
                        <>
                          {formatPrice(slPrice)}
                          {slDist !== null && <span className="text-text-muted"> ({slDist >= 0 ? '+' : ''}{slDist.toFixed(1)}%)</span>}
                        </>
                      ) : '-'}
                    </td>
                    <td className="px-4 py-3 text-text-muted font-mono text-xs">
                      {tpPrice > 0 ? (
                        <>
                          {formatPrice(tpPrice)}
                          {tpDist !== null && <span className="text-text-muted"> ({tpDist >= 0 ? '+' : ''}{tpDist.toFixed(1)}%)</span>}
                        </>
                      ) : '-'}
                    </td>
                    <td className={clsx(
                      'px-4 py-3 font-mono',
                      pos.portfolioPct > 15 ? 'text-loss' : pos.portfolioPct > 10 ? 'text-warning' : 'text-text-secondary',
                    )}>
                      {pos.portfolioPct.toFixed(1)}%
                    </td>
                  </tr>
                )
              })}
              {positions.length > 0 && positions.some((p) => p.unrealized_pnl != null) && (() => {
                const total = positions.reduce((sum, p) => sum + (p.unrealized_pnl != null ? parseDecimal(p.unrealized_pnl) : 0), 0)
                return (
                  <tr className="border-t-2 border-surface-border bg-surface-hover/30">
                    <td colSpan={5} className="px-4 py-2 text-xs text-text-muted text-right">Total Unrealized P&L</td>
                    <td className={clsx('px-4 py-2 font-mono font-medium', total >= 0 ? 'text-profit' : 'text-loss')}>
                      {total >= 0 ? '+' : ''}{formatCurrency(total)}
                    </td>
                    <td colSpan={4} />
                  </tr>
                )
              })()}
            </tbody>
          </table>
        </div>
      </div>

      {/* Concentration Bar Chart */}
      <div className="rounded-lg border border-surface-border bg-surface-card p-6">
        <h2 className="text-sm font-medium text-text-secondary mb-4">Asset Concentration</h2>
        <div className="space-y-3">
          {concentrationData.map((item) => (
            <div key={item.pair} className="flex items-center gap-3">
              <span className="text-xs text-text-secondary w-20 text-right font-mono">
                {item.pair}
              </span>
              <div className="flex-1 relative h-5 bg-surface-hover rounded overflow-hidden">
                <div
                  className={clsx(
                    'absolute left-0 top-0 h-full rounded transition-all duration-500',
                    item.pair === 'Cash'
                      ? 'bg-accent/30'
                      : item.pct > 15
                        ? 'bg-loss/60'
                        : item.pct > 10
                          ? 'bg-warning/60'
                          : 'bg-accent/60',
                  )}
                  style={{ width: `${Math.min(item.pct, 100)}%` }}
                />
                {/* 50% concentration limit line */}
                <div
                  className="absolute top-0 h-full w-px bg-loss/50"
                  style={{ left: '50%' }}
                />
              </div>
              <span className="text-xs text-text-muted w-14 text-right font-mono">
                {item.pct.toFixed(1)}%
              </span>
            </div>
          ))}
        </div>
        <div className="flex items-center gap-2 mt-3 text-xs text-text-muted">
          <span className="inline-block h-px w-4 bg-loss/50" />
          50% concentration limit
        </div>
      </div>
    </div>
  )
}
