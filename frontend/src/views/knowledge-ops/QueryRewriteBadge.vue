<template>
  <t-button
    v-if="run.query_context?.rewrite?.status === 'rewritten'"
    size="small"
    variant="text"
    theme="primary"
    @click="emit('open', run.id)"
  >{{ k('queryRewrite_rewritten') }}</t-button>
</template>
<script setup lang="ts">
import type { OpsRun } from '@/api/knowledge-ops'
import { useOpsText } from './locales'
defineProps<{ run: OpsRun }>()
const emit = defineEmits<{ open: [id: string] }>()
const { k } = useOpsText()
</script>
