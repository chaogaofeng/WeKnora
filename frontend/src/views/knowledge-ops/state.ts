import type {
  OpsFilters,
  AnswerReviewAction,
  OpsReview,
  ReviewInput,
} from '../../api/knowledge-ops'
export interface ReviewDraft {
  verdict: string
  action: AnswerReviewAction
  reason: string
}
export function createReviewDraft(): ReviewDraft {
  return { verdict: 'usable', action: 'accepted', reason: '' }
}
export function reviewPayload(draft: ReviewDraft, analysisID: string): ReviewInput {
  return {
    target: 'answer',
    analysis_id: analysisID,
    verdict: reviewVerdict(draft.action),
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
export function reviewVerdict(action: AnswerReviewAction): string {
  return {
    accepted: 'usable',
    revision_required: 'revision_required',
    excluded: 'excluded',
    needs_more_evidence: 'unverifiable',
  }[action]
}
export function applyReviewAction(
  draft: ReviewDraft,
  action: AnswerReviewAction,
): void {
  draft.action = action
  draft.verdict = reviewVerdict(action)
}
export function reviewNeedsReason(action: AnswerReviewAction): boolean {
  return action === 'revision_required' || action === 'needs_more_evidence'
}
export function latestAnswerReview(reviews: OpsReview[], analysisID: string): OpsReview | undefined {
  // Detail returns created_at DESC, id ASC using the database's full precision.
  // Re-sorting with Date.parse would collapse distinct sub-millisecond reviews.
  return reviews.find((review) => review.analysis_id === analysisID && review.target === 'answer')
}

// RFC3339 timestamps from Go/SQL may contain up to nine fractional digits.
// Date.parse handles time zones but discards everything below milliseconds.
export function compareOpsTimestamps(a = '', b = ''): number {
  const difference = Date.parse(a) - Date.parse(b)
  if (!Number.isFinite(difference)) return 0
  if (difference !== 0) return difference
  const remainder = (value: string) =>
    (value.match(/\.(\d+)(?:Z|[+-]\d{2}:\d{2})$/i)?.[1] || '').slice(3, 9).padEnd(6, '0')
  return remainder(a).localeCompare(remainder(b))
}
