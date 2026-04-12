// Formatting utilities for the trading dashboard.
// All monetary values from the API are decimal strings.

export function formatCurrency(value: string | number | undefined, compact = false): string {
  if (value === undefined || value === '') return '-'
  const num = typeof value === 'string' ? parseFloat(value) : value
  if (isNaN(num)) return '-'

  if (compact && Math.abs(num) >= 1_000_000) {
    const sign = num < 0 ? '-' : ''
    return sign + '$' + (Math.abs(num) / 1_000_000).toFixed(1) + 'M'
  }
  if (compact && Math.abs(num) >= 10_000) {
    const sign = num < 0 ? '-' : ''
    return sign + '$' + (Math.abs(num) / 1_000).toFixed(1) + 'K'
  }

  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: Math.abs(num) < 10 ? 2 : 0,
    maximumFractionDigits: Math.abs(num) < 10 ? 2 : 0,
  }).format(num)
}

// formatPrice formats an asset price with decimals scaled to its magnitude.
// Use for prices, not USD totals: BTC at 65000 → $65,000; ARB at 0.10 → $0.1023.
export function formatPrice(value: string | number | undefined): string {
  if (value === undefined || value === '') return '-'
  const num = typeof value === 'string' ? parseFloat(value) : value
  if (isNaN(num)) return '-'
  const abs = Math.abs(num)
  let decimals: number
  if (abs >= 1000) decimals = 2
  else if (abs >= 1) decimals = 4
  else if (abs >= 0.01) decimals = 4
  else if (abs >= 0.0001) decimals = 6
  else decimals = 8
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  }).format(num)
}

export function formatPercent(value: number | undefined, decimals = 1): string {
  if (value === undefined) return '-'
  const sign = value > 0 ? '+' : ''
  return `${sign}${value.toFixed(decimals)}%`
}

export function formatPnl(current: string | undefined, initial: number): string {
  if (current === undefined || current === '') return '-'
  const num = parseFloat(current)
  if (isNaN(num)) return '-'
  const diff = num - initial
  const pct = initial > 0 ? (diff / initial) * 100 : 0
  const sign = diff >= 0 ? '+' : ''
  return `${sign}${formatCurrency(diff)} (${sign}${pct.toFixed(1)}%)`
}

export function formatQty(value: string | undefined): string {
  if (value === undefined || value === '') return '-'
  const num = parseFloat(value)
  if (isNaN(num)) return '-'
  if (num >= 1) return num.toFixed(4)
  if (num >= 0.01) return num.toFixed(6)
  return num.toFixed(8)
}

export function formatTime(timestamp: string | undefined): string {
  if (!timestamp) return '-'
  const d = new Date(timestamp)
  if (isNaN(d.getTime())) return '-'
  return d.toLocaleTimeString('en-US', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
    timeZone: 'UTC',
  }) + ' UTC'
}

export function formatTimeShort(timestamp: string | undefined): string {
  if (!timestamp) return '-'
  const d = new Date(timestamp)
  if (isNaN(d.getTime())) return '-'
  return d.toLocaleTimeString('en-US', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: 'UTC',
  })
}

export function parseDecimal(value: string | undefined): number {
  if (value === undefined || value === '') return 0
  const num = parseFloat(value)
  return isNaN(num) ? 0 : num
}

export const AGENT_COLORS = [
  '#6366f1', // indigo (accent)
  '#22c55e', // green (profit)
  '#3b82f6', // blue (info)
  '#f59e0b', // amber (warning)
  '#ec4899', // pink
  '#8b5cf6', // violet
  '#14b8a6', // teal
  '#ef4444', // red
]

export function agentColor(index: number): string {
  return AGENT_COLORS[index % AGENT_COLORS.length]
}
