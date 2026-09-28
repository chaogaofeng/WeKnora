<template>
  <div class="insights">
    <section class="coverage">
      <h2>{{ k('coverageMatrix') }}</h2>
      <t-table
        row-key="mode"
        :data="coverageRows"
        :columns="coverageColumns"
        size="small"
      >
        <template #mode="{ row }">{{ label(row.mode) }}</template>
        <template #empty><EmptyState :title="k('none')" compact /></template>
      </t-table>
    </section>
    <section>
      <h2>{{ k('sourceStats') }}</h2>
      <p
        v-for="status in [
          'consistent',
          'inconsistent',
          'unresolved',
          'not_checked',
        ]"
        :key="status"
        class="stat"
      >
        <span>{{ label(status) }}</span>
        <strong>{{ stats.source_checks?.[status] || 0 }}</strong>
      </p>
    </section>
    <section>
      <h2>{{ k('causeStats') }}</h2>
      <p v-if="!Object.keys(stats.causes || {}).length" class="muted">
        {{ k('noCause') }}
      </p>
      <p
        v-for="(count, category) in stats.causes || {}"
        :key="category"
        class="stat"
      >
        <span>{{ label(String(category)) }}</span>
        <strong>{{ count }}</strong>
      </p>
    </section>
    <section>
      <h2>{{ k('sampleStats') }}</h2>
      <p class="stat">
        <span>{{ k('sampleTotal') }}</span>
        <strong>{{ stats.sample_total || 0 }}</strong>
      </p>
      <p class="stat">
        <span>{{ k('sampleReviewed') }}</span>
        <strong>{{ stats.sample_reviewed || 0 }}</strong>
      </p>
      <p class="stat">
        <span>{{ k('sampleCorrected') }}</span>
        <strong>{{ stats.sample_corrected || 0 }}</strong>
      </p>
      <p class="muted">{{ k('sampleNotice') }}</p>
    </section>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import EmptyState from '@/components/EmptyState.vue'
import type { OpsOverview } from '@/api/knowledge-ops'
import { useOpsText } from './locales'
const props = defineProps<{ stats: OpsOverview }>()
const { k, label } = useOpsText()
const coverageRows = computed(() =>
  Object.entries(props.stats.coverage_by_mode || {}).map(([mode, row]) => ({
    mode,
    ...row,
  })),
)
const coverageColumns = computed(() => [
  { colKey: 'mode', title: k('qa_type'), width: 150 },
  ...['total', 'complete', 'partial', 'missing'].map((key) => ({
    colKey: key,
    title: k(key),
    width: 70,
  })),
])
</script>
<style scoped>
.insights {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--app-space-4);
  margin: var(--app-space-5) 0;
}
section {
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-xl);
  padding: var(--app-space-5);
  overflow: auto;
}
h2 {
  font-size: var(--app-text-lg);
  margin: 0 0 var(--app-space-5);
  font-weight: 600;
}
.stat {
  display: flex;
  justify-content: space-between;
  margin: var(--app-space-4) 0;
  font-size: var(--app-text-md);
}
.muted {
  font-size: var(--app-text-sm);
  color: var(--td-text-color-secondary);
  line-height: 1.7;
}
@media (max-width: 1000px) {
  .insights {
    grid-template-columns: 1fr;
  }
}
.stat strong {
  font-weight: 600;
}
</style>
