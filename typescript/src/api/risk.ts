import { mcpCall } from './mcp-client'
import type { AgentList, AgentStatus, HaltResumeResult } from '@/types/api'

const ENDPOINT = '/mcp/risk'

export function listAgents(): Promise<AgentList> {
  return mcpCall<AgentList>(ENDPOINT, 'risk/list_agents')
}

export function getAgentStatus(agentId: string): Promise<AgentStatus> {
  return mcpCall<AgentStatus>(ENDPOINT, 'risk/get_agent_status', { agent_id: agentId })
}

export function haltAgent(agentId: string): Promise<HaltResumeResult> {
  return mcpCall<HaltResumeResult>(ENDPOINT, 'risk/halt_agent', { agent_id: agentId })
}

export function resumeAgent(agentId: string): Promise<HaltResumeResult> {
  return mcpCall<HaltResumeResult>(ENDPOINT, 'risk/resume_agent', { agent_id: agentId })
}
