<template>
  <div class="table-scroll">
    <t-table row-key="id" :data="runs" :columns="columns" hover size="medium">
      <template #question="{ row: run }">
        <div class="question-cell">
          <span>{{ run.question }}</span>
          <small>{{ run.execution_mode || k('unknown') }}</small>
          <div
            v-if="run.conversation || run.session_id"
            class="conversation-meta"
          >
            <button
              v-if="run.conversation"
              class="conversation-link"
              type="button"
              @click="emit('open', run.id)"
            >
              {{
                run.conversation.turn_known
                  ? `${k(run.conversation.turn_number > 1 ? 'multiTurn' : 'firstTurn')} · ${k('turnNumber')} ${run.conversation.turn_number}`
                  : k(
                      run.conversation.has_earlier
                        ? 'laterTurnUnknown'
                        : 'turnUnknown',
                    )
              }}
              · {{ k('viewConversation') }}
            </button>
            <span v-if="run.session_id" class="session-id">
              {{ k('sessionIdLabel') }}: <span>{{ run.session_id }}</span>
            </span>
          </div>
        </div>
      </template>
      <template #type="{ row: run }">
        <div>
          {{ label(run.qa_type) }}
          <small>{{ run.channel || k('unknown') }}</small>
        </div>
      </template>
      <template #capture="{ row: run }">
        <CaptureCoverage
          :coverage="run.coverage"
          :show-source-versions="showSourceVersions"
        />
      </template>
      <template #verdict="{ row: run }">
        <div>
          <t-tooltip :content="k('supportScopeNotice')">
            <StatusBadge :value="run.analysis?.report.verdict" verdict />
          </t-tooltip>
          <small v-if="run.analysis?.report.verdict === 'supported'">
            {{ k('capturedSupport') }}
          </small>
          <small v-if="run.analysis?.report.verdict_adjustment">
            {{ k('systemAdjusted') }}
          </small>
        </div>
      </template>
      <template #route="{ row: run }">
        <div>
          {{ label(run.analysis?.route) }}
          <t-tooltip
            v-if="run.analysis?.route_reason"
            :content="run.analysis.route_reason"
          >
            <small class="route-reason">{{ run.analysis.route_reason }}</small>
          </t-tooltip>
        </div>
      </template>
      <template #diagnosis="{ row: run }">
        <div>
          <StatusBadge :value="run.analysis?.status || 'pending'" />
        </div>
      </template>
      <template #review="{ row: run }">
        <div class="review-state-cell">
          <StatusBadge :value="run.review_state" />
          <small
            v-if="showSourceVersions !== false && run.source_followup_pending"
            class="source-followup"
          >
            <t-tooltip :content="k('sourceFollowupIndependent')">
              <span>{{ label('source_verification') }}</span>
            </t-tooltip>
          </small>
        </div>
      </template>
      <template #time="{ row: run }">
        <div>{{ date(run.started_at) }}</div>
      </template>
      <template #operation="{ row: run }">
        <div>
          <t-button
            variant="text"
            theme="primary"
            class="link-button"
            @click="emit('open', run.id)"
          >
            {{ k('detail') }}
          </t-button>
        </div>
      </template>
    </t-table>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import type { OpsRun } from '@/api/knowledge-ops'
import StatusBadge from './StatusBadge.vue'
import CaptureCoverage from './CaptureCoverage.vue'
import { useOpsText } from './locales'
const props = defineProps<{ runs: OpsRun[]; showSourceVersions?: boolean }>()
const emit = defineEmits<{ open: [id: string, analysis?: string] }>()
const { k, label, date } = useOpsText()
const columns = computed(() => [
  { colKey: 'question', title: k('question'), width: 260 },
  { colKey: 'type', title: `${k('qa_type')} / ${k('channel')}`, width: 170 },
  {
    colKey: 'capture',
    title: k(
      props.showSourceVersions === false ? 'content_status' : 'captureCoverage',
    ),
    width: 160,
  },
  { colKey: 'verdict', title: k('verdict'), width: 150 },
  { colKey: 'route', title: k('route'), width: 140 },
  { colKey: 'diagnosis', title: k('diagnosis'), width: 120 },
  { colKey: 'review', title: k('reviewState'), width: 120 },
  { colKey: 'time', title: k('time'), width: 170 },
  { colKey: 'operation', title: k('operation'), width: 90 },
])
</script>
<style scoped>
.table-scroll {
  overflow: auto;
  min-width: 0;
}
.table-scroll :deep(.t-table) {
  font-size: var(--app-text-md);
}
.table-scroll :deep(td) {
  vertical-align: top;
}
small {
  display: block;
  color: var(--td-text-color-secondary);
  margin-top: var(--app-space-1);
  font-size: var(--app-text-sm);
  overflow-wrap: anywhere;
}
.question-cell {
  font-weight: 500;
  overflow-wrap: anywhere;
}
.question-cell small {
  font-weight: 400;
}
.source-followup {
  color: var(--td-warning-color-6);
}
.route-reason,
.question-cell > span {
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.route-reason {
  -webkit-line-clamp: 2;
}
.conversation-link {
  border: 0;
  padding: 0;
  background: transparent;
  color: var(--td-brand-color);
  font-size: var(--app-text-sm);
  cursor: pointer;
  text-align: left;
}
.conversation-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--app-space-1) var(--app-space-3);
}
.session-id {
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
  font-weight: 400;
  overflow-wrap: anywhere;
  user-select: text;
}
.conversation-link:hover {
  text-decoration: underline;
}
.conversation-link:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 3px;
}
</style>
