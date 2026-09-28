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
.insights{display:grid;grid-template-columns:1fr 1fr;gap:16px;margin:20px 0}section{background:var(--td-bg-color-container);border:1px solid var(--td-component-stroke);border-radius:12px;padding:22px;overflow:auto}h2{font-size:15px;margin:0 0 20px;font-weight:600}.stat{display:flex;justify-content:space-between;margin:16px 0;font-size:13px}.muted{font-size:12px;color:var(--td-text-color-secondary);line-height:1.7}table{width:100%;border-collapse:collapse;font-size:13px}th,td{text-align:left;padding:12px 8px;border-bottom:1px solid var(--td-component-stroke)}@media(max-width:1000px){.insights{grid-template-columns:1fr}}
thead th{font-size:12px;color:var(--td-text-color-secondary);font-weight:500}tbody th{font-weight:500}tbody tr:last-child th,tbody tr:last-child td{border-bottom:0}.stat strong{font-weight:600}table{font-variant-numeric:tabular-nums}
</style>
