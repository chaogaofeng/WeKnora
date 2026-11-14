<template>
  <div class="msg-feedback" :class="{ 'is-disabled': disabled }">
    <div ref="anchorRef" class="msg-feedback__actions">
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
    <!-- Teleported to body: the message list scroll container clips absolutely
         positioned descendants, which used to cut the comment box in half. -->
    <Teleport to="body">
      <transition name="feedback-pop">
        <div v-if="showComment" ref="popoverRef" class="msg-feedback__comment" :style="popoverStyle">
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
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
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

// The comment box lives in a body teleport (see template), so it cannot rely on
// being a child of the toolbar: it is positioned with `fixed` coordinates taken
// from the action row and kept in sync while the message list scrolls/resizes.
const anchorRef = ref<HTMLElement | null>(null)
const popoverRef = ref<HTMLElement | null>(null)
const popoverStyle = ref<Record<string, string>>({})

const POPOVER_WIDTH = 280
const POPOVER_GAP = 8
const VIEWPORT_PADDING = 12
const POPOVER_ESTIMATED_HEIGHT = 176

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), Math.max(min, max))
}

function updatePopoverPosition() {
  const anchor = anchorRef.value
  if (!anchor || typeof window === 'undefined') return
  const rect = anchor.getBoundingClientRect()
  // If the toolbar scrolled entirely out of the viewport, a viewport-clamped
  // popover would float detached from its anchor — close it instead.
  if (showComment.value && (rect.bottom < 0 || rect.top > window.innerHeight)) {
    showComment.value = false
    return
  }
  const width = Math.min(POPOVER_WIDTH, Math.max(window.innerWidth - VIEWPORT_PADDING * 2, 160))
  const height = popoverRef.value?.offsetHeight || POPOVER_ESTIMATED_HEIGHT
  const spaceBelow = window.innerHeight - rect.bottom - VIEWPORT_PADDING - POPOVER_GAP
  const spaceAbove = rect.top - VIEWPORT_PADDING - POPOVER_GAP
  const openUpward = spaceBelow < Math.min(height, spaceAbove) && spaceAbove > 0
  const rawTop = openUpward ? rect.top - height - POPOVER_GAP : rect.bottom + POPOVER_GAP
  const top = clamp(rawTop, VIEWPORT_PADDING, window.innerHeight - height - VIEWPORT_PADDING)
  const left = clamp(rect.left, VIEWPORT_PADDING, window.innerWidth - width - VIEWPORT_PADDING)
  popoverStyle.value = {
    top: `${Math.round(top)}px`,
    left: `${Math.round(left)}px`,
    width: `${Math.round(width)}px`,
  }
}

// Dismiss on outside click: the rating itself is already persisted when the
// thumb is clicked, so closing here never loses data. Clicks inside the
// popover (textarea / skip / submit) and on the thumb row (toggle logic in
// handleRate) are left untouched.
function onDocPointerdown(event: Event) {
  const target = event.target as Node | null
  if (!target || !showComment.value) return
  if (popoverRef.value?.contains(target) || anchorRef.value?.contains(target)) return
  showComment.value = false
}

// Esc is a plain close (no stopPropagation) so it never swallows shortcuts of
// dialogs that may be stacked above.
function onDocKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && showComment.value) showComment.value = false
}

function bindReposition() {
  window.addEventListener('resize', updatePopoverPosition)
  window.addEventListener('scroll', updatePopoverPosition, true)
  document.addEventListener('pointerdown', onDocPointerdown, true)
  document.addEventListener('keydown', onDocKeydown)
}

// The autosize textarea grows as the visitor types; re-anchor when the box
// gets taller so an upward-opened popover never overshoots the toolbar.
// Must run after the popover is mounted (popoverRef is null before nextTick).
function bindHeightObserver() {
  const popover = popoverRef.value
  if (!popover || typeof ResizeObserver === 'undefined') return
  heightObserver = new ResizeObserver(updatePopoverPosition)
  heightObserver.observe(popover)
}

function unbindReposition() {
  window.removeEventListener('resize', updatePopoverPosition)
  window.removeEventListener('scroll', updatePopoverPosition, true)
  document.removeEventListener('pointerdown', onDocPointerdown, true)
  document.removeEventListener('keydown', onDocKeydown)
  heightObserver?.disconnect()
  heightObserver = null
}

let heightObserver: ResizeObserver | null = null

watch(showComment, async (open) => {
  if (open) {
    bindReposition()
    await nextTick()
    updatePopoverPosition()
    bindHeightObserver()
  } else {
    unbindReposition()
  }
})

onBeforeUnmount(unbindReposition)

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
      updatePopoverPosition()
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

  &.is-disabled {
    opacity: 0.5;
    pointer-events: none;
  }
}

// Touch devices have no hover: enlarge the thumb targets (26px is well below
// the ~44px touch guideline) and give the pair a bit more separation.
@media (hover: none) {
  .msg-feedback__actions {
    gap: 10px;
  }

  .msg-feedback__btn {
    width: 32px;
    height: 32px;
  }

  :deep(.chat-action-icon) {
    width: 18px;
    height: 18px;
  }
}

// Teleported to body, hence the standalone selectors: `.msg-feedback` is no
// longer an ancestor once the box is rendered outside the message tree.
.msg-feedback__comment {
  position: fixed;
  z-index: 2000;
  box-sizing: border-box;
  max-height: min(60vh, 320px);
  overflow-y: auto;
  padding: 10px;
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--td-radius-medium);
  background: var(--td-bg-color-container);
  box-shadow: var(--td-shadow-2);
}

.msg-feedback__comment-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 8px;
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
