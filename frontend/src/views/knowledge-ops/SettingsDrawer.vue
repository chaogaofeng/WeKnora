<template>
  <t-drawer :visible="visible" :header="k('settings')" size="520px" :footer="false" @close="emit('close')">
    <div class="ops-settings">
      <p class="notice">{{ k('scopeNotice') }}</p>
      <div v-if="loading" role="status">{{ k('loading') }}</div>
      <div v-else-if="loadError" class="error" role="alert">{{ k('settingsError') }} · {{ loadError }} <t-button variant="text" @click="load">{{ k('retry') }}</t-button></div>
      <form v-else-if="draft" @submit.prevent="save">
        <label class="toggle"><span>{{ k('enabled') }}</span><t-switch v-model="draft.enabled" /></label>
        <label>{{ k('model') }}<select v-model="draft.model_id" :required="draft.enabled"><option value="">{{ k('modelRequired') }}</option><option v-for="model in models" :key="model.id" :value="model.id">{{ model.display_name || model.name }}</option></select></label>
        <p v-if="!models.length" class="error">{{ k('modelEmpty') }}</p>
        <label class="toggle"><span>{{ k('auto_record_enabled') }}</span><t-switch v-model="draft.auto_record_enabled" /></label>
        <p class="help">{{ k('calibrationHint') }}</p>
        <label>{{ k('calibration_note') }}<textarea v-model="draft.calibration_note" rows="4" maxlength="4000" :required="draft.auto_record_enabled" /></label>
        <div class="number-grid"><label v-for="field in numberFields" :key="field.key">{{ k(field.key) }}<input v-model.number="draft[field.key]" type="number" :min="field.min" :max="field.max" step="1" required /><small>{{ field.min }} – {{ field.max }}</small></label></div>
        <p class="help">{{ k('captureSince') }}: {{ date(draft.capture_enabled_at) }}</p>
        <p v-if="saveError" class="error" role="alert">{{ saveError }}</p>
        <t-button type="submit" :loading="saving" block>{{ k('save') }}</t-button>
      </form>
    </div>
  </t-drawer>
</template>
<script setup lang="ts">
import { ref, watch } from 'vue'
import { listModels, type ModelConfig } from '@/api/model'
import { getOpsSettings, saveOpsSettings, opsErrorCode, type OpsSettings } from '@/api/knowledge-ops'
import { useOpsText } from './locales'
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ close: []; saved: [OpsSettings] }>()
const { k, date } = useOpsText()
const draft = ref<OpsSettings>(); const models = ref<ModelConfig[]>([])
const loading = ref(false); const saving = ref(false); const loadError = ref(''); const saveError = ref('')
const numberFields: { key: 'sample_percent' | 'max_evidence_chars' | 'max_output_tokens' | 'timeout_seconds'; min: number; max: number }[] = [
  { key: 'sample_percent', min: 0, max: 100 }, { key: 'max_evidence_chars', min: 2000, max: 120000 },
  { key: 'max_output_tokens', min: 512, max: 8192 }, { key: 'timeout_seconds', min: 10, max: 180 },
]
async function load() {
  loading.value = true; loadError.value = ''; saveError.value = ''
  try {
    const [settings, choices] = await Promise.all([getOpsSettings(), listModels('KnowledgeQA')])
    draft.value = { ...settings }; models.value = choices.filter(model => model.id && !model.deleted_at && model.status !== 'inactive' && model.status !== 'disabled')
  } catch (error) { loadError.value = k(opsErrorCode(error)) }
  finally { loading.value = false }
}
async function save() {
  if (!draft.value) return
  saveError.value = ''
  if (draft.value.enabled && !models.value.some(model => model.id === draft.value?.model_id)) { saveError.value = k('modelRequired'); return }
  if (draft.value.auto_record_enabled && !draft.value.calibration_note.trim()) { saveError.value = k('calibrationRequired'); return }
  if (new TextEncoder().encode(draft.value.calibration_note).length > 4000) { saveError.value = k('calibrationTooLong'); return }
  saving.value = true
  try { const saved = await saveOpsSettings(draft.value); emit('saved', saved); emit('close') }
  catch (error) { saveError.value = k(opsErrorCode(error)) }
  finally { saving.value = false }
}
watch(() => props.visible, visible => { if (visible) void load() })
</script>
<style scoped>
.ops-settings{color:var(--td-text-color-primary);line-height:1.6}.notice{padding:14px;background:var(--td-bg-color-secondarycontainer);border-radius:8px}.ops-settings label{display:flex;flex-direction:column;gap:8px;margin:18px 0;font-weight:500}.ops-settings .toggle{flex-direction:row;justify-content:space-between;align-items:center}.help,small{color:var(--td-text-color-secondary);font-size:13px}.number-grid{display:grid;grid-template-columns:1fr 1fr;gap:0 20px}input,select,textarea{width:100%;box-sizing:border-box;padding:10px;border:1px solid var(--td-component-border);border-radius:6px;background:var(--td-bg-color-container);color:inherit;font:inherit}textarea{resize:vertical}.error{color:var(--td-error-color)}input:focus-visible,select:focus-visible,textarea:focus-visible{outline:2px solid var(--td-brand-color);outline-offset:2px}
</style>
