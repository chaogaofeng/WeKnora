<template>
  <div class="capture-coverage">
    <t-tooltip :content="contentHint">
      <StatusBadge
        :value="coverage?.content_status || 'unknown'"
        :text="noSourceProvided ? k('content_not_provided') : k(`content_${coverage?.content_status || 'unknown'}`)"
      />
    </t-tooltip>
    <span v-if="noSourceProvided" class="provenance">{{ k('queryRetrievalSkipped') }}</span>
    <t-tooltip
      v-if="
        showSourceVersions !== false &&
        coverage?.content_status !== 'not_required'
      "
      :content="provenanceHint"
    >
      <span
        class="provenance"
        :class="{ pending: coverage?.provenance_status === 'pending' }"
      >
        {{ k(`provenance_${coverage?.provenance_status || 'unknown'}`) }}
      </span>
    </t-tooltip>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import type { OpsCaptureCoverage } from '@/api/knowledge-ops'
import StatusBadge from './StatusBadge.vue'
import { useOpsText } from './locales'
const props = defineProps<{
  coverage?: OpsCaptureCoverage
  showSourceVersions?: boolean
}>()
const { k, label } = useOpsText()
const noSourceProvided = computed(() =>
  props.coverage?.content_status === 'missing' && props.coverage.content_reason === 'not_provided',
)
const contentHint = computed(() =>
  noSourceProvided.value
    ? k('contentNotProvidedNotice')
    : props.coverage?.content_status === 'not_required'
      ? k('nonKnowledgeNotice')
      : props.coverage?.content_gaps?.length
        ? props.coverage.content_gaps.map(label).join('；')
        : k('contentCoverageNotice'),
)
const provenanceHint = computed(() =>
  props.coverage?.provenance_gaps?.length
    ? props.coverage.provenance_gaps.map(label).join('；')
    : k('provenanceNotice'),
)
</script>
<style scoped>
.capture-coverage {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: var(--app-space-1);
}
.provenance {
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
  line-height: 1.5;
  overflow-wrap: anywhere;
}
.provenance.pending {
  color: var(--td-warning-color-6);
}
</style>
