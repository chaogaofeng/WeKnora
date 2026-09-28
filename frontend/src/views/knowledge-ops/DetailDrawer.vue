<template>
  <t-drawer :visible="!!runID" :header="k('detailTitle')" size="min(1180px, 95vw)" :footer="false" @close="emit('close')">
    <div class="ops-detail">
      <div class="toolbar"><t-button variant="outline" :loading="loading" @click="load">{{ k('refresh') }}</t-button><t-button :loading="retrying" :disabled="!enabled || !detail?.run.finished_at || !!detail?.pending_tasks" @click="rerun">{{ k('rerun') }}</t-button></div>
      <div v-if="loading && !detail" class="state" role="status">{{ k('loading') }}</div>
      <div v-else-if="loadError" class="state error" role="alert">{{ loadError }}<t-button variant="text" @click="load">{{ k('retry') }}</t-button></div>
      <template v-else-if="detail">
        <p v-if="notice" class="notice" role="status">{{ notice }}</p>
        <div class="chips"><span>{{ label(detail.run.qa_type) }}</span><span>{{ detail.run.channel || k('unknown') }}</span><span>{{ label(detail.run.capture_status) }}</span><span>{{ label(detail.run.review_state) }}</span></div>
        <section><h3>{{ k('question') }}</h3><p class="body-text">{{ detail.run.question }}</p><h3>{{ k('answer') }}</h3><p class="body-text">{{ detail.run.answer || k('none') }}</p></section>
        <section><h3>{{ k('metadata') }}</h3><dl><dt>ID</dt><dd>{{ detail.run.id }}</dd><dt>{{ k('time') }}</dt><dd>{{ date(detail.run.started_at) }}</dd><dt>{{ k('execution') }}</dt><dd>{{ detail.run.execution_mode || k('unknown') }}</dd><dt>{{ k('runStatus') }}</dt><dd>{{ label(detail.run.status) }}</dd><dt>{{ k('pendingTasks') }}</dt><dd>{{ detail.pending_tasks ?? 0 }}</dd><dt>{{ k('generationModel') }}</dt><dd>{{ detail.run.model_id || k('unknown') }}</dd></dl><h4>{{ k('captureGaps') }}</h4><ul v-if="detail.run.gaps?.length"><li v-for="gap in detail.run.gaps" :key="gap">{{ gap }}</li></ul><p v-else>{{ k('noGaps') }}</p></section>
        <section>
          <h3>{{ k('reports') }}</h3><select v-if="detail.analyses.length" v-model="selectedID" :aria-label="k('reports')"><option v-for="item in detail.analyses" :key="item.id" :value="item.id">v{{ item.version }} · {{ label(item.status) }} · {{ date(item.created_at) }}{{ item.id === latestID ? ` · ${k('latest')}` : '' }}</option></select>
          <p v-else>{{ k('noAnalysis') }} · {{ k('noAnalysisHint') }}</p>
          <template v-if="analysis">
            <p v-if="!isCurrent" class="notice">{{ k('historical') }}</p>
            <p v-if="analysis.error" class="error" role="alert">{{ analysis.error }}</p>
            <dl><dt>{{ k('model') }}</dt><dd>{{ analysis.model_name || analysis.model_id || k('unknown') }}</dd><dt>{{ k('policy') }}</dt><dd>{{ analysis.policy_version || k('unknown') }}</dd><dt>{{ k('verdict') }}</dt><dd>{{ labelVerdict(analysis.report.verdict) }}</dd><dt>{{ k('sourceCheck') }}</dt><dd>{{ label(analysis.report.source_check) }}</dd><dt>{{ k('risk') }}</dt><dd>{{ label(analysis.report.risk) }}</dd><dt>{{ k('scope') }}</dt><dd>{{ analysis.report.scope || k('unknown') }}</dd><dt>{{ k('route') }}</dt><dd>{{ label(analysis.route) }} · {{ analysis.route_reason }}</dd><dt>{{ k('group') }}</dt><dd>{{ analysis.group_key || k('none') }}</dd><dt>{{ k('tokens') }}</dt><dd>{{ analysis.input_tokens }} / {{ analysis.output_tokens }}</dd><dt>{{ k('duration') }}</dt><dd>{{ analysis.duration_ms }}</dd><dt>{{ k('stratum') }}</dt><dd>{{ analysis.sample_stratum || k('none') }} · {{ analysis.sample_percent }}%</dd></dl>
            <h4>{{ k('claims') }}</h4><article v-for="(claim, index) in analysis.report.claims || []" :key="index" class="item"><strong>{{ labelVerdict(claim.verdict) }}</strong><p class="body-text">{{ claim.text }}</p><p class="muted">{{ k('position') }}: {{ claim.position }} · {{ claim.reason }}</p><div class="evidence-links"><button v-for="id in claim.evidence_ids" :key="id" type="button" @click="showEvidence(id)">{{ evidenceTitle(id) }}</button></div></article><p v-if="!analysis.report.claims?.length">{{ k('none') }}</p>
            <h4>{{ k('facts') }}</h4><ul v-if="analysis.report.facts?.length"><li v-for="(fact, index) in analysis.report.facts" :key="index">{{ fact }}</li></ul><p v-else>{{ k('none') }}</p>
            <h4>{{ k('causes') }}</h4><article v-for="(cause, index) in analysis.report.causes || []" :key="index" class="item"><strong>{{ label(cause.category) }} · {{ label(cause.status) }}</strong><p>{{ cause.summary }}</p><p>{{ k('supporting') }}</p><div class="evidence-links"><button v-for="id in cause.evidence_ids" :key="id" type="button" @click="showEvidence(id)">{{ evidenceTitle(id) }}</button><span v-if="!cause.evidence_ids?.length">{{ k('none') }}</span></div><p>{{ k('opposing') }}</p><div class="evidence-links"><template v-for="(item, counterIndex) in cause.counter_evidence" :key="counterIndex"><button v-if="hasEvidence(item)" type="button" @click="showEvidence(item)">{{ evidenceTitle(item) }}</button><span v-else class="body-text">{{ item }}</span></template><span v-if="!cause.counter_evidence?.length">{{ k('none') }}</span></div></article><p v-if="!analysis.report.causes?.length">{{ k('none') }}</p>
            <h4>{{ k('unknowns') }}</h4><ul v-if="analysis.report.unknowns?.length"><li v-for="(item, index) in analysis.report.unknowns" :key="index">{{ item }}</li></ul><p v-else>{{ k('none') }}</p>
            <h4>{{ k('reviewQuestions') }}</h4><ul v-if="analysis.report.review_questions?.length"><li v-for="(item, index) in analysis.report.review_questions" :key="index">{{ item }}</li></ul><p v-else>{{ k('none') }}</p>
          </template>
        </section>
        <section v-for="group in evidenceGroups" :key="group.key"><h3>{{ k(group.key) }}</h3><p v-if="group.key === 'postEvidence'" class="notice">{{ k('postNotice') }}</p><p v-if="!group.items.length">{{ k('none') }}</p><article v-for="item in group.items" :id="`ops-evidence-${item.id}`" :key="item.id" class="item evidence-item" tabindex="-1"><h4>{{ item.title || item.kind }} <small>{{ item.id }}</small></h4><p class="muted">{{ item.kind }} · {{ k('version') }} {{ item.version || k('unknown') }} · {{ date(item.acquired_at) }} · {{ k(item.truncated ? 'truncated' : 'notTruncated') }}</p><dl><dt>KB / Knowledge</dt><dd>{{ item.kb_id || '—' }} / {{ item.knowledge_id || '—' }}</dd><dt>Page / Chunk</dt><dd>{{ item.page_id || item.slug || '—' }} / {{ item.chunk_id || '—' }}</dd><dt>Tool / Round</dt><dd>{{ item.tool_call_id || '—' }} / {{ item.round }}</dd></dl><pre>{{ item.content || k('none') }}</pre></article></section>
        <section v-if="analysis"><p v-if="analysis.status === 'failed'" class="notice">{{ k('failedReviewNotice') }}</p><p v-if="!canReview" class="notice">{{ k('reviewUnavailable') }}</p><p class="notice">{{ k('reviewNotice') }}</p><div class="review-grid"><ReviewForm :key="`${analysis.id}:answer:${savedReviews.answer}`" target="answer" :analysisID="analysis.id" :initial-verdict="analysis.report.verdict || 'unverifiable'" :disabled="!canReview" :busy="reviewing.answer" :error="reviewErrors.answer" @submit="saveReview" /><ReviewForm :key="`${analysis.id}:cause:${savedReviews.cause}`" target="cause" :analysisID="analysis.id" :initial-verdict="analysis.report.causes?.[0]?.status || 'unresolved'" :initial-category="analysis.report.causes?.[0]?.category" :disabled="!canReview" :busy="reviewing.cause" :error="reviewErrors.cause" @submit="saveReview" /></div></section>
        <section><h3>{{ k('reviewHistory') }}</h3><p v-if="!detail.reviews.length">{{ k('none') }}</p><article v-for="review in detail.reviews" :key="review.id" class="item"><strong>{{ k(review.target === 'answer' ? 'answerTarget' : 'causeTarget') }} · {{ label(review.action) }} · {{ review.target === 'answer' ? labelVerdict(review.verdict) : label(review.verdict) }}<template v-if="review.cause_category"> · {{ label(review.cause_category) }}</template></strong><p class="body-text">{{ review.reason }}</p><small>{{ date(review.created_at) }} · {{ review.reviewer_id }} · {{ review.analysis_id }}</small></article></section>
      </template>
    </div>
  </t-drawer>
</template>
<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { addOpsReview, getOpsDetail, retryOpsRun, opsErrorCode, type OpsDetail, type ReviewInput } from '@/api/knowledge-ops'
import ReviewForm from './ReviewForm.vue'
import { useOpsText } from './locales'
const props = defineProps<{ runID: string; enabled: boolean }>()
const emit = defineEmits<{ close: []; changed: [] }>()
const { k, label, labelVerdict, date } = useOpsText()
const detail = ref<OpsDetail>(); const loading = ref(false); const loadError = ref(''); const selectedID = ref(''); const retrying = ref(false); const notice = ref('')
const savedReviews = reactive({ answer: 0, cause: 0 });
const reviewing = reactive({ answer: false, cause: false }); const reviewErrors = reactive({ answer: '', cause: '' })
let requestID = 0
const latestID = computed(() => detail.value?.run.analysis?.id || [...(detail.value?.analyses || [])].sort((a, b) => b.version - a.version)[0]?.id || '')
const analysis = computed(() => detail.value?.analyses.find(item => item.id === selectedID.value))
const isCurrent = computed(() => selectedID.value === latestID.value)
const canReview = computed(() => isCurrent.value && !!detail.value?.run.finished_at && !!analysis.value)
const evidenceGroups = computed(() => {
  const items = detail.value?.evidence || []
  return [
    { key: 'originalEvidence', items: items.filter(item => !item.post_hoc && item.sent_to_model) },
    { key: 'candidateEvidence', items: items.filter(item => !item.post_hoc && !item.sent_to_model) },
    { key: 'postEvidence', items: items.filter(item => item.post_hoc) },
  ]
})
function hasEvidence(id: string) { return !!detail.value?.evidence.some(item => item.id === id) }
function evidenceTitle(id: string) { return detail.value?.evidence.find(item => item.id === id)?.title || id }
function showEvidence(id: string) {
  const element = document.getElementById(`ops-evidence-${id}`)
  if (element) { element.scrollIntoView({ block: 'center' }); element.focus() }
  else notice.value = `${k('evidenceMissing')}: ${id}`
}
async function load() {
  if (!props.runID) return
  const token = ++requestID; const id = props.runID
  loading.value = true; loadError.value = ''
  try { const value = await getOpsDetail(id); if (token !== requestID) return; detail.value = value; selectedID.value = value.run.analysis?.id || [...value.analyses].sort((a, b) => b.version - a.version)[0]?.id || '' }
  catch (error) { if (token === requestID) { detail.value = undefined; loadError.value = k(opsErrorCode(error)) } }
  finally { if (token === requestID) loading.value = false }
}
async function rerun() {
  retrying.value = true; notice.value = ''
  try { await retryOpsRun(props.runID); notice.value = k('queued'); emit('changed') }
  catch (error) { notice.value = k(opsErrorCode(error)) }
  finally { retrying.value = false }
}
async function saveReview(value: ReviewInput) {
  const target = value.target; const runID = props.runID; reviewing[target] = true; reviewErrors[target] = ''
  try { await addOpsReview(runID, value); if (props.runID !== runID) { emit('changed'); return }; notice.value = k('saved'); await load(); savedReviews[target]++; emit('changed') }
  catch (error) { if (props.runID === runID) reviewErrors[target] = k(opsErrorCode(error)) }
  finally { reviewing[target] = false }
}
watch(() => props.runID, id => { requestID++; detail.value = undefined; notice.value = ''; reviewErrors.answer = ''; reviewErrors.cause = ''; if (id) void load() }, { immediate: true })
</script>
<style scoped>
.ops-detail{line-height:1.65;color:var(--td-text-color-primary)}.toolbar{display:flex;gap:12px;justify-content:flex-end;margin-bottom:18px}.chips{display:flex;flex-wrap:wrap;gap:10px;margin:16px 0}.chips span{background:var(--td-bg-color-secondarycontainer);padding:4px 10px;border-radius:5px;font-size:13px}section{border:1px solid var(--td-component-border);border-radius:10px;padding:20px;margin:18px 0}h3{font-size:18px;margin:0 0 14px}h4{margin:20px 0 10px}.body-text{white-space:pre-wrap;overflow-wrap:anywhere}.notice{padding:12px 16px;background:var(--td-brand-color-light);border-radius:8px}.muted,small{color:var(--td-text-color-secondary);font-size:13px}dl{display:grid;grid-template-columns:160px minmax(0,1fr);font-size:13px;gap:6px 12px}dt{color:var(--td-text-color-secondary)}dd{margin:0;overflow-wrap:anywhere}.item{border-top:1px solid var(--td-component-border);padding:14px 0}.item p{margin:7px 0}.evidence-links{display:flex;gap:8px;flex-wrap:wrap}.evidence-links button{background:var(--td-brand-color-light);color:var(--td-brand-color);border:0;padding:5px 10px;border-radius:5px;cursor:pointer;font:inherit;font-size:13px}pre{font:inherit;font-size:13px;white-space:pre-wrap;overflow-wrap:anywhere;max-height:360px;overflow:auto;background:var(--td-bg-color-secondarycontainer);padding:14px;border-radius:6px}.review-grid{display:grid;grid-template-columns:1fr 1fr;gap:20px}.error{color:var(--td-error-color)}.state{padding:48px;text-align:center}.evidence-item:focus{outline:2px solid var(--td-brand-color);outline-offset:6px}select{border:1px solid var(--td-component-border);border-radius:6px;padding:10px;background:var(--td-bg-color-container);color:inherit;max-width:100%;font:inherit}button:focus-visible,select:focus-visible{outline:2px solid var(--td-brand-color);outline-offset:2px}@media(max-width:700px){.review-grid{grid-template-columns:1fr}section{padding:14px}dl{grid-template-columns:1fr}dt{font-weight:500}}
</style>
