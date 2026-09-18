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
import { ref, onMounted } from 'vue';
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

const displayName = (path: string) => (path ? path : t('knowledgeBase.folderSummary.root'));

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
    setTimeout(refreshTree, 3000);
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
    setTimeout(refreshTree, 3000);
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('knowledgeBase.folderSummary.generateFailed'));
  } finally {
    refreshing.value = '';
  }
};

onMounted(refreshTree);
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

  .row-state {
    font-size: 12px;

    &.state-missing { color: var(--td-text-color-placeholder); }
    &.state-fresh { color: var(--td-success-color); }
    &.state-stale { color: var(--td-warning-color); }
    &.state-manual_edit { color: var(--td-brand-color); }
  }
}
</style>
