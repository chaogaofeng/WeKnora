<template>
  <div class="table-scroll">
    <t-table row-key="id" :data="runs" :columns="columns" hover size="medium">
      <template #question="{ row: run }">
        <div class="question-cell">
          <div class="question-heading">
            <span class="question-text" :title="run.question">{{ run.question }}</span>
            <QueryRewriteBadge :run="run" @open="emit('open', $event)" />
          </div>
          <ConversationMeta :run="run" @open="emit('open', $event)" />
        </div>
      </template>
      <template #type="{ row: run }">
        <div>
          {{ label(run.qa_type) }}
          <small>
            {{ label(run.execution_mode) }} · {{ run.channel || k('unknown') }}
          </small>
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
          <StatusBadge :value="run.answer_outcome || run.review_state" />
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
import ConversationMeta from './ConversationMeta.vue'
import QueryRewriteBadge from './QueryRewriteBadge.vue'
import { useOpsText } from './locales'
const props = defineProps<{ runs: OpsRun[]; showSourceVersions?: boolean }>()
const emit = defineEmits<{ open: [id: string, analysis?: string] }>()
const { k, label, date } = useOpsText()
const columns = computed(() => [
  { colKey: 'question', title: k('question'), width: 320 },
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
  display: flex;
  flex-direction: column;
  gap: var(--app-space-2);
  min-width: 0;
  overflow-wrap: anywhere;
}
.question-heading {
  display: flex;
  align-items: flex-start;
  gap: var(--app-space-2);
  min-width: 0;
}
.question-text {
  min-width: 0;
  font-weight: 500;
  line-height: 1.6;
}
.source-followup {
  color: var(--td-warning-color-6);
}
.route-reason,
.question-text {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.route-reason { -webkit-line-clamp: 3; }
.route-reason {
  -webkit-line-clamp: 2;
}
</style>
