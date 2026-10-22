<template>
  <t-tag class="ops-badge" :theme="tone" variant="light" size="small">
    {{ text || (verdict ? labelVerdict(value) : label(value)) }}
  </t-tag>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useOpsText } from './locales'
const props = defineProps<{
  value?: string
  text?: string
  verdict?: boolean
}>()
const { label, labelVerdict } = useOpsText()
const tone = computed(() => {
  if (
    ['high', 'failed', 'contradicted', 'inconsistent'].includes(
      props.value || '',
    )
  )
    return 'danger'
  if (
    [
      'partial',
      'medium',
      'suspected',
      'needs_evidence',
      'source_verification',
      'needs_more_evidence',
      'revision_required',
      'unresolved',
      'unsupported',
      'human_review',
    ].includes(props.value || '')
  )
    return 'warning'
  if (
    [
      'complete',
      'supported',
      'consistent',
      'succeeded',
      'reviewed',
      'confirmed',
      'usable',
      'accepted',
    ].includes(props.value || '')
  )
    return 'success'
  if (
    ['quality_sample', 'running', 'pending', 'partially_reviewed', 'publishing'].includes(
      props.value || '',
    )
  )
    return 'primary'
  return 'default'
})
</script>
<style scoped>
.ops-badge {
  white-space: nowrap;
}
</style>
