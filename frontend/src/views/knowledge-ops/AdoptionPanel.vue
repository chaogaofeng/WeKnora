<template>
  <section class="adoption-panel" :aria-label="k('adoptionTitle')">
    <div class="panel-heading">
      <div>
        <h3>{{ k('adoptionTitle') }}</h3>
        <p class="muted">{{ k('adoptionIndependent') }}</p>
      </div>
      <StatusBadge v-if="publication" :value="publication.status" :text="k(`adoption_${publication.status}`)" />
    </div>
    <template v-if="publication">
      <dl class="publication-summary">
        <dt>{{ k('adoptionTarget') }}</dt>
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
        <p class="success" role="status">{{ k(publication.reused ? 'adoptionReused' : 'adoptionSucceeded') }}</p>
        <router-link class="library-link" :to="`/platform/knowledge-bases/${encodeURIComponent(publication.kb_id)}`">
          {{ k('openAdoptionLibrary') }}<template v-if="publication.entry_id"> · FAQ #{{ publication.entry_id }}</template>
        </router-link>
      </template>
      <p v-if="publication.status === 'publishing'" class="muted" role="status">{{ k('adoptionPublishingHint') }}</p>
      <p v-if="publication.status === 'failed'" class="error" role="alert">{{ publicationError }}</p>
      <p v-if="publication.status === 'failed' && !approvalMatches" class="muted">{{ k('adoptionApprovalChanged') }}</p>
      <div class="panel-actions">
        <t-button v-if="['failed', 'publishing'].includes(publication.status) && approvalMatches" :disabled="!allowed || publishing" :loading="publishing" @click="retryPublication">
          {{ k('retryPublication') }}
        </t-button>
        <t-button v-if="publication.status === 'publishing'" variant="outline" :loading="refreshing" @click="refreshPublication">
          {{ k('refreshPublication') }}
        </t-button>
      </div>
    </template>
    <template v-else>
      <t-button v-if="!expanded" variant="outline" :disabled="!allowed" @click="openDraft">{{ k('adoptAsKnowledge') }}</t-button>
      <form v-else class="adoption-form" @submit.prevent="publish">
        <p class="muted">{{ k('adoptionDraftHint') }}</p>
        <label :id="`${fieldID}-kb`">
          {{ k('adoptionTarget') }}
          <t-select
            v-model="draft.kb_id"
            :aria-labelledby="`${fieldID}-kb`"
            :placeholder="k('selectAdoptionTarget')"
            :options="targets.map((target) => ({ value: target.id, label: target.name }))"
            :loading="loadingTargets"
            :disabled="!allowed || publishing || !targets.length || !!targetError"
            filterable
          />
        </label>
        <p v-if="targetError" class="error" role="alert">{{ targetError }}</p>
        <p v-else-if="!loadingTargets && !targets.length" class="muted" role="status">{{ k('noAdoptionTargets') }}</p>
        <t-button v-if="targetError" size="small" variant="text" :disabled="!allowed" @click="loadTargets">{{ k('retry') }}</t-button>
        <label :id="`${fieldID}-question`">
          {{ k('standardQuestion') }}
          <t-textarea v-model="draft.question" :aria-labelledby="`${fieldID}-question`" :autosize="{ minRows: 2, maxRows: 5 }" :maxlength="16000" :disabled="!allowed || publishing" />
        </label>
        <label :id="`${fieldID}-answer`">
          {{ k('standardAnswer') }}
          <t-textarea v-model="draft.answer" :aria-labelledby="`${fieldID}-answer`" :autosize="{ minRows: 5, maxRows: 14 }" :maxlength="24000" :disabled="!allowed || publishing" />
        </label>
        <p class="muted retrieval-hint">{{ k('adoptionRetrievalHint') }}</p>
        <div class="panel-actions">
          <t-button type="submit" :loading="publishing" :disabled="!canPublish">{{ k('publishToKnowledge') }}</t-button>
          <t-button variant="text" theme="default" :disabled="publishing" @click="expanded = false">{{ k('collapseDraft') }}</t-button>
        </div>
      </form>
    </template>
    <p v-if="publishError" class="error" role="alert">{{ publishError }}</p>
    <div v-if="uncertain" class="uncertain-state">
      <p class="muted">{{ k('adoptionUncertain') }}</p>
      <t-button variant="outline" :loading="refreshing" @click="refreshPublication">{{ k('refreshPublication') }}</t-button>
    </div>
    <p v-if="disabled || !auth.hasRole('admin')" class="muted">{{ k('adoptionReadOnly') }}</p>
    <p v-if="stale" class="muted">{{ k('adoptionApprovalChanged') }}</p>
  </section>
</template>
<script setup lang="ts">
import { computed, onScopeDispose, reactive, ref, watch } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { adoptOpsAnswer, getOpsAdoptionTargets, getOpsDetail, opsErrorCode, retryOpsAdoption, type OpsAdoption, type OpsAdoptionTarget, type OpsReview, type OpsRun } from '@/api/knowledge-ops'
import { compareOpsTimestamps, latestAnswerReview } from './state'
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
const targets = ref<OpsAdoptionTarget[]>([])
const loadingTargets = ref(false)
const targetError = ref('')
const publishError = ref('')
const publishing = ref(false)
const refreshing = ref(false)
const uncertain = ref(false)
const stale = ref(false)
const localPublication = ref<OpsAdoption>()
const draft = reactive({ kb_id: '', question: '', answer: '' })
let scopeID = 0
let targetRequest = 0
const fieldID = computed(() => `ops-adopt-${props.run.id}-${props.analysisID}`)
const publication = computed(() => {
  const values = [...(localPublication.value ? [localPublication.value] : []), ...(props.adoptions || [])]
  return values.filter((item) => item.analysis_id === props.analysisID && item.run_id === props.run.id)
    .sort((a, b) => compareOpsTimestamps(b.updated_at || b.created_at, a.updated_at || a.created_at))[0]
})
const allowed = computed(() => !props.disabled && !stale.value && auth.hasRole('admin') && props.review?.action === 'accepted' && props.review.verdict === 'usable' && props.review.analysis_id === props.analysisID)
const approvalMatches = computed(() => !!props.review && publication.value?.review_id === props.review.id)
const canPublish = computed(() => allowed.value && expanded.value && !publishing.value && !uncertain.value && !publication.value && !targetError.value && !loadingTargets.value && targets.value.some((target) => target.id === draft.kb_id) && !!draft.question.trim() && !!draft.answer.trim())
const publicationError = computed(() => {
  const code = publication.value?.error || ''
  return k(['faq_publish_failed', 'faq_index_failed', 'faq_lookup_failed', 'faq_question_conflict', 'access_revoked', 'approval_changed'].includes(code) ? code : 'adoptionFailed')
})
function targetName(id: string) {
  return targets.value.find((target) => target.id === id)?.name || id
}
function operationScope() {
  const scope = scopeID
  const tenant = auth.effectiveTenantId
  const runID = props.run.id
  const analysisID = props.analysisID
  return () => scope === scopeID && tenant === auth.effectiveTenantId && runID === props.run.id && analysisID === props.analysisID
}
async function loadTargets() {
  if (!auth.hasRole('admin')) return
  const token = ++targetRequest
  const current = operationScope()
  loadingTargets.value = true
  targetError.value = ''
  try {
    const response = await getOpsAdoptionTargets()
    if (token !== targetRequest || !current()) return
    targets.value = response.items || []
    if (!targets.value.some((target) => target.id === draft.kb_id)) draft.kb_id = ''
  } catch (error) {
    if (token === targetRequest && current()) {
      targets.value = []
      targetError.value = k(opsErrorCode(error) === 'forbidden' ? 'adoptionForbidden' : 'adoptionTargetsFailed')
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
async function refreshPublication() {
  if (refreshing.value || publishing.value) return
  const current = operationScope()
  refreshing.value = true
  try {
    const value = await getOpsDetail(props.run.id)
    if (!current()) return
    const latestID = value.run.analysis?.id || [...value.analyses].sort((a, b) => b.version - a.version)[0]?.id
    const review = latestAnswerReview(value.reviews, props.analysisID)
    stale.value = latestID !== props.analysisID || review?.id !== props.review?.id || review?.action !== 'accepted' || !!value.pending_tasks
    localPublication.value = value.adoptions?.find((item) => item.analysis_id === props.analysisID)
    uncertain.value = false
    if (localPublication.value) publishError.value = ''
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
      kb_id: draft.kb_id,
      question: draft.question.trim(),
      answer: draft.answer.trim(),
    })
    if (!current()) return
    localPublication.value = result
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
  if (!allowed.value || !approvalMatches.value || publishing.value || !value || !['failed', 'publishing'].includes(value.status)) return
  const current = operationScope()
  publishing.value = true
  publishError.value = ''
  let reconcile = false
  try {
    const result = await retryOpsAdoption(props.run.id, value.id)
    if (!current()) return
    localPublication.value = result
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
    targets.value = []
    targetError.value = ''
    publishError.value = ''
    loadingTargets.value = false
    publishing.value = false
    refreshing.value = false
    uncertain.value = false
    stale.value = false
    localPublication.value = undefined
    const rewrite = props.run.query_context?.rewrite
    Object.assign(draft, { kb_id: '', question: rewrite?.status === 'rewritten' && rewrite.query.trim() ? rewrite.query : props.run.question, answer: props.run.answer })
    if ((props.adoptions || []).some((item) => item.analysis_id === props.analysisID)) void loadTargets()
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
.adoption-form label { display: flex; flex-direction: column; gap: var(--app-space-2); margin: var(--app-space-4) 0; font-size: var(--app-text-sm); }
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
