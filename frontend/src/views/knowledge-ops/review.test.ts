import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createReviewDrafts, reviewPayload, buildFilters, applyReviewAction } from './state.ts'

test('answer and cause reviews have independent drafts and payloads', () => {
  const drafts = createReviewDrafts()
  drafts.answer.reason = 'Answer evidence verified'
  drafts.answer.verdict = 'supported'
  drafts.cause.reason = 'Historical version is missing'
  drafts.cause.action = 'needs_more_evidence'
  assert.deepEqual(reviewPayload('answer', drafts.answer, 'analysis-2'), { target: 'answer', analysis_id: 'analysis-2', verdict: 'supported', action: 'confirmed', reason: 'Answer evidence verified' })
  assert.equal(reviewPayload('cause', drafts.cause, 'analysis-2').reason, 'Historical version is missing')
  assert.equal(drafts.answer.action, 'confirmed')
})
test('filters keep inclusive start and exclusive end timestamps and omit empty values', () => {
  const filters = buildFilters({ from: '2026-09-01T08:00', to: '2026-09-02T08:00', qa_type: 'rag', channel: '', search: ' question ' })
  assert.equal(filters.from, new Date('2026-09-01T08:00').toISOString())
  assert.equal(filters.to, new Date('2026-09-02T08:00').toISOString())
  assert.equal(filters.search, 'question')
  assert.equal(filters.channel, undefined)
})

test('needs-evidence constrains answer and cause verdicts without changing the other object', () => {
  const drafts = createReviewDrafts()
  drafts.answer.verdict = 'supported'
  drafts.cause.verdict = 'evidence_backed'
  applyReviewAction('answer', drafts.answer, 'needs_more_evidence', 'supported')
  assert.equal(drafts.answer.verdict, 'unverifiable')
  assert.equal(drafts.cause.verdict, 'evidence_backed')
  applyReviewAction('cause', drafts.cause, 'needs_more_evidence', 'suspected', 'retrieval')
  assert.equal(drafts.cause.verdict, 'unresolved')
  assert.equal(reviewPayload('answer', drafts.answer, 'v2').cause_category, undefined)
})
test('confirm restores the machine conclusion while corrected preserves the selected correction', () => {
  const draft = createReviewDrafts().cause
  draft.verdict = 'unresolved'
  draft.cause_category = 'capture'
  applyReviewAction('cause', draft, 'confirmed', 'suspected', 'retrieval')
  assert.equal(draft.verdict, 'suspected')
  assert.equal(draft.cause_category, 'retrieval')
  draft.verdict = 'evidence_backed'
  draft.cause_category = 'source_conflict'
  applyReviewAction('cause', draft, 'corrected', 'suspected', 'retrieval')
  assert.equal(draft.verdict, 'evidence_backed')
  assert.equal(draft.cause_category, 'source_conflict')
})
