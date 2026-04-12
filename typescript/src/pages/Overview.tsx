import { useMemo } from 'react'
import toast from 'react-hot-toast'
import { useAgents, useHaltAgent, useResumeAgent } from '@/hooks/use-risk'
import {
  useAllAgentLimits,
  useAllEquityCurves,
  useRecentTrades,
} from '@/hooks/use-overview'
import { MetricsBar } from '@/components/overview/MetricsBar'
import { AgentsTable } from '@/components/overview/AgentsTable'
import { EquityChart } from '@/components/overview/EquityChart'
import { ActivityFeed } from '@/components/overview/ActivityFeed'

export function Overview() {
  const { data: agentList } = useAgents()
  const haltMutation = useHaltAgent()
  const resumeMutation = useResumeAgent()

  const agents = useMemo(() => agentList?.agents ?? [], [agentList])
  const agentIds = useMemo(() => agents.map((a) => a.agent_id), [agents])
  const agentNames = useMemo(() => {
    const map: Record<string, string> = {}
    for (const a of agents) {
      map[a.agent_id] = a.name || a.agent_id
    }
    return map
  }, [agents])

  const { limitsMap, allLimits } = useAllAgentLimits(agentIds)
  const { curves } = useAllEquityCurves(agentIds)
  const { events } = useRecentTrades(agentIds, 10)

  const haltedCount = agents.filter((a) => limitsMap[a.agent_id]?.halted ?? a.halted ?? false).length

  const handleHalt = (agentId: string) => {
    haltMutation.mutate(agentId, {
      onSuccess: () => toast.success(`Agent ${agentId} halted`),
      onError: (err) => toast.error(`Failed to halt: ${err.message}`),
    })
  }

  const handleResume = (agentId: string) => {
    resumeMutation.mutate(agentId, {
      onSuccess: () => toast.success(`Agent ${agentId} resumed`),
      onError: (err) => toast.error(`Failed to resume: ${err.message}`),
    })
  }

  return (
    <div>
      <h1 className="text-xl font-semibold text-text-primary mb-6">Overview</h1>

      <MetricsBar
        agentCount={agents.length}
        haltedCount={haltedCount}
        allLimits={allLimits}
        agentIds={agentIds}
      />

      <AgentsTable
        agents={agents}
        limitsMap={limitsMap}
        onHalt={handleHalt}
        onResume={handleResume}
      />

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <div className="lg:col-span-2">
          <EquityChart
            curves={curves}
            agentNames={agentNames}
          />
        </div>
        <div>
          <ActivityFeed events={events} />
        </div>
      </div>
    </div>
  )
}
