import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import vm from 'node:vm'
import { setImmediate } from 'node:timers/promises'
import ts from 'typescript'
import * as vue from 'vue'
import * as reviewState from './state.ts'

// Run the actual SFC setup code with Vue reactivity. Only HTTP and app context
// are substituted so delayed responses can be delivered in a deterministic order.
function setup(
  file: string,
  props: Record<string, unknown>,
  dependencies: Record<string, unknown> = {},
) {
  const source = readFileSync(new URL(file, import.meta.url), 'utf8')
  const setupSource = source.match(
    /<script setup lang="ts">([\s\S]*?)<\/script>/,
  )![1]
  const parsed = ts.createSourceFile(
    file,
    setupSource,
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.TS,
  )
  const script = parsed.statements
    .filter((statement) => !ts.isImportDeclaration(statement))
    .map((statement) => statement.getFullText(parsed))
    .join('\n')
  const names = [
    'load',
    'onToggle',
    'conversation',
    'selectedCoverage',
    'save',
    'rerun',
    'saveReview',
    'submit',
    'actionChanged',
    'draft',
    'models',
    'notice',
    'loadError',
    'localError',
    'saveError',
    'savedReviews',
    'reviewErrors',
    'retrying',
    'evidenceGroups',
    'selectedID',
    'showEvidence',
    'sourceEvidenceNotice',
    'sourceIssues',
    'sourcePending',
    'showSourceSection',
    'visibleSourceIssues',
    'hiddenVersionIssues',
    'versionGapSources',
    'showSourceVersions',
    'setShowSourceVersions',
    'successfulCount',
    'unassessedCount',
    'supportRows',
    'causeRows',
    'rows',
    'causeCandidates',
    'unlinkedCauses',
    'filterCause',
    'filters',
    'draftFilters',
    'page',
    'grouped',
    'selectedGroup',
    'groups',
    'runs',
    'total',
    'filterQueue',
    'applyFilters',
    'changeFilter',
    'removeFilter',
    'resetFilters',
    'tab',
    'filterError',
    'reload',
    'effectiveRisk',
    'displaySourceCheck',
    'pendingEvidenceRequests',
    'pendingReviewTargets',
    'answerReview',
    'answerReviewComplete',
    'showReviewEditor',
    'rewrite',
    'rewriteStatus',
    'searchQueries',
    'showRewriteText',
  ]
  const expose = names
    .map(
      (name) => `${name}: typeof ${name} === 'undefined' ? undefined : ${name}`,
    )
    .join(',')
  const events: unknown[][] = []
  const auth = vue.reactive({ effectiveTenantId: 1, currentUserId: 'user-1' })
  const context = {
    ...vue,
    ...reviewState,
    TextEncoder,
    defineProps: () => props,
    defineEmits:
      () =>
      (...args: unknown[]) =>
        events.push(args),
    useAuthStore: () => auth,
    useOpsText: () => ({
      k: (x: string) => x,
      label: (x: string) => x,
      labelVerdict: (x: string) => x,
      date: (x: string) => x,
    }),
    opsErrorCode: () => 'knowledge_ops_failed',
    safeGetItem: () => null,
    safeSetItem: () => {},
    ...dependencies,
  }
  const scope = vue.effectScope()
  scope.run(() =>
    vm.runInNewContext(
      ts.transpileModule(`${script}\nglobalThis.api = {${expose}}`, {
        compilerOptions: {
          target: ts.ScriptTarget.ES2022,
          module: ts.ModuleKind.None,
        },
      }).outputText,
      context,
    ),
  )
  return {
    api: (context as typeof context & { api: Record<string, any> }).api,
    auth,
    events,
    stop: () => scope.stop(),
  }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}

function queueWorkbench(grouped = true) {
  const requests: Record<string, any>[] = []
  const response = { total: 20 }
  const fetchPage = async (params: Record<string, any>) => {
    requests.push({ ...params })
    return {
      total: response.total,
      items: params.page === 1 ? [{ id: 'remaining-case' }] : [],
    }
  }
  const h = setup('Workbench.vue', {}, {
    useRouter: () => ({ replace: () => {} }),
    useRoute: () => ({ query: { tab: 'queue' } }),
    onMounted: () => {},
    getOpsSettings: async () => ({}),
    getOpsOverview: async () => ({}),
    getOpsGroups: fetchPage,
    getOpsRuns: fetchPage,
  })
  h.api.grouped.value = grouped
  return { ...h, requests, response }
}

test('session filter applies to every view and its stats, and can be removed or reset', async () => {
  for (const tab of ['overview', 'queue', 'records']) {
    const requests: { endpoint: string; params: Record<string, any> }[] = []
    const capture = (endpoint: string) => async (params: Record<string, any>) => {
      requests.push({ endpoint, params: { ...params } })
      return { total: 0, items: [] }
    }
    const h = setup('Workbench.vue', {}, {
      useRouter: () => ({ replace: () => {} }),
      useRoute: () => ({ query: { tab } }),
      onMounted: () => {},
      getOpsSettings: async () => ({}),
      getOpsOverview: capture('overview'),
      getOpsGroups: capture('groups'),
      getOpsRuns: capture('runs'),
      getOpsReviews: capture('reviews'),
    })
    h.api.changeFilter('session_id', ' session-1 ')
    h.api.changeFilter(tab === 'records' ? 'review_search' : 'search', 'keyword')
    h.api.page.value = 3
    h.api.applyFilters()
    await setImmediate()
    assert.equal(h.api.page.value, 1)
    assert.deepEqual(requests.map((r) => r.endpoint), tab === 'records' ? ['reviews'] : ['overview', tab === 'queue' ? 'groups' : 'runs'])
    assert.ok(requests.every((r) => r.params.session_id === 'session-1'), tab)

    requests.length = 0
    h.api.removeFilter('session_id')
    await setImmediate()
    assert.ok(requests.every((r) => r.params.session_id === undefined))
    assert.ok(requests.every((r) => r.params[tab === 'records' ? 'review_search' : 'search'] === 'keyword'))

    h.api.changeFilter('session_id', 'session-2')
    h.api.applyFilters()
    await setImmediate()
    requests.length = 0
    h.api.resetFilters()
    await setImmediate()
    assert.ok(requests.every((r) => r.params.session_id === undefined))
    h.stop()
  }
})

test('queue count drilldown changes only the route in the counted scope', async () => {
  const h = queueWorkbench()
  h.api.filters.value = { channel: 'web', search: 'applied' }
  h.api.draftFilters.channel = 'api'
  h.api.draftFilters.search = 'unapplied'
  h.api.filterQueue('human_review')
  await setImmediate()
  assert.equal(h.requests[0].channel, 'web')
  assert.equal(h.requests[0].search, 'applied')
  assert.equal(h.requests[0].route, 'human_review')
  h.api.filterQueue('human_review')
  await setImmediate()
  assert.equal(h.requests[1].route, undefined, 'clicking again clears only the route')
  assert.equal(h.requests[1].channel, 'web')
  h.stop()
})

for (const grouped of [true, false]) {
  test(`reviewing the last item on the last ${grouped ? 'group' : 'case'} page returns to remaining work`, async () => {
    const h = queueWorkbench(grouped)
    h.api.page.value = 2
    await h.api.reload()
    assert.equal(h.api.page.value, 1)
    assert.deepEqual(h.requests.map((r) => r.page), [2, 1])
    const rows = grouped ? h.api.groups : h.api.runs
    assert.equal(rows.value[0]?.id, 'remaining-case')
    assert.equal(h.api.total.value, 20)
    h.stop()
  })
}

test('an invalid period preserves the selected group and does not reload', () => {
  const h = queueWorkbench()
  h.api.selectedGroup.value = 'group-1'
  h.api.page.value = 2
  h.api.runs.value = [{ id: 'group-member' }]
  h.api.draftFilters.from = '2026-10-10T00:00:00Z'
  h.api.draftFilters.to = '2026-10-09T00:00:00Z'
  h.api.applyFilters()
  assert.equal(h.api.selectedGroup.value, 'group-1')
  assert.equal(h.api.page.value, 2)
  assert.equal(h.api.runs.value[0].id, 'group-member')
  assert.equal(h.api.filterError.value, 'dateInvalid')
  assert.equal(h.requests.length, 0)
  h.stop()
})

test('detail shows only current answer evidence requests and leaves historical cause notes in the audit', async () => {
  const latest = { id: 'latest', version: 2, report: { risk: 'low', source_check: 'not_checked' } }
  const historical = { id: 'old', version: 1, report: { risk: 'medium', source_check: 'unresolved' } }
  const h = setup('DetailDrawer.vue', vue.reactive({ runID: 'A', enabled: true }), {
    getOpsDetail: async () => ({
      ...detail('A'),
      run: {
        id: 'A', analysis: latest, effective_risk: 'high', source_check_display: 'not_applicable',
        pending_review_targets: ['answer', 'cause'],
        pending_items: [
          { route: 'source_verification', reason: 'source unknown' },
          { route: 'needs_evidence', target: 'cause', cause_category: 'retrieval', review_id: 'request-1', reason: 'missing retrieval logs' },
          { route: 'needs_evidence', target: 'answer', review_id: 'request-2', reason: 'missing original terms' },
        ],
      },
      analyses: [latest, historical],
    }),
  })
  await setImmediate()
  assert.equal(h.api.effectiveRisk?.value, 'high')
  assert.equal(h.api.displaySourceCheck?.value, 'not_applicable')
  assert.equal(h.api.pendingEvidenceRequests?.value.length, 1)
  assert.equal(h.api.pendingEvidenceRequests.value[0].reason, 'missing original terms')
  assert.deepEqual(Array.from(h.api.pendingReviewTargets.value), ['answer'])
  h.api.selectedID.value = 'old'
  assert.equal(h.api.effectiveRisk.value, 'medium')
  assert.equal(h.api.displaySourceCheck.value, 'unresolved')
  assert.equal(h.api.pendingEvidenceRequests.value.length, 0)
  assert.equal(h.api.pendingReviewTargets.value.length, 0)
  h.stop()
})

test('a completed answer review can collapse independently of automatic causes and legacy cause reviews', async () => {
  const analysis = { id: 'latest', version: 2, report: { verdict: 'unverifiable', causes: [{ category: 'capture', status: 'unresolved', summary: 'Missing source content' }] } }
  const h = setup('DetailDrawer.vue', vue.reactive({ runID: 'A', enabled: true }), {
    getOpsDetail: async () => ({
      ...detail('A'),
      run: { id: 'A', analysis, review_state: 'reviewed' },
      analyses: [analysis, { id: 'old', version: 1, report: {} }],
      reviews: [
        { id: 'old-cause', analysis_id: 'latest', target: 'cause', action: 'needs_more_evidence', verdict: 'unresolved' },
        { id: 'answer', analysis_id: 'latest', target: 'answer', action: 'corrected', verdict: 'partial', reason: 'Missing condition' },
      ],
    }),
  })
  await setImmediate()
  assert.equal(h.api.answerReviewComplete?.value, true)
  assert.equal(h.api.answerReview?.value.verdict, 'partial')
  assert.equal(h.api.showReviewEditor?.value, false)
  assert.equal(h.api.causeCandidates.value[0].summary, 'Missing source content', 'Automatic reasons remain available')
  h.api.showReviewEditor.value = true
  h.api.selectedID.value = 'old'
  await vue.nextTick()
  assert.equal(h.api.answerReviewComplete.value, false, 'A review cannot carry across analysis versions')
  assert.equal(h.api.showReviewEditor.value, false)
  h.stop()
})
const settings = (sample: number) => ({
  enabled: false,
  model_id: '',
  auto_record_enabled: false,
  calibration_note: '',
  sample_percent: sample,
})
const detail = (id: string) => ({
  run: { id },
  analyses: [],
  evidence: [],
  reviews: [],
})

test('source version display is an account preference and never applies filters or sends requests', async () => {
  const storage = new Map<string, string>()
  let requests = 0
  const h = setup(
    'Workbench.vue',
    {},
    {
      useRouter: () => ({ replace: () => {} }),
      useRoute: () => ({ query: {} }),
      onMounted: () => {},
      safeGetItem: (key: string) => storage.get(key) ?? null,
      safeSetItem: (key: string, value: string) => storage.set(key, value),
      getOpsSettings: () => {
        requests++
        throw new Error('unexpected request')
      },
    },
  )
  assert.equal(h.api.showSourceVersions?.value, true)
  h.api.filters.value = { search: 'applied' }
  h.api.draftFilters.search = 'unapplied'
  h.api.page.value = 3
  h.api.setShowSourceVersions(false)
  await vue.nextTick()
  assert.equal(h.api.showSourceVersions.value, false)
  h.auth.currentUserId = 'user-2'
  await vue.nextTick()
  assert.equal(
    h.api.showSourceVersions.value,
    true,
    'another account keeps its own preference',
  )
  h.auth.currentUserId = 'user-1'
  await vue.nextTick()
  assert.equal(
    h.api.showSourceVersions.value,
    false,
    'returning to the account restores its choice',
  )
  assert.equal(h.api.filters.value.search, 'applied')
  assert.equal(h.api.draftFilters.search, 'unapplied')
  assert.equal(h.api.page.value, 3)
  assert.equal(requests, 0)
  h.stop()
})

test('hiding source versions folds only version gaps while keeping lookup failures and source contradictions', async () => {
  const analysis = {
    id: 'analysis',
    version: 1,
    status: 'succeeded',
    report: {
      source_check: 'inconsistent',
      source_audit: {
        issues: ['source_linked_versions_unknown', 'source_lookup_failed'],
      },
    },
  }
  const props = vue.reactive({
    runID: 'A',
    enabled: true,
    showSourceVersions: false,
  })
  const h = setup('DetailDrawer.vue', props, {
    getOpsDetail: async () => ({
      ...detail('A'),
      run: {
        id: 'A',
        analysis,
        source_followup_pending: true,
        coverage: { provenance_gaps: ['wiki_linked_summary_versions_unknown'] },
      },
      analyses: [analysis],
    }),
  })
  await setImmediate()
  assert.deepEqual(Array.from(h.api.visibleSourceIssues?.value ?? []), [
    'source_lookup_failed',
  ])
  assert.deepEqual(Array.from(h.api.hiddenVersionIssues.value), [
    'wiki_linked_summary_versions_unknown',
  ])
  assert.equal(h.api.sourcePending.value, true)
  assert.equal(analysis.report.source_check, 'inconsistent')
  props.showSourceVersions = true
  assert.equal(h.api.hiddenVersionIssues.value.length, 0)
  assert.equal(h.api.visibleSourceIssues.value.length, 2)
  h.stop()
})

test('version gaps identify consumed unversioned summaries without including candidates or later lookups', async () => {
  const h = setup(
    'DetailDrawer.vue',
    vue.reactive({ runID: 'A', enabled: true }),
    {
      getOpsDetail: async () => ({
        ...detail('A'),
        evidence: [
          {
            id: 'used',
            kind: 'wiki_summary',
            title: '实际缺版本的关联页',
            kb_id: 'kb',
            page_id: 'p',
            sent_to_model: true,
            version: 'unknown',
          },
          {
            id: 'repeat',
            kind: 'wiki_summary',
            kb_id: 'kb',
            page_id: 'p',
            sent_to_model: true,
            version: 'unknown',
          },
          {
            id: 'known',
            kind: 'wiki_summary',
            kb_id: 'kb',
            page_id: 'q',
            sent_to_model: true,
            version: '7',
          },
          {
            id: 'candidate',
            kind: 'wiki_summary',
            kb_id: 'kb',
            page_id: 'r',
            sent_to_model: false,
            version: 'unknown',
          },
          {
            id: 'later',
            kind: 'wiki_summary',
            kb_id: 'kb',
            page_id: 's',
            sent_to_model: true,
            post_hoc: true,
            version: 'unknown',
          },
        ],
      }),
    },
  )
  await setImmediate()
  assert.equal(typeof h.api.versionGapSources, 'function')
  assert.deepEqual(
    Array.from(
      h.api.versionGapSources('wiki_linked_summary_versions_unknown'),
      (source: any) => source.id,
    ),
    ['used'],
  )
  assert.equal(h.api.versionGapSources('source_lookup_failed').length, 0)
  assert.equal(
    h.api.versionGapSources('source_folder_versions_unknown').length,
    0,
    'uncaptured folder descendants cannot be identified by unrelated captured unversioned summaries',
  )
  h.stop()
})

test('overview percentages use successful diagnoses and react to scope changes', () => {
  const props = vue.reactive({
    stats: {
      total: 8,
      diagnosis_status: { succeeded: 4, failed: 4 },
      by_verdict: { supported: 3, partial: 1 },
      source_checks: { unresolved: 2, not_applicable: 2 },
    },
  })
  const h = setup('OverviewInsights.vue', props)
  assert.equal(h.api.successfulCount?.value, 4)
  assert.equal(h.api.unassessedCount?.value, 4)
  assert.equal(
    h.api.supportRows.value.find((row: any) => row.key === 'supported').percent,
    75,
  )
  assert.equal(
    h.api.supportRows.value.find((row: any) => row.key === 'partial').percent,
    25,
  )
  props.stats.total = 2
  props.stats.diagnosis_status = { succeeded: 0, failed: 2 }
  props.stats.by_verdict = {} as typeof props.stats.by_verdict
  assert.equal(h.api.successfulCount.value, 0)
  assert.equal(h.api.unassessedCount.value, 2)
  assert.equal(h.api.supportRows.value.length, 0)
  h.stop()
})

test('cause drilldown keeps the applied scope and resets pagination without applying draft edits', async () => {
  const requests: Record<string, unknown>[] = []
  const h = setup(
    'Workbench.vue',
    {},
    {
      useRouter: () => ({ replace: () => {} }),
      useRoute: () => ({ query: {} }),
      onMounted: () => {},
      document: { getElementById: () => null },
      getOpsSettings: async () => settings(10),
      getOpsOverview: async () => ({ total: 1 }),
      getOpsRuns: async (query: Record<string, unknown>) => {
        requests.push(query)
        return { items: [], total: 0 }
      },
    },
  )
  h.api.filters.value = { from: '2026-10-01T00:00:00Z', search: 'applied' }
  h.api.draftFilters.search = 'not yet applied'
  h.api.page.value = 3
  assert.equal(typeof h.api.filterCause, 'function')
  await h.api.filterCause('generation')
  assert.equal(requests.length, 1)
  assert.deepEqual(JSON.parse(JSON.stringify(requests[0])), {
    from: '2026-10-01T00:00:00Z',
    search: 'applied',
    cause_category: 'generation',
    page: 1,
    page_size: 20,
  })
  assert.equal(h.api.draftFilters.search, 'applied')
  assert.equal(h.api.draftFilters.cause_category, 'generation')
  await h.api.filterCause('retrieval')
  assert.equal(
    requests.length,
    1,
    'a stale cross-category click must not replace the existing cause scope',
  )
  h.stop()
})

test('an applied cause category only exposes drilldown counts within that category', () => {
  const props = vue.reactive({
    stats: { causes: { generation: 1, retrieval: 2 } },
    causeCategory: 'retrieval',
  })
  const h = setup('OverviewInsights.vue', props)
  assert.deepEqual(JSON.parse(JSON.stringify(h.api.causeRows?.value ?? [])), [
    { category: 'retrieval', count: 2 },
  ])
  props.causeCategory = ''
  assert.equal(h.api.causeRows.value.length, 2)
  h.stop()
})

test('queue diagnosis uses assessed candidates in both list and group views', () => {
  const positive = {
    category: 'generation',
    summary: 'accurate extraction',
    status: 'evidence_backed',
  }
  const run = {
    id: 'A',
    analysis: {
      report: { causes: [positive] },
      cause_assessment: { candidates: [], unlinked: [positive] },
    },
  }
  const props = vue.reactive({
    runs: [run],
    groups: [{ key: 'run:A', count: 1, representative: run }],
    grouped: false,
    selectedGroup: '',
  })
  const h = setup('QueuePanel.vue', props)
  for (const grouped of [false, true]) {
    props.grouped = grouped
    assert.equal(h.api.rows.value[0].candidates?.length, 0)
    assert.equal(h.api.rows.value[0].unlinked?.length, 1)
  }
  h.stop()
})

test('detail separates unlinked generation attributions for the selected analysis', async () => {
  const positive = { category: 'generation', summary: '准确提取，无幻觉或遗漏' }
  const valid = { category: 'generation', summary: '遗漏限制条件' }
  const analyses = [
    {
      id: 'v2',
      version: 2,
      status: 'succeeded',
      report: { causes: [positive] },
      cause_assessment: { candidates: [], unlinked: [positive] },
    },
    {
      id: 'v1',
      version: 1,
      status: 'succeeded',
      report: { causes: [valid] },
      cause_assessment: { candidates: [valid], unlinked: [] },
    },
  ]
  const h = setup(
    'DetailDrawer.vue',
    vue.reactive({ runID: 'A', enabled: true }),
    {
      getOpsDetail: async () => ({
        ...detail('A'),
        run: { id: 'A', analysis: analyses[0] },
        analyses,
      }),
    },
  )
  await setImmediate()
  assert.equal(h.api.causeCandidates?.value.length, 0)
  assert.equal(h.api.unlinkedCauses?.value[0].summary, positive.summary)
  h.api.selectedID.value = 'v1'
  assert.equal(h.api.causeCandidates.value[0].summary, valid.summary)
  assert.equal(h.api.unlinkedCauses.value.length, 0)
  h.stop()
})

test('a late settings load cannot overwrite another tenant draft', async () => {
  const old = deferred<unknown>()
  const fresh = deferred<unknown>()
  let calls = 0
  const props = vue.reactive({ visible: false })
  const h = setup('SettingsDrawer.vue', props, {
    getOpsSettings: () => (++calls === 1 ? old.promise : fresh.promise),
    listModels: async () => [],
  })
  props.visible = true
  await vue.nextTick()
  props.visible = false
  await vue.nextTick()
  h.auth.effectiveTenantId = 2
  await vue.nextTick()
  props.visible = true
  await vue.nextTick()
  fresh.resolve(settings(25))
  await setImmediate()
  old.resolve(settings(80))
  await setImmediate()
  assert.equal(h.api.draft.value.sample_percent, 25)
  h.stop()
})

test('source audit displays this analysis supplements even when there are no root causes', async () => {
  const report = {
    id: 'v2',
    version: 2,
    report: {
      claims: [{ evidence_ids: ['original'] }],
      causes: [],
      source_audit: {
        evidence_ids: ['v2-source'],
        issues: [],
        chars: 100,
        char_limit: 16000,
      },
    },
  }
  const h = setup(
    'DetailDrawer.vue',
    vue.reactive({ runID: 'A', enabled: true }),
    {
      getOpsDetail: async () => ({
        ...detail('A'),
        run: { id: 'A', analysis: report },
        analyses: [report],
        evidence: [
          { id: 'original', post_hoc: false, sent_to_model: true },
          { id: 'v1-source', post_hoc: true },
          { id: 'v2-source', post_hoc: true },
        ],
      }),
    },
  )
  await setImmediate()
  const group = h.api.evidenceGroups.value.find(
    (g: { key: string }) => g.key === 'postEvidence',
  )
  assert.deepEqual(
    Array.from(group.items, (e: any) => e.id),
    ['v2-source'],
  )
  assert.equal(h.api.sourceEvidenceNotice?.value, 'auditedSourceNotice')
  h.stop()
})

test('following an evidence reference opens all containing disclosures before scrolling', async () => {
  const outer = { tagName: 'DETAILS', open: false, parentElement: null }
  const target = {
    tagName: 'DETAILS',
    open: false,
    parentElement: outer,
    scrollIntoView: () => assert.ok(target.open && outer.open),
    focus: () => assert.ok(target.open && outer.open),
  }
  const h = setup(
    'DetailDrawer.vue',
    vue.reactive({ runID: '', enabled: true }),
    {
      document: { getElementById: () => target },
    },
  )
  await h.api.showEvidence('candidate')
  assert.ok(outer.open && target.open)
  h.stop()
})

test('source section hides only inapplicable empty checks and preserves real follow-up and history', async () => {
  for (const kind of ['empty', 'issue', 'material', 'pending', 'historical']) {
    const selected = {
      id: 'a', version: 1, status: 'succeeded',
      report: { source_check: kind === 'historical' ? 'unresolved' : 'not_checked', source_audit: {
        chars: kind === 'material' ? 4 : 0, char_limit: 16000,
        issues: kind === 'issue' ? ['source_lookup_failed'] : [],
        evidence_ids: kind === 'material' ? ['post'] : [],
      } },
    }
    const latest = kind === 'historical' ? { ...selected, id: 'latest', version: 2 } : selected
    const h = setup('DetailDrawer.vue', vue.reactive({ runID: 'A', enabled: true, showSourceVersions: false }), {
      getOpsDetail: async () => ({ ...detail('A'),
        run: { id: 'A', qa_type: 'rag-qa', analysis: latest, source_check_display: 'not_applicable', source_followup_pending: kind === 'pending', coverage: { provenance_gaps: [] } },
        analyses: kind === 'historical' ? [latest, selected] : [selected],
        evidence: kind === 'material' ? [{ id: 'post', post_hoc: true, content: 'body' }] : [],
      }),
    })
    await setImmediate()
    h.api.selectedID.value = 'a'
    assert.equal(h.api.showSourceSection?.value, kind !== 'empty', kind)
    h.stop()
  }
})

test('diagnosis failures show source follow-up only for established gaps, including historical versions', async () => {
  for (const historical of [false, true]) {
    for (const status of ['failed', 'succeeded']) {
      for (const hasGap of [false, true]) {
        const issues = hasGap ? ['source_lookup_failed'] : []
        const selected = {
          id: 'selected',
          version: 1,
          status,
          report: {
            source_check: 'unresolved',
            source_audit: {
              issues,
              evidence_ids: [],
              chars: 0,
              char_limit: 16000,
            },
          },
        }
        const latest = historical
          ? { ...selected, id: 'latest', version: 2 }
          : selected
        const expectedPending = hasGap || status === 'succeeded'
        const h = setup(
          'DetailDrawer.vue',
          vue.reactive({ runID: 'A', enabled: true }),
          {
            getOpsDetail: async () => ({
              ...detail('A'),
              run: {
                id: 'A',
                qa_type: 'wiki-qa',
                analysis: latest,
                source_followup_pending: expectedPending,
                coverage: { provenance_gaps: [] },
              },
              analyses: historical ? [latest, selected] : [selected],
            }),
          },
        )
        await setImmediate()
        h.api.selectedID.value = selected.id
        assert.deepEqual(
          Array.from(h.api.sourceIssues.value),
          hasGap
            ? ['source_lookup_failed']
            : status === 'succeeded'
              ? ['source_scope_unresolved']
              : [],
        )
        assert.equal(h.api.sourcePending.value, expectedPending)
        h.stop()
      }
    }
  }
})

test('a settings save completed after tenant change cannot emit saved or close', async () => {
  const pending = deferred<unknown>()
  const props = vue.reactive({ visible: false })
  const h = setup('SettingsDrawer.vue', props, {
    getOpsSettings: async () => settings(10),
    listModels: async () => [],
    saveOpsSettings: () => pending.promise,
  })
  props.visible = true
  await setImmediate()
  const save = h.api.save()
  props.visible = false
  h.auth.effectiveTenantId = 2
  await vue.nextTick()
  pending.resolve(settings(10))
  await save
  assert.equal(h.events.length, 0)
  h.stop()
})

test('retry completion and failure stay with the run that was retried', async () => {
  for (const fails of [false, true]) {
    const pending = deferred<unknown>()
    const props = vue.reactive({ runID: 'A', enabled: true })
    const reads: string[] = []
    const h = setup('DetailDrawer.vue', props, {
      getOpsDetail: async (id: string) => {
        reads.push(id)
        return detail(id)
      },
      retryOpsRun: () => pending.promise,
    })
    await vue.nextTick()
    const rerun = h.api.rerun()
    props.runID = 'B'
    await vue.nextTick()
    await vue.nextTick()
    const readsBefore = reads.length
    if (fails) pending.reject(new Error('failed'))
    else pending.resolve({ queued: true })
    await rerun
    assert.equal(h.api.notice.value, '')
    assert.equal(reads.length, readsBefore)
    h.stop()
  }
})

test('confirming a review permits an empty note without changing the machine assessment', () => {
  for (const verdict of ['supported', 'partial', 'unverifiable']) {
    const h = setup('ReviewForm.vue', {
      analysisID: 'v1', initialVerdict: verdict,
      disabled: false, busy: false,
    })
    h.api.draft.reason = ' \n '
    h.api.submit()
    assert.equal(h.events.length, 1)
    const payload = h.events[0][1] as Record<string, string>
    assert.equal(payload.action, 'confirmed')
    assert.equal(payload.verdict, verdict)
    assert.equal(payload.reason, '')
    assert.equal(payload.analysis_id, 'v1')
    h.stop()
  }
})

test('corrections and evidence requests still require explanations, with errors cleared on action change', () => {
  for (const verdict of ['supported', 'partial', 'unverifiable']) {
    const h = setup('ReviewForm.vue', {
      analysisID: 'v1', initialVerdict: verdict,
      disabled: false, busy: false,
    })
    for (const action of ['corrected', 'needs_more_evidence']) {
      h.api.draft.action = action
      h.api.actionChanged()
      h.api.draft.reason = ' \n '
      h.api.submit()
      assert.equal(h.events.length, 0)
      assert.equal(h.api.localError.value, 'reasonRequired')
    }
    h.api.draft.action = 'confirmed'
    h.api.actionChanged()
    assert.equal(h.api.localError.value, '')
    h.api.draft.reason = '证'.repeat(1334)
    h.api.submit()
    assert.equal(h.events.length, 0, 'Optional notes retain the UTF-8 byte limit')
    assert.equal(h.api.localError.value, 'reasonTooLong')
    h.api.draft.reason = ''
    h.api.submit()
    assert.equal(h.events.length, 1)
    h.stop()
  }
})

test('historical post-hoc materials follow explicit report references rather than the run archive', async () => {
  const evidence = [
    { id: 'original', post_hoc: false, sent_to_model: true },
    { id: 'candidate', post_hoc: false, sent_to_model: false },
    { id: 'v1-support', post_hoc: true },
    { id: 'v1-counter', post_hoc: true },
    { id: 'v2-support', post_hoc: true },
    { id: 'uncited', post_hoc: true },
  ]
  const v1 = {
    id: 'v1',
    version: 1,
    report: {
      claims: [{ evidence_ids: ['original'] }],
      causes: [
        {
          evidence_ids: ['v1-support'],
          counter_evidence: ['v1-counter', 'Unresolved applicability'],
        },
      ],
    },
  }
  const v2 = {
    id: 'v2',
    version: 2,
    report: {
      claims: [],
      causes: [{ evidence_ids: ['v2-support'], counter_evidence: [] }],
    },
  }
  const h = setup(
    'DetailDrawer.vue',
    vue.reactive({ runID: 'A', analysisID: 'v1', enabled: true }),
    {
      getOpsDetail: async () => ({
        ...detail('A'),
        run: { id: 'A', analysis: v2 },
        analyses: [v2, v1],
        evidence,
      }),
    },
  )
  await setImmediate()
  const ids = (key: string) =>
    Array.from(
      h.api.evidenceGroups.value.find(
        (group: { key: string }) => group.key === key,
      ).items,
      (item: any) => item.id,
    )
  assert.deepEqual(ids('postEvidence'), ['v1-support', 'v1-counter'])
  assert.equal(h.api.sourceEvidenceNotice.value, 'postNotice')
  assert.deepEqual(ids('originalEvidence'), ['original'])
  assert.deepEqual(ids('candidateEvidence'), ['candidate'])
  h.api.selectedID.value = 'v2'
  assert.deepEqual(ids('postEvidence'), ['v2-support'])
  h.api.selectedID.value = ''
  assert.deepEqual(ids('postEvidence'), [])
  h.stop()
})

test('non-knowledge conversations stay outside support percentages', () => {
  const h = setup(
    'OverviewInsights.vue',
    vue.reactive({
      stats: {
        total: 4,
        diagnosis_status: { succeeded: 4 },
        non_knowledge_runs: 1,
        by_verdict: { supported: 3 },
        source_checks: {},
        content_coverage: { total: 3, complete: 3, partial: 0, missing: 0 },
      },
    }),
  )
  assert.equal(h.api.supportRows.value[0].percent, 100)
  assert.equal(h.api.successfulCount.value, 4)
  h.stop()
})

test('conversation navigation and tenant changes discard stale history responses', async () => {
  const pending = deferred<unknown>()
  const props = vue.reactive({ run: { id: 'A', updated_at: '1' } })
  const h = setup('ConversationPanel.vue', props, {
    getOpsConversation: (id: string) =>
      id === 'A' ? pending.promise : Promise.resolve({ title: 'B', turns: [] }),
  })
  const loading = h.api.load()
  props.run = { id: 'B', updated_at: '1' }
  await h.api.load()
  pending.resolve({ title: 'private A', turns: [{ answer: 'old content' }] })
  await loading
  assert.equal(h.api.conversation.value.title, 'B')
  h.auth.effectiveTenantId = 2
  assert.equal(h.api.conversation.value, undefined)
  h.stop()
})

test('historical diagnosis uses its own capture assessment', async () => {
  const older = {
    id: 'v1',
    version: 1,
    status: 'succeeded',
    capture_coverage: { content_status: 'missing' },
    report: { verdict: 'not_applicable', claims: [], causes: [], unknowns: [] },
  }
  const latest = {
    id: 'v2',
    version: 2,
    status: 'succeeded',
    route: 'not_required',
    capture_coverage: { content_status: 'not_required' },
    report: { verdict: 'not_applicable', claims: [], causes: [], unknowns: [] },
  }
  const h = setup(
    'DetailDrawer.vue',
    vue.reactive({ runID: 'A', enabled: true }),
    {
      getOpsDetail: async () => ({
        ...detail('A'),
        run: {
          ...detail('A').run,
          analysis: latest,
          coverage: latest.capture_coverage,
        },
        analyses: [latest, older],
      }),
    },
  )
  await setImmediate()
  assert.equal(h.api.selectedCoverage.value.content_status, 'not_required')
  h.api.selectedID.value = 'v1'
  assert.equal(h.api.selectedCoverage.value.content_status, 'missing')
  h.stop()
})

test('query context distinguishes unknown, disabled and failed without replacing the original question', () => {
  const props = vue.reactive({ run: { question: '原始问题' } as any })
  const harness = setup('QueryContextPanel.vue', props)
  try {
    assert.equal(harness.api.rewriteStatus.value, 'unrecorded')
    props.run.query_context = { version: 1, rewrite: { kind: 'rewrite', status: 'disabled', query: '原始问题' } }
    assert.equal(harness.api.rewriteStatus.value, 'disabled')
    assert.equal(harness.api.showRewriteText.value, false)
    props.run.query_context.rewrite = { kind: 'rewrite', status: 'failed', query: '原始问题' }
    assert.equal(harness.api.rewriteStatus.value, 'failed')
    assert.equal(harness.api.showRewriteText.value, false)
    props.run.query_context.rewrite = { kind: 'rewrite', status: 'rewritten', query: '改写的检索问题', history_turns: 0 }
    assert.equal(harness.api.showRewriteText.value, true, 'single-turn rewrites must also be visible')
    assert.equal(props.run.question, '原始问题')
    props.run.query_context.searches = [
      { query: '实际子问题', tool: 'wiki_search', tool_call_id: 'a', round: 1 },
      { query: '实际子问题', tool: 'wiki_search', tool_call_id: 'b', round: 2 },
    ]
    assert.equal(harness.api.searchQueries.value.length, 2)
  } finally { harness.stop() }
})
