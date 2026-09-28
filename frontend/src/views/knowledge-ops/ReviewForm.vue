<template>
  <form class="review-form" @submit.prevent="submit">
    <h3><span class="review-step">{{ target === 'answer' ? '01' : '02' }}</span>{{ k(target === 'answer' ? 'answerReview' : 'causeReview') }}</h3>
    <label :id="`review-action-${target}`">{{ k('reviewAction') }}<t-select class="review-action" v-model="draft.action" :aria-labelledby="`review-action-${target}`" :options="actions.map(value => ({value, label: label(value)}))" :disabled="disabled || busy" @change="actionChanged" /></label>
    <label v-if="target === 'cause' && draft.action === 'confirmed' && machineCauses?.length" :id="`review-candidate-${target}`">{{ k('causes') }}<t-select class="review-candidate" :value="selectedCandidate" :aria-labelledby="`review-candidate-${target}`" :options="machineCauses.map((cause, index) => ({value: String(index), label: `${label(cause.category)} · ${label(cause.status)} — ${cause.summary}`}))" :disabled="disabled || busy" @change="selectCandidate" /></label>
    <label :id="`review-verdict-${target}`">{{ k(target === 'answer' ? 'verdict' : 'causeConclusion') }}<t-select v-model="draft.verdict" :aria-labelledby="`review-verdict-${target}`" :options="(target === 'answer' ? answerVerdicts : causeVerdicts).map(value => ({value, label: target === 'answer' ? labelVerdict(value) : label(value)}))" :disabled="disabled || busy || draft.action !== 'corrected'" /></label>
    <label v-if="target === 'cause'" :id="`review-category-${target}`">{{ k('causeCategory') }}<t-select v-model="draft.cause_category" :aria-labelledby="`review-category-${target}`" :options="categories.map(value => ({value, label: label(value)}))" :disabled="disabled || busy || draft.action === 'confirmed'" /></label>
    <label>{{ k('reviewReason') }}<t-textarea v-model="draft.reason" :autosize="{minRows:3,maxRows:7}" :maxlength="4000" :placeholder="k('reviewReasonHint')" :aria-label="k('reviewReason')" :disabled="disabled || busy" /></label>
    <p v-if="error || localError" class="error" role="alert">{{ error || localError }}</p>
    <t-button type="submit" block :loading="busy" :disabled="disabled">{{ k('submit') }}</t-button>
  </form>
</template>
<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import type { OpsCause, ReviewTarget, ReviewInput } from '@/api/knowledge-ops'
import { createReviewDrafts, reviewPayload, answerVerdicts, causeVerdicts, applyReviewAction } from './state'
import { useOpsText } from './locales'
const props = defineProps<{ target: ReviewTarget; analysisID: string; initialVerdict: string; initialCategory?: string; machineCauses?: OpsCause[]; disabled: boolean; busy: boolean; error?: string }>()
const emit = defineEmits<{ submit: [ReviewInput] }>()
const { k, label, labelVerdict } = useOpsText()
const localError = ref('')
const draft = reactive(createReviewDrafts()[props.target])
const selectedCandidate = ref('0')
const actions = ['confirmed', 'corrected', 'needs_more_evidence']
const categories = ['generation', 'retrieval', 'wiki_source', 'source_conflict', 'applicability', 'capture', 'knowledge_gap', 'unknown']
function actionChanged() {
  if (props.target === 'cause' && draft.action === 'confirmed') {
    if (!props.machineCauses?.length) { applyReviewAction('cause', draft, 'needs_more_evidence', 'unresolved'); return }
    selectCandidate(selectedCandidate.value)
    return
  }
  applyReviewAction(props.target, draft, draft.action, props.initialVerdict, props.initialCategory)
}
function selectCandidate(value: unknown) {
  if (props.disabled || props.busy || draft.action !== 'confirmed') return
  const candidate = props.machineCauses?.[Number(value)]
  if (!candidate) return
  selectedCandidate.value = String(value)
  applyReviewAction('cause', draft, 'confirmed', candidate.status, candidate.category)
}
watch(() => props.analysisID, () => {
  selectedCandidate.value = '0'
  draft.action = props.target === 'cause' && !props.machineCauses?.length ? 'needs_more_evidence' : 'confirmed'; draft.reason = ''; draft.verdict = props.initialVerdict
  draft.cause_category = props.initialCategory || 'unknown'
}, { immediate: true })
function submit() {
  localError.value = ''
  if (!draft.reason.trim()) { localError.value = k('reasonRequired'); return }
  if (new TextEncoder().encode(draft.reason.trim()).length > 4000) { localError.value = k('reasonTooLong'); return }
  if (!props.disabled && !props.busy) emit('submit', reviewPayload(props.target, draft, props.analysisID))
}
</script>
<style scoped>
.review-form{border:1px solid var(--td-component-stroke);border-radius:10px;padding:18px;background:var(--td-bg-color-container)}.review-form h3{margin:0 0 18px;font-size:15px;display:flex;gap:10px;align-items:center;font-weight:600}.review-form label{display:flex;flex-direction:column;gap:8px;margin:14px 0;font-size:12px;color:var(--td-text-color-secondary)}select,textarea{box-sizing:border-box;width:100%;border:1px solid var(--td-component-border);background:var(--td-bg-color-container);color:var(--td-text-color-primary);border-radius:6px;padding:10px;font:inherit}textarea{resize:vertical}select:focus-visible,textarea:focus-visible{outline:2px solid var(--td-brand-color);outline-offset:2px}.error{color:var(--td-error-color);font-size:14px}
.review-step{display:inline-flex;align-items:center;justify-content:center;width:26px;height:26px;border:1px solid var(--td-component-stroke);border-radius:7px;background:var(--td-bg-color-secondarycontainer);font-size:11px;color:var(--td-text-color-secondary);font-variant-numeric:tabular-nums}.review-form :deep(.t-input){border-radius:6px;height:36px;font-size:13px}.review-form :deep(.t-textarea__inner){border-radius:6px;font-size:13px;line-height:1.8}.review-form :deep(.t-button){border-radius:6px;height:36px;font-size:13px}
</style>
