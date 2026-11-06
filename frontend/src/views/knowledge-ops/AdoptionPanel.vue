<template>
  <section class="adoption-panel" :aria-label="k('adoptionTitle')">
    <div class="panel-heading">
      <div>
        <h3>{{ k('adoptionTitle') }}</h3>
        <p class="muted">{{ k('adoptionIndependent') }}</p>
      </div>
      <StatusBadge v-if="publication" :value="publication.status" :text="k(`adoption_${publication.status}`)" />
    </div>
    <div class="target-type">
      <span :id="`${fieldID}-type`" class="field-label">{{ k('adoptionType') }}</span>
      <t-radio-group :value="targetType" :aria-labelledby="`${fieldID}-type`" :disabled="typeChangeBlocked" variant="default-filled" @change="targetTypeChanged">
        <t-radio-button v-for="type in targetTypes" :key="type" :value="type">
          {{ k(`adoptionType_${type}`) }}<span v-if="publicationFor(type)" class="type-status"> · {{ k(`adoption_${publicationFor(type)!.status}`) }}</span>
        </t-radio-button>
      </t-radio-group>
    </div>
    <p class="muted retrieval-hint">{{ targetText('adoptionRetrievalHint') }}</p>
    <template v-if="publication">
      <dl class="publication-summary">
        <dt>{{ targetText('adoptionTarget') }}</dt>
        <dd>{{ targetName(publication.kb_id) }}</dd>
        <dt>{{ k('standardQuestion') }}</dt>
        <dd>{{ publication.question }}</dd>
        <dt>{{ k('adoptionTime') }}</dt>
        <dd>{{ date(publication.updated_at || publication.created_at) }}</dd>
      </dl>
      <details class="answer-preview">
        <summary>{{ k('standardAnswer') }}</summary>
        <AnswerContent :content="publication.answer" />
      </details>
      <template v-if="publication.status === 'succeeded'">
        <p class="success" role="status">{{ targetText(publication.reused ? 'adoptionReused' : 'adoptionSucceeded') }}</p>
        <router-link class="library-link" :to="publicationLink">
          {{ targetText('openAdoptionLibrary') }}<template v-if="targetType === 'faq' && publication.entry_id"> · FAQ #{{ publication.entry_id }}</template>
        </router-link>
      </template>
      <p v-if="publication.status === 'publishing'" class="muted" role="status">{{ targetText('adoptionPublishingHint') }}</p>
      <p v-if="publication.status === 'failed'" class="error" role="alert">{{ publicationError }}</p>
      <p v-if="publication.status === 'failed' && !approvalMatches" class="muted">{{ k('adoptionApprovalChanged') }}</p>
      <div class="panel-actions">
        <t-button v-if="['failed', 'publishing'].includes(publication.status) && approvalMatches" :disabled="!allowed || publishing || refreshing || uncertain" :loading="publishing" @click="retryPublication">
          {{ targetText('retryPublication') }}
        </t-button>
        <t-button v-if="publication.status === 'publishing'" variant="outline" :disabled="publishing" :loading="refreshing" @click="refreshPublication">
          {{ k('refreshPublication') }}
        </t-button>
      </div>
    </template>
    <template v-else>
      <t-button v-if="!expanded" variant="outline" :disabled="!allowed" @click="openDraft">{{ targetText('adoptAsKnowledge') }}</t-button>
      <form v-else class="adoption-form" @submit.prevent="publish">
        <p class="muted">{{ k('adoptionDraftHint') }}</p>
        <label :id="`${fieldID}-kb`">
          {{ targetText('adoptionTarget') }}
          <t-select
            v-model="draft.kb_id"
            :aria-labelledby="`${fieldID}-kb`"
            :placeholder="targetText('selectAdoptionTarget')"
            :options="targets.map((target) => ({ value: target.id, label: target.name }))"
            :loading="loadingTargets"
            :disabled="!allowed || publishing || !targets.length || !!targetError"
            filterable
          />
        </label>
        <p v-if="targetError" class="error" role="alert">{{ targetError }}</p>
        <p v-else-if="!loadingTargets && !targets.length" class="muted" role="status">{{ targetText('noAdoptionTargets') }}</p>
        <t-button v-if="targetError" size="small" variant="text" :disabled="!allowed" @click="loadTargets">{{ k('retry') }}</t-button>
        <label :id="`${fieldID}-question`">
          {{ k('standardQuestion') }}
          <t-textarea v-model="draft.question" :aria-labelledby="`${fieldID}-question`" :aria-describedby="`${fieldID}-question-hint`" :aria-invalid="questionTooLong" :status="questionTooLong ? 'error' : undefined" :autosize="{ minRows: 2, maxRows: 5 }" :disabled="!allowed || publishing" />
        </label>
        <p :id="`${fieldID}-question-hint`" :class="questionTooLong ? 'error' : 'muted'" :role="questionTooLong ? 'alert' : undefined">
          {{ targetText(questionTooLong ? 'adoptionQuestionTooLong' : 'adoptionQuestionLimit') }} · {{ questionLength }} / {{ questionLimit }}
        </p>
        <div class="answer-draft">
          <div class="answer-heading">
            <label :id="`${fieldID}-answer`">{{ k('standardAnswer') }}</label>
            <t-button variant="text" size="small" :disabled="!allowed || publishing" :aria-controls="`${fieldID}-answer-content`" @click="answerEditing ? previewAnswer() : editAnswer()">
              {{ k(answerEditing ? 'previewAdoptionAnswer' : 'editAdoptionMarkdown') }}
            </t-button>
          </div>
          <t-textarea v-if="answerEditing" :id="`${fieldID}-answer-content`" v-model="draft.answer" :aria-labelledby="`${fieldID}-answer`" :autosize="{ minRows: 5, maxRows: 14 }" :maxlength="24000" :disabled="!allowed || publishing" />
          <div v-else :id="`${fieldID}-answer-content`" class="draft-preview" role="region" :aria-labelledby="`${fieldID}-answer`" tabindex="0">
            <AnswerContent v-if="draft.answer.trim()" :content="draft.answer" />
            <p v-else class="muted">{{ k('emptyAdoptionAnswer') }}</p>
          </div>
          <p class="muted">{{ k(answerEditing ? 'previewBeforePublication' : 'adoptionAnswerPrepared') }}</p>
          <p v-if="wikiCitationRemaining" class="error" role="alert">{{ k('adoptionWikiCitationRemaining') }}</p>
        </div>
        <div class="panel-actions">
          <t-button type="submit" :loading="publishing" :disabled="!canPublish">{{ targetText('publishToKnowledge') }}</t-button>
          <t-button variant="text" theme="default" :disabled="publishing" @click="expanded = false">{{ k('collapseDraft') }}</t-button>
        </div>
      </form>
    </template>
    <p v-if="publishError" class="error" role="alert">{{ publishError }}</p>
    <div v-if="uncertain" class="uncertain-state">
      <p class="muted">{{ targetText('adoptionUncertain') }}</p>
      <t-button variant="outline" :loading="refreshing" @click="refreshPublication">{{ k('refreshPublication') }}</t-button>
    </div>
    <p v-if="disabled || !auth.hasRole('admin')" class="muted">{{ k('adoptionReadOnly') }}</p>
    <p v-if="stale" class="muted">{{ k('adoptionApprovalChanged') }}</p>
  </section>
</template>
<script setup lang="ts">
import { computed, onScopeDispose, reactive, ref, watch } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { adoptOpsAnswer, getOpsAdoptionTargets, getOpsDetail, opsErrorCode, retryOpsAdoption, type OpsAdoption, type OpsAdoptionTarget, type OpsAdoptionTargetType, type OpsReview, type OpsRun } from '@/api/knowledge-ops'
import { compareOpsTimestamps, latestAnswerReview } from './state'
import { prepareAdoptionAnswer } from './adoption-answer'
import AnswerContent from './AnswerContent.vue'
import StatusBadge from './StatusBadge.vue'
import { useOpsText } from './locales'
const props = defineProps<{
  run: OpsRun
  analysisID: string
  review?: OpsReview
  adoptions?: OpsAdoption[]
  disabled: boolean
}>()
const emit = defineEmits<{ changed: [] }>()
const { k, date } = useOpsText()
const auth = useAuthStore()
const expanded = ref(false)
const answerEditing = ref(false)
const targets = ref<OpsAdoptionTarget[]>([])
const loadingTargets = ref(false)
const targetError = ref('')
const publishError = ref('')
const publishing = ref(false)
const refreshing = ref(false)
const uncertain = ref(false)
const stale = ref(false)
const targetTypes: OpsAdoptionTargetType[] = ['faq', 'wiki']
const targetType = ref<OpsAdoptionTargetType>('faq')
const localPublications = ref<OpsAdoption[]>([])
const draft = reactive({ kb_id: '', question: '', answer: '' })
let scopeID = 0
let targetRequest = 0
const fieldID = computed(() => `ops-adopt-${props.run.id}-${props.analysisID}`)
function publicationFor(type: OpsAdoptionTargetType) {
  const values = [...localPublications.value, ...(props.adoptions || [])]
  return values.filter((item) => item.analysis_id === props.analysisID && item.run_id === props.run.id && (item.target_type || 'faq') === type)
    .sort((a, b) => compareOpsTimestamps(b.updated_at || b.created_at, a.updated_at || a.created_at))[0]
}
const publication = computed(() => publicationFor(targetType.value))
const publicationLink = computed(() => {
  const value = publication.value
  if (!value) return ''
  const library = `/platform/knowledge-bases/${encodeURIComponent(value.kb_id)}`
  return targetType.value === 'wiki' ? `${library}?tab=wiki&slug=${encodeURIComponent(value.wiki_slug || '')}` : library
})
const allowed = computed(() => !props.disabled && !stale.value && auth.hasRole('admin') && props.review?.action === 'accepted' && props.review.verdict === 'usable' && props.review.analysis_id === props.analysisID)
const approvalMatches = computed(() => !!props.review && publication.value?.review_id === props.review.id)
const typeChangeBlocked = computed(() => publishing.value || refreshing.value || uncertain.value)
const questionLimit = computed(() => targetType.value === 'wiki' ? 512 : 16000)
const questionLength = computed(() => Array.from(draft.question.trim()).length)
const questionTooLong = computed(() => questionLength.value > questionLimit.value)
// Wiki's save path removes these even inside code. Preserve literal examples
// in the draft and require the reviewer to edit them before confirming.
const wikiCitationRemaining = computed(() => targetType.value === 'wiki' && /\[c\d{3,}(?:\s*[,;]\s*c\d{3,})*\]/.test(draft.answer))
const canPublish = computed(() => allowed.value && expanded.value && !answerEditing.value && !typeChangeBlocked.value && !publication.value && !targetError.value && !loadingTargets.value && targets.value.some((target) => target.id === draft.kb_id) && !!draft.question.trim() && !questionTooLong.value && !!draft.answer.trim() && !wikiCitationRemaining.value && Array.from(draft.answer.trim()).length <= 24000 && prepareAdoptionAnswer(draft.answer) === draft.answer.trim())
const publicationError = computed(() => {
  const code = publication.value?.error || ''
  return ['faq_publish_failed', 'faq_index_failed', 'faq_lookup_failed', 'faq_question_conflict', 'wiki_publish_failed', 'wiki_lookup_failed', 'wiki_question_conflict', 'access_revoked', 'approval_changed'].includes(code) ? k(code) : targetText('adoptionFailed')
})
function targetText(key: string) {
  return k(targetType.value === 'wiki' ? `${key}Wiki` : key)
}
function targetName(id: string) {
  return targets.value.find((target) => target.id === id)?.name || id
}
function operationScope() {
  const scope = scopeID
  const tenant = auth.effectiveTenantId
  const runID = props.run.id
  const analysisID = props.analysisID
  const type = targetType.value
  return () => scope === scopeID && tenant === auth.effectiveTenantId && runID === props.run.id && analysisID === props.analysisID && type === targetType.value
}
async function targetTypeChanged(value: unknown) {
  if ((value !== 'faq' && value !== 'wiki') || value === targetType.value || typeChangeBlocked.value) return
  scopeID++
  targetRequest++
  targetType.value = value
  targets.value = []
  draft.kb_id = ''
  loadingTargets.value = false
  targetError.value = ''
  publishError.value = ''
  if (expanded.value || publication.value) await loadTargets()
}
async function loadTargets() {
  if (!auth.hasRole('admin')) return
  const token = ++targetRequest
  const current = operationScope()
  loadingTargets.value = true
  targetError.value = ''
  try {
    const response = await getOpsAdoptionTargets(targetType.value)
    if (token !== targetRequest || !current()) return
    targets.value = response.items || []
    if (!targets.value.some((target) => target.id === draft.kb_id)) draft.kb_id = ''
  } catch (error) {
    if (token === targetRequest && current()) {
      targets.value = []
      targetError.value = targetText(opsErrorCode(error) === 'forbidden' ? 'adoptionForbidden' : 'adoptionTargetsFailed')
    }
  } finally {
    if (token === targetRequest && current()) loadingTargets.value = false
  }
}
async function openDraft() {
  if (!allowed.value || publication.value) return
  expanded.value = true
  await loadTargets()
}
function editAnswer() {
  if (allowed.value && !publishing.value) answerEditing.value = true
}
function previewAnswer() {
  if (!allowed.value || publishing.value) return
  draft.answer = prepareAdoptionAnswer(draft.answer)
  answerEditing.value = false
}
async function refreshPublication() {
  if (refreshing.value || publishing.value) return
  const current = operationScope()
  refreshing.value = true
  try {
    const value = await getOpsDetail(props.run.id)
    if (!current()) return
    const latestID = value.run.analysis?.id || [...value.analyses].sort((a, b) => b.version - a.version)[0]?.id
    const review = latestAnswerReview(value.reviews, props.analysisID)
    stale.value = latestID !== props.analysisID || review?.id !== props.review?.id || review?.action !== 'accepted' || review?.verdict !== 'usable' || !!value.pending_tasks
    localPublications.value = [...(value.adoptions || []), ...localPublications.value]
    uncertain.value = false
    if (publication.value) publishError.value = ''
    emit('changed')
  } catch (error) {
    if (current()) {
      uncertain.value = true
      publishError.value = k(opsErrorCode(error))
    }
  } finally {
    if (current()) refreshing.value = false
  }
}
async function publish() {
  if (!canPublish.value) return
  const current = operationScope()
  publishing.value = true
  publishError.value = ''
  let reconcile = false
  try {
    const result = await adoptOpsAnswer(props.run.id, {
      analysis_id: props.analysisID,
      review_id: props.review!.id,
      target_type: targetType.value,
      kb_id: draft.kb_id,
      question: draft.question.trim(),
      answer: draft.answer.trim(),
    })
    if (!current()) return
    localPublications.value = [result, ...localPublications.value]
    emit('changed')
  } catch (error) {
    if (current()) {
      publishError.value = k(opsErrorCode(error))
      uncertain.value = true
      reconcile = true
    }
  } finally {
    if (current()) publishing.value = false
  }
  if (current() && reconcile) await refreshPublication()
}
async function retryPublication() {
  const value = publication.value
  if (!allowed.value || !approvalMatches.value || typeChangeBlocked.value || !value || !['failed', 'publishing'].includes(value.status)) return
  const current = operationScope()
  publishing.value = true
  publishError.value = ''
  let reconcile = false
  try {
    const result = await retryOpsAdoption(props.run.id, value.id)
    if (!current()) return
    localPublications.value = [result, ...localPublications.value]
    emit('changed')
  } catch (error) {
    if (current()) {
      publishError.value = k(opsErrorCode(error))
      uncertain.value = true
      reconcile = true
    }
  } finally {
    if (current()) publishing.value = false
  }
  if (current() && reconcile) await refreshPublication()
}
watch(
  [() => props.run.id, () => props.analysisID, () => props.review?.id, () => auth.effectiveTenantId],
  () => {
    scopeID++
    targetRequest++
    expanded.value = false
    answerEditing.value = false
    targets.value = []
    targetError.value = ''
    publishError.value = ''
    loadingTargets.value = false
    publishing.value = false
    refreshing.value = false
    uncertain.value = false
    stale.value = false
    localPublications.value = []
    targetType.value = props.run.qa_type === 'wiki-qa' ? 'wiki' : 'faq'
    const rewrite = props.run.query_context?.rewrite
    Object.assign(draft, { kb_id: '', question: rewrite?.status === 'rewritten' && rewrite.query.trim() ? rewrite.query : props.run.question, answer: prepareAdoptionAnswer(props.run.answer) })
    if (publication.value) void loadTargets()
  },
  { immediate: true, flush: 'sync' },
)
onScopeDispose(() => { scopeID++; targetRequest++ })
</script>
<style scoped>
.adoption-panel {
  padding: var(--app-space-4);
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-lg);
  background: var(--td-bg-color-container);
}
.panel-heading, .panel-actions { display: flex; align-items: center; flex-wrap: wrap; gap: var(--app-space-3); }
.panel-heading { justify-content: space-between; margin-bottom: var(--app-space-3); }
h3 { margin: 0; font-size: var(--app-text-lg); font-weight: 600; }
p { margin: var(--app-space-2) 0; }
.muted { color: var(--td-text-color-secondary); font-size: var(--app-text-sm); }
.target-type { display: flex; flex-direction: column; align-items: flex-start; gap: var(--app-space-2); }
.target-type :deep(.t-radio-group) { flex-wrap: wrap; max-width: 100%; height: auto; }
.field-label { font-size: var(--app-text-sm); color: var(--td-text-color-secondary); }
.type-status { font-size: var(--app-text-sm); }
.adoption-form label { display: flex; flex-direction: column; gap: var(--app-space-2); margin: var(--app-space-4) 0; font-size: var(--app-text-sm); }
.answer-draft { margin: var(--app-space-4) 0; }
.answer-heading { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: var(--app-space-2); margin-bottom: var(--app-space-2); }
.answer-heading label { margin: 0; }
.draft-preview { max-height: 30rem; overflow: auto; padding: var(--app-space-4); border: 1px solid var(--td-component-stroke); border-radius: var(--app-radius-md); background: var(--td-bg-color-container); }
.draft-preview:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
.error { color: var(--td-error-color); }
.success { color: var(--td-success-color); }
.publication-summary { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: var(--app-space-2) var(--app-space-4); font-size: var(--app-text-sm); }
dt { color: var(--td-text-color-secondary); }
dd { margin: 0; overflow-wrap: anywhere; white-space: pre-wrap; }
.answer-preview { margin: var(--app-space-3) 0; }
summary { cursor: pointer; color: var(--td-text-color-secondary); }
.library-link { color: var(--td-brand-color); display: inline-block; margin: var(--app-space-2) 0; }
.retrieval-hint { margin-bottom: var(--app-space-4); }
.panel-actions:empty { display: none; }
@media (max-width: 600px) { .publication-summary { grid-template-columns: 1fr; } }
</style>
