import assert from 'node:assert/strict'
import { after, before, test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { createServer, type ViteDevServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { createSSRApp, h, type Component } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { createI18n } from 'vue-i18n'
import { Button, Checkbox, Icon, Tag, Tooltip } from 'tdesign-vue-next'
import type { OpsRun } from '@/api/knowledge-ops'

let server: ViteDevServer
let QueuePanel: Component
before(async () => {
  server = await createServer({
    configFile: false,
    optimizeDeps: { noDiscovery: true, entries: [] },
    plugins: [vue()],
    resolve: { alias: { '@': fileURLToPath(new URL('../../', import.meta.url)) } },
    server: { middlewareMode: true, hmr: false },
    appType: 'custom',
  })
  QueuePanel = (await server.ssrLoadModule('/src/views/knowledge-ops/QueuePanel.vue')).default
})
after(async () => { await server?.close() })

function run(overrides: Partial<OpsRun> & Record<string, unknown> = {}): OpsRun {
  return {
    id: 'run-1', actor_id: 'user-1', session_id: 'session-1', message_id: 'message-1',
    request_id: 'request-1', channel: 'web', qa_type: 'rag', execution_mode: 'normal',
    agent_id: '', model_id: 'model-1', question: '申请额度需要哪些材料？', answer: '需要合同。',
    status: 'completed', capture_status: 'complete', gaps: [],
    coverage: { content_status: 'complete', provenance_status: 'pending', content_gaps: [], provenance_gaps: ['source_version_unknown'] },
    started_at: '2026-10-01T08:00:00Z', finished_at: '2026-10-01T08:00:01Z',
    updated_at: '2026-10-01T08:01:00Z', review_state: 'unreviewed',
    analysis: {
      id: 'analysis-1', run_id: 'run-1', version: 1, status: 'succeeded',
      report: { verdict: 'supported', claims: [], facts: [], causes: [], unknowns: [], review_questions: [], source_check: 'consistent', risk: 'low', scope: 'captured content' },
      route: 'auto_record', route_reason: '原始自动记录理由', policy_version: 'v1',
      model_id: 'model-1', model_name: 'Test model', group_key: 'opaque:group:key',
      created_at: '2026-10-01T08:01:00Z', sample_percent: 0, sample_stratum: '',
      input_tokens: 10, output_tokens: 10, duration_ms: 100,
    },
    ...overrides,
  }
}

async function render(runs: OpsRun[] = [], props: Record<string, unknown> = {}) {
  const app = createSSRApp({ render: () => h(QueuePanel, {
    runs, groups: [], grouped: false, selectedGroup: '', ...props,
  }) })
  app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': {} } }))
  for (const [name, component] of Object.entries({ Button, Checkbox, Icon, Tag, Tooltip })) {
    app.component(`t-${name.toLowerCase()}`, component)
  }
  return renderToString(app)
}

// A closed disclosure's contents are retained in HTML, but are not initially visible.
const currentContent = (html: string) => html.replace(/<details\b[^>]*>[\s\S]*?<\/details>/g, '')

test('human evidence requests replace historical automatic routing as current work', async () => {
  const html = await render([run({
    review_state: 'partially_reviewed', effective_risk: 'high',
    pending_review_targets: ['cause'],
    pending_items: [{ route: 'needs_evidence', reason: '请补充 <contract> 原件', target: 'cause', cause_category: 'retrieval', review_id: 'review-1', reviewer_id: 'reviewer-7', created_at: '2026-10-02T08:00:00Z' }],
  })])
  const current = currentContent(html)
  assert.match(current, /请补充 &lt;contract&gt; 原件/)
  assert.match(current, /待补证/)
  assert.match(current, /人工补证要求/)
  assert.match(current, /复核人[^<]*reviewer-7/)
  assert.match(current, /2026\/10\/2/)
  assert.match(current, /仍需复核[^<]*根因/)
  assert.doesNotMatch(current, /仍需复核[^<]*回答/)
  assert.doesNotMatch(current, /机器自动记录|原始自动记录理由/)
  assert.equal(html.match(/原始自动记录理由/g)?.length, 1)
  assert.match(html, /<details\b(?![^>]*\bopen)[^>]*>[\s\S]*原始机器分流[\s\S]*机器自动记录/)
})

test('effective risk and normalized RAG source assessment override the original report', async () => {
  const html = await render([run({ effective_risk: 'high', source_check_display: 'not_applicable', pending_items: [], pending_review_targets: [] })])
  assert.match(currentContent(html), /风险 · 高/)
  assert.match(html, /不适用 · 无 Wiki 对照/)
  assert.doesNotMatch(html, /知识来源一致性核验[^<]*一致/)
})

test('successful original answer support remains scannable without claiming the original risk is current', async () => {
  const html = await render([run({ effective_risk: 'high', pending_items: [], pending_review_targets: [] })])
  assert.match(currentContent(html), /机器原结论 · 得到支持/)
  assert.match(html, /原始机器风险: 低/)
  assert.match(currentContent(html), /风险 · 高/)
})

test('long machine causes and adjudication questions are initially collapsed', async () => {
  const item = run({ pending_items: [{ route: 'human_review', reason: '当前需要核对合同期限' }], pending_review_targets: ['answer'] })
  item.analysis!.report.causes = [{ category: 'retrieval', summary: '冗长的机器根因说明', status: 'suspected', evidence_ids: [], counter_evidence: [] }]
  item.analysis!.report.unknowns = ['冗长的机器未知项']
  item.analysis!.report.review_questions = ['冗长的机器待裁决问题']
  const html = await render([item])
  assert.match(currentContent(html), /当前需要核对合同期限/)
  assert.doesNotMatch(currentContent(html), /冗长的机器/)
  assert.match(html, /<details\b(?![^>]*\bopen)[^>]*>[\s\S]*冗长的机器根因说明[\s\S]*冗长的机器待裁决问题[\s\S]*冗长的机器未知项/)
})

test('lengthy human evidence reasons retain a readable preview and an expandable complete request', async () => {
  const reason = `请提供签署版本。${'补充合同及对应附件。'.repeat(60)}核验完成后再次提交。`
  const html = await render([run({ pending_items: [{ route: 'needs_evidence', reason, review_id: 'review-long' }] })])
  assert.match(html, /<details\b(?![^>]*\bopen)[^>]*>[\s\S]*?<summary[^>]*>[\s\S]*?请提供签署版本。[\s\S]*?查看完整要求[\s\S]*?<\/summary>/)
  assert.ok(html.includes(reason), 'The full request must remain available to expand')
  assert.doesNotMatch(currentContent(html), /核验完成后再次提交。/)
})

test('human reasons that match a translation key remain verbatim', async () => {
  const html = await render([run({
    pending_items: [{ route: 'needs_evidence', reason: 'confirmed', review_id: 'review-literal' }],
  })])
  assert.match(currentContent(html), /class="pending-reason"[^>]*>\s*confirmed\s*<\/p>/)
})

test('a failed diagnosis clearly has no valid verdict and preserves its technical task', async () => {
  const item = run({ pending_items: [{ route: 'technical_followup', reason: '检查模型服务后重试' }], pending_review_targets: [] })
  item.analysis!.status = 'failed'
  item.analysis!.error = 'analysis_timeout'
  item.analysis!.report.verdict = 'unverifiable'
  const html = await render([item])
  assert.match(currentContent(html), /尚无有效判定/)
  assert.match(currentContent(html), /检查模型服务后重试/)
  assert.doesNotMatch(html, /回答判定[^<]*不可核验/)
})

test('a successfully diagnosed cancelled answer keeps its valid verdict and technical follow-up', async () => {
  const html = await render([run({
    status: 'cancelled',
    pending_items: [{ route: 'technical_followup', reason: '排查问答中断原因' }],
    pending_review_targets: ['answer'],
  })])
  assert.match(currentContent(html), /机器原结论 · 得到支持/)
  assert.match(currentContent(html), /技术排查/)
  assert.match(currentContent(html), /排查问答中断原因/)
  assert.doesNotMatch(html, /尚无有效判定/)
})

test('hiding source metadata keeps active source verification work and its reason visible', async () => {
  const html = await render([run({
    source_followup_pending: true,
    pending_items: [{ route: 'source_verification', reason: '核验回答时原文版本' }],
    pending_review_targets: [],
  })], { showSourceVersions: false })
  assert.match(currentContent(html), /来源待核验/)
  assert.match(currentContent(html), /核验回答时原文版本/)
  assert.doesNotMatch(html, /来源版本待核验|来源版本未记录/)
})

test('older responses retain an explicit open source task without treating machine routing as current', async () => {
  const html = await render([run({ source_followup_pending: true })], { showSourceVersions: false })
  assert.match(currentContent(html), /来源待核验/)
  assert.doesNotMatch(currentContent(html), /机器自动记录|原始自动记录理由/)
})

test('queue empty states distinguish filters, completed groups and absence of current tasks', async () => {
  const empty = await render([], { total: 0 })
  const filtered = await render([], { filtered: true, total: 0 })
  const group = await render([], { selectedGroup: 'opaque:group:key', total: 0 })
  const filteredGroup = await render([], { selectedGroup: 'opaque:group:key', filtered: true, total: 0 })
  assert.match(empty, /当前暂无待办/)
  assert.match(filtered, /没有符合筛选条件的待办/)
  assert.match(group, /本组已无待办/)
  assert.match(filteredGroup, /没有符合筛选条件的待办/)
  for (const html of [empty, filtered, group, filteredGroup]) {
    assert.doesNotMatch(html, /在诊断设置中启用采集|opaque:group:key/)
  }
})

test('group context shows counts without exposing the opaque group fingerprint', async () => {
  const item = run({ pending_items: [], pending_review_targets: [] })
  const plain = await render([item])
  const grouped = await render([], { grouped: true, groups: [{ key: 'opaque:group:key', count: 3, representative: item }] })
  const selected = await render([item], { selectedGroup: 'opaque:group:key', total: 3 })
  assert.doesNotMatch(plain, /仅按同来源、版本和适用范围归组/)
  assert.match(grouped, /仅按同来源、版本和适用范围归组/)
  assert.match(grouped, /查看组内案例/)
  assert.match(selected, /当前问题组/)
  assert.doesNotMatch(selected, /opaque:group:key/)
})
