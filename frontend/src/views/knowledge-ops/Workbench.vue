<template>
  <main class="ops-workbench">
    <header class="page-header"><div><h1>{{ k('title') }}</h1><p>{{ k('subtitle') }}</p></div><div class="header-actions"><t-tag :theme="settings?.enabled ? 'success' : 'default'" variant="light">{{ settings ? k(settings.enabled ? 'enabled' : 'disabled') : k('unknown') }}</t-tag><t-button variant="outline" @click="settingsVisible = true">{{ k('settings') }}</t-button><t-button variant="outline" :loading="loading" @click="reload">{{ k('refresh') }}</t-button></div></header>
    <nav class="tabs" :aria-label="k('title')"><button v-for="item in tabs" :key="item" type="button" :class="{ active: tab === item }" :aria-current="tab === item ? 'page' : undefined" @click="changeTab(item)">{{ k(item) }}</button></nav>
    <form class="filters" @submit.prevent="applyFilters">
      <label>{{ k('from') }}<input v-model="draftFilters.from" type="datetime-local" /></label><label>{{ k('to') }}<input v-model="draftFilters.to" type="datetime-local" /></label>
      <label>{{ k('qa_type') }}<select v-model="draftFilters.qa_type"><option value="">{{ k('all') }}</option><option v-for="mode in qaTypes" :key="mode" :value="mode">{{ label(mode) }}</option></select></label>
      <label>{{ k('channel') }}<input v-model.trim="draftFilters.channel" :placeholder="k('all')" list="ops-channels" /><datalist id="ops-channels"><option v-for="channel in channels" :key="channel" :value="channel" /></datalist></label>
      <label>{{ k('capture_status') }}<select v-model="draftFilters.capture_status"><option value="">{{ k('all') }}</option><option v-for="value in ['complete', 'partial', 'missing', 'unknown']" :key="value" :value="value">{{ label(value) }}</option></select></label>
      <label>{{ k('route') }}<select v-model="draftFilters.route"><option value="">{{ k('all') }}</option><option v-for="value in routes" :key="value" :value="value">{{ label(value) }}</option></select></label>
      <label>{{ k('verdict') }}<select v-model="draftFilters.verdict"><option value="">{{ k('all') }}</option><option v-for="value in answerVerdicts" :key="value" :value="value">{{ labelVerdict(value) }}</option></select></label>
      <label class="search-label">{{ k('search') }}<input v-model.trim="draftFilters.search" type="search" /></label><div class="filter-actions"><t-button type="submit">{{ k('apply') }}</t-button><t-button variant="text" @click="resetFilters">{{ k('reset') }}</t-button></div>
    </form>
    <p v-if="filters.route === 'technical_followup'" class="scope-note">{{ k('technicalNotice') }}</p>
    <p v-if="filterError" class="error" role="alert">{{ filterError }}</p>
    <p class="scope-note">{{ k('scopeNotice') }} {{ k('timeZone') }}: {{ timeZone }}. <span v-if="settings?.capture_enabled_at">{{ k('captureSince') }}: {{ date(settings.capture_enabled_at) }}</span></p>
    <div v-if="loading" class="loading-state" role="status"><t-loading size="medium" />{{ k('loading') }}</div>
    <section v-else-if="error" class="empty-state" role="alert"><h2>{{ k('errorTitle') }}</h2><p>{{ error }}</p><t-button @click="reload">{{ k('retry') }}</t-button></section>
    <template v-else>
      <template v-if="tab === 'overview' && overview">
        <div class="metrics"><article v-for="item in metrics" :key="item.key" class="metric"><span>{{ k(item.key) }}</span><strong :class="item.key">{{ item.value.toLocaleString() }}</strong><small v-if="item.key !== 'total'">{{ overview.total ? (item.value / overview.total * 100).toFixed(1) : '0.0' }}%</small><small v-else>{{ k('scope') }}</small></article></div>
        <div class="distribution-grid"><section v-for="item in distributions" :key="item.key" class="panel"><h2>{{ k(item.key) }}</h2><p v-if="!Object.keys(item.values).length" class="muted">{{ k('none') }}</p><div v-for="(value, key) in item.values" :key="key" class="distribution"><span>{{ item.key === 'verdicts' ? labelVerdict(String(key)) : label(String(key)) }}</span><div class="bar" aria-hidden="true"><i :style="{ width: `${overview.total ? value / overview.total * 100 : 0}%` }" /></div><strong>{{ value }}</strong></div></section></div>
        <div class="summary"><span>{{ k('pendingTasks') }} <strong>{{ overview.pending_tasks }}</strong></span><span>{{ k('reviewCount') }} <strong>{{ overview.review_count }}</strong></span></div>
      </template>
      <section class="panel list-panel"><div class="section-header"><h2>{{ k(tab === 'overview' ? 'runs' : tab) }}</h2><span class="muted">{{ total }} {{ k('count') }}</span></div><p class="scope-note">{{ k('autoNotice') }}</p>
        <div v-if="!total" class="empty-state"><h3>{{ k('empty') }}</h3><p>{{ k('emptyHint') }}</p></div>
        <div v-else class="table-scroll"><table v-if="tab !== 'records'"><thead><tr><th>{{ k('question') }}</th><th>{{ k('qa_type') }} / {{ k('channel') }}</th><th>{{ k('capture_status') }}</th><th>{{ k('verdict') }}</th><th>{{ k('route') }}</th><th>{{ k('diagnosis') }}</th><th>{{ k('reviewState') }}</th><th>{{ k('time') }}</th><th>{{ k('operation') }}</th></tr></thead><tbody><tr v-for="run in runs" :key="run.id"><td class="question-cell"><span>{{ run.question }}</span><small>{{ run.execution_mode || k('unknown') }}</small></td><td>{{ label(run.qa_type) }}<small>{{ run.channel || k('unknown') }}</small></td><td><span class="status-tag" :class="run.capture_status">{{ label(run.capture_status) }}</span></td><td>{{ labelVerdict(run.analysis?.report.verdict) }}</td><td>{{ label(run.analysis?.route) }}</td><td>{{ label(run.analysis?.status || 'pending') }}</td><td>{{ label(run.review_state) }}</td><td>{{ date(run.started_at) }}</td><td><button type="button" class="link-button" @click="openRun(run.id)">{{ k('detail') }}</button></td></tr></tbody></table>
        <table v-else><thead><tr><th>{{ k('target') }}</th><th>{{ k('reviewAction') }}</th><th>{{ k('verdict') }} / {{ k('causeCategory') }}</th><th>{{ k('reviewReason') }}</th><th>{{ k('reviewer') }}</th><th>{{ k('created') }}</th><th>{{ k('operation') }}</th></tr></thead><tbody><tr v-for="review in reviews" :key="review.id"><td>{{ k(review.target === 'answer' ? 'answerTarget' : 'causeTarget') }}<small>{{ review.analysis_id }}</small></td><td>{{ label(review.action) }}</td><td>{{ review.target === 'answer' ? labelVerdict(review.verdict) : label(review.verdict) }}<small v-if="review.cause_category">{{ label(review.cause_category) }}</small></td><td class="reason-cell">{{ review.reason }}</td><td>{{ review.reviewer_id }}</td><td>{{ date(review.created_at) }}</td><td><button type="button" class="link-button" @click="openRun(review.run_id)">{{ k('detail') }}</button></td></tr></tbody></table></div>
        <footer v-if="total" class="pagination"><t-button variant="outline" :disabled="page <= 1" @click="changePage(page - 1)">{{ k('previous') }}</t-button><span>{{ k('page') }} {{ page }} / {{ Math.max(1, Math.ceil(total / pageSize)) }}</span><t-button variant="outline" :disabled="page * pageSize >= total" @click="changePage(page + 1)">{{ k('next') }}</t-button></footer>
      </section>
    </template>
    <SettingsDrawer :visible="settingsVisible" @close="settingsVisible = false" @saved="settingsSaved" />
    <DetailDrawer :runID="activeRun" :enabled="!!settings?.enabled" @close="closeRun" @changed="reload" />
  </main>
</template>
<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import { useAuthStore } from '@/stores/auth'
import { getOpsSettings, getOpsOverview, getOpsRuns, getOpsReviews, opsErrorCode, type OpsSettings, type OpsOverview, type OpsRun, type OpsReview, type OpsFilters } from '@/api/knowledge-ops'
import { buildFilters, answerVerdicts, routes } from './state'
import { useOpsText } from './locales'
import SettingsDrawer from './SettingsDrawer.vue'
import DetailDrawer from './DetailDrawer.vue'
const { k, label, labelVerdict, date } = useOpsText()
const router = useRouter(); const route = useRoute(); const auth = useAuthStore()
const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone
const tabs = ['overview', 'queue', 'records'] as const
type Tab = typeof tabs[number]
const tab = ref<Tab>(tabs.includes(route.query.tab as Tab) ? route.query.tab as Tab : 'overview')
const settings = ref<OpsSettings>(); const overview = ref<OpsOverview>(); const runs = ref<OpsRun[]>([]); const reviews = ref<OpsReview[]>([])
const loading = ref(false); const error = ref(''); const settingsVisible = ref(false); const filterError = ref(''); const page = ref(1); const pageSize = 20; const total = ref(0)
const activeRun = computed(() => typeof route.query.run === 'string' ? route.query.run : '')
const blankFilters = () => ({ from: '', to: '', qa_type: '', channel: '', capture_status: '', route: '', verdict: '', search: '' })
const draftFilters = reactive(blankFilters()); const filters = ref<OpsFilters>({}); let token = 0
const qaTypes = ['rag-qa', 'wiki-qa', 'hybrid-rag-wiki']
const channels = ['web', 'api', 'embed', 'wecom', 'feishu', 'lark', 'slack', 'telegram', 'dingtalk', 'mattermost', 'wechat', 'qqbot']
const metrics = computed(() => [{ key: 'total', value: overview.value?.total || 0 }, { key: 'complete', value: overview.value?.complete || 0 }, { key: 'partial', value: overview.value?.partial || 0 }, { key: 'missing', value: overview.value?.missing || 0 }])
const distributions = computed(() => [{ key: 'modes', values: overview.value?.by_mode || {} }, { key: 'verdicts', values: overview.value?.by_verdict || {} }, { key: 'routing', values: overview.value?.by_route || {} }])
async function reload() {
  const current = ++token; loading.value = true; error.value = ''
  const params = { ...filters.value, page: page.value, page_size: pageSize, ...(tab.value === 'queue' ? { view: 'queue' as const } : {}) }
  try {
    const [config, stats, result] = await Promise.all([getOpsSettings(), tab.value === 'overview' ? getOpsOverview(filters.value) : Promise.resolve(undefined), tab.value === 'records' ? getOpsReviews(params) : getOpsRuns(params)])
    if (current !== token) return
    settings.value = config; overview.value = stats; total.value = result.total
    if (tab.value === 'records') { reviews.value = result.items as OpsReview[]; runs.value = [] } else { runs.value = result.items as OpsRun[]; reviews.value = [] }
  } catch (failure) { if (current === token) { error.value = k(opsErrorCode(failure)); runs.value = []; reviews.value = []; overview.value = undefined; total.value = 0 } }
  finally { if (current === token) loading.value = false }
}
function applyFilters() {
  filterError.value = ''
  if (draftFilters.from && draftFilters.to && new Date(draftFilters.from) >= new Date(draftFilters.to)) { filterError.value = k('dateInvalid'); return }
  filters.value = buildFilters(draftFilters); page.value = 1; void reload()
}
function resetFilters() { Object.assign(draftFilters, blankFilters()); applyFilters() }
function changeTab(value: Tab) { if (tab.value === value) return; tab.value = value; page.value = 1; void router.replace({ query: { ...route.query, tab: value } }); void reload() }
function changePage(value: number) { page.value = value; void reload() }
function openRun(id: string) { void router.replace({ query: { ...route.query, run: id } }) }
function closeRun() { const query = { ...route.query }; delete query.run; void router.replace({ query }) }
function settingsSaved(value: OpsSettings) { settings.value = value; MessagePlugin.success(k('settingsSaved')); void reload() }
watch(() => auth.selectedTenantId, () => { token++; settings.value = undefined; settingsVisible.value = false; closeRun(); page.value = 1; void reload() })
watch(() => route.query.tab, value => { if (tabs.includes(value as Tab) && value !== tab.value) { tab.value = value as Tab; page.value = 1; void reload() } })
onMounted(reload)
</script>
<style scoped>
.ops-workbench{height:100%;box-sizing:border-box;overflow:auto;padding:30px 32px 40px;background:var(--td-bg-color-page);color:var(--td-text-color-primary);font-size:14px;line-height:1.5}.page-header{display:flex;justify-content:space-between;gap:20px;align-items:flex-start;margin-bottom:24px}h1{margin:0;font-size:26px;font-weight:600}h2{margin:0 0 18px;font-size:17px}h3{font-size:16px}.page-header p{color:var(--td-text-color-secondary);margin:8px 0 0}.header-actions{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.tabs{display:flex;gap:28px;border-bottom:1px solid var(--td-component-border);margin-bottom:20px}.tabs button{min-height:44px;font:inherit;font-weight:500;cursor:pointer;background:transparent;border:0;border-bottom:3px solid transparent;color:var(--td-text-color-secondary);padding:10px 0}.tabs button.active{border-color:var(--td-brand-color);color:var(--td-brand-color)}.filters{display:grid;grid-template-columns:repeat(4,minmax(130px,1fr));gap:14px}.filters label{display:flex;flex-direction:column;gap:6px;font-size:12px;color:var(--td-text-color-secondary)}.filters input,.filters select{box-sizing:border-box;width:100%;font:inherit;font-size:14px;min-height:38px;padding:8px;border:1px solid var(--td-component-border);border-radius:6px;background:var(--td-bg-color-container);color:var(--td-text-color-primary)}.filter-actions{display:flex;gap:8px;align-items:flex-end}.scope-note,.muted{font-size:13px;color:var(--td-text-color-secondary)}.scope-note{margin:16px 0}.scope-note span{display:block;margin-top:4px}.metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:16px;margin:22px 0}.metric{padding:20px;border:1px solid var(--td-component-border);border-radius:10px;background:var(--td-bg-color-container);display:flex;flex-direction:column;gap:8px}.metric span{color:var(--td-text-color-secondary)}.metric strong{font-size:32px;font-weight:600;color:var(--td-brand-color)}.metric strong.complete{color:var(--td-success-color)}.metric strong.partial{color:var(--td-warning-color)}.metric strong.missing{color:var(--td-text-color-secondary)}.metric small{color:var(--td-text-color-secondary)}.panel{background:var(--td-bg-color-container);border:1px solid var(--td-component-border);border-radius:10px;padding:20px}.distribution-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:16px}.distribution{display:grid;grid-template-columns:minmax(90px,1fr) 1fr 40px;gap:10px;align-items:center;margin:14px 0;font-size:13px}.distribution strong{text-align:right}.bar{height:6px;background:var(--td-bg-color-secondarycontainer);border-radius:4px;overflow:hidden}.bar i{display:block;height:100%;background:var(--td-brand-color)}.summary{display:flex;gap:30px;margin:20px 0;color:var(--td-text-color-secondary)}.summary strong{margin-left:8px;color:var(--td-text-color-primary)}.list-panel{margin-top:20px}.section-header{display:flex;align-items:baseline;justify-content:space-between}.section-header h2{margin-bottom:0}.table-scroll{overflow:auto}table{border-collapse:collapse;width:100%;min-width:950px;font-size:13px;text-align:left}th{font-weight:500;background:var(--td-bg-color-secondarycontainer);color:var(--td-text-color-secondary);white-space:nowrap}th,td{padding:14px 12px;border-bottom:1px solid var(--td-component-border);vertical-align:top}td small{display:block;color:var(--td-text-color-secondary);margin-top:6px;font-size:12px;max-width:180px;overflow-wrap:anywhere}.question-cell{max-width:260px;min-width:160px}.question-cell>span{display:-webkit-box;-webkit-line-clamp:3;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere}.reason-cell{max-width:300px;white-space:pre-wrap;overflow-wrap:anywhere}.status-tag{display:inline-block;white-space:nowrap;padding:3px 7px;background:var(--td-bg-color-secondarycontainer);border-radius:4px}.status-tag.complete{background:var(--td-success-color-light);color:var(--td-success-color)}.status-tag.partial{background:var(--td-warning-color-light);color:var(--td-warning-color)}.link-button{border:0;padding:0;background:transparent;color:var(--td-brand-color);font:inherit;cursor:pointer;white-space:nowrap;min-height:32px}.link-button:hover{text-decoration:underline}tbody tr:hover{background:var(--td-bg-color-container-hover)}.pagination{display:flex;justify-content:flex-end;align-items:center;gap:14px;margin-top:20px}.empty-state{text-align:center;padding:40px 20px;color:var(--td-text-color-secondary)}.loading-state{display:flex;justify-content:center;align-items:center;gap:12px;min-height:280px}.error{color:var(--td-error-color)}button:focus-visible,input:focus-visible,select:focus-visible{outline:2px solid var(--td-brand-color);outline-offset:3px}@media(max-width:1100px){.page-header{flex-direction:column}.distribution-grid{grid-template-columns:1fr}.filters{grid-template-columns:repeat(3,minmax(130px,1fr))}}@media(max-width:700px){.ops-workbench{padding:20px 16px}.filters,.metrics{grid-template-columns:repeat(2,minmax(0,1fr))}.tabs{gap:18px}.header-actions{gap:8px}.summary{flex-wrap:wrap;gap:12px}.pagination{justify-content:center}}
</style>
