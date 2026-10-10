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
    'save',
    'rerun',
    'saveReview',
    'submit',
    'actionChanged',
    'selectCandidate',
    'draft',
    'models',
    'notice',
    'loadError',
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

test('a non-first machine cause can be confirmed without recording a correction', () => {
  const causes = [
    { category: 'generation', status: 'suspected', summary: 'First' },
    { category: 'retrieval', status: 'evidence_backed', summary: 'Second' },
  ]
  const h = setup(
    'ReviewForm.vue',
    vue.reactive({
      target: 'cause',
      analysisID: 'v1',
      initialVerdict: 'suspected',
      initialCategory: 'generation',
      machineCauses: causes,
      disabled: false,
      busy: false,
    }),
  )
  h.api.selectCandidate?.('1')
  h.api.draft.reason = 'Checked the second candidate'
  h.api.submit()
  const payload = h.events[0][1] as Record<string, string>
  assert.equal(payload.action, 'confirmed')
  assert.equal(payload.cause_category, 'retrieval')
  assert.equal(payload.verdict, 'evidence_backed')
  h.stop()
})

test('an explicit manual cause addition starts as a correction without inventing a machine candidate', () => {
  const h = setup(
    'ReviewForm.vue',
    vue.reactive({
      target: 'cause',
      analysisID: 'v2',
      initialVerdict: 'unresolved',
      initialAction: 'corrected',
      machineCauses: [],
      disabled: false,
      busy: false,
    }),
  )
  h.api.draft.reason = 'A human identifies a missing source linkage'
  h.api.submit()
  assert.equal((h.events[0][1] as any).action, 'corrected')
  h.stop()
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
