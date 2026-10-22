<template>
  <form class="review-form" @submit.prevent="submit">
    <h3>{{ k('answerReview') }}</h3>
    <div class="review-fields">
      <label id="review-action-answer">
        {{ k('reviewAction') }}
        <t-select
          class="review-action"
          v-model="draft.action"
          aria-labelledby="review-action-answer"
          :options="actions.map((value) => ({
            value,
            label: k(`reviewDecision_${value}`),
          }))"
          :disabled="disabled || busy"
          @change="actionChanged"
        />
      </label>
    </div>
    <p class="review-hint">{{ k('answerQualityHint') }}</p>
    <label>
      {{ k(reviewNeedsReason(draft.action) ? 'reviewReasonRequired' : 'reviewNoteOptional') }}
      <t-textarea
        v-model="draft.reason"
        :autosize="{ minRows: 3, maxRows: 7 }"
        :maxlength="4000"
        :placeholder="k(reviewNeedsReason(draft.action) ? 'reviewReasonHint' : 'reviewNoteOptionalHint')"
        :aria-label="k(reviewNeedsReason(draft.action) ? 'reviewReasonRequired' : 'reviewNoteOptional')"
        :aria-required="reviewNeedsReason(draft.action)"
        :disabled="disabled || busy"
      />
    </label>
    <p v-if="error || localError" class="error" role="alert">
      {{ error || localError }}
    </p>
    <t-button type="submit" :loading="busy" :disabled="disabled || busy">
      {{ k('submit') }}
    </t-button>
  </form>
</template>
<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import type { AnswerReviewAction, ReviewInput } from '@/api/knowledge-ops'
import { createReviewDraft, reviewPayload, applyReviewAction, reviewNeedsReason } from './state'
import { useOpsText } from './locales'
const props = defineProps<{
  analysisID: string
  disabled: boolean
  busy: boolean
  error?: string
}>()
const emit = defineEmits<{ submit: [ReviewInput] }>()
const { k } = useOpsText()
const localError = ref('')
const draft = reactive(createReviewDraft())
const actions: AnswerReviewAction[] = ['accepted', 'revision_required', 'needs_more_evidence', 'excluded']
function actionChanged() {
  localError.value = ''
  applyReviewAction(draft, draft.action)
}
watch(
  () => props.analysisID,
  () => {
    localError.value = ''
    Object.assign(draft, createReviewDraft())
  },
  { immediate: true },
)
function submit() {
  localError.value = ''
  if (reviewNeedsReason(draft.action) && !draft.reason.trim()) {
    localError.value = k('reasonRequired')
    return
  }
  if (new TextEncoder().encode(draft.reason.trim()).length > 4000) {
    localError.value = k('reasonTooLong')
    return
  }
  if (!props.disabled && !props.busy)
    emit('submit', reviewPayload(draft, props.analysisID))
}
</script>
<style scoped>
.review-form {
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-lg);
  padding: var(--app-space-4);
  background: var(--td-bg-color-container);
}
.review-form h3 {
  margin: 0 0 var(--app-space-4);
  font-size: var(--app-text-lg);
  display: flex;
  gap: var(--app-space-2);
  align-items: center;
  font-weight: 600;
}
.review-form label {
  display: flex;
  flex-direction: column;
  gap: var(--app-space-2);
  margin: var(--app-space-3) 0;
  font-size: var(--app-text-sm);
  color: var(--td-text-color-secondary);
}
.error {
  color: var(--td-error-color);
  font-size: var(--app-text-base);
}
.review-hint {
  margin: calc(-1 * var(--app-space-1)) 0 var(--app-space-3);
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
  line-height: 1.6;
}
.review-fields {
  display: grid;
  grid-template-columns: 1fr;
  gap: var(--app-space-4);
}
@media (max-width: 600px) {
  .review-fields { grid-template-columns: 1fr; gap: 0; }
}
</style>
