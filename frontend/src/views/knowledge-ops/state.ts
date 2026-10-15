import type {
  OpsFilters,
  ReviewAction,
  ReviewInput,
} from '../../api/knowledge-ops'
export interface ReviewDraft {
  verdict: string
  action: ReviewAction
  reason: string
}
export function createReviewDraft(): ReviewDraft {
  return { verdict: 'unverifiable', action: 'confirmed', reason: '' }
}
export function reviewPayload(draft: ReviewDraft, analysisID: string): ReviewInput {
  return {
    target: 'answer',
    analysis_id: analysisID,
    verdict: draft.verdict,
    action: draft.action,
    reason: draft.reason.trim(),
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
export const routes = [
  'not_required',
  'auto_record',
  'human_review',
  'quality_sample',
  'technical_followup',
  'needs_evidence',
  'source_verification',
]
export function applyReviewAction(
  draft: ReviewDraft,
  action: ReviewAction,
  machineVerdict: string,
): void {
  draft.action = action
  if (action === 'needs_more_evidence') draft.verdict = 'unverifiable'
  if (action === 'confirmed') draft.verdict = machineVerdict
}
