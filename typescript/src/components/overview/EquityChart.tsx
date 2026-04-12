import { useState, useMemo } from 'react'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  ReferenceLine,
} from 'recharts'
import { clsx } from 'clsx'
import { formatCurrency, agentColor, parseDecimal } from '@/lib/format'
import type { EquityCurve } from '@/types/api'

type TimeRange = '1h' | '6h' | '24h' | 'all'

interface EquityChartProps {
  curves: Record<string, EquityCurve>
  agentNames: Record<string, string>
  initialCapital?: number
}

const TIME_RANGES: { label: string; value: TimeRange }[] = [
  { label: '1H', value: '1h' },
  { label: '6H', value: '6h' },
  { label: '24H', value: '24h' },
  { label: 'All', value: 'all' },
]

function filterByTimeRange(
  points: { timestamp: string; portfolio?: { value: string } }[],
  range: TimeRange,
): { timestamp: string; portfolio?: { value: string } }[] {
  if (range === 'all') return points
  const now = Date.now()
  const hours = range === '1h' ? 1 : range === '6h' ? 6 : 24
  const cutoff = now - hours * 3600_000
  return points.filter((p) => new Date(p.timestamp).getTime() >= cutoff)
}

export function EquityChart({
  curves,
  agentNames,
  initialCapital = 10000,
}: EquityChartProps) {
  const [timeRange, setTimeRange] = useState<TimeRange>('all')

  const agentIds = Object.keys(curves)

  // Merge all agent equity curves into a unified timeline
  const chartData = useMemo(() => {
    const timeMap = new Map<number, Record<string, number>>()

    for (const agentId of agentIds) {
      const curve = curves[agentId]
      if (!curve?.equity_curve) continue

      const filtered = filterByTimeRange(curve.equity_curve, timeRange)
      for (const point of filtered) {
        const ts = new Date(point.timestamp).getTime()
        if (!timeMap.has(ts)) timeMap.set(ts, {})
        timeMap.get(ts)![agentId] = parseDecimal(point.portfolio?.value)
      }
    }

    // Sort by time and forward-fill missing values
    const sorted = Array.from(timeMap.entries()).sort((a, b) => a[0] - b[0])
    const lastValues: Record<string, number> = {}

    return sorted.map(([ts, values]) => {
      for (const id of agentIds) {
        if (values[id] !== undefined) {
          lastValues[id] = values[id]
        } else if (lastValues[id] !== undefined) {
          values[id] = lastValues[id]
        }
      }
      return { time: ts, ...values }
    })
  }, [curves, agentIds, timeRange])

  if (agentIds.length === 0) {
    return (
      <div className="rounded-lg border border-surface-border bg-surface-card p-6">
        <h2 className="text-sm font-medium text-text-secondary mb-4">
          Combined Equity
        </h2>
        <div className="h-64 flex items-center justify-center text-text-muted text-sm">
          No equity data yet. Agents will appear once they start trading.
        </div>
      </div>
    )
  }

  return (
    <div className="rounded-lg border border-surface-border bg-surface-card p-6">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-sm font-medium text-text-secondary">
          Combined Equity
        </h2>
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

      <div className="h-64">
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={chartData}>
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
              labelFormatter={(ts: number) => {
                const d = new Date(ts)
                return d.toUTCString()
              }}
              formatter={(value: number, name: string) => [
                formatCurrency(value),
                agentNames[name] || name,
              ]}
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
            {agentIds.map((id, idx) => (
              <Line
                key={id}
                type="monotone"
                dataKey={id}
                stroke={agentColor(idx)}
                strokeWidth={2}
                dot={false}
                name={id}
                connectNulls
              />
            ))}
          </LineChart>
        </ResponsiveContainer>
      </div>

      {/* Legend */}
      <div className="flex items-center gap-4 mt-3">
        {agentIds.map((id, idx) => (
          <div key={id} className="flex items-center gap-1.5 text-xs text-text-secondary">
            <span
              className="inline-block h-2 w-2 rounded-full"
              style={{ backgroundColor: agentColor(idx) }}
            />
            {agentNames[id] || id}
          </div>
        ))}
      </div>
    </div>
  )
}
