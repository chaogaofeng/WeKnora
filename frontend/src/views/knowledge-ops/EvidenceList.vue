<template>
  <div class="evidence-list">
    <details
      v-for="item in items"
      :id="`ops-evidence-${item.id}`"
      :key="item.id"
      class="evidence-item"
      tabindex="-1"
    >
      <summary>
        <span class="evidence-title">{{
          item.title || evidenceKind(item.kind)
        }}</span>
        <span class="evidence-kind"
          >{{ evidenceKind(item.kind) }} · {{ k('version') }}
          {{ item.version || k('unknown') }}</span
        >
      </summary>
      <div class="evidence-body">
        <p class="evidence-meta">
          {{ k('acquired') }} {{ date(item.acquired_at) }}
          <template v-if="!metadataOnly">
            · {{ k(item.truncated ? 'excerptOnly' : 'notTruncated') }}</template
          >
        </p>
        <template v-if="!metadataOnly">
          <pre v-if="item.content">{{ item.content }}</pre>
          <p v-else class="evidence-meta">{{ k('evidenceBodyUnavailable') }}</p>
        </template>
        <dl>
          <dt>{{ k('sourceMetadata') }}</dt>
          <dd>
            {{
              item.slug ||
              item.page_id ||
              item.chunk_id ||
              item.knowledge_id ||
              '—'
            }}
          </dd>
          <dt>{{ k('sourceKBAndDocument') }}</dt>
          <dd>{{ item.kb_id || '—' }} / {{ item.knowledge_id || '—' }}</dd>
          <dt>{{ k('sourcePageAndChunk') }}</dt>
          <dd>{{ item.page_id || '—' }} / {{ item.chunk_id || '—' }}</dd>
          <dt>{{ k('sourceToolAndRound') }}</dt>
          <dd>{{ item.tool_call_id || '—' }} / {{ item.round }}</dd>
        </dl>
      </div>
    </details>
  </div>
</template>
<script setup lang="ts">
import type { OpsEvidence } from '@/api/knowledge-ops'
import { useOpsText } from './locales'
defineProps<{ items: OpsEvidence[]; metadataOnly?: boolean }>()
const { k, label, date } = useOpsText()
function evidenceKind(kind: string) {
  const key = `evidenceKind_${kind}`
  return k(key) === key ? label(kind) : k(key)
}
</script>
<style scoped>
.evidence-item {
  border-top: 1px solid var(--td-component-border);
  scroll-margin-block: var(--app-space-5);
}
.evidence-item:first-child {
  border-top: 0;
}
summary {
  cursor: pointer;
  padding: var(--app-space-3) 0;
  font-size: var(--app-text-base);
}
.evidence-title {
  font-weight: 500;
  overflow-wrap: anywhere;
}
.evidence-kind {
  margin-left: var(--app-space-3);
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
}
.evidence-meta {
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
  margin: 0 0 var(--app-space-3);
}
.evidence-body {
  padding-bottom: var(--app-space-3);
}
pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  max-height: 400px;
  overflow: auto;
  font: inherit;
  font-size: var(--app-text-sm);
  padding: var(--app-space-4);
  background: var(--td-bg-color-secondarycontainer);
  border-radius: var(--app-radius-md);
}
dl {
  display: grid;
  grid-template-columns: 130px minmax(0, 1fr);
  gap: var(--app-space-2);
  font-size: var(--app-text-sm);
}
dt {
  color: var(--td-text-color-secondary);
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.evidence-item:focus,
summary:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 3px;
  border-radius: var(--app-radius-sm);
}
@media (max-width: 700px) {
  .evidence-kind {
    display: block;
    margin-left: var(--app-space-4);
  }
  dl {
    grid-template-columns: 1fr;
    gap: var(--app-space-1);
  }
}
</style>
