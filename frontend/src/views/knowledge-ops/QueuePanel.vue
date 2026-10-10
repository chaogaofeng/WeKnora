<template>
  <section class="queue-panel">
    <header>
      <div>
        <h2>{{ k('queue') }}</h2>
        <p>{{ k('queueIntro') }}</p>
      </div>
      <t-button
        variant="outline"
        v-if="selectedGroup"
        @click="emit('group', '')"
      >
        {{ k('allGroups') }}
      </t-button>
      <t-checkbox v-else :checked="grouped" @change="emit('toggle')">
        {{ k('groupView') }}
      </t-checkbox>
    </header>
    <p class="notice">{{ k('groupNotice') }}</p>
    <p v-if="selectedGroup" class="muted">
      {{ k('groupSelected') }}: {{ selectedGroup }}
    </p>
    <EmptyState
      v-if="!rows.length"
      :title="k('empty')"
      :description="k('emptyHint')"
      icon="search"
    />
    <article v-for="row in rows" :key="row.run.id" class="queue-case">
      <div class="case-heading">
        <StatusBadge
          :value="row.run.analysis?.report.risk"
          :text="`${k('risk')} · ${label(row.run.analysis?.report.risk)}`"
        />
        <span>{{ label(row.run.analysis?.route) }}</span>
        <span
          v-if="
            row.run.analysis?.status === 'failed' ||
            ['failed', 'cancelled'].includes(row.run.status)
          "
          class="failure"
        >
          {{ k('technicalIssue') }}
        </span>
        <small>
          {{ date(row.run.analysis?.created_at || row.run.started_at) }}
        </small>
      </div>
      <div class="case-content">
        <div>
          <h3>{{ row.run.question }}</h3>
          <p class="muted">
            {{ label(row.run.qa_type) }} · {{ row.run.channel }} ·
            {{ label(row.run.review_state) }}
          </p>
          <CaptureCoverage :coverage="row.run.coverage" />
          <p>
            <strong>{{ k('verdict') }}:</strong>
            {{ labelVerdict(row.run.analysis?.report.verdict) }} ·
            {{ k('sourceCheck') }}:
            {{ label(row.run.analysis?.report.source_check) }}
          </p>
          <p v-if="row.count > 1">
            {{ k('relatedCount') }}:
            <strong>{{ row.count }}</strong>
          </p>
        </div>
        <div class="diagnosis">
          <h4>{{ k('diagnosisSummary') }}</h4>
          <p v-if="row.run.analysis?.error" class="failure">
            {{ label(row.run.analysis.error) }}
          </p>
          <p v-for="(cause, i) in row.candidates" :key="i">
            <strong>
              {{ label(cause.category) }} · {{ label(cause.status) }}
            </strong>
            — {{ cause.summary }}
          </p>
          <p v-if="row.unlinked.length" class="unlinked-cause-notice">
            {{ k('unlinkedCauseShort') }}
          </p>
          <p v-if="!row.candidates.length && !row.unlinked.length">
            {{ row.run.analysis?.route_reason || k('noAnalysisHint') }}
          </p>
          <ul>
            <li
              v-for="(question, i) in row.run.analysis?.report
                .review_questions || []"
              :key="`q${i}`"
            >
              {{ question }}
            </li>
            <li
              v-for="(unknown, i) in row.run.analysis?.report.unknowns || []"
              :key="`u${i}`"
            >
              {{ k('unknowns') }}: {{ unknown }}
            </li>
          </ul>
        </div>
      </div>
      <footer>
        <span class="muted">{{ row.run.analysis?.route_reason }}</span>
        <t-button
          variant="outline"
          v-if="grouped && row.count > 1"
          @click="emit('group', row.key)"
        >
          {{ k('viewMembers') }}
        </t-button>
        <t-button v-else theme="primary" @click="emit('open', row.run.id)">
          {{ k('detail') }}
        </t-button>
      </footer>
    </article>
  </section>
</template>
<script setup lang="ts">
import EmptyState from '@/components/EmptyState.vue'
import StatusBadge from './StatusBadge.vue'
import CaptureCoverage from './CaptureCoverage.vue'
import { computed } from 'vue'
import type { OpsRun, OpsGroup } from '@/api/knowledge-ops'
import { useOpsText } from './locales'
const props = defineProps<{
  runs: OpsRun[]
  groups: OpsGroup[]
  grouped: boolean
  selectedGroup: string
}>()
const emit = defineEmits<{
  open: [id: string]
  group: [key: string]
  toggle: []
}>()
const { k, label, labelVerdict, date } = useOpsText()
const rows = computed(() => {
  const items = props.grouped
    ? props.groups.map((g) => ({
        key: g.key,
        count: g.count,
        run: g.representative,
      }))
    : props.runs.map((run) => ({ key: run.id, count: 1, run }))
  return items.map((row) => ({
    ...row,
    candidates:
      row.run.analysis?.cause_assessment?.candidates ??
      row.run.analysis?.report.causes ??
      [],
    unlinked: row.run.analysis?.cause_assessment?.unlinked ?? [],
  }))
})
</script>
<style scoped>
header,
.case-heading,
footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--app-space-3);
  flex-wrap: wrap;
}
h2 {
  font-size: var(--app-text-2xl);
  margin: 0;
}
h3 {
  font-size: var(--app-text-xl);
  margin: 0 0 var(--app-space-2);
}
h4 {
  margin: 0 0 var(--app-space-2);
}
header p,
.muted,
small {
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-md);
}
header label {
  white-space: nowrap;
}
.notice {
  padding: var(--app-space-3);
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-md);
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-md);
}
.unlinked-cause-notice {
  color: var(--td-warning-color-6);
}
.queue-case {
  padding: var(--app-space-5);
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-xl);
  background: var(--td-bg-color-container);
  margin: var(--app-space-4) 0;
}
.case-heading {
  justify-content: flex-start;
  font-size: var(--app-text-md);
}
.case-heading small {
  margin-left: auto;
}
.case-content {
  display: grid;
  grid-template-columns: 1fr 1.2fr;
  gap: var(--app-space-6);
  margin: var(--app-space-4) 0;
}
.diagnosis {
  background: color-mix(
    in srgb,
    var(--td-bg-color-secondarycontainer) 60%,
    var(--td-bg-color-container)
  );
  border-radius: var(--app-radius-md);
  padding: var(--app-space-4) var(--app-space-4);
  font-size: var(--app-text-md);
}
.diagnosis p {
  margin: var(--app-space-2) 0;
}
.diagnosis ul {
  padding-left: var(--app-space-5);
}
.failure {
  color: var(--td-error-color);
}
@media (max-width: 900px) {
  .case-content {
    grid-template-columns: 1fr;
  }
  .diagnosis {
    border-left: 0;
    border-top: 1px solid var(--td-component-border);
    padding: var(--app-space-4) 0 0;
  }
}
footer {
  border-top: 1px solid var(--td-component-stroke);
  padding-top: var(--app-space-3);
}
.case-heading small {
  font-size: var(--app-text-sm);
}
.case-content > div:first-child {
  padding-top: var(--app-space-3);
}
.case-content p {
  line-height: 1.8;
}
.queue-panel > header {
  margin-top: var(--app-space-6);
}
.queue-panel > header p {
  max-width: 820px;
  line-height: 1.8;
  font-size: var(--app-text-sm);
}
</style>
