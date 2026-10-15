<template>
  <section class="query-context">
    <div class="query-heading">
      <div class="query-title">
        <h3>{{ k('queryContextTitle') }}</h3>
        <t-popup
          v-model:visible="showHelp"
          trigger="click"
          placement="bottom-left"
          destroy-on-close
          :show-arrow="true"
        >
          <t-button
            class="query-help"
            size="small"
            variant="text"
            theme="default"
            shape="square"
            :aria-label="k('queryJudgmentHelp')"
            :aria-expanded="showHelp"
            @keydown.esc.stop="showHelp = false"
          ><t-icon name="help-circle" /></t-button>
          <template #content>
            <div class="query-help-content" @keydown.esc.stop="showHelp = false">
              {{ k('queryJudgmentNotice') }}
            </div>
          </template>
        </t-popup>
      </div>
      <div class="query-badges">
        <t-tag v-if="hasRewriteHistory" variant="light">
          {{ k('queryRewriteHistory') }} {{ rewrite?.history_turns }} {{ k('queryHistoryUnit') }}
        </t-tag>
        <t-tag variant="light" :theme="rewriteStatus === 'failed' ? 'warning' : 'default'">
          {{ k(`queryRewrite_${rewriteStatus}`) }}
        </t-tag>
      </div>
    </div>
    <p v-if="!run.query_context" class="muted">{{ k('queryContextUnrecorded') }}</p>
    <template v-else>
      <div v-if="showRewriteText" class="query-comparison">
        <div>
          <span class="field-label">{{ k('queryOriginal') }}</span>
          <p>{{ run.question }}</p>
        </div>
        <div class="rewritten-query">
          <span class="field-label">{{ k('queryRewritten') }}</span>
          <p>{{ rewrite?.query }}</p>
        </div>
      </div>
      <p v-else class="muted">{{ k(`queryRewriteHint_${rewriteStatus}`) }}</p>
      <p v-if="rewrite?.truncated || rewrite?.redacted || run.query_context.truncated" class="muted">
        {{ k('queryContextLimited') }}
      </p>
      <p v-if="retrieval?.status === 'skipped' || retrieval?.status === 'required'" class="query-context-meta">
        {{ k(retrieval.status === 'skipped' ? 'queryRetrievalSkipped' : 'queryRetrievalRequired') }}
        <span v-if="retrieval.reason === 'context_only_follow_up'"> · {{ k('queryRetrievalContextOnly') }}</span>
      </p>
      <details v-if="hasProcessingDetails" class="processing-details">
        <summary>{{ k('queryProcessingDetails') }}</summary>
        <p v-if="hasRewriteHistory" class="muted">{{ k('queryRewriteHistoryIDsUnknown') }}</p>
        <p v-if="rewrite?.history_status === 'unavailable'" class="muted">{{ k('queryHistoryUnavailable') }}</p>
        <div v-if="rewrite?.has_images || rewrite?.has_attachments || rewrite?.has_memory" class="processing-inputs muted">
          <span v-if="rewrite.has_images">{{ k('queryImageContext') }}</span>
          <span v-if="rewrite.has_attachments">{{ k('queryAttachmentContext') }}</span>
          <span v-if="rewrite.has_memory">{{ k('queryMemoryContext') }}</span>
        </div>
        <p v-if="run.history_context?.ids_known && run.history_context.turns > 0" class="muted">
          {{ k('queryGenerationHistoryBound') }}
        </p>
        <template v-if="searchQueries.length">
          <h4>{{ k('querySearchCalls') }} · {{ searchQueries.length }}</h4>
          <p class="muted">{{ k('querySearchNotice') }}</p>
          <ol>
            <li v-for="(query, index) in searchQueries" :key="`${query.tool_call_id || query.tool}-${index}`">
              <div class="search-meta">
                <span>{{ k(`queryTool_${query.tool}`) }}</span>
                <span v-if="query.round">{{ k('queryAgentRound') }} {{ query.round }}</span>
                <span v-if="query.truncated || query.redacted">{{ k('queryTextLimited') }}</span>
              </div>
              <p>{{ query.query }}</p>
            </li>
          </ol>
        </template>
        <p v-else-if="retrieval?.status !== 'skipped'" class="muted">{{ k('querySearchNotRecorded') }}</p>
      </details>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { OpsRun } from '@/api/knowledge-ops'
import { useOpsText } from './locales'
const props = defineProps<{ run: OpsRun }>()
const { k } = useOpsText()
const showHelp = ref(false)
const rewrite = computed(() => props.run.query_context?.rewrite)
const retrieval = computed(() => props.run.query_context?.retrieval)
const rewriteStatus = computed(() => rewrite.value?.status || 'unrecorded')
const searchQueries = computed(() => props.run.query_context?.searches || [])
const showRewriteText = computed(() => rewrite.value?.status === 'rewritten')
const hasRewriteHistory = computed(() =>
  rewrite.value?.history_status === 'available' && rewrite.value.history_turns > 0,
)
const hasProcessingDetails = computed(() =>
  searchQueries.value.length > 0 || retrieval.value?.status !== 'skipped' ||
  hasRewriteHistory.value || rewrite.value?.history_status === 'unavailable' ||
  rewrite.value?.has_images || rewrite.value?.has_attachments || rewrite.value?.has_memory ||
  (props.run.history_context?.ids_known && props.run.history_context.turns > 0),
)
</script>

<style scoped>
.query-context {
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-xl);
  padding: var(--app-space-5);
  background: var(--td-bg-color-container);
  overflow-wrap: anywhere;
}
.query-heading, .query-title, .query-badges, .search-meta, .processing-inputs {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--app-space-2) var(--app-space-3);
}
.query-heading { justify-content: space-between; margin-bottom: var(--app-space-3); }
h3 { margin: 0; font-size: var(--app-text-md); font-weight: 600; }
p { margin: var(--app-space-2) 0 0; line-height: 1.7; white-space: pre-wrap; }
.muted, .query-context-meta, .search-meta, .field-label {
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
}
.query-comparison { display: grid; grid-template-columns: 1fr 1fr; gap: var(--app-space-3); }
.query-comparison > div { min-width: 0; padding: var(--app-space-3); border-radius: var(--app-radius-md); background: var(--td-bg-color-secondarycontainer); }
.query-comparison p { font-size: var(--app-text-md); }
.query-comparison .rewritten-query { border-inline-start: 2px solid var(--td-brand-color); }
.query-title { gap: var(--app-space-1); }
.query-help { color: var(--td-text-color-secondary); }
.query-help-content {
  width: min(320px, calc(100vw - 64px));
  padding: var(--app-space-1);
  line-height: 1.7;
  font-size: var(--app-text-sm);
  color: var(--td-text-color-primary);
}
.processing-inputs { margin-top: var(--app-space-2); }
h4 { margin: var(--app-space-3) 0 0; font-size: var(--app-text-sm); font-weight: 500; }
.processing-details { margin-top: var(--app-space-3); border-top: 1px solid var(--td-component-stroke); padding-top: var(--app-space-3); }
summary { cursor: pointer; font-size: var(--app-text-sm); font-weight: 500; }
summary:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: var(--app-space-1); }
ol { padding-left: var(--app-space-5); margin: var(--app-space-3) 0 0; }
li + li { margin-top: var(--app-space-3); }
@media (max-width: 640px) { .query-comparison { grid-template-columns: 1fr; } }
</style>
