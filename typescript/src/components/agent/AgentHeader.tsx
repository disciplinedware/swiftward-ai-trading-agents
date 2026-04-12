import { useState } from 'react'
import { clsx } from 'clsx'
import { Pause, Play, Shield } from 'lucide-react'
import toast from 'react-hot-toast'
import { useHaltAgent, useResumeAgent, useAgentStatus } from '@/hooks/use-risk'
import { useLimits } from '@/hooks/use-trading'

interface AgentHeaderProps {
  agentId: string
}

function StatusDot({ halted }: { halted: boolean }) {
  return (
    <span
      className={clsx(
        'inline-block h-3 w-3 rounded-full',
        halted ? 'bg-loss animate-pulse' : 'bg-profit',
      )}
    />
  )
}

export function AgentHeader({ agentId }: AgentHeaderProps) {
  const { data: status } = useAgentStatus(agentId)
  const { data: limits } = useLimits(agentId)
  const haltMutation = useHaltAgent()
  const resumeMutation = useResumeAgent()
  const [confirmingHalt, setConfirmingHalt] = useState(false)

  const isHalted = limits?.halted ?? status?.halted ?? false
  const agentName = status?.name || agentId

  const handleHalt = () => {
    haltMutation.mutate(agentId, {
      onSuccess: () => {
        toast.success(`Agent ${agentId} halted`)
        setConfirmingHalt(false)
      },
      onError: (err) => {
        toast.error(`Failed to halt: ${err.message}`)
        setConfirmingHalt(false)
      },
    })
  }

  const handleResume = () => {
    resumeMutation.mutate(agentId, {
      onSuccess: () => toast.success(`Agent ${agentId} resumed`),
      onError: (err) => toast.error(`Failed to resume: ${err.message}`),
    })
  }

  return (
    <div className="rounded-lg border border-surface-border bg-surface-card p-5 mb-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <div>
            <div className="flex items-center gap-2.5 mb-1">
              <StatusDot halted={isHalted} />
              <h1 className="text-lg font-semibold text-text-primary">
                Agent: {agentName}
              </h1>
              <span className="text-xs font-mono text-text-muted">
                ({agentId})
              </span>
            </div>
            <div className="flex items-center gap-3 text-xs text-text-secondary">
              <span className={clsx(
                'inline-flex items-center gap-1 rounded-full px-2 py-0.5 font-medium',
                isHalted
                  ? 'bg-loss/15 text-loss'
                  : 'bg-profit/15 text-profit',
              )}>
                {isHalted ? 'HALTED' : 'ACTIVE'}
              </span>
              <span className="inline-flex items-center gap-1 text-text-muted">
                <Shield size={12} />
                ERC-8004 Agent
              </span>
            </div>
          </div>
        </div>

        <div className="flex gap-2">
          {isHalted ? (
            <button
              className="inline-flex items-center gap-1.5 rounded-md bg-profit/15 px-3.5 py-2 text-sm font-medium text-profit hover:bg-profit/25 transition-colors disabled:opacity-50"
              onClick={handleResume}
              disabled={resumeMutation.isPending}
            >
              <Play size={14} />
              Resume
            </button>
          ) : confirmingHalt ? (
            <div className="flex items-center gap-2">
              <span className="text-xs text-text-muted">Halt agent?</span>
              <button
                className="inline-flex items-center gap-1.5 rounded-md bg-loss/15 px-3 py-2 text-sm font-medium text-loss hover:bg-loss/25 transition-colors disabled:opacity-50"
                onClick={handleHalt}
                disabled={haltMutation.isPending}
              >
                Confirm
              </button>
              <button
                className="inline-flex items-center gap-1.5 rounded-md bg-surface-hover px-3 py-2 text-sm font-medium text-text-secondary hover:bg-surface-border transition-colors"
                onClick={() => setConfirmingHalt(false)}
              >
                Cancel
              </button>
            </div>
          ) : (
            <button
              className="inline-flex items-center gap-1.5 rounded-md bg-loss/15 px-3.5 py-2 text-sm font-medium text-loss hover:bg-loss/25 transition-colors disabled:opacity-50"
              onClick={() => setConfirmingHalt(true)}
              disabled={haltMutation.isPending}
            >
              <Pause size={14} />
              Halt
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
