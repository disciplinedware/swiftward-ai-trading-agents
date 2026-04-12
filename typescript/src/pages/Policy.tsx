import { useState, useMemo } from 'react'
import { clsx } from 'clsx'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { vscDarkPlus } from 'react-syntax-highlighter/dist/esm/styles/prism'
import {
  Shield,
  ChevronDown,
  ChevronRight,
  Eye,
  ExternalLink,
  Filter,
  AlertTriangle,
  ShieldCheck,
  ShieldOff,
  Zap,
} from 'lucide-react'
import { useAgents } from '@/hooks/use-risk'
import { useAllRejections, type RejectionEvent } from '@/hooks/use-rejections'
import { V1_RULES, SHADOW_DIFFS, type PolicyRule } from '@/lib/policy-rules'
import { formatTime } from '@/lib/format'

// --- Helpers (pure functions, no Date.now inside useMemo) ---

function eventTypeBadgeClass(eventType: string): string {
  switch (eventType) {
    case 'trade_order':
      return 'bg-accent/15 text-accent'
    case 'heartbeat':
      return 'bg-info/15 text-info'
    case 'execution_report':
      return 'bg-warning/15 text-warning'
    default:
      return 'bg-surface-hover text-text-muted'
  }
}

function formatRelativeTime(timestamp: string): string {
  const diff = Date.now() - new Date(timestamp).getTime()
  const minutes = Math.floor(diff / 60_000)
  if (minutes < 1) return 'just now'
  if (minutes < 60) return `${minutes}min ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}

// Infer which rule was triggered from the rejection reason text.
// Each v1 rule produces a distinct reason string, so we can match by substring.
// Rules that never reject (count_orders) or don't appear in trade history
// (heartbeat rules, execution_report rules) won't match here.
const REASON_TO_RULE: Array<[string, string]> = [
  ['halted', 'Halted Agent Check'],
  ['Close-only', 'Close-Only Mode'],
  ['consecutive', 'Loss Streak Pause'],
  ['exceeds 5%', 'Tier 2 Order Size'],
  ['exceeds 10%', 'Tier 1 Order Size'],
  ['exceeds 15%', 'Max Order Size'],
  ['not in permitted', 'Pair Whitelist'],
  ['concentration exceeds 25%', 'Tier 2 Concentration'],
  ['concentration exceeds 35%', 'Tier 1 Concentration'],
  ['Concentration exceeds 50%', 'Concentration Limit'],
  ['rate exceeded', 'Order Rate Limit'],
  ['Trailing 24h', 'Trailing 24h Drawdown Halt'],
  ['Absolute drawdown', 'Absolute Floor Halt'],
]

function inferRuleFromTrade(trade: { status: string; reject?: { source: string; reason: string } }): string {
  const reason = trade.reject?.reason ?? ''
  for (const [pattern, ruleName] of REASON_TO_RULE) {
    if (reason.includes(pattern)) return ruleName
  }
  return 'Trade Order Rejection'
}

type TimeFilter = '1h' | '6h' | '24h' | 'all'

function timeCutoff(filter: TimeFilter): number {
  if (filter === 'all') return 0
  const hours = filter === '1h' ? 1 : filter === '6h' ? 6 : 24
  return Date.now() - hours * 3600_000
}

function dayAgoCutoff(): number {
  return Date.now() - 24 * 3600_000
}

function computeTriggerCounts(
  rejections: RejectionEvent[],
  cutoffMs: number,
): Record<string, { count: number; lastTriggered: string | null }> {
  const counts: Record<string, { count: number; lastTriggered: string | null }> = {}
  for (const { trade } of rejections) {
    const ts = new Date(trade.timestamp).getTime()
    if (ts < cutoffMs) continue
    const ruleName = inferRuleFromTrade(trade)
    if (!counts[ruleName]) {
      counts[ruleName] = { count: 0, lastTriggered: null }
    }
    counts[ruleName].count++
    if (!counts[ruleName].lastTriggered || trade.timestamp > counts[ruleName].lastTriggered!) {
      counts[ruleName].lastTriggered = trade.timestamp
    }
  }
  return counts
}

function countRecentRejections(rejections: RejectionEvent[], cutoffMs: number): number {
  return rejections.filter((r) => new Date(r.trade.timestamp).getTime() >= cutoffMs).length
}

function filterRejections(
  rejections: RejectionEvent[],
  agentFilter: string,
  ruleFilter: string,
  cutoffMs: number,
): RejectionEvent[] {
  return rejections.filter((r) => {
    if (agentFilter !== 'all' && r.agentId !== agentFilter) return false
    if (ruleFilter !== 'all' && inferRuleFromTrade(r.trade) !== ruleFilter) return false
    if (cutoffMs > 0 && new Date(r.trade.timestamp).getTime() < cutoffMs) return false
    return true
  })
}

// --- ActiveRulesTable ---

function ActiveRulesTable({ rejections }: { rejections: RejectionEvent[] }) {
  const [expandedRule, setExpandedRule] = useState<number | null>(null)

  // Compute trigger counts by inferred rule name (dayAgoCutoff wraps Date.now)
  const triggerCounts = computeTriggerCounts(rejections, dayAgoCutoff())

  // Rules that can be attributed via rejection reason text matching.
  // Excludes: Order Counter (never rejects), Track Fill / Router Rejection Alert
  // (execution_report events not in trade history), and heartbeat rules
  // (halt the agent but rejection doesn't appear as a trade_order reject).
  const ATTRIBUTABLE_RULES = new Set(REASON_TO_RULE.map(([, name]) => name))

  return (
    <div className="rounded-lg border border-surface-border bg-surface-card overflow-hidden">
      <div className="flex items-center gap-2 px-5 py-4 border-b border-surface-border">
        <Shield size={16} className="text-accent" />
        <h2 className="text-sm font-medium text-text-primary">Active Rules (v1)</h2>
        <span className="ml-auto text-xs text-text-muted">{V1_RULES.length} rules</span>
      </div>

      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-surface-border">
              <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted w-8">#</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Rule Name</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Event Type</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Threshold</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Status</th>
              <th className="px-4 py-2.5 text-right text-xs font-medium text-text-muted">Triggered (24h)</th>
              <th className="px-4 py-2.5 text-right text-xs font-medium text-text-muted">Last Triggered</th>
              <th className="px-4 py-2.5 w-8"></th>
            </tr>
          </thead>
          <tbody>
            {V1_RULES.map((rule) => {
              const isExpanded = expandedRule === rule.id
              const isAttributable = ATTRIBUTABLE_RULES.has(rule.name)
              const stats = isAttributable ? triggerCounts[rule.name] : undefined
              const count = stats?.count ?? 0
              const lastTriggered = stats?.lastTriggered ?? null

              return (
                <RuleRow
                  key={rule.id}
                  rule={rule}
                  isExpanded={isExpanded}
                  triggerCount={count}
                  lastTriggered={lastTriggered}
                  isAttributable={isAttributable}
                  onToggle={() => setExpandedRule(isExpanded ? null : rule.id)}
                />
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function RuleRow({
  rule,
  isExpanded,
  triggerCount,
  lastTriggered,
  isAttributable,
  onToggle,
}: {
  rule: PolicyRule
  isExpanded: boolean
  triggerCount: number
  lastTriggered: string | null
  isAttributable: boolean
  onToggle: () => void
}) {
  const eventTypeBadge = eventTypeBadgeClass(rule.eventType)

  return (
    <>
      <tr
        className="border-b border-surface-border hover:bg-surface-hover cursor-pointer transition-colors"
        onClick={onToggle}
      >
        <td className="px-4 py-3 text-xs font-mono text-text-muted">{rule.id}</td>
        <td className="px-4 py-3">
          <div className="text-text-primary font-medium">{rule.name}</div>
          <div className="text-xs text-text-muted mt-0.5">{rule.description}</div>
        </td>
        <td className="px-4 py-3">
          <span className={clsx(
            'inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium',
            eventTypeBadge,
          )}>
            {rule.eventType}
          </span>
        </td>
        <td className="px-4 py-3 text-xs font-mono text-text-secondary">{rule.threshold}</td>
        <td className="px-4 py-3">
          <span className="inline-flex items-center gap-1 rounded-full bg-profit/15 px-2 py-0.5 text-[10px] font-medium text-profit">
            <span className="inline-block h-1.5 w-1.5 rounded-full bg-profit" />
            Active
          </span>
        </td>
        <td className="px-4 py-3 text-right">
          {isAttributable ? (
            <span className={clsx(
              'text-xs font-mono',
              triggerCount > 0 ? 'text-text-primary' : 'text-text-muted',
            )}>
              {triggerCount > 0 ? `${triggerCount}x` : '0x'}
            </span>
          ) : (
            <span className="text-xs text-text-muted" title="Per-rule attribution unavailable">N/A</span>
          )}
        </td>
        <td className="px-4 py-3 text-right text-xs text-text-muted">
          {isAttributable
            ? (lastTriggered ? formatRelativeTime(lastTriggered) : 'never')
            : 'N/A'}
        </td>
        <td className="px-4 py-3 text-text-muted">
          {isExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        </td>
      </tr>

      {isExpanded && (
        <tr>
          <td colSpan={8} className="bg-surface-base px-6 py-4">
            <div className="flex items-center gap-2 mb-2">
              <Eye size={12} className="text-text-muted" />
              <span className="text-xs font-medium text-text-muted uppercase tracking-wide">
                Rule Definition (YAML)
              </span>
            </div>
            <SyntaxHighlighter
              language="yaml"
              style={vscDarkPlus}
              customStyle={{
                background: 'transparent',
                margin: 0,
                padding: '1rem',
                fontSize: '0.75rem',
                lineHeight: '1.625',
                border: '1px solid var(--color-surface-border)',
                borderRadius: '0.375rem',
              }}
            >
              {rule.yamlSnippet}
            </SyntaxHighlighter>
          </td>
        </tr>
      )}
    </>
  )
}

// --- ShadowPolicyPanel ---

function ShadowPolicyPanel({ rejectionCount: _rejectionCount }: { rejectionCount: number }) {
  void _rejectionCount

  return (
    <div className="rounded-lg border border-surface-border bg-surface-card p-5">
      <div className="flex items-center gap-2 mb-4">
        <Eye size={16} className="text-info" />
        <h2 className="text-sm font-medium text-text-primary">Shadow Policy v2 (stricter limits, logged only)</h2>
      </div>

      <div className="flex items-center gap-2 mb-4">
        <span className="inline-flex items-center gap-1 rounded-full bg-info/15 px-2.5 py-1 text-xs font-medium text-info">
          <Eye size={10} />
          SHADOW
        </span>
        <span className="text-xs text-text-muted">Evaluates but does not enforce</span>
      </div>

      <div className="mb-4">
        <h3 className="text-xs font-medium text-text-secondary mb-2">Threshold Differences from v1</h3>
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b border-surface-border">
                <th className="py-2 pr-4 text-left text-text-muted font-medium">Rule</th>
                <th className="py-2 px-4 text-right text-text-muted font-medium">v1 (active)</th>
                <th className="py-2 pl-4 text-right text-text-muted font-medium">v2 (shadow)</th>
              </tr>
            </thead>
            <tbody>
              {SHADOW_DIFFS.map((diff) => (
                <tr key={diff.rule} className="border-b border-surface-border last:border-0">
                  <td className="py-2 pr-4 text-text-primary">{diff.rule}</td>
                  <td className="py-2 px-4 text-right font-mono text-text-secondary">{diff.v1Value}</td>
                  <td className="py-2 pl-4 text-right font-mono text-warning">{diff.v2Value}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      <div className="flex items-center justify-between pt-3 border-t border-surface-border">
        <span className="text-xs text-text-secondary">
          Would-have-blocked (24h): <span className="font-mono text-text-muted">N/A (shadow logs not exposed via API)</span>
        </span>
        <a
          href={`http://${window.location.hostname}:5174`}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 text-xs text-accent hover:text-accent-hover transition-colors"
        >
          View in Swiftward UI
          <ExternalLink size={10} />
        </a>
      </div>
    </div>
  )
}

// --- RejectionLog ---

function RejectionLog({
  rejections,
  agentIds,
}: {
  rejections: RejectionEvent[]
  agentIds: string[]
}) {
  const [agentFilter, setAgentFilter] = useState<string>('all')
  const [ruleFilter, setRuleFilter] = useState<string>('all')
  const [timeFilter, setTimeFilter] = useState<TimeFilter>('24h')

  // Collect unique rule names from rejections
  const ruleNames = useMemo(() => {
    const names = new Set<string>()
    for (const { trade } of rejections) {
      names.add(inferRuleFromTrade(trade))
    }
    return Array.from(names).sort()
  }, [rejections])

  // Filter rejections (timeCutoff wraps Date.now)
  const filtered = filterRejections(rejections, agentFilter, ruleFilter, timeCutoff(timeFilter))

  return (
    <div className="rounded-lg border border-surface-border bg-surface-card overflow-hidden">
      <div className="flex items-center gap-2 px-5 py-4 border-b border-surface-border">
        <ShieldOff size={16} className="text-loss" />
        <h2 className="text-sm font-medium text-text-primary">Rejection Log</h2>
        <span className="ml-auto text-xs text-text-muted">{filtered.length} events</span>
      </div>

      {/* Filters */}
      <div className="flex items-center gap-3 px-5 py-3 border-b border-surface-border bg-surface-base">
        <Filter size={12} className="text-text-muted" />

        <select
          className="bg-surface-card border border-surface-border rounded-md px-2 py-1 text-xs text-text-primary"
          value={agentFilter}
          onChange={(e) => setAgentFilter(e.target.value)}
        >
          <option value="all">All Agents</option>
          {agentIds.map((id) => (
            <option key={id} value={id}>{id}</option>
          ))}
        </select>

        <select
          className="bg-surface-card border border-surface-border rounded-md px-2 py-1 text-xs text-text-primary"
          value={ruleFilter}
          onChange={(e) => setRuleFilter(e.target.value)}
        >
          <option value="all">All Rules</option>
          {ruleNames.map((name) => (
            <option key={name} value={name}>{name}</option>
          ))}
        </select>

        <div className="flex rounded-md border border-surface-border overflow-hidden ml-auto">
          {(['1h', '6h', '24h', 'all'] as TimeFilter[]).map((tf) => (
            <button
              key={tf}
              className={clsx(
                'px-2.5 py-1 text-[10px] font-medium transition-colors',
                timeFilter === tf
                  ? 'bg-accent text-white'
                  : 'bg-surface-card text-text-secondary hover:text-text-primary',
              )}
              onClick={() => setTimeFilter(tf)}
            >
              {tf.toUpperCase()}
            </button>
          ))}
        </div>
      </div>

      {/* Table */}
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-surface-border">
              <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Time</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Agent</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Rule</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Market</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Detail</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 ? (
              <tr>
                <td colSpan={5} className="px-4 py-8 text-center text-xs text-text-muted">
                  No rejection events match the current filters.
                </td>
              </tr>
            ) : (
              filtered.slice(0, 100).map((event, i) => (
                <RejectionRow key={`${event.trade.timestamp}-${event.agentId}-${i}`} event={event} />
              ))
            )}
          </tbody>
        </table>
      </div>

      {filtered.length > 100 && (
        <div className="px-5 py-3 border-t border-surface-border text-center">
          <span className="text-xs text-text-muted">
            Showing 100 of {filtered.length} events
          </span>
        </div>
      )}
    </div>
  )
}

function RejectionRow({ event }: { event: RejectionEvent }) {
  const { trade, agentId } = event
  const ruleName = inferRuleFromTrade(trade)
  const isHalt = ruleName === 'Halted Agent Check'

  return (
    <tr className="border-b border-surface-border hover:bg-surface-hover transition-colors">
      <td className="px-4 py-2.5 text-xs font-mono text-text-muted whitespace-nowrap">
        {formatTime(trade.timestamp)}
      </td>
      <td className="px-4 py-2.5 text-xs font-mono text-text-primary">{agentId}</td>
      <td className="px-4 py-2.5">
        <span className={clsx(
          'inline-flex items-center gap-1 text-xs font-medium',
          isHalt ? 'text-warning' : 'text-loss',
        )}>
          {isHalt ? <AlertTriangle size={10} /> : <ShieldOff size={10} />}
          {ruleName}
        </span>
      </td>
      <td className="px-4 py-2.5 text-xs font-mono text-text-secondary">
        {trade.pair || '-'}
      </td>
      <td className="px-4 py-2.5 text-xs text-text-muted">
        {trade.side ? `${trade.side.toUpperCase()} ` : ''}{trade.pair || ''}
        {trade.reject?.reason ? ` - ${trade.reject.reason}` : ''}
      </td>
    </tr>
  )
}

// --- Main Policy Page ---

export function Policy() {
  const { data: agentList } = useAgents()
  const agentIds = useMemo(
    () => (agentList?.agents ?? []).map((a) => a.agent_id),
    [agentList],
  )

  const { rejections, isLoading } = useAllRejections(agentIds)

  // Compute summary stats (dayAgoCutoff wraps Date.now)
  const totalRejections24h = countRecentRejections(rejections, dayAgoCutoff())

  return (
    <div className="space-y-6">
      {/* Page header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <ShieldCheck size={20} className="text-accent" />
          <h1 className="text-xl font-semibold text-text-primary">Policy</h1>
        </div>
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2">
            <Zap size={12} className="text-profit" />
            <span className="text-xs text-text-secondary">
              {V1_RULES.length} rules active
            </span>
          </div>
          <div className="flex items-center gap-2">
            <ShieldOff size={12} className={totalRejections24h > 0 ? 'text-loss' : 'text-text-muted'} />
            <span className="text-xs text-text-secondary">
              {totalRejections24h} rejections (24h)
            </span>
          </div>
        </div>
      </div>

      {/* Loading state */}
      {isLoading && agentIds.length === 0 && (
        <div className="rounded-lg border border-surface-border bg-surface-card p-8 text-center">
          <p className="text-sm text-text-muted">Loading policy data...</p>
        </div>
      )}

      {/* Active Rules Table */}
      <ActiveRulesTable rejections={rejections} />

      {/* Shadow Policy Panel */}
      <ShadowPolicyPanel rejectionCount={totalRejections24h} />

      {/* Rejection Log */}
      <RejectionLog rejections={rejections} agentIds={agentIds} />
    </div>
  )
}
