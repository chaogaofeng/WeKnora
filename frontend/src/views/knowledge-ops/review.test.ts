import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  createReviewDraft,
  reviewPayload,
  buildFilters,
  applyReviewAction,
} from './state.ts'

test('answer review binds the diagnosis and trims its optional note', () => {
  const draft = createReviewDraft()
  draft.reason = ' Answer evidence verified '
  draft.verdict = 'supported'
  assert.deepEqual(reviewPayload(draft, 'analysis-2'), {
    target: 'answer', analysis_id: 'analysis-2', verdict: 'supported',
    action: 'confirmed', reason: 'Answer evidence verified',
  })
})
test('filters keep inclusive start and exclusive end timestamps and omit empty values', () => {
  const filters = buildFilters({
    from: '2026-09-01T08:00',
    to: '2026-09-02T08:00',
    qa_type: 'rag',
    channel: '',
    search: ' question ',
  })
  assert.equal(filters.from, new Date('2026-09-01T08:00').toISOString())
  assert.equal(filters.to, new Date('2026-09-02T08:00').toISOString())
  assert.equal(filters.search, 'question')
  assert.equal(filters.channel, undefined)
})

test('evidence requests cannot imply support; confirmation restores the machine answer verdict', () => {
  const draft = createReviewDraft()
  applyReviewAction(draft, 'needs_more_evidence', 'supported')
  assert.equal(draft.verdict, 'unverifiable')
  applyReviewAction(draft, 'confirmed', 'supported')
  assert.equal(draft.verdict, 'supported')
  draft.verdict = 'partial'
  applyReviewAction(draft, 'corrected', 'supported')
  assert.equal(draft.verdict, 'partial')
})

test('review date filters apply to review timestamps with explicit UTC conversion', () => {
  const result = buildFilters({
    review_from: '2026-09-01T08:00',
    review_to: '2026-09-02T08:00',
    target: 'cause',
  })
  assert.equal(result.review_from, new Date('2026-09-01T08:00').toISOString())
  assert.equal(result.review_to, new Date('2026-09-02T08:00').toISOString())
  assert.equal(result.target, 'cause')
})
