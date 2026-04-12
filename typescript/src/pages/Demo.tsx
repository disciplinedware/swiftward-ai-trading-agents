import { useState, useMemo } from 'react'
import { GitCompare, Shield, ShieldOff } from 'lucide-react'
import { useAgents } from '@/hooks/use-risk'
import { useLimits, useTradeHistory, usePortfolioHistory } from '@/hooks/use-trading'
import { DemoPanel } from '@/components/demo/DemoPanel'
import { DemoChart } from '@/components/demo/DemoChart'
import type { AgentSummary } from '@/types/api'

export function Demo() {
  const { data: agentList } = useAgents()
  const agents = useMemo(() => agentList?.agents ?? [], [agentList])

  // Agent selection state - default to first two agents
  const [withAgentId, setWithAgentId] = useState<string>('')
  const [withoutAgentId, setWithoutAgentId] = useState<string>('')

  // Auto-select when agents arrive
  const resolvedWithId = withAgentId || agents[0]?.agent_id || ''
  const resolvedWithoutId =
    withoutAgentId || agents.find((a) => a.agent_id !== resolvedWithId)?.agent_id || ''

  // Hooks for "with guardrails" agent
  const { data: withLimits, isLoading: withLimitsLoading } = useLimits(resolvedWithId)
  const { data: withTradeHistory } = useTradeHistory(resolvedWithId, 200)
  const { data: withCurve } = usePortfolioHistory(resolvedWithId)

  // Hooks for "without guardrails" agent
  const { data: withoutLimits, isLoading: withoutLimitsLoading } =
    useLimits(resolvedWithoutId)
  const { data: withoutTradeHistory } = useTradeHistory(resolvedWithoutId, 200)
  const { data: withoutCurve } = usePortfolioHistory(resolvedWithoutId)

  const withTrades = withTradeHistory?.trades ?? []
  const withoutTrades = withoutTradeHistory?.trades ?? []

  const withName = agentName(agents, resolvedWithId)
  const withoutName = agentName(agents, resolvedWithoutId)

  return (
    <div>
      <div className="flex items-center gap-3 mb-6">
        <GitCompare className="h-5 w-5 text-accent" />
        <h1 className="text-xl font-semibold text-text-primary">
          With vs Without Guardrails
        </h1>
      </div>

      <p className="text-sm text-text-secondary mb-6 max-w-2xl">
        Side-by-side comparison showing the value of policy guardrails. Select
        one agent running through the MCP Gateway (with policy enforcement) and
        another running directly (no policy) to compare performance and risk
        control.
      </p>

      {/* Agent selectors */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6 mb-6">
        <AgentSelector
          label="With Guardrails"
          icon={<Shield className="h-4 w-4 text-profit" />}
          agents={agents}
          selectedId={resolvedWithId}
          excludeId={resolvedWithoutId}
          onChange={setWithAgentId}
          accent="profit"
        />
        <AgentSelector
          label="Without Guardrails"
          icon={<ShieldOff className="h-4 w-4 text-loss" />}
          agents={agents}
          selectedId={resolvedWithoutId}
          excludeId={resolvedWithId}
          onChange={setWithoutAgentId}
          accent="loss"
        />
      </div>

      {/* Side-by-side panels */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6 mb-6">
        <DemoPanel
          variant="with"
          agentId={resolvedWithId || null}
          limits={withLimits}
          trades={withTrades}
          isLoading={withLimitsLoading}
        />
        <DemoPanel
          variant="without"
          agentId={resolvedWithoutId || null}
          limits={withoutLimits}
          trades={withoutTrades}
          isLoading={withoutLimitsLoading}
        />
      </div>

      {/* Overlay equity chart */}
      <DemoChart
        withCurve={withCurve}
        withoutCurve={withoutCurve}
        withTrades={withTrades}
        withAgentName={withName}
        withoutAgentName={withoutName}
      />

      {/* How it works explanation */}
      <div className="mt-6 rounded-lg border border-surface-border bg-surface-card p-5">
        <h3 className="text-xs font-medium text-text-secondary uppercase tracking-wider mb-3">
          How the Demo Works
        </h3>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6 text-xs text-text-muted">
          <div>
            <p className="text-profit font-medium mb-1">With Guardrails</p>
            <p>
              Agent connects via <span className="font-mono text-text-secondary">swiftward-server:8095</span> (MCP
              Gateway). Every trade intent is evaluated against 10+ policy rules.
              Risky trades are blocked. Circuit breakers halt trading on excessive
              drawdown.
            </p>
          </div>
          <div>
            <p className="text-loss font-medium mb-1">Without Guardrails</p>
            <p>
              Agent connects directly to <span className="font-mono text-text-secondary">trading-server:8091</span>.
              No policy evaluation. No position limits. No drawdown protection. No
              circuit breakers. The agent can trade without any constraints.
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}

function agentName(agents: AgentSummary[], agentId: string): string {
  const agent = agents.find((a) => a.agent_id === agentId)
  return agent?.name || agentId || 'Unknown'
}

function AgentSelector({
  label,
  icon,
  agents,
  selectedId,
  excludeId,
  onChange,
  accent,
}: {
  label: string
  icon: React.ReactNode
  agents: AgentSummary[]
  selectedId: string
  excludeId: string
  onChange: (id: string) => void
  accent: 'profit' | 'loss'
}) {
  const borderClass = accent === 'profit' ? 'border-profit/20' : 'border-loss/20'

  return (
    <div className="flex items-center gap-3">
      <div className="flex items-center gap-1.5">
        {icon}
        <span className="text-xs font-medium text-text-secondary">{label}:</span>
      </div>
      <select
        value={selectedId}
        onChange={(e) => onChange(e.target.value)}
        className={`flex-1 rounded-md border bg-surface-base px-3 py-1.5 text-xs text-text-primary outline-none focus:ring-1 focus:ring-accent/50 ${borderClass}`}
      >
        <option value="">Select agent...</option>
        {agents.map((a) => (
          <option
            key={a.agent_id}
            value={a.agent_id}
            disabled={a.agent_id === excludeId}
          >
            {a.name || a.agent_id}
            {a.agent_id === excludeId ? ' (selected in other panel)' : ''}
          </option>
        ))}
      </select>
    </div>
  )
}
