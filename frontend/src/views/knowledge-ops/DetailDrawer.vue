<template>
  <t-drawer
    :visible="!!runID"
    :header="k('detailTitle')"
    size="min(1180px, 95vw)"
    :footer="false"
    @close="emit('close')"
  >
    <div class="ops-detail">
      <div class="toolbar">
        <div v-if="detail?.analyses.length" class="version-picker">
          <span>{{ k('reports') }}</span>
          <t-select
            v-model="selectedID"
            :options="analysisOptions"
            :aria-label="k('reports')"
          />
        </div>
        <div class="toolbar-actions">
          <t-button variant="outline" :loading="loading" @click="load">{{
            k('refresh')
          }}</t-button>
          <t-button
            :loading="retrying"
            :disabled="
              !enabled || !detail?.run.finished_at || !!detail?.pending_tasks
            "
            @click="rerun"
            >{{ k('rerun') }}</t-button
          >
        </div>
      </div>
      <div v-if="loading && !detail" class="state" role="status">
        {{ k('loading') }}
      </div>
      <div v-else-if="loadError" role="alert">
        <EmptyState
          :title="k('errorTitle')"
          :description="loadError"
          icon="error-circle"
          compact
        >
          <t-button variant="outline" @click="load">{{ k('retry') }}</t-button>
        </EmptyState>
      </div>
      <template v-else-if="detail">
        <p v-if="notice" class="notice" role="status">{{ notice }}</p>
        <div class="run-heading">
          <h2>{{ detail.run.question }}</h2>
          <div class="run-meta">
            <span>{{ label(detail.run.qa_type) }}</span
            ><span>{{ detail.run.channel || k('unknown') }}</span>
            <span>{{ date(detail.run.started_at) }}</span
            ><StatusBadge
              :value="detail.run.review_state"
              :text="`${k('currentHumanReview')} · ${label(detail.run.review_state)}`"
            />
            <t-tooltip
              v-if="
                showSourceVersions !== false &&
                detail.run.source_followup_pending
              "
              :content="k('sourceFollowupIndependent')"
            >
              <StatusBadge value="source_verification" />
            </t-tooltip>
          </div>
        </div>
        <ConversationPanel
          v-if="detail.run.session_id"
          :key="detail.run.id"
          :run="detail.run"
          @open="emit('open', $event)"
        />
        <p v-if="detail.pending_tasks" class="notice" role="status">
          {{ k('diagnosisInProgress') }}
        </p>
        <section v-if="analysis" class="diagnosis-first" :class="effectiveRisk">
          <div class="section-heading">
            <h3>{{ k('diagnosisSummaryTitle') }} · v{{ analysis.version }}</h3>
            <StatusBadge :value="analysis.status" />
          </div>
          <p v-if="!isCurrent" class="notice">{{ k('historical') }}</p>
          <div class="decision-grid">
            <div>
              <span class="field-label">{{ k('verdict') }}</span
              ><StatusBadge
                :value="analysis.report.verdict"
                :text="labelVerdict(analysis.report.verdict)"
              />
            </div>
            <div>
              <span class="field-label">{{ k('sourceCheck') }}</span
              ><StatusBadge
                :value="displaySourceCheck"
                :text="labelSourceCheck(displaySourceCheck)"
              />
            </div>
            <div>
              <span class="field-label">{{ k('risk') }}</span
              ><StatusBadge :value="effectiveRisk" />
            </div>
            <div>
              <span class="field-label">{{
                k(
                  showSourceVersions === false
                    ? 'content_status'
                    : 'captureCoverage',
                )
              }}</span
              ><CaptureCoverage
                :coverage="selectedCoverage"
                :show-source-versions="showSourceVersions"
              />
            </div>
          </div>
          <p class="muted">{{ k('supportScopeNotice') }}</p>
          <p v-if="analysis.report.applicability_reason" class="muted">
            {{ analysis.report.applicability_reason }}
          </p>
          <p class="route-reason">
            <strong>{{ label(analysis.route) }}</strong
            ><span>{{ analysis.route_reason }}</span>
          </p>
          <p v-if="analysis.error" class="error" role="alert">
            {{ label(analysis.error) }}
          </p>
          <p v-if="analysis.report.verdict_adjustment" class="notice">
            {{ k('adjustmentReason') }}:
            {{ label(analysis.report.verdict_adjustment) }}
          </p>
          <p
            v-for="gap in selectedCoverage?.content_gaps || []"
            :key="gap"
            class="notice"
          >
            {{ k('contentGaps') }}: {{ label(gap) }}
          </p>
        </section>
        <section v-else>
          <CaptureCoverage
            :coverage="selectedCoverage"
            :show-source-versions="showSourceVersions"
          />
          <p class="notice">
            {{ k('noAnalysis') }} · {{ k('noAnalysisHint') }}
          </p>
        </section>

        <section>
          <div class="section-heading">
            <h3>{{ k('claimAssessment') }}</h3>
            <span v-if="analysis?.report.claims?.length" class="count"
              >{{ analysis.report.claims.length }} {{ k('claimCount') }}</span
            >
          </div>
          <details class="answer-disclosure">
            <summary>{{ k('originalAnswer') }}</summary>
            <AnswerContent
              v-if="detail.run.answer"
              :content="detail.run.answer"
            />
            <p v-else class="muted">{{ k('none') }}</p>
          </details>
          <div v-if="analysis?.report.claims?.length" class="claims">
            <article
              v-for="(claim, index) in analysis.report.claims"
              :key="index"
              class="claim"
            >
              <div class="claim-heading">
                <span class="claim-number">{{
                  String(index + 1).padStart(2, '0')
                }}</span
                ><StatusBadge
                  :value="claim.verdict"
                  :text="labelVerdict(claim.verdict)"
                /><span class="muted">{{ claim.position }}</span>
              </div>
              <p class="body-text claim-text">{{ claim.text }}</p>
              <p class="muted">{{ claim.reason }}</p>
              <div class="evidence-links">
                <button
                  v-for="id in claim.evidence_ids"
                  :key="id"
                  type="button"
                  @click="showEvidence(id)"
                >
                  {{ evidenceTitle(id) }}
                </button>
              </div>
            </article>
          </div>
          <p v-else class="muted">
            {{ k(noKnowledgeCheck ? 'nonKnowledgeNotice' : 'noClaims') }}
          </p>
        </section>

        <section v-if="showSourceSection" id="ops-source-check">
          <div class="section-heading">
            <h3>{{ k('sourceReviewTitle') }}</h3>
            <StatusBadge
              :value="displaySourceCheck"
              :text="labelSourceCheck(displaySourceCheck)"
            />
          </div>
          <p v-if="showSourceFollowupNotice" class="notice">
            {{ k('sourceFollowupNotice') }}
          </p>
          <div v-if="visibleSourceIssues.length" class="source-issues">
            <article
              v-for="issue in visibleSourceIssues"
              :key="issue"
              class="source-issue"
            >
              <strong>{{ label(issue) }}</strong>
              <p>{{ sourceAction(issue) }}</p>
              <div
                v-if="versionGapSources(issue).length"
                class="evidence-links"
              >
                <button
                  v-for="item in versionGapSources(issue)"
                  :key="item.id"
                  type="button"
                  @click="showEvidence(item.id)"
                >
                  {{ item.title || item.slug || item.page_id || item.chunk_id }}
                </button>
              </div>
            </article>
          </div>
          <details
            v-if="hiddenVersionIssues.length"
            class="version-details material-disclosure"
          >
            <summary>
              {{ k('sourceVersionDetails') }} · {{ hiddenVersionIssues.length }}
            </summary>
            <div class="source-issues">
              <article
                v-for="issue in hiddenVersionIssues"
                :key="issue"
                class="source-issue"
              >
                <strong>{{ label(issue) }}</strong>
                <p>{{ sourceAction(issue) }}</p>
                <div
                  v-if="versionGapSources(issue).length"
                  class="evidence-links"
                >
                  <button
                    v-for="item in versionGapSources(issue)"
                    :key="item.id"
                    type="button"
                    @click="showEvidence(item.id)"
                  >
                    {{
                      item.title || item.slug || item.page_id || item.chunk_id
                    }}
                  </button>
                </div>
              </article>
            </div>
          </details>
          <p v-if="analysis?.report.source_audit" class="muted">
            {{ k('sourceInspected') }}:
            {{ analysis.report.source_audit.chars.toLocaleString() }}
            {{ k('characters') }} · {{ postEvidence.length }}
            {{ k('materials') }}
          </p>
          <p v-else class="muted">{{ k('legacySourceAudit') }}</p>
          <details v-if="postEvidence.length" class="material-disclosure">
            <summary>
              {{
                k(
                  analysis?.report.source_audit
                    ? 'analysisSourceEvidence'
                    : 'postEvidence',
                )
              }}
              · {{ postEvidence.length }}
            </summary>
            <p class="muted">{{ sourceEvidenceNotice }}</p>
            <EvidenceList
              :items="postEvidence"
              :show-source-versions="showSourceVersions"
            />
          </details>
          <p v-else class="muted">{{ k('noBoundSourceEvidence') }}</p>
        </section>
        <section
          v-if="
            analysis?.report.review_questions?.length ||
            analysis?.report.unknowns?.length
          "
        >
          <div
            v-if="analysis?.report.review_questions?.length"
            class="review-questions"
          >
            <h4>{{ k('reviewQuestions') }}</h4>
            <ul>
              <li v-for="(q, i) in analysis.report.review_questions" :key="i">
                {{ q }}
              </li>
            </ul>
          </div>
          <details
            v-if="analysis?.report.unknowns?.length"
            class="report-limitations"
          >
            <summary>
              {{ k('diagnosisLimitations') }} ·
              {{ analysis.report.unknowns.length }}
            </summary>
            <ul>
              <li v-for="(unknown, i) in analysis.report.unknowns" :key="i">
                {{ unknown }}
              </li>
            </ul>
          </details>
        </section>

        <section v-if="causeCandidates.length">
          <h3>{{ k('causes') }}</h3>
          <article
            v-for="(cause, index) in causeCandidates"
            :key="index"
            class="cause"
          >
            <div class="section-heading">
              <strong>{{ label(cause.category) }}</strong
              ><StatusBadge :value="cause.status" />
            </div>
            <p>{{ cause.summary }}</p>
            <p class="field-label">{{ k('supporting') }}</p>
            <div class="evidence-links">
              <button
                v-for="id in cause.evidence_ids"
                :key="id"
                type="button"
                @click="showEvidence(id)"
              >
                {{ evidenceTitle(id) }}</button
              ><span v-if="!cause.evidence_ids?.length" class="muted">{{
                k('none')
              }}</span>
            </div>
            <p class="field-label">{{ k('opposing') }}</p>
            <div class="evidence-links">
              <template v-for="(item, i) in cause.counter_evidence" :key="i"
                ><button
                  v-if="hasEvidence(item)"
                  type="button"
                  @click="showEvidence(item)"
                >
                  {{ evidenceTitle(item) }}</button
                ><span v-else class="body-text">{{ item }}</span></template
              ><span v-if="!cause.counter_evidence?.length" class="muted">{{
                k('none')
              }}</span>
            </div>
          </article>
        </section>

        <section v-if="unlinkedCauses.length" class="unlinked-causes">
          <h3>{{ k('unlinkedCauses') }}</h3>
          <p class="notice">{{ k('unlinkedCauseNotice') }}</p>
          <details class="disclosure">
            <summary>
              {{ k('originalAttributions') }} · {{ unlinkedCauses.length }}
            </summary>
            <article
              v-for="(cause, index) in unlinkedCauses"
              :key="index"
              class="cause"
            >
              <strong>{{ label(cause.category) }}</strong>
              <p>{{ cause.summary }}</p>
              <div class="evidence-links">
                <button
                  v-for="id in cause.evidence_ids"
                  :key="id"
                  type="button"
                  @click="showEvidence(id)"
                >
                  {{ evidenceTitle(id) }}
                </button>
              </div>
            </article>
          </details>
        </section>

        <section
          v-if="noKnowledgeCheck && !showOptionalReview"
          class="optional-review"
        >
          <p>{{ k('nonKnowledgeNotice') }}</p>
          <t-button
            variant="text"
            theme="primary"
            @click="showOptionalReview = true"
            >{{ k('addOptionalReview') }}</t-button
          >
        </section>
        <section
          v-if="analysis && (!noKnowledgeCheck || showOptionalReview)"
          id="ops-review-form"
          tabindex="-1"
        >
          <div class="section-heading">
            <h3>{{ k('humanDecision') }}</h3>
            <span class="muted"
              >{{ k('reviewBoundVersion') }} v{{ analysis.version }}</span
            >
          </div>
          <p v-if="analysis.status === 'failed'" class="notice">
            {{ k('failedReviewNotice') }}
          </p>
          <p v-if="!canReview" class="notice">{{ k('reviewUnavailable') }}</p>
          <p class="muted">{{ k('reviewNotice') }}</p>
          <p v-if="pendingReviewTargets.length" class="muted">
            {{ k('remainingReviews') }}：
            {{
              pendingReviewTargets
                .map((target) =>
                  k(target === 'answer' ? 'answerTarget' : 'causeTarget'),
                )
                .join(' · ')
            }}
          </p>
          <div v-if="pendingEvidenceRequests.length" class="pending-evidence">
            <h4>{{ k('pendingEvidenceRequests') }}</h4>
            <article
              v-for="item in pendingEvidenceRequests"
              :key="item.review_id"
            >
              <strong>
                {{ k(item.target === 'answer' ? 'answerTarget' : 'causeTarget') }}
                <template v-if="item.cause_category">
                  · {{ label(item.cause_category) }}
                </template>
              </strong>
              <p>{{ item.reason }}</p>
              <small class="muted">
                {{ item.reviewer_id }}
                <template v-if="item.created_at"> · {{ date(item.created_at) }}</template>
              </small>
            </article>
          </div>
          <p v-if="showSourceFollowupNotice" class="notice">
            {{ k('answerReviewKeepsSourceOpen') }}
          </p>
          <div
            class="review-grid"
            :class="{
              'with-causes':
                causeCandidates.length || showCauseEditor || pendingReviewTargets.includes('cause'),
            }"
          >
            <ReviewForm
              :key="`${analysis.id}:answer:${savedReviews.answer}`"
              target="answer"
              :analysisID="analysis.id"
              :initial-verdict="analysis.report.verdict || 'unverifiable'"
              :disabled="!canReview"
              :busy="reviewing.answer"
              :error="reviewErrors.answer"
              @submit="saveReview"
            />
            <ReviewForm
              v-if="
                causeCandidates.length || showCauseEditor || pendingReviewTargets.includes('cause')
              "
              :key="`${analysis.id}:cause:${savedReviews.cause}`"
              target="cause"
              :analysisID="analysis.id"
              :initial-verdict="causeCandidates[0]?.status || 'unresolved'"
              :initial-category="
                causeCandidates[0]?.category ||
                pendingEvidenceRequests.find((item) => item.target === 'cause')?.cause_category
              "
              :initial-action="causeCandidates.length ? undefined : 'corrected'"
              :machine-causes="causeCandidates"
              :disabled="!canReview"
              :busy="reviewing.cause"
              :error="reviewErrors.cause"
              @submit="saveReview"
            />
          </div>
          <div
            v-if="!causeCandidates.length && !pendingReviewTargets.includes('cause')"
            class="no-cause"
          >
            <span>{{
              k(
                analysis.status === 'failed'
                  ? 'noCauseAfterFailure'
                  : unlinkedCauses.length
                    ? 'noQualifiedCause'
                    : 'noCauseReviewNeeded',
              )
            }}</span>
            <t-button
              v-if="canReview && !showCauseEditor"
              variant="text"
              @click="showCauseEditor = true"
              >{{ k('addCauseCorrection') }}</t-button
            >
          </div>
        </section>

        <section>
          <div class="section-heading">
            <h3>{{ k('reviewHistory') }}</h3>
            <span class="count">{{ detail.reviews.length }}</span>
          </div>
          <p v-if="!detail.reviews.length" class="muted">
            {{ k('noReviewHistory') }}
          </p>
          <article
            v-for="review in detail.reviews"
            :key="review.id"
            class="review-record"
          >
            <strong
              >{{
                k(review.target === 'answer' ? 'answerTarget' : 'causeTarget')
              }}
              · {{ label(review.action) }} ·
              {{
                review.target === 'answer'
                  ? labelVerdict(review.verdict)
                  : label(review.verdict)
              }}<template v-if="review.cause_category">
                · {{ label(review.cause_category) }}</template
              ></strong
            >
            <p class="body-text">{{ review.reason }}</p>
            <small
              >{{ date(review.created_at) }} · {{ review.reviewer_id }} ·
              {{ reviewVersion(review.analysis_id) }}</small
            >
          </article>
        </section>

        <div class="supporting-details">
          <details v-if="originalEvidence.length" class="secondary-details">
            <summary>
              {{ k('originalEvidence') }} · {{ originalEvidence.length }}
            </summary>
            <p class="muted">{{ k('originalEvidenceNotice') }}</p>
            <EvidenceList
              :items="originalEvidence"
              :show-source-versions="showSourceVersions"
            />
          </details>
          <details class="secondary-details technical-details">
            <summary>{{ k('technicalDetails') }}</summary>
            <p class="muted">{{ k('technicalDetailsNotice') }}</p>
            <details v-if="candidateEvidence.length" class="secondary-details">
              <summary>
                {{ k('candidateEvidence') }} · {{ candidateEvidence.length }}
              </summary>
              <p class="muted">{{ k('candidateMetadataNotice') }}</p>
              <EvidenceList
                :items="candidateEvidence"
                :show-source-versions="showSourceVersions"
                metadata-only
              />
            </details>
            <details v-if="unboundEvidence.length" class="secondary-details">
              <summary>
                {{ k('unboundEvidence') }} · {{ unboundEvidence.length }}
              </summary>
              <p class="notice">{{ k('unboundEvidenceNotice') }}</p>
              <EvidenceList
                :items="unboundEvidence"
                :show-source-versions="showSourceVersions"
              />
            </details>
            <details class="secondary-details">
              <summary>{{ k('diagnosticDetails') }}</summary>
              <template v-if="analysis">
                <h4>{{ k('analysisRecord') }}</h4>
                <dl>
                  <dt>{{ k('model') }}</dt>
                  <dd>
                    {{ analysis.model_name || analysis.model_id || k('unknown') }}
                  </dd>
                  <dt>{{ k('policy') }}</dt>
                  <dd>{{ analysis.policy_version || k('unknown') }}</dd>
                  <dt>{{ k('analysisTime') }}</dt>
                  <dd>{{ date(analysis.created_at) }}</dd>
                  <dt>{{ k('modelVerdict') }}</dt>
                  <dd>
                    {{
                      analysis.report.model_verdict
                        ? labelVerdict(analysis.report.model_verdict)
                        : k('legacyModelVerdict')
                    }}
                  </dd>
                  <dt>{{ k('policyVerdict') }}</dt>
                  <dd>{{ labelVerdict(analysis.report.verdict) }}</dd>
                  <dt>{{ k('tokens') }}</dt>
                  <dd>
                    {{ analysis.input_tokens }} / {{ analysis.output_tokens }}
                  </dd>
                  <dt>{{ k('duration') }}</dt>
                  <dd>{{ (analysis.duration_ms / 1000).toFixed(1) }} s</dd>
                  <dt>{{ k('scope') }}</dt>
                  <dd>{{ analysis.report.scope || k('unknown') }}</dd>
                  <template v-if="analysis.report.source_audit">
                    <dt>{{ k('sourceBudget') }}</dt>
                    <dd>
                      {{ analysis.report.source_audit.chars.toLocaleString() }} /
                      {{ analysis.report.source_audit.char_limit.toLocaleString() }}
                      {{ k('characters') }}
                    </dd>
                    <template v-if="analysis.report.source_audit.lookup_policy">
                      <dt>{{ k('sourceChunkLimit') }}</dt>
                      <dd>
                        {{ analysis.report.source_audit.lookup_policy.max_chunks_per_document }}
                      </dd>
                      <dt>{{ k('sourceDocumentLimit') }}</dt>
                      <dd>
                        {{ analysis.report.source_audit.lookup_policy.max_documents_per_page }}
                      </dd>
                      <dt>{{ k('sourcePageLimit') }}</dt>
                      <dd>
                        {{ analysis.report.source_audit.lookup_policy.max_linked_pages }}
                      </dd>
                    </template>
                    <template v-if="analysis.report.source_audit.selection_strategy">
                      <dt>{{ k('sourceSelectionStrategy') }}</dt>
                      <dd>
                        {{ label(analysis.report.source_audit.selection_strategy) }}
                      </dd>
                    </template>
                  </template>
                  <dt v-if="analysis.group_key">{{ k('group') }}</dt>
                  <dd v-if="analysis.group_key">{{ analysis.group_key }}</dd>
                  <dt v-if="analysis.sample_stratum">{{ k('stratum') }}</dt>
                  <dd v-if="analysis.sample_stratum">
                    {{ analysis.sample_stratum }} · {{ analysis.sample_percent }}%
                  </dd>
                </dl>
                <p class="muted">{{ k('originalReportNotice') }}</p>
                <template v-if="analysis.report.facts?.length"
                  ><h4>{{ k('facts') }}</h4>
                  <ul>
                    <li v-for="(fact, i) in analysis.report.facts" :key="i">
                      {{ fact }}
                    </li>
                  </ul></template
                >
              </template>
              <h4>{{ k('metadata') }}</h4>
              <dl>
                <dt>{{ k('runID') }}</dt>
                <dd>{{ detail.run.id }}</dd>
                <dt>{{ k('time') }}</dt>
                <dd>{{ date(detail.run.started_at) }}</dd>
                <dt>{{ k('finishedAt') }}</dt>
                <dd>
                  {{
                    detail.run.finished_at ? date(detail.run.finished_at) : '—'
                  }}
                </dd>
                <dt>{{ k('generationModel') }}</dt>
                <dd>{{ detail.run.model_id || k('unknown') }}</dd>
                <dt>{{ k('execution') }}</dt>
                <dd>{{ detail.run.execution_mode || k('unknown') }}</dd>
                <dt>{{ k('runStatus') }}</dt>
                <dd>{{ label(detail.run.status) }}</dd>
                <dt>{{ k('pendingTasks') }}</dt>
                <dd>{{ detail.pending_tasks ?? 0 }}</dd>
              </dl>
              <template v-if="detail.run.gaps?.length"
                ><h4>{{ k('originalCaptureGaps') }}</h4>
                <ul>
                  <li v-for="gap in detail.run.gaps" :key="gap">
                    {{ label(gap) }}
                  </li>
                </ul></template
              >
            </details>
          </details>
        </div>
      </template>
    </div>
  </t-drawer>
</template>
<script setup lang="ts">
import { computed, nextTick, onScopeDispose, reactive, ref, watch } from 'vue'
import { useAuthStore } from '@/stores/auth'
import CaptureCoverage from './CaptureCoverage.vue'
import ConversationPanel from './ConversationPanel.vue'
import EvidenceList from './EvidenceList.vue'
import AnswerContent from './AnswerContent.vue'
import StatusBadge from './StatusBadge.vue'
import {
  addOpsReview,
  getOpsDetail,
  retryOpsRun,
  opsErrorCode,
  type OpsDetail,
  type ReviewInput,
} from '@/api/knowledge-ops'
import EmptyState from '@/components/EmptyState.vue'
import ReviewForm from './ReviewForm.vue'
import { useOpsText } from './locales'
const props = defineProps<{
  runID: string
  analysisID?: string
  enabled: boolean
  showSourceVersions?: boolean
}>()
const emit = defineEmits<{ close: []; changed: []; open: [id: string] }>()
const { k, label, labelVerdict, labelSourceCheck, date } = useOpsText()
const detail = ref<OpsDetail>()
const loading = ref(false)
const loadError = ref('')
const selectedID = ref('')
const retrying = ref(false)
const showCauseEditor = ref(false)
const showOptionalReview = ref(false)
const noKnowledgeCheck = computed(
  () =>
    analysis.value?.route === 'not_required' &&
    analysis.value?.status === 'succeeded' &&
    !(isCurrent.value && detail.value?.run.pending_items?.length),
)
const notice = ref('')
const savedReviews = reactive({ answer: 0, cause: 0 })
const reviewing = reactive({ answer: false, cause: false })
const reviewErrors = reactive({ answer: '', cause: '' })
const auth = useAuthStore()
let requestID = 0
let scopeID = 0
function operationScope() {
  const scope = scopeID
  const runID = props.runID
  const tenant = auth.effectiveTenantId
  return () =>
    scope === scopeID &&
    runID === props.runID &&
    tenant === auth.effectiveTenantId
}
const latestID = computed(
  () =>
    detail.value?.run.analysis?.id ||
    [...(detail.value?.analyses || [])].sort((a, b) => b.version - a.version)[0]
      ?.id ||
    '',
)
const analysis = computed(() =>
  detail.value?.analyses.find((item) => item.id === selectedID.value),
)
// Current work is derived from the latest diagnosis; historical reports retain
// the original machine judgment and must not inherit today's pending requests.
const effectiveRisk = computed(() =>
  isCurrent.value
    ? detail.value?.run.effective_risk || analysis.value?.report.risk
    : analysis.value?.report.risk,
)
const displaySourceCheck = computed(() =>
  analysis.value?.route === 'not_required' &&
  analysis.value.status === 'succeeded'
    ? 'not_applicable'
    : isCurrent.value
      ? detail.value?.run.source_check_display ||
        analysis.value?.report.source_check
      : analysis.value?.report.source_check,
)
const selectedCoverage = computed(
  () => analysis.value?.capture_coverage || detail.value?.run.coverage,
)
const pendingReviewTargets = computed(() =>
  isCurrent.value ? detail.value?.run.pending_review_targets || [] : [],
)
const pendingEvidenceRequests = computed(() =>
  isCurrent.value
    ? (detail.value?.run.pending_items || []).filter(
        (item) => item.route === 'needs_evidence' && item.review_id,
      )
    : [],
)
const causeCandidates = computed(
  () =>
    analysis.value?.cause_assessment?.candidates ??
    analysis.value?.report.causes ??
    [],
)
const unlinkedCauses = computed(
  () => analysis.value?.cause_assessment?.unlinked ?? [],
)
const isCurrent = computed(() => selectedID.value === latestID.value)
const canReview = computed(
  () => isCurrent.value && !!detail.value?.run.finished_at && !!analysis.value,
)
const analysisOptions = computed(() =>
  (detail.value?.analyses || []).map((item) => ({
    value: item.id,
    label: `v${item.version} · ${label(item.status)} · ${date(item.created_at)}${item.id === latestID.value ? ` · ${k('latest')}` : ''}`,
  })),
)
const evidenceGroups = computed(() => {
  const items = detail.value?.evidence || []
  // New reports bind their supplements explicitly. Legacy reports retain only
  // their citations; unbound archives are shown separately without attribution.
  const report = analysis.value?.report
  const referenced = new Set([
    ...(report?.source_audit?.evidence_ids || []),
    ...(report?.claims || []).flatMap((claim) => claim.evidence_ids || []),
    ...(report?.causes || []).flatMap((cause) => [
      ...(cause.evidence_ids || []),
      ...(cause.counter_evidence || []),
    ]),
  ])
  return [
    {
      key: 'originalEvidence',
      items: items.filter((item) => !item.post_hoc && item.sent_to_model),
    },
    {
      key: 'candidateEvidence',
      items: items.filter((item) => !item.post_hoc && !item.sent_to_model),
    },
    {
      key: 'postEvidence',
      items: items.filter((item) => item.post_hoc && referenced.has(item.id)),
    },
    {
      key: 'unboundEvidence',
      items:
        report && !report.source_audit
          ? items.filter((item) => item.post_hoc && !referenced.has(item.id))
          : [],
    },
  ]
})
const originalEvidence = computed(() => evidenceGroups.value[0].items)
const candidateEvidence = computed(() => evidenceGroups.value[1].items)
const postEvidence = computed(() => evidenceGroups.value[2].items)
const sourceEvidenceNotice = computed(() =>
  k(analysis.value?.report.source_audit ? 'auditedSourceNotice' : 'postNotice'),
)
const unboundEvidence = computed(() => evidenceGroups.value[3].items)
const sourceIssues = computed(() => {
  const captured = selectedCoverage.value?.provenance_gaps || []
  const audit = (analysis.value?.report.source_audit?.issues || []).filter(
    (issue) =>
      issue !== 'source_linked_versions_unknown' ||
      !captured.some((gap) =>
        [
          'source_version_unknown',
          'wiki_linked_summary_versions_unknown',
        ].includes(gap),
      ),
  )
  const issues = [...new Set([...captured, ...audit])]
  if (
    !issues.length &&
    analysis.value?.status !== 'failed' &&
    analysis.value?.report.source_check === 'unresolved'
  )
    issues.push('source_scope_unresolved')
  return issues
})
const sourcePending = computed(() => {
  if (noKnowledgeCheck.value) return false
  if (
    isCurrent.value &&
    detail.value?.run.source_followup_pending !== undefined
  )
    return detail.value.run.source_followup_pending
  return (
    sourceIssues.value.length > 0 ||
    (analysis.value?.status !== 'failed' &&
      (analysis.value?.report.source_check === 'unresolved' ||
        (['wiki-qa', 'hybrid-rag-wiki'].includes(
          detail.value?.run.qa_type || '',
        ) &&
          analysis.value?.report.source_check === 'not_checked')))
  )
})
const showSourceSection = computed(() => {
  if (!analysis.value) return false
  const audit = analysis.value.report.source_audit
  if (
    sourcePending.value ||
    sourceIssues.value.length ||
    postEvidence.value.length ||
    audit?.chars ||
    audit?.evidence_ids?.length
  )
    return true
  if (displaySourceCheck.value === 'not_applicable') return false
  const plainRag =
    detail.value?.run.qa_type === 'rag-qa' &&
    !originalEvidence.value.some((item) => item.page_id)
  return !(plainRag && displaySourceCheck.value === 'not_checked')
})
// Keep lookup failures and unresolved content comparisons visible. Only missing
// version metadata moves into the disclosure when version reminders are hidden.
const versionIssueCodes = new Set([
  'source_folder_versions_unknown',
  'source_version_unknown',
  'wiki_linked_summary_versions_unknown',
  'source_linked_versions_unknown',
])
const hiddenVersionIssues = computed(() =>
  props.showSourceVersions === false
    ? sourceIssues.value.filter((issue) => versionIssueCodes.has(issue))
    : [],
)
const visibleSourceIssues = computed(() =>
  sourceIssues.value.filter(
    (issue) => !hiddenVersionIssues.value.includes(issue),
  ),
)
const showSourceFollowupNotice = computed(
  () =>
    sourcePending.value &&
    (props.showSourceVersions !== false ||
      visibleSourceIssues.value.length > 0),
)
function versionGapSources(issue: string) {
  if (!versionIssueCodes.has(issue)) return []
  // Uncaptured descendants have no answer-time evidence row to identify here.
  if (issue === 'source_folder_versions_unknown') return []
  const seen = new Set<string>()
  return (detail.value?.evidence || []).filter((item) => {
    if (
      !item.sent_to_model ||
      item.post_hoc ||
      (item.version && item.version !== 'unknown')
    )
      return false
    if (issue !== 'source_version_unknown' && item.kind !== 'wiki_summary')
      return false
    const key = `${item.kb_id}/${item.page_id || item.chunk_id || item.id}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}
function sourceAction(issue: string) {
  const key = `${issue}_action`
  const translated = k(key)
  return translated === key ? k('source_scope_unresolved_action') : translated
}
function reviewVersion(id: string) {
  const version = detail.value?.analyses.find((item) => item.id === id)?.version
  return version ? `v${version}` : id
}
function hasEvidence(id: string) {
  return !!detail.value?.evidence.some((item) => item.id === id)
}
function evidenceTitle(id: string) {
  return detail.value?.evidence.find((item) => item.id === id)?.title || id
}
async function showEvidence(id: string) {
  const element = document.getElementById(`ops-evidence-${id}`)
  if (element) {
    let ancestor: HTMLElement | null = element
    while (ancestor) {
      if (ancestor.tagName === 'DETAILS')
        (ancestor as HTMLDetailsElement).open = true
      ancestor = ancestor.parentElement
    }
    await nextTick()
    element.scrollIntoView({ block: 'center' })
    element.focus()
  } else notice.value = `${k('evidenceMissing')}: ${id}`
}
async function load() {
  if (!props.runID) return
  const token = ++requestID
  const id = props.runID
  const current = operationScope()
  loading.value = true
  loadError.value = ''
  try {
    const value = await getOpsDetail(id)
    if (token !== requestID || !current()) return
    detail.value = value
    selectedID.value =
      (props.analysisID && value.analyses.some((a) => a.id === props.analysisID)
        ? props.analysisID
        : '') ||
      (value.analyses.some((a) => a.id === selectedID.value)
        ? selectedID.value
        : '') ||
      value.run.analysis?.id ||
      [...value.analyses].sort((a, b) => b.version - a.version)[0]?.id ||
      ''
  } catch (error) {
    if (token === requestID && current()) {
      detail.value = undefined
      loadError.value = k(opsErrorCode(error))
    }
  } finally {
    if (token === requestID && current()) loading.value = false
  }
}
async function rerun() {
  if (!props.runID || retrying.value) return
  const runID = props.runID
  const current = operationScope()
  retrying.value = true
  notice.value = ''
  try {
    await retryOpsRun(runID)
    if (!current()) return
    await load()
    if (!current()) return
    notice.value = k('queued')
    emit('changed')
  } catch (error) {
    if (current()) notice.value = k(opsErrorCode(error))
  } finally {
    if (current()) retrying.value = false
  }
}
async function saveReview(value: ReviewInput) {
  const target = value.target
  const runID = props.runID
  const current = operationScope()
  if (!runID || reviewing[target]) return
  reviewing[target] = true
  reviewErrors[target] = ''
  try {
    await addOpsReview(runID, value)
    if (!current()) return
    await load()
    if (!current()) return
    notice.value = k('saved')
    savedReviews[target]++
    emit('changed')
  } catch (error) {
    if (current()) reviewErrors[target] = k(opsErrorCode(error))
  } finally {
    if (current()) reviewing[target] = false
  }
}
watch(selectedID, () => {
  showCauseEditor.value = false
  showOptionalReview.value = false
})
watch(
  () => [props.runID, props.analysisID, auth.effectiveTenantId],
  () => {
    scopeID++
    requestID++
    selectedID.value = ''
    detail.value = undefined
    notice.value = ''
    loadError.value = ''
    reviewing.answer = false
    reviewing.cause = false
    retrying.value = false
    reviewErrors.answer = ''
    reviewErrors.cause = ''
    if (props.runID) void load()
  },
  { immediate: true, flush: 'sync' },
)
onScopeDispose(() => {
  scopeID++
  requestID++
})
</script>
<style scoped>
.ops-detail {
  font-family: var(--app-font-family);
  font-size: var(--app-text-base);
  line-height: 1.65;
  color: var(--td-text-color-primary);
}
.toolbar,
.toolbar-actions,
.section-heading,
.run-meta,
.claim-heading,
.no-cause {
  display: flex;
  align-items: center;
  gap: var(--app-space-3);
}
.toolbar,
.section-heading {
  justify-content: space-between;
  flex-wrap: wrap;
}
.toolbar {
  margin-bottom: var(--app-space-5);
}
.toolbar-actions {
  margin-left: auto;
  flex-shrink: 0;
}
.version-picker {
  display: flex;
  align-items: center;
  gap: var(--app-space-2);
  max-width: 100%;
}
.version-picker > span {
  white-space: nowrap;
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
}
.version-picker :deep(.t-select__wrap) {
  width: 350px;
  max-width: 100%;
}
.run-heading {
  margin-bottom: var(--app-space-5);
}
.run-heading h2 {
  margin: 0 0 var(--app-space-3);
  font-size: var(--app-text-xl);
  line-height: 1.5;
  font-weight: 600;
  overflow-wrap: anywhere;
}
.run-meta {
  flex-wrap: wrap;
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
}
.run-meta > span + span::before {
  content: '·';
  margin-right: var(--app-space-3);
  color: var(--td-text-color-placeholder);
}
section {
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-lg);
  padding: var(--app-space-5);
  margin: var(--app-space-4) 0;
}
h3 {
  font-size: var(--app-text-lg);
  font-weight: 600;
  margin: 0 0 var(--app-space-3);
}
.section-heading h3 {
  margin: 0;
}
.section-heading {
  margin-bottom: var(--app-space-4);
}
h4 {
  font-size: var(--app-text-base);
  font-weight: 600;
  margin: var(--app-space-5) 0 var(--app-space-2);
}
p {
  margin: var(--app-space-2) 0;
}
.body-text {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.count,
.muted,
small {
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
}
.field-label {
  display: block;
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
  margin-bottom: var(--app-space-2);
}
.diagnosis-first {
  background: var(--td-bg-color-container);
  border-top: 3px solid var(--td-brand-color);
}
.diagnosis-first.high {
  border-top-color: var(--td-warning-color);
}
.decision-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr)) minmax(0, 1.5fr);
  gap: var(--app-space-4);
  margin: var(--app-space-4) 0;
}
.route-reason {
  display: flex;
  gap: var(--app-space-3);
  align-items: baseline;
  margin-top: var(--app-space-4);
  padding-top: var(--app-space-3);
  border-top: 1px solid var(--td-component-border);
}
.route-reason strong {
  flex-shrink: 0;
}
.notice {
  padding: var(--app-space-3) var(--app-space-4);
  background: var(--td-brand-color-light);
  border-radius: var(--app-radius-md);
  font-size: var(--app-text-sm);
}
.unlinked-causes .notice {
  background: var(--td-warning-color-light);
}
.error {
  color: var(--td-error-color);
}
.answer-disclosure {
  padding: var(--app-space-3) var(--app-space-4);
  border-radius: var(--app-radius-md);
  background: var(--td-bg-color-secondarycontainer);
  margin-bottom: var(--app-space-4);
}
.answer-disclosure[open] summary {
  margin-bottom: var(--app-space-4);
}
summary {
  cursor: pointer;
  font-weight: 500;
  overflow-wrap: anywhere;
}
.claim {
  padding: var(--app-space-4) 0;
  border-top: 1px solid var(--td-component-border);
}
.claim:first-child {
  border-top: 0;
  padding-top: 0;
}
.claim:last-child {
  padding-bottom: 0;
}
.claim-heading {
  flex-wrap: wrap;
  gap: var(--app-space-2);
}
.claim-number {
  color: var(--td-text-color-placeholder);
  font-size: var(--app-text-sm);
  font-variant-numeric: tabular-nums;
}
.claim-text {
  font-weight: 500;
}
.evidence-links {
  display: flex;
  gap: var(--app-space-2);
  flex-wrap: wrap;
  margin-top: var(--app-space-2);
}
.evidence-links button {
  background: var(--td-brand-color-light);
  color: var(--td-brand-color);
  border: 0;
  border-radius: var(--app-radius-sm);
  padding: var(--app-space-1) var(--app-space-2);
  cursor: pointer;
  font: inherit;
  font-size: var(--app-text-sm);
  max-width: 100%;
  overflow-wrap: anywhere;
  text-align: left;
}
.source-issues {
  display: grid;
  gap: var(--app-space-3);
  margin: var(--app-space-4) 0;
}
.source-issue {
  padding-left: var(--app-space-3);
  border-left: 2px solid var(--td-warning-color);
}
.source-issue p {
  color: var(--td-text-color-secondary);
}
.material-disclosure,
.report-limitations {
  border-top: 1px solid var(--td-component-border);
  padding-top: var(--app-space-3);
  margin-top: var(--app-space-4);
}
.cause + .cause {
  border-top: 1px solid var(--td-component-border);
  margin-top: var(--app-space-4);
  padding-top: var(--app-space-4);
}
.review-grid {
  display: grid;
  gap: var(--app-space-4);
  margin-top: var(--app-space-4);
}
.review-grid.with-causes {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}
.pending-evidence {
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-lg);
  padding: var(--app-space-4);
  margin-top: var(--app-space-4);
  overflow-wrap: anywhere;
}
.pending-evidence h4 {
  margin: 0 0 var(--app-space-3);
  font-size: var(--app-text-md);
}
.pending-evidence article + article {
  border-top: 1px solid var(--td-component-border);
  padding-top: var(--app-space-3);
  margin-top: var(--app-space-3);
}
.pending-evidence p {
  margin: var(--app-space-1) 0;
  white-space: pre-wrap;
}
.no-cause {
  justify-content: space-between;
  flex-wrap: wrap;
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
  margin-top: var(--app-space-4);
}
.review-record {
  padding: var(--app-space-3) 0;
  border-top: 1px solid var(--td-component-border);
}
.supporting-details {
  margin-top: var(--app-space-5);
}
.secondary-details {
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-lg);
  padding: var(--app-space-4) var(--app-space-5);
  margin: var(--app-space-3) 0;
}
.secondary-details[open] > summary {
  margin-bottom: var(--app-space-4);
}
dl {
  display: grid;
  grid-template-columns: 160px minmax(0, 1fr);
  gap: var(--app-space-2) var(--app-space-4);
  font-size: var(--app-text-sm);
}
dt {
  color: var(--td-text-color-secondary);
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}
li {
  overflow-wrap: anywhere;
}
ul {
  padding-left: var(--app-space-5);
}
.state {
  padding: var(--app-space-10);
  text-align: center;
}
button:focus-visible,
summary:focus-visible,
#ops-review-form:focus {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 3px;
  border-radius: var(--app-radius-sm);
}
@media (max-width: 800px) {
  .review-grid.with-causes {
    grid-template-columns: 1fr;
  }
  .decision-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 600px) {
  section {
    padding: var(--app-space-4);
  }
  .version-picker {
    width: 100%;
  }
  .version-picker :deep(.t-select__wrap) {
    width: 100%;
    min-width: 0;
  }
  .version-picker > span {
    display: none;
  }
  dl {
    grid-template-columns: 1fr;
    gap: var(--app-space-1);
  }
  dd {
    margin-bottom: var(--app-space-2);
  }
  .route-reason {
    flex-direction: column;
    gap: var(--app-space-1);
  }
}
</style>
