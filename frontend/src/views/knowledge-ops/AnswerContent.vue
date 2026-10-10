<template><div class="answer-content" v-html="html" /></template>
<script setup lang="ts">
import { computed } from 'vue'
import { renderDocumentPreviewMarkdown } from '@/utils/documentPreviewMarkdown'
const props = defineProps<{ content: string }>()
const html = computed(() => renderDocumentPreviewMarkdown(props.content))
</script>
<style scoped>
.answer-content {
  line-height: 1.8;
  overflow-wrap: anywhere;
}
.answer-content :deep(:first-child) {
  margin-top: 0;
}
.answer-content :deep(:last-child) {
  margin-bottom: 0;
}
.answer-content :deep(h1),
.answer-content :deep(h2),
.answer-content :deep(h3) {
  font-size: var(--app-text-lg);
  font-weight: 600;
  margin: var(--app-space-5) 0 var(--app-space-2);
}
.answer-content :deep(p),
.answer-content :deep(ul),
.answer-content :deep(ol) {
  margin: var(--app-space-2) 0;
}
.answer-content :deep(ul),
.answer-content :deep(ol) {
  padding-left: var(--app-space-5);
}
.answer-content :deep(a) {
  color: var(--td-brand-color);
}
.answer-content :deep(img) {
  max-width: 100%;
}
.answer-content :deep(pre) {
  max-width: 100%;
  overflow: auto;
  padding: var(--app-space-3);
  background: var(--td-bg-color-secondarycontainer);
  border-radius: var(--app-radius-md);
}
.answer-content :deep(table) {
  display: block;
  max-width: 100%;
  overflow: auto;
  border-collapse: collapse;
}
.answer-content :deep(td),
.answer-content :deep(th) {
  padding: var(--app-space-2);
  border: 1px solid var(--td-component-border);
}
.answer-content :deep(blockquote) {
  margin: var(--app-space-3) 0;
  padding-left: var(--app-space-4);
  border-left: 3px solid var(--td-component-stroke);
  color: var(--td-text-color-secondary);
}
</style>
