import type {
  OpsFilters,
  ReviewAction,
  ReviewInput,
  ReviewTarget,
} from '../../api/knowledge-ops'
export interface ReviewDraft {
  verdict: string
  action: ReviewAction
  reason: string
  cause_category?: string
}
export function createReviewDrafts(): Record<ReviewTarget, ReviewDraft> {
  return {
    answer: { verdict: 'unverifiable', action: 'confirmed', reason: '' },
    cause: { verdict: 'unresolved', action: 'confirmed', reason: '' },
  }
}
export function reviewPayload(
  target: ReviewTarget,
  draft: ReviewDraft,
  analysisID: string,
): ReviewInput {
  return {
    target,
    analysis_id: analysisID,
    verdict: draft.verdict,
    action: draft.action,
    reason: draft.reason.trim(),
    ...(target === 'cause'
      ? { cause_category: draft.cause_category || 'unknown' }
      : {}),
  }
}
export function buildFilters(values: Record<string, string>): OpsFilters {
  const result: Record<string, string> = {}
  for (const [key, value] of Object.entries(values)) {
    if (!value.trim()) continue
    result[key] = ['from', 'to', 'review_from', 'review_to'].includes(key)
      ? new Date(value).toISOString()
      : value.trim()
  }
  return result as OpsFilters
}
export const answerVerdicts = [
  'supported',
  'partial',
  'contradicted',
  'unsupported',
  'unverifiable',
  'not_applicable',
]
export const causeVerdicts = ['suspected', 'evidence_backed', 'unresolved']
export const routes = [
  'auto_record',
  'human_review',
  'quality_sample',
  'technical_followup',
  'needs_evidence',
]
export function applyReviewAction(
  target: ReviewTarget,
  draft: ReviewDraft,
  action: ReviewAction,
  machineVerdict: string,
  machineCategory = 'unknown',
): void {
  draft.action = action
  if (action === 'needs_more_evidence')
    draft.verdict = target === 'answer' ? 'unverifiable' : 'unresolved'
  if (action === 'confirmed') {
    draft.verdict = machineVerdict
    if (target === 'cause') draft.cause_category = machineCategory
  }
}
