<template>
  <t-drawer :visible="visible" :header="k('settings')" size="min(540px, 95vw)" :footer="false" @close="emit('close')">
    <div class="ops-settings">
      <p class="notice">{{ k('scopeNotice') }}</p>
      <div v-if="loading" role="status">{{ k('loading') }}</div>
      <div v-else-if="loadError" class="error" role="alert">{{ k('settingsError') }} · {{ loadError }} <t-button variant="text" @click="load">{{ k('retry') }}</t-button></div>
      <form v-else-if="draft" @submit.prevent="save">
        <div class="settings-section"><h3>{{ k('captureSettings') }}</h3><label class="toggle"><span>{{ k('enabled') }}</span><t-switch v-model="draft.enabled" /></label>
        <label>{{ k('model') }}<t-select class="model-select" v-model="draft.model_id" :aria-label="k('model')" :options="models.map(model => ({value:model.id, label:model.display_name || model.name}))" :placeholder="k('modelRequired')" filterable /></label>
        <p v-if="!models.length" class="error">{{ k('modelEmpty') }}</p>
        </div><div class="settings-section"><h3>{{ k('triageSettings') }}</h3><label class="toggle"><span>{{ k('auto_record_enabled') }}</span><t-switch v-model="draft.auto_record_enabled" /></label>
        <p class="help">{{ k('calibrationHint') }}</p>
        <label>{{ k('calibration_note') }}<t-textarea v-model="draft.calibration_note" :autosize="{minRows:3,maxRows:6}" :maxlength="4000" :aria-label="k('calibration_note')" /></label>
        </div><div class="settings-section"><h3>{{ k('budgetSettings') }}</h3><div class="number-grid"><label v-for="field in numberFields" :key="field.key">{{ k(field.key) }}<input v-model.number="draft[field.key]" type="number" :min="field.min" :max="field.max" step="1" required /><small>{{ field.min }} – {{ field.max }}</small></label></div>
        </div><p class="help">{{ k('captureSince') }}: {{ date(draft.capture_enabled_at) }}</p>
        <p v-if="saveError" class="error" role="alert">{{ saveError }}</p>
        <t-button type="submit" :loading="saving" block>{{ k('save') }}</t-button>
      </form>
    </div>
  </t-drawer>
</template>
<script setup lang="ts">
import { onScopeDispose, ref, watch } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { listModels, type ModelConfig } from '@/api/model'
import { getOpsSettings, saveOpsSettings, opsErrorCode, type OpsSettings } from '@/api/knowledge-ops'
import { useOpsText } from './locales'
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ close: []; saved: [OpsSettings] }>()
const { k, date } = useOpsText()
const auth = useAuthStore()
let requestID = 0
let loadedTenant: number | null | undefined
const draft = ref<OpsSettings>(); const models = ref<ModelConfig[]>([])
const loading = ref(false); const saving = ref(false); const loadError = ref(''); const saveError = ref('')
const numberFields: { key: 'sample_percent' | 'max_evidence_chars' | 'max_output_tokens' | 'timeout_seconds'; min: number; max: number }[] = [
  { key: 'sample_percent', min: 0, max: 100 }, { key: 'max_evidence_chars', min: 2000, max: 120000 },
  { key: 'max_output_tokens', min: 512, max: 8192 }, { key: 'timeout_seconds', min: 10, max: 180 },
]
async function load() {
  const token = ++requestID; const tenant = auth.effectiveTenantId
  const current = () => token === requestID && props.visible && tenant === auth.effectiveTenantId
  draft.value = undefined; loadedTenant = undefined; models.value = []
  loading.value = true; loadError.value = ''; saveError.value = ''
  try {
    const [settings, choices] = await Promise.all([getOpsSettings(), listModels('KnowledgeQA')])
    if (!current()) return
    loadedTenant = tenant
    draft.value = { ...settings }; models.value = choices.filter(model => model.id && !model.deleted_at && model.status !== 'inactive' && model.status !== 'disabled')
  } catch (error) { if (current()) loadError.value = k(opsErrorCode(error)) }
  finally { if (current()) loading.value = false }
}
async function save() {
  if (!draft.value || saving.value || loading.value || !props.visible || loadedTenant !== auth.effectiveTenantId) return
  const token = requestID; const tenant = auth.effectiveTenantId
  const current = () => token === requestID && props.visible && tenant === auth.effectiveTenantId
  saveError.value = ''
  if (draft.value.enabled && !models.value.some(model => model.id === draft.value?.model_id)) { saveError.value = k('modelRequired'); return }
  if (draft.value.auto_record_enabled && !draft.value.calibration_note.trim()) { saveError.value = k('calibrationRequired'); return }
  if (new TextEncoder().encode(draft.value.calibration_note).length > 4000) { saveError.value = k('calibrationTooLong'); return }
  saving.value = true
  try { const saved = await saveOpsSettings({ ...draft.value }); if (current()) { emit('saved', saved); emit('close') } }
  catch (error) { if (current()) saveError.value = k(opsErrorCode(error)) }
  finally { if (current()) saving.value = false }
}
watch(() => [props.visible, auth.effectiveTenantId], () => {
  requestID++; loadedTenant = undefined; draft.value = undefined; models.value = []
  saving.value = false; loading.value = false; loadError.value = ''; saveError.value = ''
  if (props.visible) void load()
}, { flush: 'sync' })
onScopeDispose(() => { requestID++ })
</script>
<style scoped>
.ops-settings{color:var(--td-text-color-primary);line-height:1.6}.notice{padding:14px;background:var(--td-bg-color-secondarycontainer);border-radius:8px}.ops-settings label{display:flex;flex-direction:column;gap:8px;margin:18px 0;font-weight:500}.ops-settings .toggle{flex-direction:row;justify-content:space-between;align-items:center}.help,small{color:var(--td-text-color-secondary);font-size:13px}.number-grid{display:grid;grid-template-columns:1fr 1fr;gap:0 20px}input,select,textarea{width:100%;box-sizing:border-box;padding:10px;border:1px solid var(--td-component-border);border-radius:6px;background:var(--td-bg-color-container);color:inherit;font:inherit}textarea{resize:vertical}.error{color:var(--td-error-color)}input:focus-visible,select:focus-visible,textarea:focus-visible{outline:2px solid var(--td-brand-color);outline-offset:2px}
.settings-section{padding:18px 20px;margin:18px 0;border:1px solid var(--td-component-stroke);border-radius:12px}.settings-section h3{margin:0 0 16px;font-size:15px;font-weight:600}.settings-section label{font-size:13px;font-weight:400}.settings-section label:last-child{margin-bottom:0}.settings-section :deep(.t-input),.settings-section :deep(.t-textarea__inner){border-radius:6px;font-size:13px}.ops-settings .help{font-size:12px}.ops-settings .notice{font-size:12px;line-height:1.8;margin-top:0}.ops-settings input{font-size:13px}.number-grid{gap:0 16px}.number-grid label{font-size:12px;color:var(--td-text-color-secondary)}.ops-settings :deep(.t-button){border-radius:6px;height:38px}
</style>
