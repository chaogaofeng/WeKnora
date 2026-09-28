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
  ]
  const expose = names
    .map(
      (name) => `${name}: typeof ${name} === 'undefined' ? undefined : ${name}`,
    )
    .join(',')
  const events: unknown[][] = []
  const auth = vue.reactive({ effectiveTenantId: 1 })
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
  assert.deepEqual(ids('originalEvidence'), ['original'])
  assert.deepEqual(ids('candidateEvidence'), ['candidate'])
  h.api.selectedID.value = 'v2'
  assert.deepEqual(ids('postEvidence'), ['v2-support'])
  h.api.selectedID.value = ''
  assert.deepEqual(ids('postEvidence'), [])
  h.stop()
})
