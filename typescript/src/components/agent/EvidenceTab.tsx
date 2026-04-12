import { useMemo } from 'react'
import { useTradeHistory } from '@/hooks/use-trading'
import { HashChain, CompactTimeline } from '@/components/evidence/HashChain'
import { ReputationScores } from '@/components/evidence/ReputationScores'
import type { TradeEvent } from '@/hooks/use-all-trades'

export function EvidenceTab({ agentId }: { agentId: string }) {
  const { data } = useTradeHistory(agentId, 200)

  const trades: TradeEvent[] = useMemo(
    () => (data?.trades ?? []).map((t) => ({ trade: t, agentId })),
    [data, agentId],
  )

  const hashedCount = trades.filter((t) => t.trade.decision_hash).length

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <span className="text-xs text-text-secondary">
          {trades.length} decisions / {hashedCount} with hash proof
        </span>
      </div>

      <CompactTimeline trades={trades} />

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <HashChain trades={trades} />
        <ReputationScores trades={trades} agentIds={[agentId]} />
      </div>
    </div>
  )
}
