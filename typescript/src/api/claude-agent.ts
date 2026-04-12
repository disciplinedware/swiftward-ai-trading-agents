// REST API client for the Claude agent dashboard.
// Endpoints are served by the trading-server on the same origin as the dashboard.

export interface SessionEntry {
  session_id: string
  agent_id: string
  started_at: string
  elapsed_ms: number
  output_snippet: string
  error?: string
}

export interface AlertEntry {
  alert_id: string
  service: string    // "trading" | "market" | "news" | "time"
  on_trigger: string // "auto_execute" | "wake_full" | "wake_triage"
  note: string
  created_at: string
  expires_at?: string
  params: Record<string, unknown>
}

export async function getSessions(agentId: string, limit = 20): Promise<SessionEntry[]> {
  const res = await fetch(`/v1/claude-agent/sessions?agent=${encodeURIComponent(agentId)}&limit=${limit}`)
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  const data = await res.json() as { sessions: SessionEntry[] }
  return data.sessions ?? []
}

export async function getAlerts(agentId: string): Promise<AlertEntry[]> {
  const res = await fetch(`/v1/claude-agent/alerts?agent_id=${encodeURIComponent(agentId)}`)
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  const data = await res.json() as { alerts: AlertEntry[] }
  return data.alerts ?? []
}
