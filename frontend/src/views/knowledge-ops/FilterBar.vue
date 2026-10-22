<template>
  <form class="ops-filter-bar" @submit.prevent="emit('apply')">
    <div class="filter-toolbar">
      <t-input
        class="filter-search"
        :value="values[searchKey]"
        :placeholder="k(records ? 'reviewSearch' : 'searchPlaceholder')"
        :aria-label="k(records ? 'reviewSearch' : 'search')"
        clearable
        @change="(value: unknown) => change(searchKey, value)"
      >
        <template #prefix-icon><SearchIcon /></template>
      </t-input>
      <div class="filter-period">
        <span class="field-caption" :id="`${id}-period`">
          {{ k(records ? 'reviewPeriod' : 'questionPeriod') }}
        </span>
        <t-date-range-picker
          class="period-picker"
          :value="period"
          :aria-labelledby="`${id}-period`"
          :placeholder="[k('startDate'), k('endDate')]"
          :presets="presets"
          :first-day-of-week="1"
          value-type="time-stamp"
          format="YYYY-MM-DD HH:mm"
          enable-time-picker
          clearable
          :need-confirm="true"
          @change="changePeriod"
        />
      </div>
      <div class="toolbar-actions">
        <t-button
          v-if="!records"
          class="more-button"
          variant="outline"
          theme="default"
          :aria-expanded="expanded"
          :aria-controls="`${id}-advanced`"
          @click="expanded = !expanded"
        >
          <template #icon><FilterIcon /></template>
          {{ k('moreFilters') }}
          <span v-if="advancedCount" class="filter-count">
            {{ advancedCount }}
          </span>
          <ChevronDownIcon class="chevron" :class="{ expanded }" />
        </t-button>
        <t-button type="submit" theme="primary" :loading="loading">
          {{ k('apply') }}
        </t-button>
      </div>
    </div>
    <div class="quick-filters">
      <div
        v-for="field in primaryFields"
        :key="field.key"
        class="inline-field"
        :class="{ 'session-filter': field.key === 'session_id' }"
      >
        <label :id="`${id}-${field.key}`">{{ k(field.title) }}</label>
        <t-input
          v-if="field.key === 'reviewer_id' || field.key === 'session_id'"
          :value="values[field.key]"
          :aria-labelledby="`${id}-${field.key}`"
          :placeholder="k(field.key === 'session_id' ? 'sessionIdFilterPlaceholder' : 'allReviewers')"
          clearable
          @change="(value: unknown) => change(field.key, value)"
        />
        <t-select
          v-else
          :value="values[field.key] || undefined"
          :aria-labelledby="`${id}-${field.key}`"
          :options="options(field)"
          :placeholder="k('all')"
          clearable
          :filterable="field.key === 'channel'"
          :creatable="field.key === 'channel'"
          @create="(value: unknown) => change(field.key, value)"
          @change="(value: unknown) => change(field.key, value)"
        />
      </div>
      <div class="source-version-display">
        <span :id="`${id}-source-versions`">{{ k('showSourceVersions') }}</span>
        <t-tooltip :content="k('sourceVersionDisplayHint')">
          <t-switch
            size="small"
            role="switch"
            tabindex="0"
            :value="showSourceVersions !== false"
            :aria-checked="showSourceVersions !== false"
            :aria-labelledby="`${id}-source-versions`"
            @keydown.space.prevent="
              emit('sourceVersions', showSourceVersions === false)
            "
            @keydown.enter.prevent="
              emit('sourceVersions', showSourceVersions === false)
            "
            @change="
              (value: boolean | string | number) =>
                emit('sourceVersions', !!value)
            "
          />
        </t-tooltip>
      </div>
      <div class="filter-meta">
        <span class="time-note">{{ timeZone }} · {{ k('timeBoundary') }}</span>
        <button
          type="button"
          class="reset-button"
          :disabled="!hasValues"
          @click="emit('reset')"
        >
          <RefreshIcon />
          {{ k('reset') }}
        </button>
      </div>
    </div>
    <div
      v-if="expanded && !records"
      :id="`${id}-advanced`"
      class="advanced-filters"
    >
      <div
        v-for="field in advancedFields"
        :key="field.key"
        class="advanced-field"
      >
        <label :id="`${id}-${field.key}`">{{ k(field.title) }}</label>
        <t-select
          :value="values[field.key] || undefined"
          :aria-labelledby="`${id}-${field.key}`"
          :options="options(field)"
          :placeholder="k('all')"
          clearable
          @change="(value: unknown) => change(field.key, value)"
        />
      </div>
    </div>
    <div v-if="activeTags.length" class="applied-filters" aria-live="polite">
      <span class="applied-label">{{ k('activeFilters') }}</span>
      <t-tag
        v-for="tag in activeTags"
        :key="tag.key"
        closable
        variant="light"
        theme="default"
        @close="emit('remove', tag.key)"
      >
        {{ tag.text }}
      </t-tag>
    </div>
  </form>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import {
  SearchIcon,
  FilterIcon,
  ChevronDownIcon,
  RefreshIcon,
} from 'tdesign-icons-vue-next'
import type { DateRangeValue } from 'tdesign-vue-next'
import type { OpsFilters } from '@/api/knowledge-ops'
import { answerVerdicts, routes } from './state'
import { useOpsText } from './locales'
const props = defineProps<{
  values: Record<string, string>
  applied: OpsFilters
  records: boolean
  loading: boolean
  showSourceVersions?: boolean
}>()
const emit = defineEmits<{
  change: [key: string, value: string]
  period: [from: string, to: string]
  apply: []
  reset: []
  remove: [key: string]
  sourceVersions: [value: boolean]
}>()
const { k, label, labelVerdict, date } = useOpsText()
const expanded = ref(false)
const id = computed(() =>
  props.records ? 'ops-record-filters' : 'ops-run-filters',
)
const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone
const searchKey = computed(() => (props.records ? 'review_search' : 'search'))
const timeKeys = computed(() =>
  props.records ? ['review_from', 'review_to'] : ['from', 'to'],
)
interface Field {
  key: string
  title: string
  values: string[]
}
const runFields: Field[] = [
  {
    key: 'qa_type',
    title: 'qa_type',
    values: ['rag-qa', 'wiki-qa', 'hybrid-rag-wiki'],
  },
  {
    key: 'channel',
    title: 'channel',
    values: [
      'web',
      'api',
      'embed',
      'wecom',
      'feishu',
      'lark',
      'slack',
      'telegram',
      'dingtalk',
      'mattermost',
      'wechat',
      'qqbot',
    ],
  },
  { key: 'route', title: 'route', values: routes },
]
const recordFields: Field[] = [
  { key: 'target', title: 'target', values: ['answer', 'cause'] },
  {
    key: 'action',
    title: 'reviewAction',
    values: ['accepted', 'revision_required', 'needs_more_evidence', 'excluded', 'confirmed', 'corrected'],
  },
  { key: 'reviewer_id', title: 'reviewer', values: [] },
]
const advancedFields: Field[] = [
  {
    key: 'content_status',
    title: 'content_status',
    values: ['complete', 'partial', 'missing', 'unknown', 'not_required'],
  },
  {
    key: 'provenance_status',
    title: 'provenance_status',
    values: ['known', 'pending', 'unknown'],
  },
  { key: 'verdict', title: 'verdict', values: answerVerdicts },
  { key: 'risk', title: 'risk', values: ['high', 'medium', 'low', 'unknown'] },
  {
    key: 'cause_category',
    title: 'causeCategory',
    values: [
      'generation',
      'retrieval',
      'wiki_source',
      'source_conflict',
      'applicability',
      'capture',
      'knowledge_gap',
      'unknown',
    ],
  },
]
const primaryFields = computed<Field[]>(() => [
  ...(props.records ? recordFields : runFields),
  { key: 'session_id', title: 'sessionIdLabel', values: [] },
])
function displayValue(key: string, value: string) {
  if (key === 'session_id') return value
  if (key === 'content_status') return k(`content_${value}`)
  if (key === 'provenance_status') return k(`provenance_${value}`)
  if (key === 'target')
    return k(value === 'answer' ? 'answerTarget' : 'causeTarget')
  return key === 'verdict' ? labelVerdict(value) : label(value)
}
function options(field: Field) {
  return field.values.map((value) => ({
    value,
    label: displayValue(field.key, value),
  }))
}
function change(key: string, value: unknown) {
  emit('change', key, typeof value === 'string' ? value : '')
}
const period = computed(
  () =>
    timeKeys.value
      .map((key) =>
        props.values[key] ? new Date(props.values[key]).getTime() : '',
      )
      .filter((value) => value !== '') as DateRangeValue,
)
function changePeriod(value: DateRangeValue) {
  emit(
    'period',
    value[0] ? new Date(value[0]).toISOString() : '',
    value[1] ? new Date(value[1]).toISOString() : '',
  )
}
const presets = computed(() => ({
  [k('last7Days')]: () =>
    [Date.now() - 7 * 86400000, Date.now()] as [number, number],
  [k('last30Days')]: () =>
    [Date.now() - 30 * 86400000, Date.now()] as [number, number],
}))
const advancedCount = computed(
  () => advancedFields.filter((field) => !!props.values[field.key]).length,
)
const hasValues = computed(
  () =>
    Object.values(props.values).some(Boolean) ||
    Object.values(props.applied).some(Boolean),
)
const activeTags = computed(() => {
  const tags: { key: string; text: string }[] = []
  const applied = props.applied as Record<string, string>
  const [from, to] = timeKeys.value
  if (applied[from] || applied[to])
    tags.push({
      key: 'period',
      text: `${k(props.records ? 'reviewPeriod' : 'questionPeriod')}: ${applied[from] ? date(applied[from]) : '…'} → ${applied[to] ? date(applied[to]) : '…'}`,
    })
  for (const field of [
    ...primaryFields.value,
    ...(props.records ? [] : advancedFields),
  ]) {
    if (applied[field.key])
      tags.push({
        key: field.key,
        text: `${k(field.title)}: ${displayValue(field.key, applied[field.key])}`,
      })
  }
  if (applied[searchKey.value])
    tags.push({
      key: searchKey.value,
      text: `${k('searchShort')}: ${applied[searchKey.value]}`,
    })
  return tags
})
</script>

<style scoped>
.ops-filter-bar {
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-border);
  border-radius: var(--app-radius-xl);
  padding: var(--app-space-4) var(--app-space-5) 0;
}
.filter-toolbar {
  display: flex;
  align-items: center;
  gap: var(--app-space-3);
}
.filter-search {
  flex: 1;
  min-width: 180px;
}
.filter-period {
  display: flex;
  align-items: center;
  gap: var(--app-space-2);
  flex: 0 1 420px;
  min-width: 300px;
}
.field-caption {
  white-space: nowrap;
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-md);
}
.period-picker {
  flex: 1;
  min-width: 0;
}
.toolbar-actions {
  display: flex;
  gap: var(--app-space-2);
  align-items: center;
  flex-shrink: 0;
}
.chevron {
  font-size: var(--app-text-base);
  transition: transform var(--app-motion-fast);
  margin-left: var(--app-space-1);
}
.chevron.expanded {
  transform: rotate(180deg);
}
.filter-count {
  font-size: var(--app-text-xs);
  border-radius: var(--app-radius-xs);
  background: var(--td-brand-color-light);
  color: var(--td-brand-color);
  padding: 0 var(--app-space-1);
  margin-left: var(--app-space-1);
}
.quick-filters {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--app-space-6);
  padding: var(--app-space-3) 0 var(--app-space-4);
}
.source-version-display {
  display: flex;
  align-items: center;
  gap: var(--app-space-2);
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-md);
  white-space: nowrap;
}
.inline-field {
  display: flex;
  align-items: center;
  gap: var(--app-space-2);
  min-width: 0;
  flex: 0 1 202px;
}
.inline-field label {
  white-space: nowrap;
  font-size: var(--app-text-md);
  color: var(--td-text-color-secondary);
}
.inline-field.session-filter {
  flex-basis: 340px;
}
.inline-field :deep(.t-select__wrap),
.inline-field :deep(.t-input__wrap) {
  flex: 1;
  min-width: 90px;
}
.inline-field :deep(.t-input:hover) {
  border-color: var(--td-component-border);
}
.inline-field :deep(.t-input--focused) {
  border-color: var(--td-brand-color);
}
.filter-meta {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--app-space-4);
  flex: 1;
  white-space: nowrap;
}
.time-note {
  font-size: var(--app-text-xs);
  color: var(--td-text-color-placeholder);
}
.reset-button {
  display: flex;
  align-items: center;
  gap: var(--app-space-1);
  border: 0;
  padding: var(--app-space-1) 0;
  background: none;
  font: inherit;
  font-size: var(--app-text-md);
  color: var(--td-text-color-secondary);
  cursor: pointer;
  white-space: nowrap;
}
.reset-button:hover:not(:disabled) {
  color: var(--td-brand-color);
}
.reset-button:disabled {
  opacity: 0.4;
  cursor: default;
}
.advanced-filters {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: var(--app-space-5);
  padding: var(--app-space-4) 0;
  border-top: 1px dashed var(--td-component-border);
}
.advanced-field {
  display: flex;
  flex-direction: column;
  gap: var(--app-space-2);
}
.advanced-field label {
  font-size: var(--app-text-sm);
  color: var(--td-text-color-secondary);
}
.applied-filters {
  border-top: 1px solid var(--td-component-border);
  padding: var(--app-space-3) 0;
  display: flex;
  align-items: center;
  gap: var(--app-space-2);
  flex-wrap: wrap;
}
.applied-label {
  font-size: var(--app-text-sm);
  color: var(--td-text-color-secondary);
  margin-right: var(--app-space-1);
}
.applied-filters :deep(.t-tag) {
  max-width: 100%;
  height: auto;
  min-height: 26px;
  white-space: normal;
  overflow-wrap: anywhere;
  border-radius: var(--app-radius-xs);
}
.period-picker :deep(.t-range-input__inner .t-input) {
  height: 100%;
  border: 0;
  border-radius: var(--app-radius-xs);
}
.ops-filter-bar :deep(.t-input__prefix-icon) {
  color: var(--td-text-color-placeholder);
}
button:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 3px;
}
.source-version-display :deep(.t-switch:focus-visible) {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 3px;
}
@media (max-width: 1250px) {
  .filter-toolbar {
    flex-wrap: wrap;
  }
  .filter-search {
    flex: 1 1 240px;
  }
  .filter-period {
    flex: 1 1 340px;
  }
  .toolbar-actions {
    margin-left: auto;
  }
  .quick-filters {
    gap: var(--app-space-4);
    flex-wrap: wrap;
  }
  .inline-field {
    flex: 1 1 180px;
  }
  .filter-meta {
    flex: 1 1 100%;
    justify-content: space-between;
  }
  .advanced-filters {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 700px) {
  .ops-filter-bar {
    padding: var(--app-space-3) var(--app-space-3) 0;
  }
  .filter-toolbar {
    gap: var(--app-space-2);
  }
  .filter-period {
    min-width: 0;
    flex: 1 1 100%;
  }
  .filter-search {
    flex-basis: 100%;
  }
  .toolbar-actions {
    width: 100%;
    justify-content: flex-end;
  }
  .inline-field {
    flex-basis: 100%;
  }
  .inline-field label {
    min-width: 64px;
  }
  .advanced-filters {
    grid-template-columns: 1fr;
  }
  .time-note {
    white-space: normal;
  }
}
@media (prefers-reduced-motion: reduce) {
  .chevron {
    transition: none;
  }
}
</style>
