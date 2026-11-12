<template>
  <div class="msg-feedback" :class="{ 'is-disabled': disabled }">
    <div class="msg-feedback__actions">
      <button
        type="button"
        class="msg-feedback__btn"
        :class="{ 'is-active': currentRating === 1 }"
        :disabled="disabled || submitting"
        :title="t('chat.feedback.like')"
        :aria-pressed="currentRating === 1"
        :aria-label="t('chat.feedback.like')"
        @click="handleRate(1)"
      >
        <ChatActionIcon name="thumb-up" />
      </button>
      <button
        type="button"
        class="msg-feedback__btn"
        :class="{ 'is-active is-negative': currentRating === -1 }"
        :disabled="disabled || submitting"
        :title="t('chat.feedback.dislike')"
        :aria-pressed="currentRating === -1"
        :aria-label="t('chat.feedback.dislike')"
        @click="handleRate(-1)"
      >
        <ChatActionIcon name="thumb-down" />
      </button>
    </div>
    <transition name="feedback-pop">
      <div v-if="showComment" class="msg-feedback__comment">
        <t-textarea
          v-model="commentText"
          :placeholder="t('chat.feedback.commentPlaceholder')"
          :autosize="{ minRows: 2, maxRows: 4 }"
          class="msg-feedback__textarea"
        />
        <div class="msg-feedback__comment-actions">
          <t-button size="small" variant="text" @click="skipComment">{{ t('chat.feedback.skip') }}</t-button>
          <t-button size="small" theme="primary" :loading="submitting" @click="submitComment">
            {{ t('chat.feedback.submit') }}
          </t-button>
        </div>
      </div>
    </transition>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import ChatActionIcon from '@/components/ChatActionIcon.vue'
import { useMessageFeedback, type FeedbackApi } from '@/composables/useMessageFeedback'

const props = withDefaults(
  defineProps<{
    sessionId: string
    messageId: string
    api: FeedbackApi
    disabled?: boolean
  }>(),
  { disabled: false },
)

const emit = defineEmits<{ (e: 'rated', rating: number): void }>()

const { t } = useI18n()
const { cache, ensureLoaded, getRating, rate, setContent } = useMessageFeedback(props.sessionId, props.api)
const submitting = ref(false)
const showComment = ref(false)
const commentText = ref('')

const currentRating = computed(() => (props.messageId ? getRating(props.messageId) : 0))

onMounted(() => {
  if (props.sessionId && props.messageId) {
    void ensureLoaded().catch(() => undefined)
  }
})

async function handleRate(rating: number) {
  if (props.disabled || submitting.value || !props.messageId) return
  submitting.value = true
  try {
    const next = await rate(props.messageId, rating)
    if (next !== 0) {
      // Re-open the comment box so the visitor can attach an optional note.
      commentText.value = cache.byMessage.get(props.messageId)?.content || ''
      showComment.value = true
      MessagePlugin.success(t('chat.feedback.thanks'))
    } else {
      showComment.value = false
      commentText.value = ''
    }
    emit('rated', next)
  } catch {
    // Best-effort: the button state simply stays unchanged on failure.
  } finally {
    submitting.value = false
  }
}

async function submitComment() {
  if (!props.messageId || submitting.value) return
  submitting.value = true
  try {
    await setContent(props.messageId, commentText.value.trim())
    showComment.value = false
  } catch {
    // Best-effort: keep the box open so the visitor can retry.
  } finally {
    submitting.value = false
  }
}

function skipComment() {
  showComment.value = false
}
</script>

<style scoped lang="less">
.msg-feedback {
  position: relative;
  display: inline-flex;
  align-items: center;

  &__actions {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }

  &__btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    padding: 0;
    border: 1px solid var(--td-component-stroke);
    border-radius: 999px;
    background: var(--td-bg-color-container);
    color: var(--td-text-color-secondary);
    cursor: pointer;
    transition: color var(--app-motion-fast) ease, border-color var(--app-motion-fast) ease,
      background var(--app-motion-fast) ease;

    &:hover:not(:disabled) {
      color: var(--td-brand-color);
      border-color: var(--td-brand-color);
    }

    &:disabled {
      cursor: default;
      opacity: 0.6;
    }

    &.is-active {
      color: var(--td-brand-color);
      border-color: var(--td-brand-color);
      background: color-mix(in srgb, var(--td-brand-color) 12%, var(--td-bg-color-container));
    }

    &.is-negative.is-active {
      color: var(--td-error-color);
      border-color: var(--td-error-color);
      background: color-mix(in srgb, var(--td-error-color) 12%, var(--td-bg-color-container));
    }
  }

  &__comment {
    position: absolute;
    top: calc(100% + 8px);
    left: 0;
    z-index: 30;
    width: 280px;
    padding: 10px;
    border: 1px solid var(--td-component-stroke);
    border-radius: var(--td-radius-medium);
    background: var(--td-bg-color-container);
    box-shadow: var(--td-shadow-2);
  }

  &__comment-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 8px;
  }

  &.is-disabled {
    opacity: 0.5;
    pointer-events: none;
  }
}

:deep(.chat-action-icon) {
  width: 16px;
  height: 16px;
}

:deep(.msg-feedback__textarea .t-textarea__inner) {
  font-size: 12px;
}

.feedback-pop-enter-active,
.feedback-pop-leave-active {
  transition: opacity var(--app-motion-fast) ease, transform var(--app-motion-fast) ease;
}

.feedback-pop-enter-from,
.feedback-pop-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
</style>
