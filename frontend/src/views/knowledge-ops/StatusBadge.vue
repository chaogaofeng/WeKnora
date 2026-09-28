<template><span class="ops-badge" :class="tone"><i aria-hidden="true" />{{ text || (verdict ? labelVerdict(value) : label(value)) }}</span></template>
<script setup lang="ts">
import { computed } from 'vue'
import { useOpsText } from './locales'
const props = defineProps<{ value?: string; text?: string; verdict?: boolean }>()
const { label, labelVerdict } = useOpsText()
const tone = computed(() => {
 if (['high','failed','contradicted','inconsistent'].includes(props.value || '')) return 'danger'
 if (['partial','medium','suspected','needs_evidence','needs_more_evidence','unresolved','unsupported','human_review'].includes(props.value || '')) return 'warning'
 if (['complete','supported','consistent','succeeded','reviewed','confirmed'].includes(props.value || '')) return 'success'
 if (['quality_sample','running','pending','partially_reviewed'].includes(props.value || '')) return 'accent'
 return 'neutral'
})
</script>
<style scoped>
.ops-badge{display:inline-flex;align-items:center;gap:6px;white-space:nowrap;padding:3px 8px;border-radius:5px;font-size:12px;line-height:20px;font-weight:500;background:var(--td-bg-color-secondarycontainer);color:var(--td-text-color-secondary)}i{width:5px;height:5px;border-radius:50%;background:currentColor;flex:none}.success{color:var(--td-success-color-7);background:var(--td-success-color-light)}.warning{color:var(--td-warning-color-7);background:var(--td-warning-color-light)}.danger{color:var(--td-error-color-7);background:var(--td-error-color-light)}.accent{color:var(--td-brand-color-7);background:var(--td-brand-color-light)}:global([theme-mode='dark']) .success{color:var(--td-success-color)}:global([theme-mode='dark']) .warning{color:var(--td-warning-color)}:global([theme-mode='dark']) .danger{color:var(--td-error-color)}:global([theme-mode='dark']) .accent{color:var(--td-brand-color)}
</style>
