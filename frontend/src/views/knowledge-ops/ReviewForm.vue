<template>
  <form class="review-form" @submit.prevent="submit">
    <h3>{{ k(target === 'answer' ? 'answerReview' : 'causeReview') }}</h3>
    <label>{{ k('reviewAction') }}<select v-model="draft.action" :disabled="disabled || busy" @change="actionChanged"><option v-for="action in actions" :key="action" :value="action">{{ label(action) }}</option></select></label>
    <label>{{ k(target === 'answer' ? 'verdict' : 'causeConclusion') }}<select v-model="draft.verdict" :disabled="disabled || busy || draft.action !== 'corrected'"><option v-for="verdict in target === 'answer' ? answerVerdicts : causeVerdicts" :key="verdict" :value="verdict">{{ target === 'answer' ? labelVerdict(verdict) : label(verdict) }}</option></select></label>
    <label v-if="target === 'cause'">{{ k('causeCategory') }}<select v-model="draft.cause_category" :disabled="disabled || busy || draft.action === 'confirmed'"><option v-for="category in categories" :key="category" :value="category">{{ label(category) }}</option></select></label>
    <label>{{ k('reviewReason') }}<textarea v-model="draft.reason" rows="4" maxlength="4000" required :disabled="disabled || busy" /></label>
    <p v-if="error || localError" class="error" role="alert">{{ error || localError }}</p>
    <t-button type="submit" block :loading="busy" :disabled="disabled">{{ k('submit') }}</t-button>
  </form>
</template>
<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import type { ReviewTarget, ReviewInput } from '@/api/knowledge-ops'
import { createReviewDrafts, reviewPayload, answerVerdicts, causeVerdicts, applyReviewAction } from './state'
import { useOpsText } from './locales'
const props = defineProps<{ target: ReviewTarget; analysisID: string; initialVerdict: string; initialCategory?: string; disabled: boolean; busy: boolean; error?: string }>()
const emit = defineEmits<{ submit: [ReviewInput] }>()
const { k, label, labelVerdict } = useOpsText()
const localError = ref('')
const draft = reactive(createReviewDrafts()[props.target])
const actions = ['confirmed', 'corrected', 'needs_more_evidence']
const categories = ['generation', 'retrieval', 'wiki_source', 'source_conflict', 'applicability', 'capture', 'knowledge_gap', 'unknown']
function actionChanged() {
  applyReviewAction(props.target, draft, draft.action, props.initialVerdict, props.initialCategory)
}
watch(() => props.analysisID, () => {
  draft.action = props.target === 'cause' && !props.initialCategory ? 'needs_more_evidence' : 'confirmed'; draft.reason = ''; draft.verdict = props.initialVerdict
  draft.cause_category = props.initialCategory || 'unknown'
}, { immediate: true })
function submit() {
  localError.value = ''
  if (!draft.reason.trim()) { localError.value = k('reasonRequired'); return }
  if (new TextEncoder().encode(draft.reason.trim()).length > 4000) { localError.value = k('reasonTooLong'); return }
  if (!props.disabled) emit('submit', reviewPayload(props.target, draft, props.analysisID))
}
</script>
<style scoped>
.review-form{border:1px solid var(--td-component-border);border-radius:10px;padding:18px}.review-form h3{margin:0 0 16px;font-size:16px}.review-form label{display:flex;flex-direction:column;gap:8px;margin:16px 0;font-size:14px}select,textarea{box-sizing:border-box;width:100%;border:1px solid var(--td-component-border);background:var(--td-bg-color-container);color:var(--td-text-color-primary);border-radius:6px;padding:10px;font:inherit}textarea{resize:vertical}select:focus-visible,textarea:focus-visible{outline:2px solid var(--td-brand-color);outline-offset:2px}.error{color:var(--td-error-color);font-size:14px}
</style>
