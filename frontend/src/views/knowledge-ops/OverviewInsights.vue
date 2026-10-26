<template>
  <div class="insights">
    <div class="analysis-scope">
      <span
        >{{ k('successfulDiagnosisBase') }}
        <strong>{{ successfulCount }}</strong></span
      >
      <span v-if="stats.non_knowledge_runs"
        >{{ k('nonKnowledgeRuns') }}
        <strong>{{ stats.non_knowledge_runs }}</strong></span
      >
      <span v-if="unassessedCount" class="unassessed">
        {{ k('unassessedDiagnoses') }} <strong>{{ unassessedCount }}</strong>
      </span>
    </div>
    <div class="diagnostic-grid">
      <section class="support-panel">
        <h2>{{ k('verdicts') }}</h2>
        <EmptyState
          v-if="!supportRows.length"
          :title="
            k(
              stats.non_knowledge_runs
                ? 'noKnowledgeAssessments'
                : 'noSuccessfulDiagnoses',
            )
          "
          compact
        />
        <div v-for="row in supportRows" :key="row.key" class="distribution">
          <span>{{ labelVerdict(row.key) }}</span>
          <div class="bar" aria-hidden="true">
            <i :class="row.key" :style="{ width: `${row.percent}%` }" />
          </div>
          <strong>{{ row.count }}</strong>
          <small>{{ row.percent.toFixed(1) }}%</small>
        </div>
        <p class="muted">{{ k('qualityCountsNotice') }}</p>
      </section>
      <section class="source-panel">
        <h2>{{ k('sourceStats') }}</h2>
        <EmptyState
          v-if="!sourceRows.length"
          :title="
            k(
              stats.non_knowledge_runs
                ? 'noKnowledgeAssessments'
                : 'noSuccessfulDiagnoses',
            )
          "
          compact
        />
        <p v-for="row in sourceRows" :key="row.key" class="stat">
          <span>{{ labelSourceCheck(row.key) }}</span>
          <strong>{{ row.count }}</strong>
        </p>
        <p class="muted">{{ k('sourceComparisonNotice') }}</p>
      </section>
    </div>

    <details class="capture-panel">
      <summary>
        <span class="capture-title">{{
          k(showSourceVersions === false ? 'content_status' : 'captureQuality')
        }}</span>
        <span class="capture-highlights">
          <span
            >{{ k('content_complete') }}
            <strong
              >{{ stats.content_coverage?.complete || 0 }} /
              {{ stats.content_coverage?.total || 0 }}</strong
            ></span
          >
          <span v-if="stats.content_coverage?.partial"
            >{{ k('content_partial') }}
            <strong>{{ stats.content_coverage.partial }}</strong></span
          >
          <span v-if="stats.content_coverage?.missing"
            >{{ k('content_missing_unknown') }}
            <strong>{{ stats.content_coverage.missing }}</strong></span
          >
          <span
            v-if="
              showSourceVersions !== false && stats.provenance_counts?.pending
            "
            >{{ k('provenance_pending') }}
            <strong>{{ stats.provenance_counts.pending }}</strong></span
          >
          <span
            v-if="
              showSourceVersions !== false && stats.provenance_counts?.unknown
            "
            >{{ k('provenance_unknown') }}
            <strong>{{ stats.provenance_counts.unknown }}</strong></span
          >
        </span>
      </summary>
      <div class="capture-detail">
        <div class="coverage-table">
          <h3>{{ k('coverageMatrix') }}</h3>
          <t-table
            row-key="mode"
            :data="coverageRows"
            :columns="coverageColumns"
            size="small"
          >
            <template #mode="{ row }">{{ label(row.mode) }}</template>
            <template #empty
              ><EmptyState :title="k('none')" compact
            /></template>
          </t-table>
          <p class="muted">{{ k('contentCoverageNotice') }}</p>
        </div>
        <div v-if="showSourceVersions !== false" class="provenance-summary">
          <h3>{{ k('provenanceStats') }}</h3>
          <p
            v-for="status in ['known', 'pending', 'unknown']"
            :key="status"
            class="stat"
          >
            <span>{{ k(`provenance_${status}`) }}</span>
            <strong>{{ stats.provenance_counts?.[status] || 0 }}</strong>
          </p>
          <p class="muted">{{ k('provenanceNotice') }}</p>
        </div>
      </div>
    </details>

    <details v-if="Object.values(stats.answer_outcomes || {}).some(value => value > 0)" class="capture-panel">
      <summary><span class="capture-title">{{ k('humanQualityStats') }}</span></summary>
      <div class="capture-detail">
        <div class="provenance-summary">
          <p v-for="outcome in ['usable', 'revision_required', 'needs_evidence', 'excluded']" :key="outcome" class="stat">
            <span>{{ label(outcome) }}</span><strong>{{ stats.answer_outcomes?.[outcome] || 0 }}</strong>
          </p>
          <p class="muted">{{ k('humanQualityStatsHint') }}</p>
        </div>
      </div>
    </details>

    <div v-if="hasCauses || stats.sample_total" class="supplement-grid">
      <section v-if="hasCauses" class="cause-panel">
        <h2>{{ k('causeStats') }}</h2>
        <div class="cause-list">
          <button
            v-for="{ count, category } in causeRows"
            :key="category"
            type="button"
            class="stat cause-link"
            @click="emit('cause', String(category))"
          >
            <span>{{ label(String(category)) }}</span>
            <strong>{{ count }}</strong>
            <span class="cause-link-hint">{{ k('viewRelatedRuns') }} →</span>
          </button>
        </div>
        <p class="muted">{{ k('causeCountsNotice') }}</p>
      </section>
      <section v-if="stats.sample_total" class="sample-panel">
        <h2>{{ k('sampleStats') }}</h2>
        <p class="stat">
          <span>{{ k('sampleTotal') }}</span
          ><strong>{{ stats.sample_total }}</strong>
        </p>
        <p class="stat">
          <span>{{ k('sampleReviewed') }}</span
          ><strong>{{ stats.sample_reviewed || 0 }}</strong>
        </p>
        <p class="stat">
          <span>{{ k('sampleRevisionRequired') }}</span
          ><strong>{{ stats.sample_revision_required || 0 }}</strong>
        </p>
        <p v-if="stats.sample_corrected" class="stat">
          <span>{{ k('sampleCorrected') }}</span
          ><strong>{{ stats.sample_corrected || 0 }}</strong>
        </p>
        <p class="muted">{{ k('sampleNotice') }}</p>
      </section>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import EmptyState from '@/components/EmptyState.vue'
import type { OpsOverview } from '@/api/knowledge-ops'
import { useOpsText } from './locales'
const props = defineProps<{
  stats: OpsOverview
  causeCategory?: string
  showSourceVersions?: boolean
}>()
const emit = defineEmits<{ cause: [category: string] }>()
const { k, label, labelVerdict, labelSourceCheck } = useOpsText()
const successfulCount = computed(
  () => props.stats.diagnosis_status?.succeeded || 0,
)
const unassessedCount = computed(() =>
  Math.max(0, props.stats.total - successfulCount.value),
)
const qualityCount = computed(() =>
  Math.max(0, successfulCount.value - (props.stats.non_knowledge_runs || 0)),
)
const supportRows = computed(() =>
  [
    'supported',
    'partial',
    'contradicted',
    'unsupported',
    'unverifiable',
    'not_applicable',
  ]
    .map((key) => ({
      key,
      count: props.stats.by_verdict?.[key] || 0,
      percent: qualityCount.value
        ? ((props.stats.by_verdict?.[key] || 0) / qualityCount.value) * 100
        : 0,
    }))
    .filter((row) => row.count > 0),
)
const sourceRows = computed(() =>
  [
    'consistent',
    'inconsistent',
    'unresolved',
    'not_checked',
    'not_applicable',
    'unknown',
  ]
    .map((key) => ({ key, count: props.stats.source_checks?.[key] || 0 }))
    .filter((row) => row.count > 0),
)
const causeRows = computed(() =>
  Object.entries(props.stats.causes || {})
    .filter(
      ([category, count]) =>
        count > 0 && (!props.causeCategory || category === props.causeCategory),
    )
    .map(([category, count]) => ({ category, count })),
)
const hasCauses = computed(() => causeRows.value.length > 0)
const coverageRows = computed(() =>
  Object.entries(props.stats.content_coverage_by_mode || {}).map(
    ([mode, row]) => ({ mode, ...row }),
  ),
)
const coverageColumns = computed(() => [
  { colKey: 'mode', title: k('qa_type'), width: 130 },
  ...['total', 'complete', 'partial', 'missing'].map((key) => ({
    colKey: key,
    title: k(
      key === 'total'
        ? 'capturedRuns'
        : key === 'missing'
          ? 'content_missing_unknown'
          : `content_${key}`,
    ),
    width: 90,
  })),
])
</script>
<style scoped>
.insights {
  display: grid;
  gap: var(--app-space-4);
  margin: var(--app-space-4) 0;
}
.analysis-scope {
  display: flex;
  flex-wrap: wrap;
  gap: var(--app-space-5);
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
}
.analysis-scope strong {
  margin-left: var(--app-space-1);
}
.unassessed {
  color: var(--td-warning-color-6);
}
.diagnostic-grid,
.supplement-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--app-space-4);
  align-items: start;
}
section,
.capture-panel {
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-xl);
  padding: var(--app-space-5);
  min-width: 0;
}
h2 {
  font-size: var(--app-text-lg);
  margin: 0 0 var(--app-space-4);
  font-weight: 600;
}
h3 {
  margin: 0 0 var(--app-space-3);
  font-size: var(--app-text-md);
  font-weight: 500;
}
.distribution {
  display: grid;
  grid-template-columns: minmax(72px, 1fr) minmax(40px, 1fr) 24px 50px;
  gap: var(--app-space-3);
  align-items: center;
  margin: var(--app-space-4) 0;
  font-size: var(--app-text-md);
}
.distribution strong,
.distribution small {
  text-align: right;
}
.distribution small {
  color: var(--td-text-color-secondary);
}
.bar {
  height: 5px;
  background: var(--td-bg-color-secondarycontainer);
  border-radius: var(--app-radius-xs);
  overflow: hidden;
}
.bar i {
  display: block;
  height: 100%;
  background: var(--td-text-color-placeholder);
}
.bar .supported {
  background: var(--td-success-color);
}
.bar .partial {
  background: var(--td-warning-color);
}
.bar .contradicted,
.bar .unsupported {
  background: var(--td-error-color);
}
.stat {
  display: flex;
  justify-content: space-between;
  gap: var(--app-space-4);
  margin: var(--app-space-4) 0;
  font-size: var(--app-text-md);
}
strong {
  font-weight: 600;
}
.muted {
  margin: var(--app-space-3) 0 0;
  font-size: var(--app-text-sm);
  color: var(--td-text-color-secondary);
  line-height: 1.7;
}
.capture-panel {
  padding: 0;
}
summary {
  cursor: pointer;
  padding: var(--app-space-4) var(--app-space-5);
  font-size: var(--app-text-md);
}
summary:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 2px;
}
.capture-title {
  font-weight: 500;
}
.capture-highlights {
  display: inline-flex;
  flex-wrap: wrap;
  gap: var(--app-space-3) var(--app-space-5);
  margin-left: var(--app-space-5);
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
}
.capture-highlights strong {
  margin-left: var(--app-space-1);
}
.capture-detail {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(0, 1fr);
  gap: var(--app-space-6);
  padding: var(--app-space-5);
  border-top: 1px solid var(--td-component-stroke);
}
.coverage-table {
  min-width: 0;
  overflow: auto;
}
.supplement-grid > :only-child {
  grid-column: 1 / -1;
}
.cause-list {
  display: flex;
  flex-wrap: wrap;
  gap: var(--app-space-3) var(--app-space-6);
}
.cause-list .stat {
  margin: 0;
  min-width: 140px;
}
.cause-link {
  appearance: none;
  border: 0;
  border-radius: var(--app-radius-sm);
  padding: var(--app-space-2) var(--app-space-3);
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-primary);
  font: inherit;
  cursor: pointer;
  align-items: center;
}
.cause-link:hover {
  background: var(--td-brand-color-light);
}
.cause-link:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 2px;
}
.cause-link-hint {
  color: var(--td-brand-color);
  font-size: var(--app-text-sm);
}
@media (max-width: 1000px) {
  .diagnostic-grid,
  .supplement-grid,
  .capture-detail {
    grid-template-columns: 1fr;
  }
  .capture-highlights {
    display: flex;
    margin: var(--app-space-3) 0 0;
  }
}
</style>
