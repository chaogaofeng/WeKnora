<template>
  <details class="conversation-panel" @toggle="onToggle">
    <summary>
      <strong>{{ k('conversationContext') }}</strong>
      <span>{{ run.conversation?.title || k('conversation') }}</span>
      <span>{{
        run.conversation?.turn_known
          ? `${k('turnNumber')} ${run.conversation.turn_number}`
          : k('turnUnknown')
      }}</span>
    </summary>
    <div class="conversation-body">
      <div class="context-heading">
        <p class="muted">{{ k('conversationNotice') }}</p>
        <router-link
          :to="{ name: 'chat', params: { chatid: run.session_id } }"
          target="_blank"
          rel="noopener"
          >{{ k('openConversation') }} ↗</router-link
        >
      </div>
      <p v-if="run.history_context" class="history-meta">
        {{ k('lastModelHistory') }}
        <strong>{{ run.history_context.turns }}</strong> {{ k('historyTurns') }}
        <span v-if="run.history_context.compacted">
          · {{ k('historyCompacted') }}</span
        >
        <span
          v-if="run.history_context.turns && !run.history_context.ids_known"
        >
          · {{ k('historyIDsUnknown') }}</span
        >
      </p>
      <p v-else class="muted">{{ k('historyNotRecorded') }}</p>
      <p v-if="loading" role="status">{{ k('loading') }}</p>
      <div v-else-if="loadError" role="alert">
        <span>{{ loadError }}</span>
        <t-button variant="text" @click="load">{{ k('retry') }}</t-button>
      </div>
      <template v-else-if="conversation">
        <p
          v-if="conversation.incomplete || conversation.truncated"
          class="muted"
        >
          {{ k('conversationIncomplete') }}
        </p>
        <article
          v-for="(turn, i) in conversation.turns || []"
          :key="turn.run_id || i"
          :class="{ current: turn.current }"
        >
          <div class="turn-heading">
            <strong
              >{{ turn.current ? k('currentTurn') : k('previousTurn')
              }}<template v-if="turn.turn_number">
                · {{ k('turnNumber') }} {{ turn.turn_number }}</template
              ></strong
            >
            <span class="muted">{{ date(turn.started_at) }}</span>
            <t-button
              v-if="!turn.current && turn.run_id"
              variant="text"
              theme="primary"
              @click="emit('open', turn.run_id)"
              >{{ k('viewTurnDiagnosis') }}</t-button
            >
          </div>
          <template v-if="turn.status === 'available'">
            <p class="question">{{ turn.question }}</p>
            <details v-if="turn.answer" class="turn-answer">
              <summary>{{ k('expandAnswer') }}</summary>
              <AnswerContent :content="turn.answer" />
            </details>
          </template>
          <p v-else class="muted">{{ k('conversationTurnUnavailable') }}</p>
        </article>
      </template>
    </div>
  </details>
</template>
<script setup lang="ts">
import { ref, watch, onScopeDispose } from 'vue'
import {
  getOpsConversation,
  opsErrorCode,
  type OpsConversation,
  type OpsRun,
} from '@/api/knowledge-ops'
import { useAuthStore } from '@/stores/auth'
import { useOpsText } from './locales'
import AnswerContent from './AnswerContent.vue'
const props = defineProps<{ run: OpsRun }>()
const emit = defineEmits<{ open: [id: string] }>()
const { k, label, date } = useOpsText()
const auth = useAuthStore()
const conversation = ref<OpsConversation>()
const loading = ref(false)
const loadError = ref('')
const expanded = ref(false)
let request = 0
async function load() {
  const token = ++request
  const id = props.run.id
  loading.value = true
  loadError.value = ''
  conversation.value = undefined
  try {
    const value = await getOpsConversation(id)
    if (token === request) conversation.value = value
  } catch (error) {
    if (token === request) loadError.value = label(opsErrorCode(error))
  } finally {
    if (token === request) loading.value = false
  }
}
function onToggle(event: Event) {
  expanded.value = (event.target as HTMLDetailsElement).open
  if (expanded.value) void load()
  else {
    request++
    conversation.value = undefined
    loading.value = false
  }
}
watch(
  () => [
    props.run.id,
    props.run.updated_at,
    auth.effectiveTenantId,
    auth.currentUserId,
  ],
  () => {
    request++
    conversation.value = undefined
    loadError.value = ''
    loading.value = false
    if (expanded.value) void load()
  },
  { flush: 'sync' },
)
onScopeDispose(() => {
  request++
})
</script>
<style scoped>
.conversation-panel {
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-xl);
  background: var(--td-bg-color-container);
}
summary {
  cursor: pointer;
  padding: var(--app-space-4) var(--app-space-5);
  font-size: var(--app-text-md);
}
summary span {
  margin-left: var(--app-space-4);
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
}
.conversation-body {
  padding: 0 var(--app-space-5) var(--app-space-5);
}
.context-heading,
.turn-heading {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--app-space-3);
}
.context-heading {
  justify-content: space-between;
}
a {
  color: var(--td-brand-color);
  font-size: var(--app-text-sm);
}
.muted {
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
  line-height: 1.7;
}
.history-meta {
  font-size: var(--app-text-sm);
}
article {
  padding: var(--app-space-4);
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-md);
  margin-top: var(--app-space-3);
}
article.current {
  border-color: var(--td-brand-color);
  background: var(--td-brand-color-light);
}
.turn-heading {
  font-size: var(--app-text-sm);
}
.question {
  font-size: var(--app-text-md);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.turn-answer summary {
  padding: var(--app-space-2) 0;
  font-size: var(--app-text-sm);
  color: var(--td-text-color-secondary);
}
</style>
