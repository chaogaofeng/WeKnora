<template>
  <main class="ops-workbench">
    <header class="page-header">
      <div>
        <div class="eyebrow">
          {{ k('workspaceLabel') }}
          <span>/</span>
          {{ k('opsReadOnly') }}
        </div>
        <h1>{{ k('title') }}</h1>
        <p>{{ k('subtitle') }}</p>
      </div>
      <div class="header-actions">
        <t-tag
          :theme="settings?.enabled ? 'success' : 'default'"
          variant="light"
        >
          {{
            settings
              ? k(settings.enabled ? 'enabled' : 'disabled')
              : k('unknown')
          }}
        </t-tag>
        <t-button variant="outline" @click="settingsVisible = true">
          {{ k('settings') }}
        </t-button>
        <t-button variant="outline" :loading="loading" @click="reload">
          {{ k('refresh') }}
        </t-button>
      </div>
    </header>
    <t-tabs
      class="tabs"
      :value="tab"
      :aria-label="k('title')"
      @change="(value: string | number) => changeTab(value as Tab)"
    >
      <t-tab-panel
        v-for="item in tabs"
        :key="item"
        :value="item"
        :label="k(item)"
      />
    </t-tabs>
    <FilterBar
      :values="tab === 'records' ? recordDraft : draftFilters"
      :applied="tab === 'records' ? recordFilters : filters"
      :records="tab === 'records'"
      :loading="loading"
      :show-source-versions="showSourceVersions"
      @source-versions="setShowSourceVersions"
      @change="changeFilter"
      @period="changePeriod"
      @apply="applyFilters"
      @reset="resetFilters"
      @remove="removeFilter"
    />
    <p v-if="filters.route === 'technical_followup'" class="scope-note">
      {{ k('technicalNotice') }}
    </p>
    <p v-if="filterError" class="error" role="alert">{{ filterError }}</p>
    <div class="scope-strip">
      <span>
        <span class="scope-dot" />
        {{ k('scopeShort') }}
      </span>
      <span v-if="settings?.capture_enabled_at">
        {{ k('captureSince') }} {{ date(settings.capture_enabled_at) }}
      </span>
    </div>
    <div v-if="loading" class="loading-state" role="status">
      <t-loading size="medium" />
      {{ k('loading') }}
    </div>
    <section v-else-if="error" class="panel" role="alert">
      <EmptyState
        :title="k('errorTitle')"
        :description="error"
        icon="error-circle"
      >
        <t-button @click="reload">{{ k('retry') }}</t-button>
      </EmptyState>
    </section>
    <template v-else>
      <template v-if="tab === 'overview' && overview">
        <div class="metrics">
          <article v-for="item in metrics" :key="item.key" class="metric">
            <div class="metric-label">
              <span>{{ k(item.label) }}</span>
              <span class="metric-marker" :class="item.key" />
            </div>
            <strong :class="item.key">{{ item.value.toLocaleString() }}</strong>
            <div class="metric-footnote">
              <small>{{ k(item.note) }}</small>
              <t-button
                v-if="item.key === 'pending'"
                class="pending-link"
                variant="text"
                theme="primary"
                size="small"
                @click="changeTab('queue')"
                >{{ k('viewPending') }}</t-button
              >
            </div>
          </article>
        </div>
        <OverviewInsights
          :stats="overview"
          :show-source-versions="showSourceVersions"
          :cause-category="filters.cause_category"
          @cause="filterCause"
        />
        <div class="summary">
          <span>
            {{ k('pendingTasks') }}
            <strong>{{ overview.pending_tasks }}</strong>
          </span>
          <span>
            {{ k('reviewCount') }}
            <strong>{{ overview.review_count }}</strong>
          </span>
        </div>
      </template>
      <section v-if="tab === 'queue' && overview" class="panel queue-counts">
        <h2>{{ k('queueStats') }}</h2>
        <p class="scope-note">{{ k('queueCountsHint') }}</p>
        <div class="queue-count-grid">
          <button
            v-for="value in [
              'human_review',
              'quality_sample',
              'needs_evidence',
              'source_verification',
              'technical_followup',
            ]"
            :key="value"
            :class="{ active: filters.route === value }"
            :aria-pressed="filters.route === value"
            @click="filterQueue(value)"
          >
            <span>{{ label(value) }}</span>
            <strong>{{ overview.queue_counts?.[value] || 0 }}</strong>
          </button>
        </div>
      </section>
      <QueuePanel
        v-if="tab === 'queue'"
        :show-source-versions="showSourceVersions"
        :runs="runs"
        :groups="groups"
        :grouped="grouped && !selectedGroup"
        :selectedGroup="selectedGroup"
        :filtered="hasQueueFilters"
        :total="total"
        @open="openRun"
        @group="selectGroup"
        @toggle="toggleGroups"
      />
      <section
        v-if="tab !== 'queue'"
        id="ops-run-list"
        tabindex="-1"
        class="panel list-panel"
        :aria-busy="refreshing"
      >
        <div class="section-header">
          <h2>{{ k(tab === 'overview' ? 'runs' : tab) }}</h2>
          <div class="list-actions">
            <t-button
              variant="outline"
              theme="default"
              size="small"
              :loading="refreshing"
              :disabled="loading"
              @click="refreshList"
            >
              <template #icon><RefreshIcon /></template>
              {{ k('refresh') }}
            </t-button>
            <span class="muted">{{ total }} {{ k('count') }}</span>
          </div>
        </div>
        <p class="scope-note">{{ k('autoNotice') }}</p>
        <EmptyState
          v-if="!total"
          :title="k('empty')"
          :description="k('emptyHint')"
          icon="search"
        />
        <RunTable
          v-else-if="tab !== 'records'"
          :runs="runs"
          :show-source-versions="showSourceVersions"
          @open="openRun"
        />
        <ReviewTable v-else :reviews="reviews" @open="openRun" />
      </section>
      <footer v-if="total" class="pagination">
        <t-pagination
          :current="page"
          :page-size="pageSize"
          :total="total"
          :total-content="
            tab === 'queue'
              ? `${total} ${k(grouped && !selectedGroup ? 'queueGroupUnit' : 'queueRunUnit')}`
              : true
          "
          :show-page-size="false"
          :show-jumper="false"
          size="small"
          @current-change="changePage"
        />
      </footer>
    </template>
    <SettingsDrawer
      :visible="settingsVisible"
      @close="settingsVisible = false"
      @saved="settingsSaved"
    />
    <DetailDrawer
      @open="openRun"
      :show-source-versions="showSourceVersions"
      :runID="activeRun"
      :analysisID="activeAnalysis"
      :enabled="!!settings?.enabled"
      @close="closeRun"
      @changed="reload"
    />
  </main>
</template>
<script setup lang="ts">
import { nextTick, computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import { RefreshIcon } from 'tdesign-icons-vue-next'
import { useAuthStore } from '@/stores/auth'
import { safeGetItem, safeSetItem } from '@/composables/preferenceStorage'
import {
  getOpsSettings,
  getOpsOverview,
  getOpsRuns,
  getOpsReviews,
  getOpsGroups,
  type OpsGroup,
  opsErrorCode,
  type OpsSettings,
  type OpsOverview,
  type OpsRun,
  type OpsReview,
  type OpsFilters,
} from '@/api/knowledge-ops'
import { buildFilters } from './state'
import { useOpsText } from './locales'
import EmptyState from '@/components/EmptyState.vue'
import RunTable from './RunTable.vue'
import ReviewTable from './ReviewTable.vue'
import FilterBar from './FilterBar.vue'
import QueuePanel from './QueuePanel.vue'
import OverviewInsights from './OverviewInsights.vue'
import SettingsDrawer from './SettingsDrawer.vue'
import DetailDrawer from './DetailDrawer.vue'
const { k, label, date } = useOpsText()
const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
// A local display preference, independent of API filters and diagnosis settings.
const sourceVersionPreferenceKey = computed(
  () =>
    `WeKnora_${auth.currentUserId || 'anon'}_knowledge_ops_show_source_versions`,
)
const showSourceVersions = ref(true)
watch(
  sourceVersionPreferenceKey,
  (key) => {
    showSourceVersions.value = safeGetItem(key) !== 'false'
  },
  { immediate: true },
)
function setShowSourceVersions(value: boolean) {
  showSourceVersions.value = value
  safeSetItem(sourceVersionPreferenceKey.value, String(value))
}
const tabs = ['overview', 'queue', 'records'] as const
type Tab = (typeof tabs)[number]
const tab = ref<Tab>(
  tabs.includes(route.query.tab as Tab) ? (route.query.tab as Tab) : 'overview',
)
const settings = ref<OpsSettings>()
const overview = ref<OpsOverview>()
const runs = ref<OpsRun[]>([])
const reviews = ref<OpsReview[]>([])
const loading = ref(false)
const refreshing = ref(false)
const error = ref('')
const settingsVisible = ref(false)
const filterError = ref('')
const page = ref(1)
const pageSize = 20
const total = ref(0)
const activeAnalysis = computed(() =>
  typeof route.query.analysis === 'string' ? route.query.analysis : '',
)
const groups = ref<OpsGroup[]>([])
const grouped = ref(true)
const selectedGroup = ref('')
const recordDraft = reactive({
  session_id: '',
  review_from: '',
  review_to: '',
  target: '',
  action: '',
  reviewer_id: '',
  review_search: '',
})
const recordFilters = ref<OpsFilters>({})
const activeRun = computed(() =>
  typeof route.query.run === 'string' ? route.query.run : '',
)
const blankFilters = () => ({
  session_id: '',
  from: '',
  to: '',
  qa_type: '',
  channel: '',
  content_status: '',
  provenance_status: '',
  route: '',
  verdict: '',
  search: '',
  risk: '',
  cause_category: '',
})
const draftFilters = reactive(blankFilters())
const filters = ref<OpsFilters>({})
const hasQueueFilters = computed(() =>
  Object.values(filters.value).some(
    (value) => value !== undefined && value !== '',
  ),
)
let token = 0
const metrics = computed(() => [
  {
    key: 'total',
    label: 'capturedRuns',
    note: 'countRunsNotice',
    value: overview.value?.total || 0,
  },
  {
    key: 'succeeded',
    label: 'completedDiagnosis',
    note: 'latestDiagnosisNotice',
    value: overview.value?.diagnosis_status?.succeeded || 0,
  },
  {
    key: 'failed',
    label: 'failedDiagnoses',
    note: 'diagnosisFailureNotice',
    value: overview.value?.diagnosis_status?.failed || 0,
  },
  {
    key: 'pending',
    label: 'pendingRuns',
    note: 'deduplicatedPendingNotice',
    value: overview.value?.pending_runs || 0,
  },
])
async function reload() {
  await loadData()
}
async function refreshList() {
  if (loading.value || refreshing.value) return
  await loadData(true)
}
async function loadData(background = false) {
  const current = ++token
  loading.value = !background
  refreshing.value = background
  error.value = ''
  const params = {
    ...(tab.value === 'records' ? recordFilters.value : filters.value),
    ...(selectedGroup.value && tab.value === 'queue'
      ? { group_key: selectedGroup.value }
      : {}),
    page: page.value,
    page_size: pageSize,
    ...(tab.value === 'queue' ? { view: 'queue' as const } : {}),
  }
  try {
    const [config, stats, result] = await Promise.all([
      getOpsSettings(),
      tab.value !== 'records'
        ? getOpsOverview(
            tab.value === 'queue'
              ? { ...filters.value, route: undefined }
              : filters.value,
          )
        : Promise.resolve(undefined),
      tab.value === 'records'
        ? getOpsReviews(params)
        : tab.value === 'queue' && grouped.value && !selectedGroup.value
          ? getOpsGroups(params)
          : getOpsRuns(params),
    ])
    if (current !== token) return
    const lastPage = Math.max(1, Math.ceil(result.total / pageSize))
    if (page.value > lastPage) {
      page.value = lastPage
      await loadData(background)
      return
    }
    settings.value = config
    overview.value = stats
    total.value = result.total
    if (tab.value === 'records') {
      reviews.value = result.items as OpsReview[]
      runs.value = []
    } else if (tab.value === 'queue' && grouped.value && !selectedGroup.value) {
      groups.value = result.items as OpsGroup[]
      runs.value = []
      reviews.value = []
    } else {
      runs.value = result.items as OpsRun[]
      groups.value = []
      reviews.value = []
    }
  } catch (failure) {
    if (current === token) {
      if (background) {
        MessagePlugin.error(k(opsErrorCode(failure)))
      } else {
        error.value = k(opsErrorCode(failure))
        runs.value = []
        reviews.value = []
        overview.value = undefined
        total.value = 0
      }
    }
  } finally {
    if (current === token) {
      loading.value = false
      refreshing.value = false
    }
  }
}
function changeFilter(key: string, value: string) {
  const draft = tab.value === 'records' ? recordDraft : draftFilters
  if (key in draft) (draft as Record<string, string>)[key] = value
}
function changePeriod(from: string, to: string) {
  if (tab.value === 'records') {
    recordDraft.review_from = from
    recordDraft.review_to = to
  } else {
    draftFilters.from = from
    draftFilters.to = to
  }
}
function removeFilter(key: string) {
  const applied = {
    ...(tab.value === 'records' ? recordFilters.value : filters.value),
  } as Record<string, string>
  const keys =
    key === 'period'
      ? tab.value === 'records'
        ? ['review_from', 'review_to']
        : ['from', 'to']
      : [key]
  keys.forEach((item) => {
    delete applied[item]
    changeFilter(item, '')
  })
  if (tab.value === 'records') recordFilters.value = applied
  else filters.value = applied
  selectedGroup.value = ''
  page.value = 1
  void reload()
}
function applyFilters() {
  filterError.value = ''
  if (tab.value === 'records') {
    if (
      recordDraft.review_from &&
      recordDraft.review_to &&
      new Date(recordDraft.review_from) >= new Date(recordDraft.review_to)
    ) {
      filterError.value = k('dateInvalid')
      return
    }
    recordFilters.value = buildFilters(recordDraft)
    page.value = 1
    void reload()
    return
  }
  if (
    draftFilters.from &&
    draftFilters.to &&
    new Date(draftFilters.from) >= new Date(draftFilters.to)
  ) {
    filterError.value = k('dateInvalid')
    return
  }
  selectedGroup.value = ''
  filters.value = buildFilters(draftFilters)
  page.value = 1
  void reload()
}
function resetFilters() {
  if (tab.value === 'records')
    Object.keys(recordDraft).forEach((key) => {
      recordDraft[key as keyof typeof recordDraft] = ''
    })
  else Object.assign(draftFilters, blankFilters())
  applyFilters()
}
function changeTab(value: Tab) {
  if (tab.value === value) return
  tab.value = value
  page.value = 1
  void router.replace({ query: { ...route.query, tab: value } })
  void reload()
}
function changePage(value: number) {
  page.value = value
  void reload()
}
function openRun(id: string, analysis?: string) {
  void router.replace({ query: { ...route.query, run: id, analysis } })
}
function selectGroup(key: string) {
  selectedGroup.value = key
  page.value = 1
  void reload()
}
function toggleGroups() {
  grouped.value = !grouped.value
  selectedGroup.value = ''
  page.value = 1
  void reload()
}
function filterQueue(value: string) {
  const applied = { ...filters.value }
  if (applied.route === value) delete applied.route
  else applied.route = value
  filters.value = applied
  Object.assign(draftFilters, blankFilters(), applied)
  filterError.value = ''
  selectedGroup.value = ''
  page.value = 1
  void reload()
}
async function filterCause(category: string) {
  if (filters.value.cause_category && filters.value.cause_category !== category)
    return
  // Drill into the displayed scope, not unapplied edits in the filter form.
  filters.value = { ...filters.value, cause_category: category }
  Object.assign(draftFilters, blankFilters(), filters.value)
  selectedGroup.value = ''
  page.value = 1
  await loadData(true)
  await nextTick()
  const list = document.getElementById('ops-run-list')
  list?.scrollIntoView({ block: 'start' })
  list?.focus({ preventScroll: true })
}
function closeRun() {
  const query = { ...route.query }
  delete query.run
  delete query.analysis
  void router.replace({ query })
}
function settingsSaved(value: OpsSettings) {
  settings.value = value
  MessagePlugin.success(k('settingsSaved'))
  void reload()
}
watch(
  () => auth.selectedTenantId,
  () => {
    token++
    settings.value = undefined
    settingsVisible.value = false
    closeRun()
    groups.value = []
    selectedGroup.value = ''
    page.value = 1
    void reload()
  },
)
watch(
  () => route.query.tab,
  (value) => {
    if (tabs.includes(value as Tab) && value !== tab.value) {
      tab.value = value as Tab
      page.value = 1
      void reload()
    }
  },
)
onMounted(reload)
</script>
<style scoped>
.ops-workbench {
  height: 100%;
  box-sizing: border-box;
  overflow: auto;
  padding: var(--app-space-6) var(--app-space-8) var(--app-space-10);
  background: var(--td-bg-color-container);
  color: var(--td-text-color-primary);
  font-size: var(--app-text-base);
  line-height: 1.6;
  font-family: var(--app-font-family);
  font-variant-numeric: tabular-nums;
}
.page-header {
  display: flex;
  justify-content: space-between;
  gap: var(--app-space-5);
  align-items: center;
  margin-bottom: var(--app-space-6);
}
.eyebrow {
  display: flex;
  gap: var(--app-space-2);
  align-items: center;
  font-size: var(--app-text-sm);
  color: var(--td-text-color-secondary);
  margin-bottom: var(--app-space-2);
}
.eyebrow span {
  color: var(--td-text-color-placeholder);
}
h1 {
  margin: 0;
  font-size: var(--app-text-4xl);
  line-height: 32px;
  font-weight: 600;
}
h2 {
  margin: 0 0 var(--app-space-5);
  font-size: var(--app-text-lg);
  font-weight: 600;
}
.page-header p {
  color: var(--td-text-color-secondary);
  margin: var(--app-space-2) 0 0;
  font-size: var(--app-text-md);
}
.header-actions {
  display: flex;
  align-items: center;
  gap: var(--app-space-2);
  flex-wrap: wrap;
}
.tabs {
  margin-bottom: var(--app-space-5);
  background: transparent;
}
.scope-strip {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: var(--app-space-3);
  flex-wrap: wrap;
  font-size: var(--app-text-sm);
  color: var(--td-text-color-secondary);
  margin: var(--app-space-3) var(--app-space-1) var(--app-space-5);
}
.scope-strip > span {
  display: flex;
  gap: var(--app-space-2);
  align-items: center;
}
.scope-dot {
  height: 5px;
  width: 5px;
  border-radius: 50%;
  background: var(--td-text-color-placeholder);
}
.scope-note,
.muted {
  font-size: var(--app-text-sm);
  color: var(--td-text-color-secondary);
}
.scope-note {
  margin: var(--app-space-3) 0;
  line-height: 1.7;
}
.metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: var(--app-space-4);
  margin: 0 0 var(--app-space-5);
}
.metric {
  padding: var(--app-space-5) var(--app-space-5);
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-xl);
  background: var(--td-bg-color-container);
  display: flex;
  flex-direction: column;
  gap: var(--app-space-1);
}
.metric-label {
  display: flex;
  align-items: center;
  justify-content: space-between;
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-md);
}
.metric-marker {
  height: 8px;
  width: 8px;
  border-radius: var(--app-radius-xs);
  background: var(--td-text-color-primary);
}
.metric strong {
  font-size: var(--td-font-size-headline-large);
  line-height: 1.4;
  letter-spacing: -1px;
  font-weight: 600;
  color: var(--td-text-color-primary);
}
.metric strong.succeeded {
  color: var(--td-success-color-6);
}
.metric strong.pending {
  color: var(--td-warning-color-6);
}
.metric strong.failed {
  color: var(--td-error-color-6);
}
.metric small {
  font-size: var(--app-text-sm);
  color: var(--td-text-color-secondary);
}
.metric-footnote {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: center;
  gap: var(--app-space-2);
  min-height: 24px;
}
.pending-link {
  flex-shrink: 0;
}
.metric-marker.succeeded {
  background: var(--td-success-color);
}
.metric-marker.pending {
  background: var(--td-warning-color);
}
.metric-marker.failed {
  background: var(--td-error-color);
}
.panel {
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-xl);
  padding: var(--app-space-5);
}
.summary {
  display: flex;
  gap: var(--app-space-6);
  padding: var(--app-space-3) var(--app-space-1);
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-md);
}
.summary strong {
  margin-left: var(--app-space-2);
  color: var(--td-text-color-primary);
}
.list-panel {
  margin-top: var(--app-space-3);
  padding: var(--app-space-5) 0 0;
  overflow: hidden;
}
.section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: var(--app-space-3);
  padding: 0 var(--app-space-5);
}
.list-actions {
  display: flex;
  align-items: center;
  gap: var(--app-space-3);
  white-space: nowrap;
}
.section-header h2 {
  margin: 0;
  font-size: var(--app-text-xl);
}
.list-panel > .scope-note {
  padding: 0 var(--app-space-5);
  margin: var(--app-space-2) 0 var(--app-space-5);
}
.pagination {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: var(--app-space-3);
  margin-top: var(--app-space-4);
  font-size: var(--app-text-sm);
  color: var(--td-text-color-secondary);
}
.loading-state {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: var(--app-space-3);
  min-height: 280px;
}
.error {
  color: var(--td-error-color);
}
button:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 3px;
}
.queue-counts {
  margin: var(--app-space-5) 0;
  padding: 0;
  background: none;
  border: 0;
}
.queue-counts h2 {
  font-size: var(--app-text-md);
  color: var(--td-text-color-secondary);
  margin: 0 0 var(--app-space-3);
}
.queue-count-grid {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: var(--app-space-4);
}
.queue-count-grid button {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--app-space-4) var(--app-space-5);
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-lg);
  background: var(--td-bg-color-container);
  font: inherit;
  color: var(--td-text-color-primary);
  cursor: pointer;
  transition: border-color var(--app-motion-fast);
}
.queue-count-grid button.active {
  border-color: var(--td-brand-color);
  background: var(--td-brand-color-light);
  color: var(--td-brand-color);
}
.queue-count-grid button:hover {
  border-color: var(--td-brand-color);
}
.queue-count-grid strong {
  font-size: var(--app-text-4xl);
  font-weight: 600;
}
.queue-count-grid button:first-child strong {
  color: var(--td-error-color-6);
}
@media (max-width: 1100px) {
  .page-header {
    align-items: flex-start;
    flex-direction: column;
  }
  .metrics,
  .queue-count-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 700px) {
  .ops-workbench {
    padding: var(--app-space-5) var(--app-space-4);
  }
  .header-actions {
    gap: var(--app-space-2);
  }
  .summary {
    flex-wrap: wrap;
    gap: var(--app-space-3);
  }
  .pagination {
    justify-content: center;
  }
  .metric {
    padding: var(--app-space-4);
  }
  .queue-count-grid button {
    padding: var(--app-space-3);
    font-size: var(--app-text-sm);
  }
}
@media (prefers-reduced-motion: reduce) {
  .queue-count-grid button {
    transition: none;
  }
}
</style>
