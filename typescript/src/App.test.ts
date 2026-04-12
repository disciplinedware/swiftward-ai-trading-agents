import { describe, it, expect } from 'vitest'
import {
  formatCurrency,
  formatPercent,
  formatPnl,
  formatQty,
  formatTime,
  parseDecimal,
  agentColor,
  AGENT_COLORS,
} from './lib/format'

describe('formatCurrency', () => {
  const cases: [string, string | number | undefined, boolean | undefined, string][] = [
    ['undefined returns dash', undefined, false, '-'],
    ['empty string returns dash', '' as unknown as string, false, '-'],
    ['NaN string returns dash', 'abc', false, '-'],
    ['zero', 0, false, '$0.00'],
    ['small positive', 5.5, false, '$5.50'],
    ['negative small', -3.25, false, '-$3.25'],
    ['large positive (no decimals)', 15000, false, '$15,000'],
    ['large negative (no decimals)', -15000, false, '-$15,000'],
    ['boundary: 9.99', 9.99, false, '$9.99'],
    ['boundary: 10', 10, false, '$10'],
    ['boundary: -10', -10, false, '-$10'],
    ['string input', '1234.56', false, '$1,235'],
    ['compact millions', 2500000, true, '$2.5M'],
    ['compact thousands', 45000, true, '$45.0K'],
    ['compact small (no suffix)', 5000, true, '$5,000'],
    ['compact negative millions', -1200000, true, '-$1.2M'],
  ]

  for (const [name, value, compact, expected] of cases) {
    it(name, () => {
      expect(formatCurrency(value, compact ?? false)).toBe(expected)
    })
  }
})

describe('formatPercent', () => {
  const cases: [string, number | undefined, string][] = [
    ['undefined returns dash', undefined, '-'],
    ['zero', 0, '0.0%'],
    ['positive', 5.23, '+5.2%'],
    ['negative', -3.1, '-3.1%'],
    ['large positive', 100, '+100.0%'],
  ]

  for (const [name, value, expected] of cases) {
    it(name, () => {
      expect(formatPercent(value)).toBe(expected)
    })
  }
})

describe('formatPnl', () => {
  const cases: [string, string | undefined, number, string][] = [
    ['undefined returns dash', undefined, 10000, '-'],
    ['empty string returns dash', '', 10000, '-'],
    ['profit', '10500', 10000, '+$500 (+5.0%)'],
    ['loss', '9500', 10000, '-$500 (-5.0%)'],
    ['zero initial returns zero pct', '100', 0, '+$100 (+0.0%)'],
  ]

  for (const [name, current, initial, expected] of cases) {
    it(name, () => {
      expect(formatPnl(current, initial)).toBe(expected)
    })
  }
})

describe('formatQty', () => {
  const cases: [string, string | undefined, string][] = [
    ['undefined returns dash', undefined, '-'],
    ['empty returns dash', '', '-'],
    ['large qty', '123.456', '123.4560'],
    ['medium qty', '0.05', '0.050000'],
    ['small qty', '0.001', '0.00100000'],
  ]

  for (const [name, value, expected] of cases) {
    it(name, () => {
      expect(formatQty(value)).toBe(expected)
    })
  }
})

describe('formatTime', () => {
  it('undefined returns dash', () => {
    expect(formatTime(undefined)).toBe('-')
  })

  it('invalid date returns dash', () => {
    expect(formatTime('not-a-date')).toBe('-')
  })

  it('valid ISO timestamp', () => {
    const result = formatTime('2026-03-10T12:30:45Z')
    expect(result).toContain('UTC')
    expect(result).toContain('12:30:45')
  })
})

describe('parseDecimal', () => {
  const cases: [string, string | undefined, number][] = [
    ['undefined returns 0', undefined, 0],
    ['empty returns 0', '', 0],
    ['NaN returns 0', 'abc', 0],
    ['valid number', '123.45', 123.45],
    ['negative', '-5.5', -5.5],
    ['zero string', '0', 0],
  ]

  for (const [name, value, expected] of cases) {
    it(name, () => {
      expect(parseDecimal(value)).toBe(expected)
    })
  }
})

describe('agentColor', () => {
  it('returns first color for index 0', () => {
    expect(agentColor(0)).toBe(AGENT_COLORS[0])
  })

  it('wraps around at array length', () => {
    expect(agentColor(AGENT_COLORS.length)).toBe(AGENT_COLORS[0])
  })

  it('handles large index', () => {
    expect(agentColor(100)).toBe(AGENT_COLORS[100 % AGENT_COLORS.length])
  })
})
