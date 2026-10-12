<template>
  <section class="queue-panel">
    <header>
      <div>
        <h2>{{ k('queue') }}</h2>
        <p>{{ k('queueIntro') }}</p>
      </div>
      <t-button
        v-if="selectedGroup"
        variant="outline"
        @click="emit('group', '')"
      >
        {{ k('allGroups') }}
      </t-button>
      <t-checkbox v-else :checked="grouped" @change="emit('toggle')">
        {{ k('groupView') }}
      </t-checkbox>
    </header>
    <p v-if="(grouped || selectedGroup) && rows.length" class="notice">
      {{ k('groupNotice') }}
    </p>
    <p v-if="selectedGroup && rows.length" class="muted">
      {{ k('groupSelected') }} · {{ total ?? rows.length }}
      {{ k('queueRunUnit') }}
    </p>
    <EmptyState
      v-if="!rows.length"
      :title="k(emptyState)"
      :description="k(`${emptyState}Hint`)"
      icon="search"
    />
    <article v-for="row in rows" :key="row.run.id" class="queue-case">
      <div class="case-heading">
        <h3>{{ row.run.question }}</h3>
        <StatusBadge
          :value="row.risk"
          :text="`${k('risk')} · ${label(row.risk)}`"
        />
      </div>
      <div class="case-metadata muted">
        <span>{{ label(row.run.qa_type) }} · {{ row.run.channel }}</span>
        <span>{{ label(row.run.review_state) }}</span>
        <StatusBadge
          v-if="row.validDiagnosis"
          :value="row.run.analysis?.report.verdict"
          :text="`${k('machineOriginal')} · ${labelVerdict(row.run.analysis?.report.verdict)}`"
        />
        <CaptureCoverage
          :coverage="row.run.coverage"
          :show-source-versions="showSourceVersions"
        />
        <small>
          {{ date(row.run.analysis?.created_at || row.run.started_at) }}
        </small>
      </div>
      <p v-if="row.failed" class="failure">
        <template v-if="!row.validDiagnosis">{{ k('noValidVerdict') }} · </template>
        {{ k('technicalIssue') }}
      </p>
      <div class="current-work">
        <div class="work-heading">
          <h4>{{ k('currentPending') }}</h4>
          <span
            v-if="row.run.pending_review_targets?.length"
            class="remaining-reviews"
          >
            {{ k('remainingReviews') }}:
            {{ row.run.pending_review_targets.map((target) => k(`${target}Target`)).join('、') }}
          </span>
        </div>
        <ul v-if="row.pendingItems.length" class="pending-items">
          <li
            v-for="(item, i) in row.pendingItems"
            :key="item.review_id || `${item.route}-${i}`"
          >
            <div class="pending-heading">
              <StatusBadge :value="item.route" />
              <strong v-if="item.review_id">
                {{ k('pendingEvidenceRequests') }}
              </strong>
              <span v-if="item.target">
                {{ k(`${item.target}Target`) }}
                <template v-if="item.cause_category">
                  · {{ label(item.cause_category) }}
                </template>
              </span>
            </div>
            <details v-if="item.reason.length > 220" class="long-reason">
              <summary>
                <span class="reason-preview">
                  {{ item.reason.slice(0, 140) }}…
                </span>
                <span class="expand-label">{{ k('showFullReason') }}</span>
                <span class="collapse-label">{{ k('hideFullReason') }}</span>
              </summary>
              <p class="pending-reason">{{ item.reason }}</p>
            </details>
            <p v-else-if="item.reason" class="pending-reason">
              {{ item.reason }}
            </p>
            <p
              v-if="item.reviewer_id || item.created_at"
              class="pending-author muted"
            >
              <span v-if="item.reviewer_id">
                {{ k('reviewer') }}: {{ item.reviewer_id }}
              </span>
              <span v-if="item.created_at">{{ date(item.created_at) }}</span>
            </p>
          </li>
        </ul>
        <p v-else-if="!row.run.pending_review_targets?.length" class="muted">
          {{ k('pendingDetailsUnavailable') }}
        </p>
      </div>
      <footer>
        <details v-if="row.run.analysis" class="diagnosis">
          <summary>
            {{ k('originalDiagnosis') }}
            <span v-if="row.candidates.length">
              · {{ k('causes') }} {{ row.candidates.length }}
            </span>
            <span v-if="row.run.analysis.report.review_questions?.length">
              · {{ k('reviewQuestions') }}
              {{ row.run.analysis.report.review_questions.length }}
            </span>
            <span v-if="row.run.analysis.report.unknowns?.length">
              · {{ k('unknowns') }} {{ row.run.analysis.report.unknowns.length }}
            </span>
          </summary>
          <div class="diagnosis-content">
            <p v-if="row.run.analysis.error" class="failure">
              {{ label(row.run.analysis.error) }}
            </p>
            <p v-if="row.validDiagnosis">
              <strong>{{ k('verdict') }}:</strong>
              {{ labelVerdict(row.run.analysis.report.verdict) }}
              <span class="diagnosis-risk">
                {{ k('originalRisk') }}: {{ label(row.run.analysis.report.risk) }}
              </span>
            </p>
            <p v-else>{{ k('noValidVerdict') }}</p>
            <p v-if="row.validDiagnosis">
              <strong>{{ k('sourceCheck') }}:</strong>
              {{ labelSourceCheck(row.sourceCheck) }}
            </p>
            <p>
              <strong>{{ k('originalRoute') }}:</strong>
              {{ label(row.run.analysis.route) }}
              <template v-if="row.run.analysis.route_reason">
                — {{ row.run.analysis.route_reason }}
              </template>
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
            <ul
              v-if="row.run.analysis.report.review_questions?.length || row.run.analysis.report.unknowns?.length"
            >
              <li
                v-for="(question, i) in row.run.analysis.report.review_questions || []"
                :key="`q${i}`"
              >
                {{ question }}
              </li>
              <li
                v-for="(unknown, i) in row.run.analysis.report.unknowns || []"
                :key="`u${i}`"
              >
                {{ k('unknowns') }}: {{ unknown }}
              </li>
            </ul>
          </div>
        </details>
        <span v-else class="muted">{{ k('noAnalysisHint') }}</span>
        <div class="case-actions">
          <span v-if="row.count > 1" class="muted">
            {{ row.count }} {{ k('queueRunUnit') }}
          </span>
          <t-button
            v-if="grouped && row.count > 1"
            variant="outline"
            @click="emit('group', row.key)"
          >
            {{ k('viewMembers') }}
          </t-button>
          <t-button v-else theme="primary" @click="emit('open', row.run.id)">
            {{ k('detail') }}
          </t-button>
        </div>
      </footer>
    </article>
  </section>
</template>
<script setup lang="ts">
import EmptyState from '@/components/EmptyState.vue'
import StatusBadge from './StatusBadge.vue'
import CaptureCoverage from './CaptureCoverage.vue'
import { computed } from 'vue'
import type { OpsRun, OpsGroup, OpsPendingItem } from '@/api/knowledge-ops'
import { useOpsText } from './locales'
const props = defineProps<{
  runs: OpsRun[]
  groups: OpsGroup[]
  grouped: boolean
  selectedGroup: string
  filtered?: boolean
  total?: number
  showSourceVersions?: boolean
}>()
const emit = defineEmits<{
  open: [id: string]
  group: [key: string]
  toggle: []
}>()
const { k, label, labelVerdict, labelSourceCheck, date } = useOpsText()
const emptyState = computed(() => {
  if (props.total && props.total > 0) return 'queuePageEmpty'
  if (props.filtered) return 'queueFilteredEmpty'
  if (props.selectedGroup) return 'queueGroupEmpty'
  return 'queueEmpty'
})
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
    risk: row.run.effective_risk || row.run.analysis?.report.risk,
    sourceCheck:
      row.run.source_check_display || row.run.analysis?.report.source_check,
    failed:
      row.run.analysis?.status === 'failed' ||
      ['failed', 'cancelled'].includes(row.run.status),
    validDiagnosis: row.run.analysis?.status === 'succeeded',
    pendingItems:
      row.run.pending_items ??
      (row.run.source_followup_pending
        ? [{
            route: 'source_verification',
            reason: k('sourceFollowupIndependent'),
          } as OpsPendingItem]
        : []),
    candidates:
      row.run.analysis?.cause_assessment?.candidates ??
      row.run.analysis?.report.causes ?? [],
    unlinked: row.run.analysis?.cause_assessment?.unlinked ?? [],
  }))
})
</script>
<style scoped>
header,
.case-heading,
.case-metadata,
.work-heading,
.pending-heading,
.pending-author,
.case-actions {
  display: flex;
  align-items: center;
  gap: var(--app-space-3);
  flex-wrap: wrap;
}
header,
.case-heading,
.work-heading {
  justify-content: space-between;
}
h2 {
  font-size: var(--app-text-2xl);
  margin: 0;
}
h3 {
  flex: 1;
  min-width: 0;
  font-size: var(--app-text-xl);
  line-height: 1.5;
  margin: 0;
  overflow-wrap: anywhere;
}
h4 {
  font-size: var(--app-text-md);
  margin: 0;
}
header p,
.muted,
small {
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-md);
}
header label,
.case-actions {
  white-space: nowrap;
}
.notice {
  padding: var(--app-space-3);
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-md);
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-md);
}
.queue-case {
  padding: var(--app-space-5);
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-xl);
  background: var(--td-bg-color-container);
  margin: var(--app-space-4) 0;
  overflow-wrap: anywhere;
}
.case-metadata {
  gap: var(--app-space-2) var(--app-space-4);
  margin-top: var(--app-space-2);
  font-size: var(--app-text-sm);
}
.case-metadata small {
  margin-left: auto;
  font-size: var(--app-text-sm);
}
.case-metadata :deep(.capture-coverage) {
  flex-direction: row;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--app-space-2);
}
.current-work {
  margin: var(--app-space-4) 0;
}
.remaining-reviews {
  font-size: var(--app-text-md);
  color: var(--td-text-color-secondary);
}
.pending-items {
  display: grid;
  gap: var(--app-space-3);
  list-style: none;
  padding: 0;
  margin: var(--app-space-3) 0 0;
  font-size: var(--app-text-md);
}
.pending-heading {
  justify-content: flex-start;
  gap: var(--app-space-2);
}
.pending-reason {
  margin: var(--app-space-1) 0 0;
  line-height: 1.7;
  white-space: pre-wrap;
}
.long-reason {
  margin-top: var(--app-space-1);
  line-height: 1.7;
}
.long-reason summary {
  cursor: pointer;
}
.long-reason .expand-label,
.long-reason .collapse-label {
  color: var(--td-brand-color);
  white-space: nowrap;
}
.long-reason .collapse-label,
.long-reason[open] .expand-label,
.long-reason[open] .reason-preview {
  display: none;
}
.long-reason[open] .collapse-label {
  display: inline;
}
.pending-author {
  margin: var(--app-space-1) 0 0;
  font-size: var(--app-text-sm);
}
footer {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: start;
  gap: var(--app-space-3);
  border-top: 1px solid var(--td-component-stroke);
  padding-top: var(--app-space-3);
}
.diagnosis {
  min-width: 0;
  font-size: var(--app-text-md);
}
.diagnosis summary {
  width: fit-content;
  max-width: 100%;
  padding: var(--app-space-2) 0;
  color: var(--td-text-color-secondary);
  cursor: pointer;
  border-radius: var(--app-radius-sm);
}
.diagnosis summary:hover {
  color: var(--td-brand-color);
}
.diagnosis summary:focus-visible,
.long-reason summary:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: var(--app-space-1);
}
.diagnosis-content {
  padding: var(--app-space-3);
  margin-top: var(--app-space-2);
  background: var(--td-bg-color-secondarycontainer);
  border-radius: var(--app-radius-md);
  line-height: 1.7;
}
.diagnosis-content p {
  margin: var(--app-space-2) 0;
}
.diagnosis-content ul {
  padding-left: var(--app-space-5);
  margin-bottom: 0;
}
.diagnosis-risk {
  margin-left: var(--app-space-3);
}
.unlinked-cause-notice {
  color: var(--td-warning-color-6);
}
.failure {
  color: var(--td-error-color);
  font-size: var(--app-text-md);
}
.queue-panel > header {
  margin-top: var(--app-space-6);
}
.queue-panel > header p {
  max-width: 820px;
  line-height: 1.8;
  font-size: var(--app-text-sm);
}
@media (max-width: 600px) {
  footer {
    grid-template-columns: minmax(0, 1fr);
  }
  .case-actions {
    justify-content: flex-end;
  }
  .case-metadata small {
    margin-left: 0;
  }
}
</style>
