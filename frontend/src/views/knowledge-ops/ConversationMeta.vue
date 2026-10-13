<template>
  <div v-if="run.conversation || run.session_id" class="conversation-meta">
    <button
      v-if="run.conversation"
      class="conversation-link"
      type="button"
      @click="emit('open', run.id)"
    >
      {{
        run.conversation.turn_known
          ? `${k(run.conversation.turn_number > 1 ? 'multiTurn' : 'firstTurn')} · ${k('turnNumber')} ${run.conversation.turn_number}`
          : k(run.conversation.has_earlier ? 'laterTurnUnknown' : 'turnUnknown')
      }}
      · {{ k('viewConversation') }}
    </button>
    <span v-if="run.session_id" class="session-id">
      {{ k('sessionIdLabel') }}: <span>{{ run.session_id }}</span>
    </span>
  </div>
</template>

<script setup lang="ts">
import type { OpsRun } from '@/api/knowledge-ops'
import { useOpsText } from './locales'
defineProps<{ run: OpsRun }>()
const emit = defineEmits<{ open: [id: string] }>()
const { k } = useOpsText()
</script>

<style scoped>
.conversation-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--app-space-1) var(--app-space-3);
  font-size: var(--app-text-sm);
  font-weight: 400;
}
.conversation-link {
  border: 0;
  padding: 0;
  background: transparent;
  color: var(--td-brand-color);
  font: inherit;
  cursor: pointer;
  text-align: left;
}
.conversation-link:hover {
  text-decoration: underline;
}
.conversation-link:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: var(--app-space-1);
}
.session-id {
  min-width: 0;
  color: var(--td-text-color-secondary);
  overflow-wrap: anywhere;
  user-select: text;
}
</style>
