import type { EvidenceTrace } from '@/types/api'

export async function getEvidence(hash: string): Promise<EvidenceTrace> {
  const res = await fetch(`/v1/evidence/${encodeURIComponent(hash)}`)

  if (res.status === 404) {
    throw new Error(`Evidence not found for hash: ${hash}`)
  }

  if (!res.ok) {
    throw new Error(`Evidence API error: HTTP ${res.status}`)
  }

  return res.json() as Promise<EvidenceTrace>
}
