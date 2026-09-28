<template>
 <div class="insights">
  <section class="coverage"><h2>{{ k('coverageMatrix') }}</h2><table><thead><tr><th>{{ k('qa_type') }}</th><th>{{ k('total') }}</th><th>{{ k('complete') }}</th><th>{{ k('partial') }}</th><th>{{ k('missing') }}</th></tr></thead><tbody><tr v-for="(row, mode) in stats.coverage_by_mode || {}" :key="mode"><th>{{ label(String(mode)) }}</th><td>{{ row.total }}</td><td>{{ row.complete }}</td><td>{{ row.partial }}</td><td>{{ row.missing }}</td></tr></tbody></table></section>
  <section><h2>{{ k('sourceStats') }}</h2><p v-for="status in ['consistent','inconsistent','unresolved','not_checked']" :key="status" class="stat"><span>{{ label(status) }}</span><strong>{{ stats.source_checks?.[status] || 0 }}</strong></p></section>
  <section><h2>{{ k('causeStats') }}</h2><p v-if="!Object.keys(stats.causes || {}).length" class="muted">{{ k('noCause') }}</p><p v-for="(count, category) in stats.causes || {}" :key="category" class="stat"><span>{{ label(String(category)) }}</span><strong>{{ count }}</strong></p></section>
  <section><h2>{{ k('sampleStats') }}</h2><p class="stat"><span>{{ k('sampleTotal') }}</span><strong>{{ stats.sample_total || 0 }}</strong></p><p class="stat"><span>{{ k('sampleReviewed') }}</span><strong>{{ stats.sample_reviewed || 0 }}</strong></p><p class="stat"><span>{{ k('sampleCorrected') }}</span><strong>{{ stats.sample_corrected || 0 }}</strong></p><p class="muted">{{ k('sampleNotice') }}</p></section>
 </div>
</template>
<script setup lang="ts">
import type { OpsOverview } from '@/api/knowledge-ops'
import { useOpsText } from './locales'
defineProps<{ stats: OpsOverview }>()
const { k, label } = useOpsText()
</script>
<style scoped>
.insights{display:grid;grid-template-columns:1fr 1fr;gap:16px;margin:20px 0}section{background:var(--td-bg-color-container);border:1px solid var(--td-component-border);border-radius:10px;padding:20px;overflow:auto}h2{font-size:17px;margin:0 0 16px}.stat{display:flex;justify-content:space-between;margin:12px 0}.muted{font-size:13px;color:var(--td-text-color-secondary)}table{width:100%;border-collapse:collapse;font-size:13px}th,td{text-align:left;padding:12px 8px;border-bottom:1px solid var(--td-component-border)}@media(max-width:1000px){.insights{grid-template-columns:1fr}}
</style>
