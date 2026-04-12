import { useState } from 'react'
import { useParams } from 'react-router-dom'
import { clsx } from 'clsx'
import { Briefcase, ArrowLeftRight, Fingerprint, Gauge } from 'lucide-react'
import { AgentHeader } from '@/components/agent/AgentHeader'
import { PortfolioTab } from '@/components/agent/PortfolioTab'
import { TradesTab } from '@/components/agent/TradesTab'
import { EvidenceTab } from '@/components/agent/EvidenceTab'
import { LimitsTab } from '@/components/agent/LimitsTab'

type Tab = 'portfolio' | 'trades' | 'evidence' | 'limits'

const TABS: { id: Tab; label: string; icon: React.ComponentType<{ size: number; className?: string }> }[] = [
  { id: 'portfolio', label: 'Portfolio', icon: Briefcase },
  { id: 'trades', label: 'Trades', icon: ArrowLeftRight },
  { id: 'evidence', label: 'Evidence', icon: Fingerprint },
  { id: 'limits', label: 'Limits', icon: Gauge },
]

export function AgentDetail() {
  const { id } = useParams<{ id: string }>()
  const [activeTab, setActiveTab] = useState<Tab>('portfolio')

  if (!id) {
    return (
      <div className="text-text-muted text-sm">No agent ID provided.</div>
    )
  }

  return (
    <div>
      <AgentHeader agentId={id} />

      {/* Sub-tab navigation */}
      <div className="flex gap-1 border-b border-surface-border mb-6">
        {TABS.map((tab) => {
          const Icon = tab.icon
          return (
            <button
              key={tab.id}
              className={clsx(
                'inline-flex items-center gap-1.5 px-4 py-2.5 text-sm font-medium border-b-2 transition-colors',
                activeTab === tab.id
                  ? 'border-accent text-accent'
                  : 'border-transparent text-text-secondary hover:text-text-primary hover:border-surface-border',
              )}
              onClick={() => setActiveTab(tab.id)}
            >
              <Icon size={14} className={activeTab === tab.id ? 'text-accent' : 'text-text-muted'} />
              {tab.label}
            </button>
          )
        })}
      </div>

      {/* Tab content */}
      {activeTab === 'portfolio' && <PortfolioTab agentId={id} />}
      {activeTab === 'trades' && <TradesTab agentId={id} />}
      {activeTab === 'evidence' && <EvidenceTab agentId={id} />}
      {activeTab === 'limits' && <LimitsTab agentId={id} />}
    </div>
  )
}
