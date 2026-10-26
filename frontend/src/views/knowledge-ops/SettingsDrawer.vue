<template>
  <t-drawer
    :visible="visible"
    :header="k('settings')"
    size="min(540px, 95vw)"
    :footer="false"
    @close="emit('close')"
  >
    <div class="ops-settings">
      <p class="notice">{{ k('scopeNotice') }}</p>
      <div v-if="loading" role="status">{{ k('loading') }}</div>
      <div v-else-if="loadError" role="alert">
        <EmptyState
          :title="k('settingsError')"
          :description="loadError"
          icon="error-circle"
          compact
        >
          <t-button variant="outline" @click="load">{{ k('retry') }}</t-button>
        </EmptyState>
      </div>
      <form v-else-if="draft" @submit.prevent="save">
        <div class="settings-section">
          <h3>{{ k('captureSettings') }}</h3>
          <label class="toggle">
            <span>{{ k('enabled') }}</span>
            <t-switch v-model="draft.enabled" />
          </label>
          <label>
            {{ k('model') }}
            <t-select
              class="model-select"
              v-model="draft.model_id"
              :aria-label="k('model')"
              :options="
                models.map((model) => ({
                  value: model.id,
                  label: model.display_name || model.name,
                }))
              "
              :placeholder="k('modelRequired')"
              filterable
            />
          </label>
          <p v-if="!models.length" class="error">{{ k('modelEmpty') }}</p>
        </div>
        <div class="settings-section">
          <h3>{{ k('triageSettings') }}</h3>
          <label class="toggle">
            <span>{{ k('auto_record_enabled') }}</span>
            <t-switch v-model="draft.auto_record_enabled" />
          </label>
          <p class="help">{{ k('calibrationHint') }}</p>
          <div class="calibration-state">
            <t-tag :theme="calibrationStatus === 'valid' ? 'success' : 'default'" variant="light">{{ k(`calibration_${calibrationStatus}`) }}</t-tag>
            <span v-if="draft.calibration_confirmed_at" class="help">{{ date(draft.calibration_confirmed_at) }}</span>
          </div>
          <p v-if="draft.auto_record_enabled && calibrationStatus !== 'valid'" class="notice">{{ k('calibrationPaused') }}</p>
          <p v-if="draft.calibration_policy_version" class="help">{{ k('calibrationBoundTo') }}: {{ draft.calibration_model_id }} · {{ draft.calibration_policy_version }}</p>
          <label>
            {{ k('calibration_note') }}
            <t-textarea
              v-model="draft.calibration_note"
              :autosize="{ minRows: 3, maxRows: 6 }"
              :maxlength="4000"
              :aria-label="k('calibration_note')"
            />
          </label>
          <t-checkbox v-if="draft.auto_record_enabled" class="calibration-confirm" v-model="draft.calibration_confirmed" :disabled="saving || snapshotLoading || !draft.calibration_snapshot">
            {{ k('calibrationConfirm') }}
          </t-checkbox>
          <p v-if="snapshotLoading" class="help" role="status">{{ k('calibrationLoading') }}</p>
          <div v-else-if="draft.auto_record_enabled && !draft.calibration_snapshot" class="help">
            {{ k('calibrationUnavailable') }}
            <t-button variant="text" size="small" :disabled="saving || !draft.model_id" @click="refreshSnapshot">{{ k('calibrationRefresh') }}</t-button>
          </div>
          <p class="help">{{ k('calibrationBindingHint') }}</p>
        </div>
        <div class="settings-section">
          <h3>{{ k('budgetSettings') }}</h3>
          <div class="number-grid">
            <label v-for="field in numberFields" :key="field.key">
              {{ k(field.key) }}
              <input
                v-model.number="draft[field.key]"
                type="number"
                :min="field.min"
                :max="field.max"
                step="1"
                required
              />
              <small>{{ field.min }} – {{ field.max }}</small>
            </label>
          </div>
        </div>
        <p class="help">
          {{ k('captureSince') }}: {{ date(draft.capture_enabled_at) }}
        </p>
        <p v-if="saveError" class="error" role="alert">{{ saveError }}</p>
        <t-button type="submit" :loading="saving" block>
          {{ k('save') }}
        </t-button>
      </form>
    </div>
  </t-drawer>
</template>
<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { listModels, type ModelConfig } from '@/api/model'
import {
  getOpsSettings,
  getOpsCalibrationSnapshot,
  saveOpsSettings,
  opsErrorCode,
  type OpsSettings,
} from '@/api/knowledge-ops'
import EmptyState from '@/components/EmptyState.vue'
import { useOpsText } from './locales'
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ close: []; saved: [OpsSettings] }>()
const { k, date } = useOpsText()
const auth = useAuthStore()
let requestID = 0
let snapshotRequestID = 0
const snapshotLoading = ref(false)
let loadedTenant: number | null | undefined
const draft = ref<OpsSettings>()
const loadedSignature = ref('')
const loadedAutoRecord = ref(false)
function calibrationSignature(value?: OpsSettings) {
  if (!value) return ''
  return JSON.stringify([value.model_id, value.calibration_snapshot, value.max_evidence_chars, value.max_output_tokens, value.timeout_seconds, value.sample_percent, value.calibration_note.trim()])
}
const calibrationStatus = computed(() => {
  const status = draft.value?.calibration_status || 'unconfirmed'
  return status === 'valid' && calibrationSignature(draft.value) !== loadedSignature.value ? 'stale' : status
})
watch(() => calibrationSignature(draft.value), () => {
  if (draft.value) draft.value.calibration_confirmed = false
}, { flush: 'sync' })
const models = ref<ModelConfig[]>([])
const loading = ref(false)
const saving = ref(false)
const loadError = ref('')
const saveError = ref('')
async function refreshSnapshot() {
  const modelID = draft.value?.model_id
  const token = ++snapshotRequestID
  const tenant = auth.effectiveTenantId
  if (draft.value) {
    draft.value.calibration_snapshot = ''
    draft.value.calibration_confirmed = false
  }
  snapshotLoading.value = false
  if (!modelID) return
  const current = () => token === snapshotRequestID && props.visible && tenant === auth.effectiveTenantId && draft.value?.model_id === modelID
  snapshotLoading.value = true
  try {
    const result = await getOpsCalibrationSnapshot(modelID)
    if (current() && draft.value && result.model_id === modelID) {
      draft.value.calibration_snapshot = result.snapshot
    }
  } catch (error) {
    if (current()) saveError.value = k(opsErrorCode(error))
  } finally {
    if (current()) snapshotLoading.value = false
  }
}
watch(() => draft.value?.model_id, (modelID, previous) => {
  if (modelID !== undefined && previous !== undefined && modelID !== previous) void refreshSnapshot()
}, { flush: 'sync' })
const numberFields: {
  key:
    | 'sample_percent'
    | 'max_evidence_chars'
    | 'max_output_tokens'
    | 'timeout_seconds'
  min: number
  max: number
}[] = [
  { key: 'sample_percent', min: 0, max: 100 },
  { key: 'max_evidence_chars', min: 2000, max: 120000 },
  { key: 'max_output_tokens', min: 512, max: 8192 },
  { key: 'timeout_seconds', min: 10, max: 180 },
]
async function load() {
  const token = ++requestID
  snapshotRequestID++
  snapshotLoading.value = false
  const tenant = auth.effectiveTenantId
  const current = () =>
    token === requestID && props.visible && tenant === auth.effectiveTenantId
  draft.value = undefined
  loadedTenant = undefined
  models.value = []
  loading.value = true
  loadError.value = ''
  saveError.value = ''
  try {
    const [settings, choices] = await Promise.all([
      getOpsSettings(),
      listModels('KnowledgeQA'),
    ])
    if (!current()) return
    loadedTenant = tenant
    loadedSignature.value = calibrationSignature(settings)
    loadedAutoRecord.value = settings.auto_record_enabled
    draft.value = { ...settings, calibration_confirmed: false }
    models.value = choices.filter(
      (model) =>
        model.id &&
        !model.deleted_at &&
        model.status !== 'inactive' &&
        model.status !== 'disabled',
    )
  } catch (error) {
    if (current()) loadError.value = k(opsErrorCode(error))
  } finally {
    if (current()) loading.value = false
  }
}
async function save() {
  if (
    !draft.value ||
    saving.value ||
    loading.value ||
    !props.visible ||
    loadedTenant !== auth.effectiveTenantId
  )
    return
  const token = requestID
  const tenant = auth.effectiveTenantId
  const current = () =>
    token === requestID && props.visible && tenant === auth.effectiveTenantId
  saveError.value = ''
  if (
    draft.value.enabled &&
    !models.value.some((model) => model.id === draft.value?.model_id)
  ) {
    saveError.value = k('modelRequired')
    return
  }
  if (draft.value.auto_record_enabled && !draft.value.calibration_note.trim()) {
    saveError.value = k('calibrationRequired')
    return
  }
  if (draft.value.calibration_confirmed && (snapshotLoading.value || !draft.value.calibration_snapshot)) {
    saveError.value = k('calibrationUnavailable')
    return
  }
  if (draft.value.auto_record_enabled && !loadedAutoRecord.value && calibrationStatus.value !== 'valid' && !draft.value.calibration_confirmed) {
    saveError.value = k('calibrationConfirmationRequired')
    return
  }
  if (new TextEncoder().encode(draft.value.calibration_note).length > 4000) {
    saveError.value = k('calibrationTooLong')
    return
  }
  saving.value = true
  try {
    const saved = await saveOpsSettings({ ...draft.value })
    if (current()) {
      emit('saved', saved)
      emit('close')
    }
  } catch (error) {
    if (current()) {
      const code = opsErrorCode(error)
      if (code === 'version_changed' && draft.value) {
        snapshotRequestID++
        snapshotLoading.value = false
        draft.value.calibration_confirmed = false
        draft.value.calibration_snapshot = ''
        saveError.value = k('calibrationChanged')
      } else saveError.value = k(code)
    }
  } finally {
    if (current()) saving.value = false
  }
}
watch(
  () => [props.visible, auth.effectiveTenantId],
  () => {
    requestID++
    snapshotRequestID++
    snapshotLoading.value = false
    loadedTenant = undefined
    draft.value = undefined
    models.value = []
    saving.value = false
    loading.value = false
    loadError.value = ''
    saveError.value = ''
    if (props.visible) void load()
  },
  { flush: 'sync' },
)
onScopeDispose(() => {
  requestID++
  snapshotRequestID++
})
</script>
<style scoped>
.ops-settings {
  font-family: var(--app-font-family);
  font-size: var(--app-text-base);
}

.ops-settings {
  color: var(--td-text-color-primary);
  line-height: 1.6;
}
.notice {
  padding: var(--app-space-3);
  background: var(--td-bg-color-secondarycontainer);
  border-radius: var(--app-radius-md);
}
.ops-settings label {
  display: flex;
  flex-direction: column;
  gap: var(--app-space-2);
  margin: var(--app-space-4) 0;
  font-weight: 500;
}
.ops-settings .toggle {
  flex-direction: row;
  justify-content: space-between;
  align-items: center;
}
.help,
small {
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-md);
}
.calibration-state { display: flex; align-items: center; flex-wrap: wrap; gap: var(--app-space-2); }
.ops-settings .calibration-confirm {
  flex-direction: row;
  align-items: flex-start;
  gap: 0;
}
.calibration-confirm :deep(.t-checkbox__input) {
  flex-shrink: 0;
  margin-top: var(--app-space-1);
}
.number-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 var(--app-space-4);
}
input {
  width: 100%;
  box-sizing: border-box;
  padding: var(--app-space-2);
  border: 1px solid var(--td-component-border);
  border-radius: var(--app-radius-sm);
  background: var(--td-bg-color-container);
  color: inherit;
  font: inherit;
}
.error {
  color: var(--td-error-color);
}
input:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 2px;
}
.settings-section {
  padding: var(--app-space-4) var(--app-space-5);
  margin: var(--app-space-4) 0;
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-xl);
}
.settings-section h3 {
  margin: 0 0 var(--app-space-4);
  font-size: var(--app-text-lg);
  font-weight: 600;
}
.settings-section label {
  font-size: var(--app-text-md);
  font-weight: 400;
}
.settings-section label:last-child {
  margin-bottom: 0;
}
.ops-settings .help {
  font-size: var(--app-text-sm);
}
.ops-settings .notice {
  font-size: var(--app-text-sm);
  line-height: 1.8;
  margin-top: 0;
}
.ops-settings input {
  font-size: var(--app-text-md);
}
.number-grid label {
  font-size: var(--app-text-sm);
  color: var(--td-text-color-secondary);
}
</style>
