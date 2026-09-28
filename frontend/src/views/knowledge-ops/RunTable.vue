<template>
  <div class="table-scroll">
    <t-table row-key="id" :data="runs" :columns="columns" hover size="medium">
      <template #question="{ row: run }">
        <div class="question-cell">
          <span>{{ run.question }}</span>
          <small>{{ run.execution_mode || k('unknown') }}</small>
        </div>
      </template>
      <template #type="{ row: run }">
        <div>
          {{ label(run.qa_type) }}
          <small>{{ run.channel || k('unknown') }}</small>
        </div>
      </template>
      <template #capture="{ row: run }">
        <div><StatusBadge :value="run.capture_status" /></div>
      </template>
      <template #verdict="{ row: run }">
        <div>
          <StatusBadge :value="run.analysis?.report.verdict" verdict />
        </div>
      </template>
      <template #route="{ row: run }">
        <div>{{ label(run.analysis?.route) }}</div>
      </template>
      <template #diagnosis="{ row: run }">
        <div>
          <StatusBadge :value="run.analysis?.status || 'pending'" />
        </div>
      </template>
      <template #review="{ row: run }">
        <div>{{ label(run.review_state) }}</div>
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
import { useOpsText } from './locales'
defineProps<{ runs: OpsRun[] }>()
const emit = defineEmits<{ open: [id: string, analysis?: string] }>()
const { k, label, date } = useOpsText()
const columns = computed(() => [
  { colKey: 'question', title: k('question'), width: 260 },
  { colKey: 'type', title: `${k('qa_type')} / ${k('channel')}`, width: 170 },
  { colKey: 'capture', title: k('capture_status'), width: 120 },
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
.question-cell > span {
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
</style>
