<template>
  <div class="conversation-logs">
    <div class="section-header">
      <h2>{{ t('conversationLogs.title') }}</h2>
      <p class="section-description">{{ t('conversationLogs.description') }}</p>
    </div>

    <div class="toolbar">
      <t-date-range-picker
        v-model="dateRange"
        value-type="YYYY-MM-DD"
        :placeholder="[t('conversationLogs.filterTime'), t('conversationLogs.filterTime')]"
        clearable
        allow-input
        class="filter-item time-select"
        @change="onFilterChange"
      />
      <t-select
        v-model="source"
        :placeholder="t('conversationLogs.filterSource')"
        class="filter-item source-select"
        @change="onFilterChange"
      >
        <t-option value="" :label="t('conversationLogs.sourceAll')" />
        <t-option value="web" :label="t('conversationLogs.sourceWeb')" />
        <t-option value="embed" :label="t('conversationLogs.sourceEmbed')" />
        <t-option value="api" :label="t('conversationLogs.sourceAPI')" />
        <t-option value="feishu" :label="t('conversationLogs.sourceFeishu')" />
        <t-option value="wecom" :label="t('conversationLogs.sourceWecom')" />
        <t-option value="channel" :label="t('conversationLogs.sourceChannel')" />
      </t-select>
      <t-select
        v-model="agentId"
        :placeholder="t('conversationLogs.filterAgent')"
        filterable
        clearable
        class="filter-item agent-select"
        @change="onFilterChange"
      >
        <t-option v-for="a in agents" :key="a.id" :value="a.id" :label="a.name" />
      </t-select>
      <t-input
        v-model="keyword"
        :placeholder="t('conversationLogs.searchPlaceholder')"
        clearable
        class="filter-item search-input"
        @enter="onFilterChange"
        @clear="onFilterChange"
      />
      <div class="sort-group">
        <span class="sort-label">{{ t('conversationLogs.sortLabel') }}</span>
        <t-select v-model="sortBy" class="sort-select" @change="onSortSelectChange">
          <t-option value="updated_at" :label="t('conversationLogs.sortUpdatedAt')" />
          <t-option value="created_at" :label="t('conversationLogs.sortCreatedAt')" />
        </t-select>
        <t-button variant="outline" class="sort-dir" @click="toggleSortOrder">
          {{ sortOrder === 'asc' ? '↑' : '↓' }}
        </t-button>
      </div>
    </div>

    <div class="stats-row">
      <div class="stat-card">
        <div class="stat-value">{{ stats.total_sessions }}</div>
        <div class="stat-label">{{ t('conversationLogs.statSessions') }}</div>
      </div>
      <div class="stat-card">
        <div class="stat-value">{{ stats.total_users }}</div>
        <div class="stat-label">{{ t('conversationLogs.statUsers') }}</div>
      </div>
      <div class="stat-card">
        <div class="stat-value">{{ stats.total_questions }}</div>
        <div class="stat-label">{{ t('conversationLogs.statQuestions') }}</div>
      </div>
      <div class="stat-card">
        <div class="stat-value">{{ stats.total_anonymous }}</div>
        <div class="stat-label">{{ t('conversationLogs.statAnonymousSessions') }}</div>
      </div>
      <div class="stat-card">
        <div class="stat-value">{{ stats.total_anonymous_questions }}</div>
        <div class="stat-label">{{ t('conversationLogs.statAnonymousQuestions') }}</div>
      </div>
    </div>

    <t-table
      :data="sessions"
      :columns="columns"
      row-key="id"
      :loading="loading"
      size="medium"
      hover
      @row-click="(ctx: any) => openSession(ctx.row)"
    >
        <template #source="{ row }">
          <t-tag :theme="sourceTheme(row)" variant="light">{{ sourceLabel(row) }}</t-tag>
        </template>
        <template #owner="{ row }">
          <t-tag v-if="userLabel(row)" variant="outline" theme="primary" size="small">
            {{ userLabel(row) }}
          </t-tag>
          <span v-else class="muted">—</span>
        </template>
        <template #agent="{ row }">
          {{ agentName(row.last_request_state?.agent_id) }}
        </template>
        <template #messages="{ row }">
          {{ row.message_count ?? 0 }}
        </template>
        <template #created_at="{ row }">
          {{ formatTime(row.created_at) }}
        </template>
        <template #updated_at="{ row }">
          {{ formatTime(row.updated_at) }}
        </template>
        <template #user_feedback="{ row }">
          <span class="fb-cell">
            <span v-if="row.user_feedback_up" class="fb-up" :title="t('conversationLogs.fbUp')">👍 {{ row.user_feedback_up }}</span>
            <span v-if="row.user_feedback_down" class="fb-down" :title="t('conversationLogs.fbDown')">👎 {{ row.user_feedback_down }}</span>
            <span v-if="!row.user_feedback_up && !row.user_feedback_down" class="muted">—</span>
          </span>
        </template>
        <template #admin_feedback="{ row }">
          <span v-if="row.admin_feedback_count" class="fb-admin">📝 {{ row.admin_feedback_count }}</span>
          <span v-else class="muted">—</span>
        </template>
        <template #actions="{ row }">
          <t-link theme="primary" @click.stop="openSession(row)">{{ t('conversationLogs.view') }}</t-link>
        </template>
      </t-table>
      <div v-if="!loading && sessions.length === 0" class="empty">
        {{ t('conversationLogs.emptySessions') }}
      </div>
      <t-pagination
        v-if="total > pageSize"
        :total="total"
        :current="page"
        :page-size="pageSize"
        class="pager"
        @change="onPageChange"
      />

    <t-drawer
      v-model:visible="drawerVisible"
      :header="t('conversationLogs.drawerTitle')"
      size="large"
      @close="drawerVisible = false"
    >
      <div v-if="drawerLoading" class="drawer-loading">{{ t('conversationLogs.loading') }}</div>
      <div v-else-if="drawerError" class="drawer-error">
        <div class="drawer-error-title">{{ t('conversationLogs.detailError') }}</div>
        <div class="drawer-error-hint">{{ t('conversationLogs.detailNoPermission') }}</div>
      </div>
      <div v-else-if="messages.length === 0" class="empty">{{ t('conversationLogs.emptyMessages') }}</div>
      <div v-else class="chat">
        <div class="chat-meta">
          <div class="meta-item">
            <span class="meta-k">{{ t('conversationLogs.colTitle') }}</span>
            <span class="meta-v">{{ drawerMeta.title }}</span>
          </div>
          <div class="meta-item">
            <span class="meta-k">{{ t('conversationLogs.colUser') }}</span>
            <span class="meta-v">{{ drawerMeta.user }}</span>
          </div>
          <div class="meta-item">
            <span class="meta-k">{{ t('conversationLogs.colAgent') }}</span>
            <span class="meta-v">{{ drawerMeta.agent }}</span>
          </div>
          <div class="meta-item">
            <span class="meta-k">{{ t('conversationLogs.colSource') }}</span>
            <span class="meta-v">{{ drawerMeta.source }}</span>
          </div>
        </div>

        <div class="chat-body">
          <div
            v-for="m in messages"
            :key="m.id"
            class="bubble-row"
            :class="m.role === 'user' ? 'is-user' : 'is-assistant'"
          >
            <div class="bubble-avatar" :class="m.role === 'user' ? 'is-user' : 'is-assistant'">
              {{ m.role === 'user' ? 'U' : 'AI' }}
            </div>
            <div class="bubble">
              <div class="bubble-head">
                <span class="bubble-role">
                  {{ m.role === 'user' ? t('conversationLogs.user') : t('conversationLogs.assistant') }}
                </span>
                <span class="bubble-time">{{ formatTime(m.created_at) }}</span>
              </div>
              <div class="bubble-content">{{ m.content }}</div>
              <div v-if="m.role === 'assistant'" class="msg-feedback">
                <span v-if="feedbackOf(m.id, 'user')" class="fb-user">
                  <template v-if="feedbackOf(m.id, 'user')!.rating === 1">👍</template>
                  <template v-else-if="feedbackOf(m.id, 'user')!.rating === -1">👎</template>
                  {{ t('conversationLogs.userFb') }}
                  <span v-if="feedbackOf(m.id, 'user')!.content" class="fb-user-note">
                    {{ feedbackOf(m.id, 'user')!.content }}
                  </span>
                </span>
                <span v-if="feedbackOf(m.id, 'admin') && annotatingId !== m.id" class="fb-admin-note">
                  📝 {{ feedbackOf(m.id, 'admin')!.content || t('conversationLogs.annotated') }}
                </span>
                <t-button
                  v-if="annotatingId !== m.id"
                  size="small"
                  variant="text"
                  theme="primary"
                  @click="startAnnotate(m)"
                >
                  {{ feedbackOf(m.id, 'admin') ? t('conversationLogs.editAnnotate') : t('conversationLogs.addAnnotate') }}
                </t-button>
                <t-button
                  v-if="feedbackOf(m.id, 'admin') && annotatingId !== m.id"
                  size="small"
                  variant="text"
                  theme="danger"
                  @click="removeAnnotate(m)"
                >
                  {{ t('conversationLogs.removeAnnotate') }}
                </t-button>
                <div v-else-if="annotatingId === m.id" class="annotate-box">
                  <t-radio-group v-model="annotateRating">
                    <t-radio-button :value="1">👍</t-radio-button>
                    <t-radio-button :value="0">—</t-radio-button>
                    <t-radio-button :value="-1">👎</t-radio-button>
                  </t-radio-group>
                  <t-textarea
                    v-model="annotateContent"
                    :placeholder="t('conversationLogs.annotatePlaceholder')"
                    :rows="2"
                  />
                  <div class="annotate-actions">
                    <t-button size="small" theme="primary" :loading="annotateSaving" @click="saveAnnotate(m)">
                      {{ t('conversationLogs.save') }}
                    </t-button>
                    <t-button size="small" variant="outline" @click="annotatingId = ''">
                      {{ t('conversationLogs.cancel') }}
                    </t-button>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </t-drawer>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import {
  listSessions,
  loadSessionMessages,
  listSessionFeedbacks,
  saveSessionFeedback,
  deleteSessionFeedback,
  getSessionStats,
  type SessionListItem,
  type ChatMessage,
  type MessageFeedback,
  type SessionStats,
} from '@/api/sessions'
import { listAgents } from '@/api/agent'

const { t } = useI18n()

const keyword = ref('')
const source = ref('') // 默认全部来源：覆盖所有用户的会话
const agentId = ref('')
// 时间范围过滤（字段跟随排序下拉：更新时间/创建时间，YYYY-MM-DD；后端对结束日含当日）
const dateRange = ref<string[]>([])
const searchQuery = ref('')
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
// 当前筛选范围内的聚合统计（会话总数 / 用户总数 / 提问数）
const stats = ref<SessionStats>({ total_sessions: 0, total_users: 0, total_questions: 0, total_anonymous: 0, total_anonymous_questions: 0 })
// 服务端排序（避免分页下的本地排序误导）
const sortBy = ref('updated_at')
const sortOrder = ref('desc')
const sessions = ref<SessionListItem[]>([])
const loading = ref(false)

const agents = ref<Array<{ id: string; name: string }>>([])
const agentMap = computed(() => {
  const m: Record<string, string> = {}
  agents.value.forEach((a) => (m[a.id] = a.name))
  return m
})

const drawerVisible = ref(false)
const drawerLoading = ref(false)
const drawerError = ref('')
const messages = ref<ChatMessage[]>([])
const drawerMeta = ref<{ title: string; user: string; agent: string; source: string }>({
  title: '',
  user: '',
  agent: '',
  source: '',
})
const drawerSessionId = ref('')
const feedbacks = ref<MessageFeedback[]>([])
// 预建 message_id+source -> feedback 的索引，避免逐条线性扫描
const feedbackMap = computed(() => {
  const m = new Map<string, MessageFeedback>()
  for (const f of feedbacks.value) m.set(`${f.message_id}:${f.from_source}`, f)
  return m
})
const annotatingId = ref('')
const annotateRating = ref(0)
const annotateContent = ref('')
const annotateSaving = ref(false)

const columns = [
  { colKey: 'title', title: t('conversationLogs.colTitle'), ellipsis: true, minWidth: 200 },
  { colKey: 'owner', title: t('conversationLogs.colUser'), width: 140 },
  { colKey: 'source', title: t('conversationLogs.colSource'), width: 120 },
  { colKey: 'agent', title: t('conversationLogs.colAgent'), width: 160, ellipsis: true },
  { colKey: 'user_feedback', title: t('conversationLogs.colUserFeedback'), width: 110 },
  { colKey: 'admin_feedback', title: t('conversationLogs.colAdminFeedback'), width: 110 },
  { colKey: 'messages', title: t('conversationLogs.colMessages'), width: 90 },
  { colKey: 'updated_at', title: t('conversationLogs.colUpdated'), width: 170 },
  { colKey: 'created_at', title: t('conversationLogs.colCreated'), width: 170 },
  { colKey: 'actions', title: t('conversationLogs.colActions'), width: 90 },
]

function userLabel(row: SessionListItem): string {
  if (row.owner_name) return row.owner_name
  if (row.im_platform) return row.im_platform
  if (row.user_id) {
    const id = row.user_id
    if (id.startsWith('api_')) return t('conversationLogs.sourceAPI')
    if (id.startsWith('embed')) return t('conversationLogs.sourceEmbed')
    return id
  }
  if (row.description && row.description.startsWith('embed')) return t('conversationLogs.sourceEmbed')
  return ''
}

function sourceLabel(row: SessionListItem): string {
  if (row.im_platform) return row.im_platform
  const desc = row.description || ''
  if (desc.startsWith('embed')) return t('conversationLogs.sourceEmbed')
  if (row.user_id && row.user_id.startsWith('api_')) return t('conversationLogs.sourceAPI')
  return t('conversationLogs.sourceWeb')
}

function sourceTheme(row: SessionListItem) {
  if (row.im_platform) return 'warning'
  if ((row.description || '').startsWith('embed')) return 'success'
  if (row.user_id && row.user_id.startsWith('api_')) return 'danger'
  return 'default'
}

function agentName(id?: string) {
  if (!id) return '—'
  return agentMap.value[id] || id
}

function formatTime(s?: string) {
  if (!s) return ''
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

async function fetchSessions() {
  loading.value = true
  try {
    const res: any = await listSessions({
      keyword: keyword.value || undefined,
      search_content: true,
      source: source.value || undefined,
      agent_id: agentId.value || undefined,
      start_time: dateRange.value?.[0] || undefined,
      end_time: dateRange.value?.[1] || undefined,
      sort_by: sortBy.value || undefined,
      order: sortOrder.value || undefined,
      page: page.value,
      page_size: pageSize.value,
    })
    sessions.value = res?.data || []
    total.value = res?.total || 0
  } catch (e: any) {
    MessagePlugin.error(e?.message || 'load failed')
  } finally {
    loading.value = false
  }
}

function onFilterChange() {
  page.value = 1
  fetchSessions()
  fetchStats()
}

async function fetchStats() {
  try {
    const res = await getSessionStats({
      keyword: keyword.value || undefined,
      search_content: true,
      source: source.value || undefined,
      agent_id: agentId.value || undefined,
      start_time: dateRange.value?.[0] || undefined,
      end_time: dateRange.value?.[1] || undefined,
      sort_by: sortBy.value || undefined,
    })
    if (res) stats.value = res
  } catch {
    // 统计失败不影响列表展示，保留上一次的聚合值
  }
}

// 关键词输入防抖后自动搜索（回车/清空立即触发）
let keywordTimer: number | null = null
watch(keyword, () => {
  if (keywordTimer) clearTimeout(keywordTimer)
  keywordTimer = window.setTimeout(() => {
    keywordTimer = null
    onFilterChange()
  }, 300)
})

onBeforeUnmount(() => {
  if (keywordTimer) clearTimeout(keywordTimer)
})

function onPageChange(p: { current: number; pageSize: number }) {
  page.value = p.current
  fetchSessions()
}

function onSortSelectChange() {
  onFilterChange()
}

function toggleSortOrder() {
  sortOrder.value = sortOrder.value === 'asc' ? 'desc' : 'asc'
  onFilterChange()
}

function buildMeta(row: SessionListItem) {
  return {
    title: row.title || t('conversationLogs.emptyMessages'),
    user: userLabel(row) || '—',
    agent: agentName(row.last_request_state?.agent_id),
    source: sourceLabel(row),
  }
}

async function openSession(row: SessionListItem) {
  drawerMeta.value = buildMeta(row)
  drawerSessionId.value = row.id
  await loadMessages(row.id)
}

async function loadMessages(id: string) {
  drawerVisible.value = true
  drawerLoading.value = true
  messages.value = []
  drawerError.value = ''
  try {
    const res: any = await loadSessionMessages(id)
    messages.value = res?.data || []
    await loadFeedbacks(id)
  } catch (e: any) {
    drawerError.value = e?.message || 'load failed'
    MessagePlugin.error(t('conversationLogs.detailError'))
  } finally {
    drawerLoading.value = false
  }
}

function feedbackOf(messageId: string, source: 'user' | 'admin') {
  return feedbackMap.value.get(`${messageId}:${source}`)
}

async function loadFeedbacks(id: string) {
  try {
    const res = await listSessionFeedbacks(id)
    feedbacks.value = res.data || []
  } catch {
    feedbacks.value = []
  }
}

function startAnnotate(m: ChatMessage) {
  const ex = feedbackOf(m.id, 'admin')
  annotatingId.value = m.id
  annotateRating.value = ex?.rating ?? 0
  annotateContent.value = ex?.content ?? ''
}

async function saveAnnotate(m: ChatMessage) {
  if (!drawerSessionId.value) return
  annotateSaving.value = true
  try {
    await saveSessionFeedback(drawerSessionId.value, {
      message_id: m.id,
      from_source: 'admin',
      rating: annotateRating.value,
      content: annotateContent.value,
    })
    MessagePlugin.success(t('conversationLogs.annotateSaved'))
    annotatingId.value = ''
    await loadFeedbacks(drawerSessionId.value)
  } catch (e: any) {
    MessagePlugin.error(e?.message || 'save failed')
  } finally {
    annotateSaving.value = false
  }
}

async function removeAnnotate(m: ChatMessage) {
  const ex = feedbackOf(m.id, 'admin')
  if (!ex || !drawerSessionId.value) return
  try {
    await deleteSessionFeedback(drawerSessionId.value, ex.id)
    MessagePlugin.success(t('conversationLogs.annotateRemoved'))
    await loadFeedbacks(drawerSessionId.value)
  } catch (e: any) {
    MessagePlugin.error(e?.message || 'delete failed')
  }
}

onMounted(async () => {
  try {
    const res: any = await listAgents({ creator: 'all' })
    const payload = res?.data ?? res
    const list = payload?.data ?? payload ?? []
    agents.value = (Array.isArray(list) ? list : []).map((a: any) => ({ id: a.id, name: a.name }))
  } catch {
    agents.value = []
  }
  fetchSessions()
  fetchStats()
})
</script>

<style scoped>
@import (reference) '@/components/css/settings-section.less';

.conversation-logs {
  width: 100%;
}
.toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  margin-bottom: 16px;
}
.stats-row {
  display: flex;
  gap: 16px;
  margin-bottom: 16px;
}
.stat-card {
  flex: 1;
  min-width: 0;
  background: var(--td-bg-color-secondarycontainer);
  border-radius: var(--app-radius-md);
  padding: 16px 20px;
}
.stat-value {
  font-size: 26px;
  font-weight: 700;
  color: var(--td-brand-color);
  line-height: 1.2;
}
.stat-label {
  margin-top: 4px;
  font-size: var(--app-text-md);
  color: var(--td-text-color-secondary);
}
.filters {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  flex: 1;
}
.filter-item {
  width: 200px;
}
.source-select {
  width: 130px;
}
.agent-select {
  width: 160px;
}
.time-select {
  width: 250px;
}
.search-input {
  flex: 1;
  min-width: 200px;
}
.sort-group {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-left: auto;
}
.sort-label {
  color: var(--td-text-color-secondary);
  font-size: 13px;
  white-space: nowrap;
}
.sort-select {
  width: 120px;
}
.sort-dir {
  width: 36px;
  padding: 0;
}
.empty {
  padding: 40px;
  text-align: center;
  color: var(--td-text-color-placeholder);
}
.pager {
  margin-top: 16px;
  justify-content: flex-end;
}
.search-results {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.search-count {
  color: var(--td-text-color-secondary);
  margin-bottom: 4px;
}
.search-item {
  border: 1px solid var(--td-component-border);
  border-radius: 8px;
  padding: 12px;
  cursor: pointer;
}
.search-item:hover {
  border-color: var(--td-brand-color);
}
.search-session {
  font-weight: 600;
  margin-bottom: 6px;
}
.qa-q {
  margin-bottom: 4px;
}
.muted {
  color: var(--td-text-color-placeholder);
}

/* Detail drawer: chat transcript */
.chat {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.chat-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 24px;
  padding: 12px 14px;
  background: var(--td-bg-color-page);
  border-radius: 8px;
}
.meta-item {
  font-size: 13px;
}
.meta-k {
  color: var(--td-text-color-secondary);
  margin-right: 6px;
}
.meta-v {
  font-weight: 500;
}
.chat-body {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.bubble-row {
  display: flex;
  gap: 10px;
  align-items: flex-start;
}
.bubble-row.is-user {
  flex-direction: row-reverse;
}
.bubble-avatar {
  flex: 0 0 auto;
  width: 32px;
  height: 32px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  font-weight: 700;
  color: #fff;
}
.bubble-avatar.is-assistant {
  background: var(--td-brand-color);
}
.bubble-avatar.is-user {
  background: var(--td-success-color);
}
.bubble {
  max-width: 78%;
}
.bubble-head {
  display: flex;
  gap: 10px;
  align-items: baseline;
  font-size: 12px;
  color: var(--td-text-color-secondary);
  margin-bottom: 4px;
}
.bubble-role {
  font-weight: 600;
}
.bubble-content {
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.65;
  padding: 10px 14px;
  border-radius: 10px;
  background: var(--td-bg-color-page);
}
.bubble-row.is-user .bubble-content {
  background: var(--td-brand-color-light);
}
.drawer-loading {
  padding: 20px;
}
.drawer-error {
  padding: 24px;
  text-align: center;
}
.drawer-error-title {
  font-weight: 600;
  color: var(--td-error-color);
  margin-bottom: 8px;
}
.drawer-error-hint {
  color: var(--td-text-color-secondary);
  font-size: 13px;
  line-height: 1.6;
}

/* Per-message feedback bar */
.msg-feedback {
  margin-top: 8px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  font-size: 12px;
}
.fb-up {
  color: var(--td-success-color);
}
.fb-down {
  color: var(--td-error-color);
}
.fb-user {
  color: var(--td-text-color-secondary);
}
.fb-user-note {
  margin-left: 4px;
  padding: 2px 8px;
  border-radius: 4px;
  background: var(--td-bg-color-page);
  color: var(--td-text-color-primary);
}
.fb-admin-note {
  background: var(--td-warning-color-light);
  color: var(--td-warning-color);
  padding: 2px 8px;
  border-radius: 6px;
  max-width: 100%;
  word-break: break-word;
}
.fb-cell {
  display: inline-flex;
  gap: 8px;
  align-items: center;
}
.fb-admin {
  color: var(--td-warning-color);
}
.annotate-box {
  margin-top: 6px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 100%;
}
.annotate-actions {
  display: flex;
  gap: 8px;
}
</style>
