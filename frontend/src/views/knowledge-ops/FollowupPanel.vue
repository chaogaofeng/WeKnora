<template>
  <section v-if="items.length || history.length" class="followup-panel" :aria-label="k('followupTitle')">
    <h3>{{ k('followupTitle') }}</h3>
    <p class="muted">{{ k('followupBoundary') }}</p>
    <article v-for="item in items" :key="item.route" class="followup-item">
      <div class="followup-heading">
        <strong>{{ label(item.route) }}</strong>
        <t-tag :theme="item.state === 'closed' ? 'default' : 'warning'" variant="light">
          {{ k(item.state === 'closed' ? 'followupClosed' : 'followupPending') }}
        </t-tag>
        <t-button variant="text" theme="primary" size="small" :disabled="!canHandle || handling" @click="beginFollowup(item)">
          {{ k(item.state === 'closed' ? 'followupReopen' : 'followupHandle') }}
        </t-button>
      </div>
      <p class="item-reason">{{ item.reason }}</p>
      <div v-if="item.latest" class="last-result">
        <span>{{ k(`followup_${item.latest.action}`) }} · {{ date(item.latest.created_at) }}</span>
        <p>{{ item.latest.reason }}</p>
      </div>
      <form v-if="editingRoute === item.route" class="followup-form" @submit.prevent="saveFollowup">
        <label>
          {{ k('followupAction') }}
          <t-select v-model="followupDraft.action" :aria-label="k('followupAction')" :placeholder="k('followupChooseAction')" :options="actions.map(value => ({ value, label: k(`followup_${value}`) }))" :disabled="handling || !canHandle" />
        </label>
        <label>
          {{ k('followupReason') }}
          <t-textarea v-model="followupDraft.reason" :aria-label="k('followupReason')" :placeholder="k('followupReasonHint')" :autosize="{ minRows: 2, maxRows: 6 }" :maxlength="4000" :disabled="handling || !canHandle" aria-required="true" />
        </label>
        <div class="followup-actions">
          <t-button type="submit" :loading="handling" :disabled="!canHandle">{{ k('followupSave') }}</t-button>
          <t-button variant="text" theme="default" :disabled="handling" @click="editingRoute = ''">{{ k('cancel') }}</t-button>
        </div>
      </form>
    </article>
    <p v-if="followupError" class="error" role="alert">{{ followupError }}</p>
    <t-button v-if="followupError" variant="text" size="small" :disabled="handling" @click="emit('changed')">{{ k('refresh') }}</t-button>
    <p v-if="!canHandle && items.length" class="muted">{{ k('followupReadOnly') }}</p>
    <details v-if="history.length" class="followup-history">
      <summary>{{ k('followupHistory') }} · {{ history.length }}</summary>
      <article v-for="record in history" :key="record.id">
        <div class="history-heading">
          <strong>{{ label(record.route) }} · {{ k(`followup_${record.action}`) }}</strong>
          <span>{{ date(record.created_at) }}</span>
        </div>
        <p>{{ record.reason }}</p>
        <span class="muted">{{ k('followupActor') }}: {{ record.actor_id }}</span>
      </article>
    </details>
  </section>
</template>
<script setup lang="ts">
import { computed, onScopeDispose, reactive, ref, watch } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { addOpsFollowup, opsErrorCode, type OpsFollowup, type OpsFollowupAction, type OpsFollowupState, type OpsRun } from '@/api/knowledge-ops'
import { useOpsText } from './locales'

const props = defineProps<{
  run: OpsRun
  analysisID: string
  records: OpsFollowup[]
  disabled: boolean
}>()
const emit = defineEmits<{ changed: [] }>()
const { k, label, date } = useOpsText()
const auth = useAuthStore()
const canHandle = computed(() => auth.hasRole('admin') && !props.disabled && props.analysisID === props.run.analysis?.id)
const items = computed(() => props.analysisID === props.run.analysis?.id ? props.run.followups || [] : [])
const history = computed(() => props.records.filter(record => record.analysis_id === props.analysisID))
const editingRoute = ref('')
const handling = ref(false)
const followupError = ref('')
const followupDraft = reactive({ action: '' as OpsFollowupAction | '', reason: '', previous_id: '' })
const actions = computed<OpsFollowupAction[]>(() => followupDraft.action === 'reopened' ? ['reopened'] : ['resolved', 'accepted_limitation', 'not_required'])
let scopeID = 0

function beginFollowup(item: OpsFollowupState) {
  if (!canHandle.value || handling.value) return
  editingRoute.value = item.route
  followupError.value = ''
  Object.assign(followupDraft, { action: item.state === 'closed' ? 'reopened' : '', reason: '', previous_id: item.latest?.id || '' })
}
async function saveFollowup() {
  if (!canHandle.value || handling.value || !editingRoute.value) return
  followupError.value = ''
  const item = items.value.find(value => value.route === editingRoute.value)
  if (!item || (item.latest?.id || '') !== followupDraft.previous_id) {
    followupError.value = k('version_changed')
    return
  }
  const reason = followupDraft.reason.trim()
  if (!reason) { followupError.value = k('followupReasonRequired'); return }
  if (new TextEncoder().encode(reason).length > 4000) { followupError.value = k('reasonTooLong'); return }
  if (!followupDraft.action) { followupError.value = k('followupChooseAction'); return }
  const token = scopeID
  const current = () => token === scopeID
  handling.value = true
  try {
    await addOpsFollowup(props.run.id, {
      analysis_id: props.analysisID, route: item.route, action: followupDraft.action,
      reason, previous_id: followupDraft.previous_id,
    })
    if (!current()) return
    editingRoute.value = ''
    emit('changed')
  } catch (error) {
    if (current()) followupError.value = k(opsErrorCode(error))
  } finally {
    if (current()) handling.value = false
  }
}
watch([() => props.run.id, () => props.analysisID, () => auth.effectiveTenantId], () => {
  scopeID++
  editingRoute.value = ''
  handling.value = false
  followupError.value = ''
  Object.assign(followupDraft, { action: '', reason: '', previous_id: '' })
}, { flush: 'sync' })
onScopeDispose(() => { scopeID++ })
</script>
<style scoped>
.followup-panel { color: var(--td-text-color-primary); }
.followup-panel h3 { margin: 0 0 var(--app-space-3); font-size: var(--app-text-lg); }
.muted, .last-result, .history-heading span { color: var(--td-text-color-secondary); font-size: var(--app-text-sm); }
.followup-heading, .followup-actions, .history-heading { display: flex; align-items: center; flex-wrap: wrap; gap: var(--app-space-2); }
.followup-heading .t-button { margin-left: auto; }
.followup-item { padding: var(--app-space-4) 0; border-top: 1px solid var(--td-component-stroke); }
.item-reason, .last-result p, .followup-history p { overflow-wrap: anywhere; white-space: pre-wrap; margin: var(--app-space-2) 0; }
.last-result { padding-left: var(--app-space-3); border-left: 2px solid var(--td-component-stroke); }
.followup-form { margin-top: var(--app-space-3); padding: var(--app-space-4); border-radius: var(--app-radius-md); background: var(--td-bg-color-secondarycontainer); }
.followup-form label { display: flex; flex-direction: column; gap: var(--app-space-2); margin-bottom: var(--app-space-3); }
.followup-history { border-top: 1px solid var(--td-component-stroke); padding-top: var(--app-space-3); }
.followup-history summary { cursor: pointer; color: var(--td-text-color-secondary); }
.followup-history article { padding: var(--app-space-3) 0; }
.history-heading { justify-content: space-between; }
.error { color: var(--td-error-color); }
</style>
