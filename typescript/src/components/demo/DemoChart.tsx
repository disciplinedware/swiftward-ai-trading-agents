import { useState, useMemo } from 'react'
import {
  ComposedChart,
  Line,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  ReferenceLine,
} from 'recharts'
import { clsx } from 'clsx'
import { formatCurrency, parseDecimal } from '@/lib/format'
import type { EquityCurve, Trade } from '@/types/api'

type TimeRange = '1h' | '6h' | '24h' | 'all'

interface DemoChartProps {
  withCurve: EquityCurve | undefined
  withoutCurve: EquityCurve | undefined
  withTrades: Trade[]
  withAgentName: string
  withoutAgentName: string
  initialCapital?: number
}

const TIME_RANGES: { label: string; value: TimeRange }[] = [
  { label: '1H', value: '1h' },
  { label: '6H', value: '6h' },
  { label: '24H', value: '24h' },
  { label: 'All', value: 'all' },
]

function timeCutoff(range: TimeRange): number {
  if (range === 'all') return 0
  const hours = range === '1h' ? 1 : range === '6h' ? 6 : 24
  return Date.now() - hours * 3600_000
}

interface ChartPoint {
  time: number
  withEquity?: number
  withoutEquity?: number
  annotation?: string
}

export function DemoChart({
  withCurve,
  withoutCurve,
  withTrades,
  withAgentName,
  withoutAgentName,
  initialCapital = 10_000,
}: DemoChartProps) {
  const [timeRange, setTimeRange] = useState<TimeRange>('all')

  // Build annotation map: timestamps where guardrails saved the agent
  const annotations = useMemo(() => {
    const map = new Map<number, string>()
    for (const t of withTrades) {
      if (t.status === 'reject') {
        const ts = new Date(t.timestamp).getTime()
        // Round to nearest minute to align with chart data
        const rounded = Math.round(ts / 60_000) * 60_000
        const isHalt = t.reject?.reason?.toLowerCase().includes('halt') ?? false
        map.set(rounded, isHalt ? 'Circuit breaker' : 'Policy block')
      }
    }
    return map
  }, [withTrades])

  const chartData = useMemo(() => {
    const cutoff = timeCutoff(timeRange)
    const timeMap = new Map<number, ChartPoint>()

    // Add "with guardrails" data
    if (withCurve?.equity_curve) {
      for (const point of withCurve.equity_curve) {
        const ts = new Date(point.timestamp).getTime()
        if (cutoff > 0 && ts < cutoff) continue
        if (!timeMap.has(ts)) timeMap.set(ts, { time: ts })
        timeMap.get(ts)!.withEquity = parseDecimal(point.portfolio?.value)
      }
    }

    // Add "without guardrails" data
    if (withoutCurve?.equity_curve) {
      for (const point of withoutCurve.equity_curve) {
        const ts = new Date(point.timestamp).getTime()
        if (cutoff > 0 && ts < cutoff) continue
        if (!timeMap.has(ts)) timeMap.set(ts, { time: ts })
        timeMap.get(ts)!.withoutEquity = parseDecimal(point.portfolio?.value)
      }
    }

    // Sort and forward-fill
    const sorted = Array.from(timeMap.values()).sort((a, b) => a.time - b.time)
    let lastWith: number | undefined
    let lastWithout: number | undefined

    for (const point of sorted) {
      if (point.withEquity !== undefined) lastWith = point.withEquity
      else if (lastWith !== undefined) point.withEquity = lastWith

      if (point.withoutEquity !== undefined) lastWithout = point.withoutEquity
      else if (lastWithout !== undefined) point.withoutEquity = lastWithout

      // Check for annotations (within 2 minute window)
      for (const [annoTs, label] of annotations) {
        if (Math.abs(point.time - annoTs) < 120_000) {
          point.annotation = label
          break
        }
      }
    }

    return sorted
  }, [withCurve, withoutCurve, timeRange, annotations])

  const hasData = chartData.length > 0

  // Find annotation points for reference lines
  const annotationPoints = useMemo(
    () => chartData.filter((p) => p.annotation),
    [chartData],
  )

  return (
    <div className="rounded-lg border border-surface-border bg-surface-card p-6">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-sm font-medium text-text-secondary">
          Overlay Equity Comparison
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

      {!hasData ? (
        <div className="h-72 flex items-center justify-center text-text-muted text-sm">
          Select agents above to compare equity curves.
        </div>
      ) : (
        <>
          <div className="h-72">
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
                    const label =
                      name === 'withEquity'
                        ? `${withAgentName} (with)`
                        : `${withoutAgentName} (without)`
                    return [formatCurrency(value), label]
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

                {/* Shaded area between curves - shows "value of guardrails" */}
                <Area
                  type="monotone"
                  dataKey="withEquity"
                  stroke="none"
                  fill="#22c55e"
                  fillOpacity={0.06}
                  connectNulls
                />
                <Area
                  type="monotone"
                  dataKey="withoutEquity"
                  stroke="none"
                  fill="#ef4444"
                  fillOpacity={0.06}
                  connectNulls
                />

                {/* Lines */}
                <Line
                  type="monotone"
                  dataKey="withEquity"
                  stroke="#22c55e"
                  strokeWidth={2.5}
                  dot={false}
                  name="withEquity"
                  connectNulls
                />
                <Line
                  type="monotone"
                  dataKey="withoutEquity"
                  stroke="#ef4444"
                  strokeWidth={2.5}
                  dot={false}
                  name="withoutEquity"
                  connectNulls
                />

                {/* Annotation reference lines for circuit breaker saves */}
                {annotationPoints.map((p, i) => (
                  <ReferenceLine
                    key={i}
                    x={p.time}
                    stroke="#f59e0b"
                    strokeDasharray="3 3"
                    strokeWidth={1}
                    label={{
                      value: p.annotation ?? '',
                      position: 'top',
                      fill: '#f59e0b',
                      fontSize: 9,
                    }}
                  />
                ))}
              </ComposedChart>
            </ResponsiveContainer>
          </div>

          {/* Legend */}
          <div className="flex items-center gap-6 mt-3">
            <div className="flex items-center gap-1.5 text-xs text-text-secondary">
              <span className="inline-block h-2 w-6 rounded-sm bg-profit" />
              {withAgentName} (with guardrails)
            </div>
            <div className="flex items-center gap-1.5 text-xs text-text-secondary">
              <span className="inline-block h-2 w-6 rounded-sm bg-loss" />
              {withoutAgentName} (without guardrails)
            </div>
            {annotationPoints.length > 0 && (
              <div className="flex items-center gap-1.5 text-xs text-text-secondary">
                <span className="inline-block h-0 w-6 border-t border-dashed border-warning" />
                Guardrail intervention
              </div>
            )}
          </div>
        </>
      )}
    </div>
  )
}
