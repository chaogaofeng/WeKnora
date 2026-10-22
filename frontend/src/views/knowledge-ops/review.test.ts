import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  createReviewDraft,
  reviewPayload,
  buildFilters,
  applyReviewAction,
  latestAnswerReview,
  compareOpsTimestamps,
} from './state.ts'
import type { OpsReview } from '../../api/knowledge-ops'

test('latest answer review retains the API ordering below millisecond precision', () => {
  const newest = {
    id: 'ffffffff-ffff-ffff-ffff-ffffffffffff', analysis_id: 'current', target: 'answer',
    action: 'revision_required', verdict: 'revision_required', created_at: '2026-10-21T12:00:00.123900Z',
  } as OpsReview
  const older = {
    ...newest, id: '00000000-0000-0000-0000-000000000000', action: 'accepted', verdict: 'usable',
    created_at: '2026-10-21T12:00:00.123100Z',
  } as OpsReview
  assert.equal(Date.parse(newest.created_at), Date.parse(older.created_at))
  const history = [{ ...newest, analysis_id: 'other' }, newest, older]
  assert.equal(latestAnswerReview(history, 'current'), newest)
  assert.equal(latestAnswerReview(history, 'current')?.action, 'revision_required')
})

test('publication timestamps compare sub-millisecond precision across UTC offsets', () => {
  assert.ok(compareOpsTimestamps('2026-10-21T20:00:00.123900+08:00', '2026-10-21T12:00:00.123100Z') > 0)
  assert.equal(compareOpsTimestamps('2026-10-21T20:00:00.123+08:00', '2026-10-21T12:00:00.123000Z'), 0)
  assert.ok(compareOpsTimestamps('2026-10-21T12:00:01Z', '2026-10-21T12:00:00.999999999Z') > 0)
})

test('answer review binds the diagnosis and trims its optional note', () => {
  const draft = createReviewDraft()
  draft.reason = ' Answer evidence verified '
  draft.verdict = 'supported'
  assert.deepEqual(reviewPayload(draft, 'analysis-2'), {
    target: 'answer', analysis_id: 'analysis-2', verdict: 'usable',
    action: 'accepted', reason: 'Answer evidence verified',
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

test('human quality decisions map to their own verdicts without copying the machine assessment', () => {
  const draft = createReviewDraft()
  assert.equal(draft.action, 'accepted')
  assert.equal(draft.verdict, 'usable')
  for (const [action, verdict] of [
    ['accepted', 'usable'],
    ['revision_required', 'revision_required'],
    ['excluded', 'excluded'],
    ['needs_more_evidence', 'unverifiable'],
  ] as const) {
    applyReviewAction(draft, action)
    assert.equal(draft.verdict, verdict)
    draft.verdict = 'supported'
    assert.equal(reviewPayload(draft, 'analysis').verdict, verdict)
  }
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
