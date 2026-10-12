import { get, post, put } from '@/utils/request'

export interface OpsSettings {
  enabled: boolean
  model_id: string
  auto_record_enabled: boolean
  calibration_note: string
  sample_percent: number
  max_evidence_chars: number
  max_output_tokens: number
  timeout_seconds: number
  capture_enabled_at: string | null
  updated_at: string
}
export interface OpsClaim {
  text: string
  position: string
  verdict: string
  evidence_ids: string[]
  reason: string
}
export interface OpsCause {
  category: string
  summary: string
  evidence_ids: string[]
  counter_evidence: string[]
  status: string
}
export interface OpsReport {
  source_audit?: {
    evidence_ids: string[]
    issues: string[]
    chars: number
    char_limit: number
  }
  model_verdict?: string
  verdict_adjustment?: string
  verdict: string
  claims: OpsClaim[]
  facts: string[]
  causes: OpsCause[]
  unknowns: string[]
  review_questions: string[]
  source_check: string
  risk: string
  scope: string
}
export interface OpsAnalysis {
  cause_assessment?: {
    candidates: OpsCause[]
    unlinked: OpsCause[]
  }
  id: string
  run_id: string
  version: number
  status: string
  report: OpsReport
  route: string
  route_reason: string
  policy_version: string
  model_id: string
  model_name: string
  group_key: string
  error?: string
  created_at: string
  sample_percent: number
  sample_stratum: string
  input_tokens: number
  output_tokens: number
  duration_ms: number
}
export interface OpsCaptureCoverage {
  content_status: 'complete' | 'partial' | 'missing' | 'unknown'
  provenance_status: 'known' | 'pending' | 'unknown'
  content_gaps: string[]
  provenance_gaps: string[]
}
export interface OpsPendingItem {
  route: string
  reason: string
  target?: 'answer' | 'cause'
  cause_category?: string
  review_id?: string
  reviewer_id?: string
  created_at?: string
}
export interface OpsRun {
  source_followup_pending?: boolean
  effective_risk?: string
  source_check_display?: string
  pending_review_targets?: ('answer' | 'cause')[]
  pending_items?: OpsPendingItem[]
  id: string
  actor_id: string
  session_id: string
  message_id: string
  request_id: string
  channel: string
  qa_type: string
  execution_mode: string
  agent_id: string
  model_id: string
  question: string
  answer: string
  status: string
  capture_status: string
  coverage: OpsCaptureCoverage
  gaps: string[]
  started_at: string
  finished_at: string | null
  updated_at: string
  review_state: string
  analysis?: OpsAnalysis
}
export interface OpsEvidence {
  id: string
  run_id: string
  kind: string
  kb_id: string
  knowledge_id: string
  chunk_id: string
  page_id: string
  slug: string
  version: string
  title: string
  content: string
  hash: string
  tool_call_id: string
  round: number
  sent_to_model: boolean
  post_hoc: boolean
  truncated: boolean
  acquired_at: string
}
export type ReviewTarget = 'answer' | 'cause'
export type ReviewAction = 'confirmed' | 'corrected' | 'needs_more_evidence'
export interface ReviewInput {
  analysis_id: string
  target: ReviewTarget
  verdict: string
  action: ReviewAction
  reason: string
  cause_category?: string
}
export interface OpsReview extends ReviewInput {
  question: string
  analysis_version: number
  machine_verdict: string
  machine_causes: OpsCause[]
  id: string
  run_id: string
  reviewer_id: string
  created_at: string
}
export interface OpsDetail {
  pending_tasks: number
  run: OpsRun
  evidence: OpsEvidence[]
  analyses: OpsAnalysis[]
  reviews: OpsReview[]
}
export interface OpsPage<T> {
  items: T[]
  total: number
}
export interface OpsGroup {
  key: string
  count: number
  representative: OpsRun
}
export interface OpsOverview {
  content_coverage: {
    total: number
    complete: number
    partial: number
    missing: number
  }
  content_coverage_by_mode: Record<
    string,
    { total: number; complete: number; partial: number; missing: number }
  >
  provenance_counts: Record<string, number>
  coverage_by_mode: Record<
    string,
    { total: number; complete: number; partial: number; missing: number }
  >
  source_checks: Record<string, number>
  causes: Record<string, number>
  queue_counts: Record<string, number>
  pending_runs: number
  diagnosis_status: Record<string, number>
  sample_total: number
  sample_reviewed: number
  sample_corrected: number
  total: number
  complete: number
  partial: number
  missing: number
  by_mode: Record<string, number>
  by_route: Record<string, number>
  by_verdict: Record<string, number>
  pending_tasks: number
  review_count: number
}
export interface OpsFilters {
  risk?: string
  cause_category?: string
  group_key?: string
  target?: string
  action?: string
  reviewer_id?: string
  review_from?: string
  review_to?: string
  review_search?: string
  from?: string
  to?: string
  qa_type?: string
  channel?: string
  capture_status?: string
  content_status?: string
  provenance_status?: string
  route?: string
  verdict?: string
  search?: string
  view?: 'queue'
  page?: number
  page_size?: number
}
interface Envelope<T> {
  success: boolean
  data: T
  error?: string
}
const base = '/api/v1/knowledge-ops'
async function unwrap<T>(request: Promise<Envelope<T>>): Promise<T> {
  const response = await request
  if (!response.success) throw response
  return response.data
}
export const getOpsSettings = () =>
  unwrap(get<Envelope<OpsSettings>>(`${base}/settings`))
export const saveOpsSettings = (value: OpsSettings) =>
  unwrap(put<Envelope<OpsSettings>>(`${base}/settings`, value))
export const getOpsOverview = (params: OpsFilters) =>
  unwrap(get<Envelope<OpsOverview>>(`${base}/overview`, { params }))
export const getOpsRuns = (params: OpsFilters) =>
  unwrap(get<Envelope<OpsPage<OpsRun>>>(`${base}/runs`, { params }))
export const getOpsGroups = (params: OpsFilters) =>
  unwrap(get<Envelope<OpsPage<OpsGroup>>>(`${base}/groups`, { params }))
export const getOpsDetail = (id: string) =>
  unwrap(get<Envelope<OpsDetail>>(`${base}/runs/${encodeURIComponent(id)}`))
export const retryOpsRun = (id: string) =>
  unwrap(
    post<Envelope<{ queued: boolean }>>(
      `${base}/runs/${encodeURIComponent(id)}/retry`,
    ),
  )
export const addOpsReview = (id: string, value: ReviewInput) =>
  unwrap(
    post<Envelope<OpsReview>>(
      `${base}/runs/${encodeURIComponent(id)}/reviews`,
      value,
    ),
  )
export const getOpsReviews = (params: OpsFilters) =>
  unwrap(get<Envelope<OpsPage<OpsReview>>>(`${base}/reviews`, { params }))
export function opsErrorCode(error: unknown): string {
  if (error && typeof error === 'object') {
    const value = error as {
      $httpStatus?: number
      status?: number
      error?: string
    }
    if (value.$httpStatus === 409 || value.status === 409)
      return 'version_changed'
    if (value.$httpStatus === 403 || value.status === 403) return 'forbidden'
    if (value.$httpStatus === 404 || value.status === 404) return 'not_found'
    if (value.error) return value.error
  }
  return 'knowledge_ops_failed'
}
