<template>
  <t-popup
    v-if="run.query_context?.rewrite?.status === 'rewritten'"
    v-model:visible="visible"
    trigger="click"
    placement="bottom-left"
    destroy-on-close
    :show-arrow="true"
  >
    <t-button
      class="rewrite-badge"
      size="small"
      variant="text"
      theme="default"
      :aria-label="k('queryCompare')"
      :aria-expanded="visible"
      @keydown.esc.stop="visible = false"
    >{{ k('queryRewriteBadge') }}</t-button>
    <template #content>
      <div class="rewrite-preview" @keydown.esc.stop="visible = false">
        <h4>{{ k('queryCompare') }}</h4>
        <div class="rewrite-texts">
          <div>
            <span class="field-label">{{ k('queryOriginal') }}</span>
            <p>{{ run.question }}</p>
          </div>
          <div>
            <span class="field-label">{{ k('queryRewritten') }}</span>
            <p>{{ run.query_context.rewrite.query }}</p>
          </div>
        </div>
        <p v-if="run.query_context.rewrite.truncated || run.query_context.rewrite.redacted" class="muted">
          {{ k('queryContextLimited') }}
        </p>
        <t-button variant="text" theme="primary" size="small" @click="openDetail">
          {{ k('detail') }}
        </t-button>
      </div>
    </template>
  </t-popup>
</template>
<script setup lang="ts">
import { ref } from 'vue'
import type { OpsRun } from '@/api/knowledge-ops'
import { useOpsText } from './locales'
const emit = defineEmits<{ open: [id: string] }>()
const props = defineProps<{ run: OpsRun }>()
const { k } = useOpsText()
const visible = ref(false)
function openDetail() {
  visible.value = false
  emit('open', props.run.id)
}
</script>
<style scoped>
.rewrite-badge {
  flex-shrink: 0;
  height: 22px;
  padding: 0 var(--app-space-2);
  border-radius: var(--app-radius-sm);
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
  font-weight: 400;
}
.rewrite-preview {
  width: min(420px, calc(100vw - 48px));
  padding: var(--app-space-2);
  color: var(--td-text-color-primary);
}
h4 { margin: 0 0 var(--app-space-3); font-size: var(--app-text-md); }
.rewrite-texts {
  display: grid;
  gap: var(--app-space-3);
  max-height: min(360px, 55vh);
  overflow: auto;
}
.rewrite-texts > div {
  padding: var(--app-space-3);
  background: var(--td-bg-color-secondarycontainer);
  border-radius: var(--app-radius-md);
}
p {
  margin: var(--app-space-1) 0 0;
  font-size: var(--app-text-md);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  line-height: 1.6;
}
.field-label, .muted { color: var(--td-text-color-secondary); font-size: var(--app-text-sm); }
.rewrite-preview > .t-button { margin-top: var(--app-space-3); }
</style>
