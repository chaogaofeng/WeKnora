<template>
  <section class="queue-panel">
    <header><div><h2>{{ k('queue') }}</h2><p>{{ k('queueIntro') }}</p></div><button v-if="selectedGroup" @click="emit('group', '')">{{ k('allGroups') }}</button><t-checkbox v-else :checked="grouped" @change="emit('toggle')">{{ k('groupView') }}</t-checkbox></header>
    <p class="notice">{{ k('groupNotice') }}</p>
    <p v-if="selectedGroup" class="muted">{{ k('groupSelected') }}: {{ selectedGroup }}</p>
    <div v-if="!rows.length" class="empty">{{ k('empty') }}</div>
    <article v-for="row in rows" :key="row.run.id" class="queue-case">
      <div class="case-heading"><StatusBadge :value="row.run.analysis?.report.risk" :text="`${k('risk')} · ${label(row.run.analysis?.report.risk)}`" /><span>{{ label(row.run.analysis?.route) }}</span><span v-if="row.run.analysis?.status === 'failed' || ['failed','cancelled'].includes(row.run.status)" class="failure">{{ k('technicalIssue') }}</span><small>{{ date(row.run.analysis?.created_at || row.run.started_at) }}</small></div>
      <div class="case-content"><div><h3>{{ row.run.question }}</h3><p class="muted">{{ label(row.run.qa_type) }} · {{ row.run.channel }} · {{ label(row.run.capture_status) }} · {{ label(row.run.review_state) }}</p><p><strong>{{ k('verdict') }}:</strong> {{ labelVerdict(row.run.analysis?.report.verdict) }} · {{ k('sourceCheck') }}: {{ label(row.run.analysis?.report.source_check) }}</p><p v-if="row.count > 1">{{ k('relatedCount') }}: <strong>{{ row.count }}</strong></p></div>
        <div class="diagnosis"><h4>{{ k('diagnosisSummary') }}</h4><p v-if="row.run.analysis?.error" class="failure">{{ row.run.analysis.error }}</p><p v-for="(cause, i) in row.run.analysis?.report.causes || []" :key="i"><strong>{{ label(cause.category) }} · {{ label(cause.status) }}</strong> — {{ cause.summary }}</p><p v-if="!row.run.analysis?.report.causes?.length">{{ row.run.analysis?.route_reason || k('noAnalysisHint') }}</p><ul><li v-for="(question, i) in row.run.analysis?.report.review_questions || []" :key="`q${i}`">{{ question }}</li><li v-for="(unknown, i) in row.run.analysis?.report.unknowns || []" :key="`u${i}`">{{ k('unknowns') }}: {{ unknown }}</li></ul></div>
      </div>
      <footer><span class="muted">{{ row.run.analysis?.route_reason }}</span><button v-if="grouped && row.count > 1" @click="emit('group', row.key)">{{ k('viewMembers') }}</button><button v-else class="primary" @click="emit('open', row.run.id)">{{ k('detail') }}</button></footer>
    </article>
  </section>
</template>
<script setup lang="ts">
import StatusBadge from './StatusBadge.vue'
import { computed } from 'vue'
import type { OpsRun, OpsGroup } from '@/api/knowledge-ops'
import { useOpsText } from './locales'
const props = defineProps<{ runs: OpsRun[]; groups: OpsGroup[]; grouped: boolean; selectedGroup: string }>()
const emit = defineEmits<{ open: [id: string]; group: [key: string]; toggle: [] }>()
const { k, label, labelVerdict, date } = useOpsText()
const rows = computed(() => props.grouped ? props.groups.map(g => ({ key: g.key, count: g.count, run: g.representative })) : props.runs.map(run => ({ key: run.id, count: 1, run })))
</script>
<style scoped>
header,.case-heading,footer{display:flex;align-items:center;justify-content:space-between;gap:14px;flex-wrap:wrap}h2{font-size:18px;margin:0}h3{font-size:16px;margin:0 0 10px}h4{margin:0 0 8px}header p,.muted,small{color:var(--td-text-color-secondary);font-size:13px}header label{white-space:nowrap}.notice{padding:14px;background:var(--td-bg-color-container);border:1px solid var(--td-component-stroke);border-radius:8px;color:var(--td-text-color-secondary);font-size:13px}.queue-case{padding:20px;border:1px solid var(--td-component-stroke);border-radius:12px;background:var(--td-bg-color-container);margin:16px 0}.case-heading{justify-content:flex-start;font-size:13px}.case-heading small{margin-left:auto}.case-content{display:grid;grid-template-columns:1fr 1.2fr;gap:26px;margin:18px 0}.diagnosis{background:color-mix(in srgb,var(--td-bg-color-secondarycontainer) 60%,var(--td-bg-color-container));border-radius:8px;padding:16px 18px;font-size:13px}.diagnosis p{margin:8px 0}.diagnosis ul{padding-left:20px}.risk{padding:4px 10px;border-radius:5px;background:var(--td-bg-color-secondarycontainer)}.risk.high,.failure{color:var(--td-error-color)}.risk.high{background:var(--td-error-color-light)}.risk.medium{background:var(--td-warning-color-light);color:var(--td-warning-color)}.risk.low{background:var(--td-success-color-light);color:var(--td-success-color)}button{cursor:pointer;min-height:36px;border:1px solid var(--td-component-border);border-radius:6px;padding:7px 14px;background:var(--td-bg-color-container);color:var(--td-brand-color);font:inherit}.primary{background:var(--td-brand-color);color:white;border-color:var(--td-brand-color)}button:focus-visible{outline:2px solid var(--td-brand-color);outline-offset:3px}.empty{padding:40px;text-align:center}@media(max-width:900px){.case-content{grid-template-columns:1fr}.diagnosis{border-left:0;border-top:1px solid var(--td-component-border);padding:16px 0 0}}
footer{border-top:1px solid var(--td-component-stroke);padding-top:14px}.case-heading small{font-size:12px}.case-content>div:first-child{padding-top:12px}.case-content p{line-height:1.8}.queue-panel>header{margin-top:26px}.queue-panel>header p{max-width:820px;line-height:1.8;font-size:12px}
</style>
