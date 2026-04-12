import { useQuery, useMutation, useQueryClient, keepPreviousData } from '@tanstack/react-query'
import * as riskApi from '@/api/risk'

export function useAgents() {
  return useQuery({
    queryKey: ['agents'],
    queryFn: riskApi.listAgents,
    refetchInterval: 5_000,
    placeholderData: keepPreviousData,
  })
}

export function useAgentStatus(agentId: string) {
  return useQuery({
    queryKey: ['agentStatus', agentId],
    queryFn: () => riskApi.getAgentStatus(agentId),
    refetchInterval: 5_000,
    enabled: !!agentId,
  })
}

export function useHaltAgent() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (agentId: string) => riskApi.haltAgent(agentId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['agents'] })
      qc.invalidateQueries({ queryKey: ['agentStatus'] })
    },
  })
}

export function useResumeAgent() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (agentId: string) => riskApi.resumeAgent(agentId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['agents'] })
      qc.invalidateQueries({ queryKey: ['agentStatus'] })
    },
  })
}
