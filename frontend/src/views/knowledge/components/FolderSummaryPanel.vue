<template>
  <div class="folder-summary-panel">
    <div class="panel-header">
      <div class="panel-title">
        <span>{{ $t('knowledgeBase.folderSummary.title') }}</span>
        <t-tooltip :content="$t('knowledgeBase.folderSummary.tooltip')" placement="top">
          <t-icon name="info-circle" size="14px" class="hint-icon" />
        </t-tooltip>
      </div>
      <div class="panel-actions">
        <t-button size="small" variant="outline" :loading="generating" @click="onGenerate">
          {{ $t('knowledgeBase.folderSummary.generate') }}
        </t-button>
        <t-button size="small" variant="text" @click="refreshTree">
          {{ $t('knowledgeBase.folderSummary.refreshList') }}
        </t-button>
      </div>
    </div>

    <div v-if="loading" class="panel-body">
      <t-loading size="small" :text="$t('common.loading')" />
    </div>
    <div v-else-if="!items.length" class="panel-body empty">
      {{ $t('knowledgeBase.folderSummary.empty') }}
    </div>
    <div v-else class="panel-body">
      <div
        v-for="item in items"
        :key="item.slug"
        class="summary-row"
        :style="{ paddingLeft: `${8 + item.depth * 16}px` }"
      >
        <span class="row-name" :title="item.path">{{ displayName(item.path) }}</span>
        <span class="row-files">{{ item.files }}</span>
        <span class="row-state" :class="`state-${item.state}`">{{ stateLabel(item.state) }}</span>
        <span v-if="formatTime(item.updated_at)" class="row-time" :title="item.updated_at">
          {{ formatTime(item.updated_at) }}
        </span>
        <t-button
          v-if="item.state !== 'fresh' && item.path"
          size="small"
          variant="text"
          theme="primary"
          :loading="refreshing === item.path"
          @click="onRefresh(item)"
        >
          {{ $t('knowledgeBase.folderSummary.refreshOne') }}
        </t-button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue';
import { MessagePlugin } from 'tdesign-vue-next';
import { useI18n } from 'vue-i18n';
import {
  listFolderSummaryTree,
  generateFolderSummaries,
  refreshFolderSummaries,
  type FolderSummaryItem,
} from '@/api/knowledge-base/index';

const props = defineProps<{ kbId: string }>();
const { t } = useI18n();

const loading = ref(false);
const generating = ref(false);
const refreshing = ref('');
const items = ref<FolderSummaryItem[]>([]);

// Generation runs as an async task (asynq + LLM call) whose latency is
// unbounded; a single fixed 3s re-fetch usually fires before the worker
// finishes and the panel keeps showing stale states. Poll a few times with
// increasing backoff instead — long enough to cover a slow LLM, bounded so
// we never spin forever. A poll that observes a non-missing state flips to
// fresh/stale early-stops the chain; the panel also stops polling when it
// is unmounted (popup closed).
let pollTimers: ReturnType<typeof setTimeout>[] = [];
const cancelPendingPolls = () => {
  pollTimers.forEach(clearTimeout);
  pollTimers = [];
};
const scheduleStatePolls = () => {
  cancelPendingPolls();
  // 3s + 5s + 8s + 13s ≈ covers typical LLM latencies (a single qwen call
  // measured ~12s locally); stops after 4 attempts whatever the result.
  const delays = [3000, 5000, 8000, 13000];
  delays.forEach((delay) => {
    pollTimers.push(setTimeout(() => {
      // Skip silently when the popup is gone (component unmounted) or a
      // generation pass is still running — onGenerate/onRefresh re-arm the
      // chain themselves when their own request completes.
      if (generating.value || refreshing.value) return;
      refreshTree();
    }, delay));
  });
};

const displayName = (path: string) => (path ? path : t('knowledgeBase.folderSummary.root'));

// De-emphasized generation timestamp next to the state label: relative time
// ("3 分钟前") inline, full timestamp in the hover title. Missing states have
// no updated_at (nothing was ever generated) and render no time at all.
const formatTime = (updatedAt?: string): string => {
  if (!updatedAt) return '';
  const ts = new Date(updatedAt).getTime();
  if (Number.isNaN(ts)) return '';
  const diffMs = Date.now() - ts;
  if (diffMs < 0) return '';
  const minutes = Math.floor(diffMs / 60_000);
  if (minutes < 1) return t('knowledgeBase.folderSummary.justNow');
  if (minutes < 60) return t('knowledgeBase.folderSummary.minutesAgo', { n: minutes });
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return t('knowledgeBase.folderSummary.hoursAgo', { n: hours });
  const days = Math.floor(hours / 24);
  if (days < 30) return t('knowledgeBase.folderSummary.daysAgo', { n: days });
  // Beyond a month, show a compact absolute date — relative "90 天前" is
  // less useful than knowing it was made in August.
  const d = new Date(ts);
  return `${d.getFullYear()}/${String(d.getMonth() + 1).padStart(2, '0')}/${String(d.getDate()).padStart(2, '0')}`;
};

const stateLabel = (state: FolderSummaryItem['state']) =>
  t(`knowledgeBase.folderSummary.state.${state}`);

const refreshTree = async () => {
  loading.value = true;
  try {
    const res = await listFolderSummaryTree(props.kbId);
    items.value = res?.data?.items ?? [];
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('knowledgeBase.folderSummary.loadFailed'));
  } finally {
    loading.value = false;
  }
};

const onGenerate = async () => {
  generating.value = true;
  try {
    await generateFolderSummaries(props.kbId);
    MessagePlugin.success(t('knowledgeBase.folderSummary.generateScheduled'));
    scheduleStatePolls();
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('knowledgeBase.folderSummary.generateFailed'));
  } finally {
    generating.value = false;
  }
};

const onRefresh = async (item: FolderSummaryItem) => {
  refreshing.value = item.path;
  try {
    await refreshFolderSummaries(props.kbId, item.path);
    MessagePlugin.success(t('knowledgeBase.folderSummary.generateScheduled'));
    scheduleStatePolls();
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('knowledgeBase.folderSummary.generateFailed'));
  } finally {
    refreshing.value = '';
  }
};

onMounted(refreshTree);
onUnmounted(cancelPendingPolls);
defineExpose({ refreshTree });
</script>

<style lang="less" scoped>
.folder-summary-panel {
  border: 1px solid var(--td-component-stroke);
  border-radius: 6px;
  padding: 12px;
  background: var(--td-bg-color-container);
}

.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}

.panel-title {
  display: flex;
  align-items: center;
  gap: 4px;
  font-weight: 500;
  color: var(--td-text-color-primary);

  .hint-icon {
    color: var(--td-text-color-placeholder);
    cursor: help;
  }
}

.panel-actions {
  display: flex;
  gap: 8px;
}

.panel-body.empty {
  color: var(--td-text-color-placeholder);
  padding: 16px 0;
  text-align: center;
}

.summary-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
  font-size: 13px;

  .row-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--td-text-color-primary);
  }

  .row-files {
    color: var(--td-text-color-secondary);
    font-size: 12px;
  }

  // Intentionally quieter than the state label — presence, not prominence.
  .row-time {
    color: var(--td-text-color-placeholder);
    font-size: 12px;
    flex-shrink: 0;
  }

  .row-state {
    font-size: 12px;

    &.state-missing { color: var(--td-text-color-placeholder); }
    &.state-fresh { color: var(--td-success-color); }
    &.state-stale { color: var(--td-warning-color); }
    &.state-manual_edit { color: var(--td-brand-color); }
  }
}
</style>
