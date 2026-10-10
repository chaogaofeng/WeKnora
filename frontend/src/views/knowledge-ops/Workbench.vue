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
            <small v-if="item.key !== 'total'">
              {{
                overview.total
                  ? ((item.value / overview.total) * 100).toFixed(1)
                  : '0.0'
              }}%
            </small>
            <small v-else>{{ k('scope') }}</small>
            <div class="metric-track" aria-hidden="true">
              <i
                :class="item.key"
                :style="{
                  width: `${overview.total ? (item.value / overview.total) * 100 : 0}%`,
                }"
              />
            </div>
          </article>
        </div>
        <div class="distribution-grid">
          <section
            v-for="item in distributions"
            :key="item.key"
            class="panel"
            :class="item.key"
          >
            <h2>{{ k(item.key) }}</h2>
            <p v-if="!Object.keys(item.values).length" class="muted">
              {{ k('none') }}
            </p>
            <div
              v-for="(value, key) in item.values"
              :key="key"
              class="distribution"
            >
              <span class="distribution-label">
                {{
                  item.key === 'verdicts'
                    ? labelVerdict(String(key))
                    : label(String(key))
                }}
              </span>
              <div class="bar" aria-hidden="true">
                <i
                  :class="String(key)"
                  :style="{
                    width: `${Object.values(item.values).reduce((sum, n) => sum + n, 0) ? (value / Object.values(item.values).reduce((sum, n) => sum + n, 0)) * 100 : 0}%`,
                  }"
                />
              </div>
              <strong>{{ value }}</strong>
            </div>
          </section>
        </div>
        <p class="scope-note">{{ k('supportDenominator') }}</p>
        <OverviewInsights :stats="overview" />
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
            @click="filterQueue(value)"
          >
            <span>{{ label(value) }}</span>
            <strong>{{ overview.queue_counts?.[value] || 0 }}</strong>
          </button>
        </div>
      </section>
      <QueuePanel
        v-if="tab === 'queue'"
        :runs="runs"
        :groups="groups"
        :grouped="grouped && !selectedGroup"
        :selectedGroup="selectedGroup"
        @open="openRun"
        @group="selectGroup"
        @toggle="toggleGroups"
      />
      <section
        v-if="tab !== 'queue'"
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
        <RunTable v-else-if="tab !== 'records'" :runs="runs" @open="openRun" />
        <ReviewTable v-else :reviews="reviews" @open="openRun" />
      </section>
      <footer v-if="total" class="pagination">
        <t-pagination
          :current="page"
          :page-size="pageSize"
          :total="total"
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
      :runID="activeRun"
      :analysisID="activeAnalysis"
      :enabled="!!settings?.enabled"
      @close="closeRun"
      @changed="reload"
    />
  </main>
</template>
<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import { RefreshIcon } from 'tdesign-icons-vue-next'
import { useAuthStore } from '@/stores/auth'
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
const { k, label, labelVerdict, date } = useOpsText()
const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
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
let token = 0
const metrics = computed(() => [
  { key: 'total', label: 'total', value: overview.value?.total || 0 },
  {
    key: 'complete',
    label: 'content_complete',
    value: overview.value?.content_coverage?.complete || 0,
  },
  {
    key: 'partial',
    label: 'content_partial',
    value: overview.value?.content_coverage?.partial || 0,
  },
  {
    key: 'missing',
    label: 'content_missing_unknown',
    value: overview.value?.content_coverage?.missing || 0,
  },
])
const distributions = computed(() => [
  { key: 'diagnosisStates', values: overview.value?.diagnosis_status || {} },
  { key: 'verdicts', values: overview.value?.by_verdict || {} },
  { key: 'routing', values: overview.value?.by_route || {} },
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
  selectedGroup.value = ''
  if (
    draftFilters.from &&
    draftFilters.to &&
    new Date(draftFilters.from) >= new Date(draftFilters.to)
  ) {
    filterError.value = k('dateInvalid')
    return
  }
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
  draftFilters.route = filters.value.route === value ? '' : value
  applyFilters()
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
.metric strong.complete {
  color: var(--td-success-color-6);
}
.metric strong.partial {
  color: var(--td-warning-color-6);
}
.metric strong.missing {
  color: var(--td-text-color-secondary);
}
.metric small {
  font-size: var(--app-text-sm);
  color: var(--td-text-color-secondary);
}
.metric-track {
  height: 3px;
  border-radius: var(--app-radius-xs);
  margin-top: var(--app-space-2);
  background: var(--td-bg-color-secondarycontainer);
  overflow: hidden;
}
.metric-track i {
  display: block;
  height: 100%;
  background: var(--td-text-color-primary);
}
.metric-marker.complete,
.metric-track .complete {
  background: var(--td-success-color);
}
.metric-marker.partial,
.metric-track .partial {
  background: var(--td-warning-color);
}
.metric-marker.missing,
.metric-track .missing {
  background: var(--td-text-color-placeholder);
}
.panel {
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-xl);
  padding: var(--app-space-5);
}
.distribution-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--app-space-4);
}
.distribution {
  display: grid;
  grid-template-columns: minmax(88px, 1fr) 1fr 30px;
  gap: var(--app-space-2);
  align-items: center;
  margin: var(--app-space-4) 0;
  font-size: var(--app-text-md);
}
.distribution strong {
  text-align: right;
  font-weight: 600;
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
  background: var(--td-brand-color);
}
.bar .partial,
.bar .human_review,
.bar .needs_evidence,
.bar .source_verification {
  background: var(--td-warning-color);
}
.bar .failed,
.bar .contradicted {
  background: var(--td-error-color);
}
.bar .unverifiable,
.bar .pending,
.bar .technical_followup {
  background: var(--td-text-color-placeholder);
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
  .distribution-grid {
    grid-template-columns: 1fr;
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
