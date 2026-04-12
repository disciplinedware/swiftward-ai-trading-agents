import { useEffect } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery, keepPreviousData } from '@tanstack/react-query'
import { Bot, BellRing, Clock, CheckCircle, XCircle, AlertCircle } from 'lucide-react'
import { clsx } from 'clsx'
import { useAgents } from '@/hooks/use-risk'
import * as claudeApi from '@/api/claude-agent'
import { formatTime } from '@/lib/format'

// ---- Session history ----

function sessionStatusBadge(s: claudeApi.SessionEntry) {
  if (s.error) {
    return (
      <span className="inline-flex items-center gap-1 text-xs text-red-400">
        <XCircle size={12} /> error
      </span>
    )
  }
  if (s.elapsed_ms > 0) {
    return (
      <span className="inline-flex items-center gap-1 text-xs text-green-400">
        <CheckCircle size={12} /> done
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1 text-xs text-yellow-400">
      <AlertCircle size={12} /> running
    </span>
  )
}

function SessionTable({ agentId }: { agentId: string }) {
  const { data: sessions = [], isLoading, isError, error } = useQuery({
    queryKey: ['claude-agent-sessions', agentId],
    queryFn: () => claudeApi.getSessions(agentId, 30),
    refetchInterval: 10_000,
    placeholderData: keepPreviousData,
    enabled: !!agentId,
  })

  if (isLoading) {
    return <p className="text-sm text-text-muted py-4">Loading sessions...</p>
  }

  if (isError) {
    return (
      <div className="rounded-lg border border-red-500/30 bg-red-500/5 p-4">
        <p className="text-sm text-red-400">Failed to load sessions: {error?.message ?? 'unknown error'}</p>
      </div>
    )
  }

  if (sessions.length === 0) {
    return (
      <div className="rounded-lg border border-surface-border bg-surface-card p-6 text-center">
        <p className="text-sm text-text-muted">No session data yet. Sessions are written to /workspace/logs/sessions.jsonl.</p>
      </div>
    )
  }

  return (
    <div className="overflow-x-auto rounded-lg border border-surface-border">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-surface-border bg-surface-card">
            <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Session ID</th>
            <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Agent</th>
            <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Started</th>
            <th className="px-4 py-2.5 text-right text-xs font-medium text-text-muted">Duration</th>
            <th className="px-4 py-2.5 text-center text-xs font-medium text-text-muted">Status</th>
            <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Output Snippet</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-surface-border">
          {sessions.map((s) => (
            <tr key={s.session_id} className="bg-surface-card hover:bg-surface-hover transition-colors">
              <td className="px-4 py-2.5 text-xs font-mono text-text-muted">{s.session_id}</td>
              <td className="px-4 py-2.5 text-xs text-text-secondary">{s.agent_id}</td>
              <td className="px-4 py-2.5 text-xs text-text-secondary whitespace-nowrap">{formatTime(s.started_at)}</td>
              <td className="px-4 py-2.5 text-xs text-right text-text-secondary">
                {s.elapsed_ms > 0 ? `${(s.elapsed_ms / 1000).toFixed(1)}s` : '-'}
              </td>
              <td className="px-4 py-2.5 text-center">{sessionStatusBadge(s)}</td>
              <td className="px-4 py-2.5 text-xs text-text-muted font-mono max-w-xs truncate" title={s.output_snippet}>
                {s.output_snippet || '-'}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

// ---- Active alerts ----

function onTriggerBadge(onTrigger: string) {
  const colors: Record<string, string> = {
    auto_execute: 'text-blue-400 bg-blue-400/10',
    wake_full:    'text-accent bg-accent/10',
    wake_triage:  'text-yellow-400 bg-yellow-400/10',
  }
  return (
    <span className={clsx('inline-block rounded px-1.5 py-0.5 text-xs font-medium', colors[onTrigger] ?? 'text-text-muted bg-surface-hover')}>
      {onTrigger}
    </span>
  )
}

function serviceBadge(service: string) {
  const colors: Record<string, string> = {
    trading: 'text-green-400',
    market:  'text-blue-400',
    news:    'text-purple-400',
    time:    'text-orange-400',
  }
  return (
    <span className={clsx('text-xs font-medium', colors[service] ?? 'text-text-muted')}>
      {service}
    </span>
  )
}

function AlertsTable({ agentId }: { agentId: string }) {
  const { data: alerts = [], isLoading, isError, error } = useQuery({
    queryKey: ['claude-agent-alerts', agentId],
    queryFn: () => claudeApi.getAlerts(agentId),
    refetchInterval: 15_000,
    enabled: !!agentId,
    placeholderData: keepPreviousData,
  })

  if (isLoading) {
    return <p className="text-sm text-text-muted py-4">Loading alerts...</p>
  }

  if (isError) {
    return (
      <div className="rounded-lg border border-red-500/30 bg-red-500/5 p-4">
        <p className="text-sm text-red-400">Failed to load alerts: {error?.message ?? 'unknown error'}</p>
      </div>
    )
  }

  if (alerts.length === 0) {
    return (
      <div className="rounded-lg border border-surface-border bg-surface-card p-6 text-center">
        <p className="text-sm text-text-muted">No active alerts for this agent.</p>
      </div>
    )
  }

  return (
    <div className="overflow-x-auto rounded-lg border border-surface-border">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-surface-border bg-surface-card">
            <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Service</th>
            <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Trigger</th>
            <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Note</th>
            <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Conditions</th>
            <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Created</th>
            <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Expires</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-surface-border">
          {alerts.map((a) => (
            <tr key={a.alert_id} className="bg-surface-card hover:bg-surface-hover transition-colors">
              <td className="px-4 py-2.5">{serviceBadge(a.service)}</td>
              <td className="px-4 py-2.5">{onTriggerBadge(a.on_trigger)}</td>
              <td className="px-4 py-2.5 text-xs text-text-secondary max-w-xs truncate" title={a.note}>{a.note || '-'}</td>
              <td className="px-4 py-2.5 text-xs font-mono text-text-muted max-w-xs truncate" title={JSON.stringify(a.params)}>
                {JSON.stringify(a.params)}
              </td>
              <td className="px-4 py-2.5 text-xs text-text-muted whitespace-nowrap">{formatTime(a.created_at)}</td>
              <td className="px-4 py-2.5 text-xs text-text-muted whitespace-nowrap">{a.expires_at ? formatTime(a.expires_at) : '-'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

// ---- Main page ----

export function ClaudeAgent() {
  const { data: agentList } = useAgents()
  const agents = agentList?.agents ?? []
  const [searchParams, setSearchParams] = useSearchParams()

  const agentFromUrl = searchParams.get('agent') ?? ''
  const activeAgentId = agentFromUrl || agents[0]?.agent_id || ''

  // Auto-populate URL with first agent on initial load.
  useEffect(() => {
    if (!agentFromUrl && activeAgentId) {
      setSearchParams({ agent: activeAgentId }, { replace: true })
    }
  }, [agentFromUrl, activeAgentId, setSearchParams])

  return (
    <div className="space-y-6">
      {/* Page header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Bot size={20} className="text-accent" />
          <h1 className="text-xl font-semibold text-text-primary">Claude Agent</h1>
        </div>
        {agents.length > 0 && (
          <select
            className="rounded-md border border-surface-border bg-surface-card px-3 py-1.5 text-sm text-text-primary focus:outline-none"
            value={activeAgentId}
            onChange={(e) => setSearchParams({ agent: e.target.value })}
          >
            {agents.map((a) => (
              <option key={a.agent_id} value={a.agent_id}>{a.name || a.agent_id}</option>
            ))}
          </select>
        )}
      </div>

      {/* Session History */}
      <section className="space-y-3">
        <div className="flex items-center gap-2">
          <Clock size={16} className="text-text-muted" />
          <h2 className="text-sm font-semibold text-text-primary">Session History</h2>
          <span className="text-xs text-text-muted">(newest first, filtered by agent)</span>
        </div>
        <SessionTable agentId={activeAgentId} />
      </section>

      {/* Active Alerts */}
      <section className="space-y-3">
        <div className="flex items-center gap-2">
          <BellRing size={16} className="text-text-muted" />
          <h2 className="text-sm font-semibold text-text-primary">Active Alerts</h2>
          {activeAgentId && <span className="text-xs text-text-muted">- {activeAgentId}</span>}
        </div>
        <AlertsTable agentId={activeAgentId} />
      </section>
    </div>
  )
}
