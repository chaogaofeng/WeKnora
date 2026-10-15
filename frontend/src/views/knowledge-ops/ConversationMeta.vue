<template>
  <div v-if="run.conversation || run.session_id" class="conversation-meta">
    <div v-if="run.conversation" class="conversation-row">
      <span class="turn-label">{{ turnLabel }}</span>
      <button class="conversation-link" type="button" @click="emit('open', run.id)">
        {{ k('viewConversation') }}
      </button>
    </div>
    <div v-if="run.session_id" class="session-row">
      <span class="session-label">{{ k('sessionIdLabel') }}:</span>
      <span class="session-value" :title="run.session_id" :aria-label="run.session_id" tabindex="0">
        <span class="session-prefix">{{ run.session_id.slice(0, -8) }}</span><span class="session-suffix">{{ run.session_id.slice(-8) }}</span>
      </span>
      <t-button
        class="copy-session"
        size="small"
        variant="text"
        theme="default"
        :title="k('copySessionId')"
        :aria-label="k('copySessionId')"
        @click.stop="copySessionID"
      ><CopyIcon /></t-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { CopyIcon } from 'tdesign-icons-vue-next'
import type { OpsRun } from '@/api/knowledge-ops'
import { useOpsText } from './locales'
const props = defineProps<{ run: OpsRun }>()
const emit = defineEmits<{ open: [id: string] }>()
const { k } = useOpsText()
const turnLabel = computed(() => {
  const conversation = props.run.conversation
  if (!conversation) return ''
  if (!conversation.turn_known) return k(conversation.has_earlier ? 'laterTurnUnknown' : 'turnUnknown')
  return `${k(conversation.turn_number > 1 ? 'multiTurn' : 'firstTurn')} · ${k('turnNumber')} ${conversation.turn_number}`
})
async function copySessionID() {
  // Load the browser clipboard/toast helper only when copying is requested.
  const { copyWithToast } = await import('@/utils/clipboard')
  await copyWithToast(props.run.session_id, 'common.copySuccess')
}
</script>

<style scoped>
.conversation-meta {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: var(--app-space-1);
  min-width: 0;
  font-size: var(--app-text-sm);
  font-weight: 400;
  line-height: 1.5;
  color: var(--td-text-color-secondary);
}
.conversation-row,
.session-row {
  display: flex;
  align-items: center;
  gap: var(--app-space-2);
  max-width: 100%;
  min-width: 0;
}
.conversation-row { flex-wrap: wrap; }
.conversation-link {
  border: 0;
  padding: 0;
  background: transparent;
  color: var(--td-brand-color);
  font: inherit;
  cursor: pointer;
  white-space: nowrap;
}
.conversation-link:hover { text-decoration: underline; }
.conversation-link:focus-visible,
.session-value:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: var(--app-space-1);
}
.session-label,
.session-suffix { flex-shrink: 0; }
.session-value {
  display: inline-flex;
  min-width: 0;
  font-family: var(--app-font-family-mono);
  user-select: text;
  white-space: nowrap;
}
.session-prefix {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
.copy-session {
  flex-shrink: 0;
  padding: 0 var(--app-space-1);
  color: var(--td-text-color-secondary);
}
</style>
