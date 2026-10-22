<template>
  <div class="table-scroll">
    <t-table
      row-key="id"
      :data="reviews"
      :columns="columns"
      hover
      size="medium"
    >
      <template #question="{ row: review }">
        <div class="question-cell">
          {{ review.question }}
          <small>
            v{{ review.analysis_version }} · {{ review.analysis_id }}
          </small>
        </div>
      </template>
      <template #machine="{ row: review }">
        <div>
          <template v-if="review.target === 'answer'">
            {{ labelVerdict(review.machine_verdict) }}
          </template>
          <template v-else>
            <p v-for="(cause, i) in review.machine_causes || []" :key="i">
              {{ label(cause.category) }} · {{ label(cause.status) }}
              <small>{{ cause.summary }}</small>
            </p>
            <span v-if="!review.machine_causes?.length">
              {{ k('noCause') }}
            </span>
          </template>
        </div>
      </template>
      <template #target="{ row: review }">
        <div>
          {{ k(review.target === 'answer' ? 'answerTarget' : 'causeTarget') }}
          <small>{{ review.analysis_id }}</small>
        </div>
      </template>
      <template #action="{ row: review }">
        <div><StatusBadge :value="review.action" /></div>
      </template>
      <template #verdict="{ row: review }">
        <div>
          {{
            review.target === 'answer'
              ? labelVerdict(review.verdict)
              : label(review.verdict)
          }}
          <small v-if="review.cause_category">
            {{ label(review.cause_category) }}
          </small>
        </div>
      </template>
      <template #reason="{ row: review }">
        <div class="reason-cell">{{ review.reason || '—' }}</div>
      </template>
      <template #reviewer="{ row: review }">
        <div>{{ review.reviewer_id }}</div>
      </template>
      <template #time="{ row: review }">
        <div>{{ date(review.created_at) }}</div>
      </template>
      <template #operation="{ row: review }">
        <div>
          <t-button
            variant="text"
            theme="primary"
            class="link-button"
            @click="emit('open', review.run_id, review.analysis_id)"
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
import type { OpsReview } from '@/api/knowledge-ops'
import StatusBadge from './StatusBadge.vue'
import { useOpsText } from './locales'
defineProps<{ reviews: OpsReview[] }>()
const emit = defineEmits<{ open: [id: string, analysis?: string] }>()
const { k, label, labelVerdict, date } = useOpsText()
const columns = computed(() => [
  { colKey: 'question', title: k('caseVersion'), width: 260 },
  { colKey: 'machine', title: k('machineOriginal'), width: 230 },
  { colKey: 'target', title: k('target'), width: 150 },
  { colKey: 'action', title: k('reviewAction'), width: 130 },
  {
    colKey: 'verdict',
    title: k('humanQualityDecision'),
    width: 170,
  },
  { colKey: 'reason', title: k('reviewReason'), width: 260 },
  { colKey: 'reviewer', title: k('reviewer'), width: 160 },
  { colKey: 'time', title: k('created'), width: 170 },
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
p {
  margin: 0 0 var(--app-space-2);
}
.question-cell {
  font-weight: 500;
  overflow-wrap: anywhere;
}
.question-cell small {
  font-weight: 400;
}
.reason-cell {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
