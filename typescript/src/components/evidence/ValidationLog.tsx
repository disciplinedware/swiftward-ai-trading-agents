import { useState } from 'react'
import { clsx } from 'clsx'
import {
  ClipboardCheck,
  Copy,
  ExternalLink,
  Download,
  Check,
} from 'lucide-react'
import { formatTime, formatCurrency, formatQty } from '@/lib/format'
import type { TradeEvent } from '@/hooks/use-all-trades'

function truncateHash(hash: string): string {
  if (hash.length <= 14) return hash
  return hash.slice(0, 8) + '...' + hash.slice(-4)
}

// Compute a mock validator score from trade data.
// In production this comes from the ERC-8004 Validation Registry.
function computeValidatorScore(event: TradeEvent): number {
  if (event.trade.status !== 'fill') return 0
  // Base score from trade completeness
  let score = 70
  if (event.trade.decision_hash) score += 10
  if (event.trade.fill?.id) score += 5
  if (event.trade.fill?.price) score += 5
  if (event.trade.portfolio?.value_after) score += 5
  if (event.trade.pnl_value !== undefined) score += 5
  return Math.min(score, 100)
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = () => {
    navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  return (
    <button
      className="text-text-muted hover:text-accent transition-colors"
      onClick={handleCopy}
      title="Copy to clipboard"
    >
      {copied ? <Check size={10} className="text-profit" /> : <Copy size={10} />}
    </button>
  )
}

function EvidenceLinks({ hash }: { hash: string }) {
  const evidenceUrl = `/v1/evidence/${hash}`
  const fullUrl = `${window.location.origin}${evidenceUrl}`

  return (
    <div className="rounded-lg border border-surface-border bg-surface-base p-3 text-xs">
      <div className="flex items-center gap-2 mb-2">
        <span className="text-text-muted">Public Evidence URL:</span>
      </div>
      <div className="flex items-center gap-2 bg-surface-card rounded-md px-3 py-2 border border-surface-border">
        <span className="font-mono text-text-secondary flex-1 truncate">{fullUrl}</span>
        <CopyButton text={fullUrl} />
        <a
          href={evidenceUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="text-text-muted hover:text-accent transition-colors"
          title="Open in browser"
        >
          <ExternalLink size={10} />
        </a>
        <a
          href={evidenceUrl}
          download={`evidence-${hash.slice(0, 8)}.json`}
          className="text-text-muted hover:text-accent transition-colors"
          title="Download JSON"
        >
          <Download size={10} />
        </a>
      </div>
      <p className="text-[10px] text-text-muted mt-2">
        Anyone can independently verify this trading decision.
      </p>
    </div>
  )
}

export function ValidationLog({ trades }: { trades: TradeEvent[] }) {
  const [expandedHash, setExpandedHash] = useState<string | null>(null)

  // Only show approved trades with decision hashes (validated on-chain)
  const approvedTrades = [...trades]
    .filter((t) => t.trade.status === 'fill' && t.trade.decision_hash)
    .reverse() // newest first

  return (
    <div className="rounded-lg border border-surface-border bg-surface-card overflow-hidden">
      <div className="flex items-center gap-2 px-5 py-4 border-b border-surface-border">
        <ClipboardCheck size={16} className="text-profit" />
        <h2 className="text-sm font-medium text-text-primary">Validation Log</h2>
        <span className="ml-auto text-xs text-text-muted">{approvedTrades.length} validated</span>
      </div>

      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-surface-border">
              <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Time</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Agent</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Trade</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Decision Hash</th>
              <th className="px-4 py-2.5 text-right text-xs font-medium text-text-muted">Validator Score</th>
              <th className="px-4 py-2.5 text-right text-xs font-medium text-text-muted">Evidence</th>
            </tr>
          </thead>
          <tbody>
            {approvedTrades.length === 0 ? (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-xs text-text-muted">
                  No validated trades yet. Approved trades with decision hashes will appear here.
                </td>
              </tr>
            ) : (
              approvedTrades.slice(0, 50).map((event, i) => {
                const hash = event.trade.decision_hash!
                const score = computeValidatorScore(event)
                const isExpanded = expandedHash === hash

                return (
                  <ValidationRow
                    key={`${hash}-${i}`}
                    event={event}
                    score={score}
                    isExpanded={isExpanded}
                    onToggle={() => setExpandedHash(isExpanded ? null : hash)}
                  />
                )
              })
            )}
          </tbody>
        </table>
      </div>

      {approvedTrades.length > 50 && (
        <div className="px-5 py-3 border-t border-surface-border text-center">
          <span className="text-xs text-text-muted">
            Showing 50 of {approvedTrades.length} validated trades
          </span>
        </div>
      )}
    </div>
  )
}

function ValidationRow({
  event,
  score,
  isExpanded,
  onToggle,
}: {
  event: TradeEvent
  score: number
  isExpanded: boolean
  onToggle: () => void
}) {
  const { trade, agentId } = event
  const hash = trade.decision_hash!

  return (
    <>
      <tr
        className="border-b border-surface-border hover:bg-surface-hover cursor-pointer transition-colors"
        onClick={onToggle}
      >
        <td className="px-4 py-2.5 text-xs font-mono text-text-muted whitespace-nowrap">
          {formatTime(trade.timestamp)}
        </td>
        <td className="px-4 py-2.5 text-xs font-mono text-text-primary">{agentId}</td>
        <td className="px-4 py-2.5 text-xs text-text-secondary">
          {trade.side.toUpperCase()} {trade.pair}{' '}
          {trade.fill?.qty && <span className="text-text-muted">{formatQty(trade.fill.qty)}</span>}
          {trade.fill?.price && (
            <span className="text-text-muted"> @ {formatCurrency(trade.fill.price)}</span>
          )}
        </td>
        <td className="px-4 py-2.5">
          <div className="flex items-center gap-1.5">
            <span className="text-xs font-mono text-text-secondary">{truncateHash(hash)}</span>
            <CopyButton text={hash} />
          </div>
        </td>
        <td className="px-4 py-2.5 text-right">
          <span className={clsx(
            'text-xs font-mono font-semibold',
            score >= 90 ? 'text-profit' : score >= 70 ? 'text-info' : 'text-warning',
          )}>
            {score}/100
          </span>
        </td>
        <td className="px-4 py-2.5 text-right">
          <a
            href={`/v1/evidence/${hash}`}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1 text-xs text-accent hover:text-accent-hover transition-colors"
            onClick={(e) => e.stopPropagation()}
          >
            API
            <ExternalLink size={10} />
          </a>
        </td>
      </tr>

      {isExpanded && (
        <tr>
          <td colSpan={6} className="bg-surface-base px-6 py-4">
            <EvidenceLinks hash={hash} />
          </td>
        </tr>
      )}
    </>
  )
}
