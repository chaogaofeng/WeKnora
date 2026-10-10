// Browser smoke test uses an isolated context and mocked APIs; no real user data or model calls.
// Start the frontend separately, then set PLAYWRIGHT_MODULE_PATH if Playwright is not on Node's module path.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE_PATH || 'playwright')
const baseURL = process.env.KNOWLEDGE_OPS_UI_URL || 'http://127.0.0.1:5178'
const fs = require('fs')
;(async () => {
  const browser = await chromium.launch({ headless: true })
  const context = await browser.newContext({
    viewport: { width: 1512, height: 1100 },
    locale: 'zh-CN',
  })
  const user = {
    id: 'acceptance-user',
    username: '验收用户',
    email: 'acceptance@example.invalid',
    tenant_id: 1,
    is_active: true,
  }
  const tenant = { id: 1, name: '知识运营验收', owner_id: user.id }
  const memberships = [{ tenant_id: 1, tenant_name: tenant.name, role: 'owner' }]
  await context.addInitScript(
    ({ user, tenant, memberships }) => {
      localStorage.setItem('weknora_token', 'local-ui-fixture')
      localStorage.setItem('weknora_user', JSON.stringify(user))
      localStorage.setItem('weknora_tenant', JSON.stringify(tenant))
      localStorage.setItem('weknora_memberships', JSON.stringify(memberships))
      localStorage.setItem('locale', 'zh-CN')
      localStorage.setItem('weknora:new-user-guide-done:v1', '1')
    },
    { user, tenant, memberships },
  )
  const settings = {
    enabled: true,
    model_id: 'fixture-model',
    auto_record_enabled: false,
    calibration_note: '',
    sample_percent: 10,
    max_evidence_chars: 96000,
    max_output_tokens: 8192,
    timeout_seconds: 180,
    capture_enabled_at: '2026-09-28T08:00:00Z',
    updated_at: '2026-09-28T08:00:00Z',
  }
  const evidence = [
    {
      id: 'ev-1',
      run_id: 'run-1',
      kind: 'wiki',
      kb_id: 'kb-1',
      knowledge_id: '',
      chunk_id: '',
      page_id: 'page-1',
      slug: '产品/适用条件',
      version: '3',
      title: '产品适用条件',
      content: '申请需满足经营满两年、资质审核通过。额度按风险评级另行审批。',
      hash: 'snapshot-hash',
      tool_call_id: 'call-1',
      round: 2,
      sent_to_model: true,
      post_hoc: false,
      truncated: false,
      acquired_at: '2026-09-28T08:01:00Z',
    },
    {
      id: 'ev-2',
      run_id: 'run-1',
      kind: 'source_document',
      kb_id: 'kb-1',
      knowledge_id: 'doc-1',
      chunk_id: 'chunk-1',
      page_id: '',
      slug: '',
      version: '1',
      title: '产品政策原文',
      content: '经营年限与资质需同时满足，额度需独立审批。',
      hash: 'source-hash',
      tool_call_id: '',
      round: 0,
      sent_to_model: false,
      post_hoc: true,
      truncated: false,
      acquired_at: '2026-09-28T08:02:00Z',
    },
  ]
  const analysis = {
    id: 'analysis-1',
    run_id: 'run-1',
    version: 1,
    status: 'succeeded',
    report: {
      verdict: 'partial',
      claims: [
        {
          text: '经营满两年即可申请固定额度',
          position: '回答第1句',
          verdict: 'partial',
          evidence_ids: ['ev-1'],
          reason: '遗漏资质审核与额度独立审批条件。',
        },
      ],
      facts: ['回答遗漏了来源中的限制条件。'],
      causes: [
        {
          category: 'generation',
          summary: '模型在归纳时遗漏了两个适用条件',
          evidence_ids: ['ev-1', 'ev-2'],
          counter_evidence: ['尚未覆盖所有相关制度'],
          status: 'evidence_backed',
        },
      ],
      unknowns: [],
      review_questions: ['确认资质和额度条件是否适用于当前申请对象。'],
      source_check: 'consistent',
      risk: 'high',
      scope: '本次回答与捕获页面及其当前关联原文；未扫描全库',
    },
    route: 'human_review',
    route_reason: '高风险业务结论，须人工复核',
    policy_version: 'knowledge-ops-v1.2-1',
    model_id: 'fixture-model',
    model_name: 'UI 验收模拟模型',
    group_key: 'same-source-v3',
    sample_percent: 10,
    sample_stratum: 'hybrid-rag-wiki/web/high/consistent',
    input_tokens: 1400,
    output_tokens: 500,
    duration_ms: 2500,
    created_at: '2026-09-28T08:02:00Z',
  }
  analysis.cause_assessment = { candidates: analysis.report.causes, unlinked: [] }
  const runs = [
    {
      id: 'run-1',
      actor_id: 'web_user:acceptance-user',
      session_id: 'session-1',
      message_id: 'message-1',
      request_id: 'request-1',
      channel: 'web',
      qa_type: 'hybrid-rag-wiki',
      execution_mode: 'smart-reasoning',
      agent_id: 'agent-1',
      model_id: 'answer-model',
      question: '经营满两年可以申请多少额度？',
      answer: '经营满两年即可申请固定额度。',
      status: 'completed',
      capture_status: 'complete',
      gaps: [],
      started_at: '2026-09-28T08:01:00Z',
      finished_at: '2026-09-28T08:01:20Z',
      updated_at: '2026-09-28T08:01:20Z',
      review_state: 'unreviewed',
      analysis,
    },
  ]
  runs.push({
    ...runs[0],
    id: 'run-2',
    message_id: 'message-2',
    question: '同类企业额度是否需要单独审批？',
  })
  let historicalMode = false
  let paginationMode = false
  let reviews = []
  let simulateConflict = false
  let apiFailure = false
  let detailDenied = false
  let awaitingDiagnosis = false
  const submitted = []
  const filterRequests = []
  await context.route('**/api/**', async (route) => {
    const req = route.request(),
      url = new URL(req.url()),
      path = url.pathname
    if (path.endsWith('/knowledge-ops/runs'))
      filterRequests.push(Object.fromEntries(url.searchParams))
    if (!path.startsWith('/api/')) {
      await route.continue()
      return
    }
    let data = []
    let status = 200
    if (apiFailure && path.endsWith('/knowledge-ops/runs')) {
      await route.fulfill({ status: 500, json: { success: false, error: 'knowledge_ops_failed' } })
      return
    }
    if (detailDenied && path.endsWith('/runs/run-1')) {
      await route.fulfill({ status: 404, json: { success: false, error: 'not_found' } })
      return
    }
    if (path.endsWith('/auth/me'))
      data = { user, tenant, memberships, capabilities: { can_create_tenant: false } }
    else if (path.includes('/knowledge-ops/settings')) {
      if (req.method() === 'PUT') Object.assign(settings, req.postDataJSON())
      data = settings
    } else if (path.includes('/knowledge-ops/overview'))
      data = {
        total: runs.length,
        complete: runs.length,
        partial: 0,
        missing: 0,
        by_mode: { 'hybrid-rag-wiki': runs.length },
        by_route: { human_review: runs.length },
        by_verdict: { partial: runs.length },
        pending_tasks: 0,
        review_count: reviews.length,
        coverage_by_mode: {
          'hybrid-rag-wiki': { total: runs.length, complete: runs.length, partial: 0, missing: 0 },
        },
        source_checks: runs.reduce((counts, run) => {
          const status = run.analysis.report.source_check
          counts[status] = (counts[status] || 0) + 1
          return counts
        }, {}),
        causes: analysis.cause_assessment.candidates.length ? { generation: runs.length } : {},
        queue_counts: { human_review: runs.length },
        diagnosis_status: { succeeded: runs.length },
        pending_runs: runs.length,
        content_coverage: { total: runs.length, complete: runs.length, partial: 0, missing: 0 },
        content_coverage_by_mode: {
          'hybrid-rag-wiki': { total: runs.length, complete: runs.length, partial: 0, missing: 0 },
        },
        provenance_counts: { known: runs.length },
        sample_total: 0,
        sample_reviewed: 0,
        sample_corrected: 0,
      }
    else if (path.endsWith('/runs/run-1/reviews')) {
      if (simulateConflict) {
        await route.fulfill({ status: 409, json: { success: false, error: 'version_changed' } })
        return
      }
      const input = req.postDataJSON()
      submitted.push(input)
      data = {
        ...input,
        question: runs[0].question,
        analysis_version: 1,
        machine_verdict: analysis.report.verdict,
        machine_causes: analysis.report.causes,
        id: 'review-' + (reviews.length + 1),
        run_id: 'run-1',
        reviewer_id: user.id,
        created_at: new Date().toISOString(),
      }
      reviews.unshift(data)
    } else if (path.endsWith('/runs/run-1/retry')) data = { queued: true }
    else if (path.endsWith('/runs/run-1')) {
      const current = awaitingDiagnosis ? null : historicalMode
        ? {
            ...analysis,
            id: 'analysis-2',
            version: 2,
            report: { ...analysis.report, verdict: 'supported' },
          }
        : analysis
      data = {
        pending_tasks: awaitingDiagnosis ? 1 : 0,
        run: { ...runs[0], analysis: current },
        evidence,
        analyses: awaitingDiagnosis ? [] : historicalMode ? [current, analysis] : [analysis],
        reviews: awaitingDiagnosis ? [] : reviews,
      }
    } else if (path.endsWith('/knowledge-ops/groups'))
      data = {
        items: runs.length
          ? [{ key: 'same-source-v3', count: runs.length, representative: runs[0] }]
          : [],
        total: runs.length ? 1 : 0,
      }
    else if (path.endsWith('/knowledge-ops/runs'))
      data = {
        items:
          paginationMode && url.searchParams.get('page') === '2'
            ? [{ ...runs[0], id: 'page-2', question: '分页第二页测试' }]
            : runs,
        total: paginationMode ? 41 : runs.length,
      }
    else if (path.endsWith('/knowledge-ops/reviews'))
      data = { items: reviews, total: reviews.length }
    else if (path.includes('/models'))
      data = [
        {
          id: 'fixture-model',
          name: 'UI 验收模拟模型',
          type: 'KnowledgeQA',
          status: 'active',
          source: 'remote',
          parameters: {},
        },
      ]
    else if (path.includes('capabilities')) data = { edition: 'standard', capabilities: {} }
    else if (path.includes('sessions')) data = { items: [], total: 0 }
    else if (path.includes('initialization')) data = { initialized: true }
    await route.fulfill({ status, json: { success: true, data } })
  })
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', (e) => errors.push(e.message))
  await page.goto(baseURL + '/platform/knowledge-ops')
  try {
    await page.waitForSelector('.ops-workbench', { timeout: 30000 })
  } catch (e) {
    console.log(
      JSON.stringify({ url: page.url(), errors, body: await page.locator('body').innerText() }),
    )
    throw e
  }
  await page.getByText('经营满两年可以申请多少额度？', { exact: true }).waitFor()
  await page.locator('.cause-link').filter({ hasText: '回答生成' }).click()
  await page.waitForFunction(() => document.activeElement?.id === 'ops-run-list')
  if (filterRequests.at(-1).cause_category !== 'generation' || filterRequests.at(-1).page !== '1')
    throw new Error('cause drilldown did not apply the category and reset pagination')
  await Promise.all([
    page.waitForResponse((response) => response.url().includes('/knowledge-ops/runs?') && !response.url().includes('cause_category')),
    page.getByRole('button', { name: '重置', exact: true }).click(),
  ])
  await page.locator('.tabs').getByText('待复核', { exact: true }).click()
  await page.waitForURL('**/*tab=queue')
  await page.getByRole('button', { name: '查看组内案例', exact: true }).waitFor()
  await page.locator('.tabs').getByText('覆盖与诊断总览', { exact: true }).click()
  await page.waitForURL('**/*tab=overview')
  paginationMode = true
  await page.locator('.list-panel').getByRole('button', { name: '刷新', exact: true }).click()
  await page.locator('.pagination').getByText('2', { exact: true }).click()
  await page.getByText('分页第二页测试', { exact: true }).waitFor()
  if (filterRequests.at(-1).page !== '2' || filterRequests.at(-1).page_size !== '20')
    throw new Error('pagination query changed')
  paginationMode = false
  await page.goto(baseURL + '/platform/knowledge-ops')
  await page.getByText('经营满两年可以申请多少额度？', { exact: true }).waitFor()
  const out = process.env.KNOWLEDGE_OPS_UI_OUTPUT || require('path').resolve(__dirname, '../docs/images/knowledge-ops-acceptance')
  fs.mkdirSync(out, { recursive: true })
  await page.locator('.ops-filter-bar').screenshot({ path: out + '/filters.png' })
  if (await page.locator('.advanced-filters').count())
    throw new Error('advanced filters should start collapsed')
  await page.getByRole('button', { name: '更多筛选', exact: true }).click()
  await page.locator('.advanced-field').filter({ hasText: '风险' }).locator('input').click()
  await page.locator('.t-select-option').filter({ hasText: /^高$/ }).click()
  await page.locator('.filter-search input').fill('额度')
  await page.locator('.period-picker input').first().click()
  await page.getByText('最近 7 天', { exact: true }).click()
  await page.getByRole('button', { name: '应用筛选', exact: true }).click()
  await page.locator('.applied-filters').getByText('风险: 高', { exact: true }).waitFor()
  const query = filterRequests.at(-1)
  if (
    query.risk !== 'high' ||
    query.search !== '额度' ||
    !query.from?.endsWith('Z') ||
    !query.to?.endsWith('Z')
  )
    throw new Error('filter contract changed: ' + JSON.stringify(query))
  await page.locator('.ops-filter-bar').screenshot({ path: out + '/filters-active.png' })
  await page
    .locator('.applied-filters .t-tag')
    .filter({ hasText: '风险: 高' })
    .locator('.t-tag__icon-close')
    .click()
  await page.waitForFunction(
    () => !document.querySelector('.applied-filters')?.textContent?.includes('风险: 高'),
  )
  if (filterRequests.at(-1).risk) throw new Error('removing filter tag did not remove query')
  await page.getByRole('button', { name: '重置', exact: true }).click()
  await page.waitForFunction(() => !document.querySelector('.applied-filters'))
  await page.getByRole('button', { name: '更多筛选', exact: true }).click()
  await page.waitForTimeout(200)
  await page.screenshot({ path: out + '/overview.png', fullPage: true })
  await page.locator('.insights').scrollIntoViewIfNeeded()
  await page.screenshot({ path: out + '/insights.png', fullPage: true })
  await page.goto(baseURL + '/platform/knowledge-ops?tab=queue')
  await page.getByRole('button', { name: '查看组内案例', exact: true }).waitFor()
  await page.getByText('确认资质和额度条件是否适用于当前申请对象。', { exact: true }).waitFor()
  await page.screenshot({ path: out + '/queue.png', fullPage: true })
  await page.evaluate(async () => {
    const { useTheme } = await import('/src/composables/useTheme.ts')
    useTheme().setTheme('dark')
  })
  await page.waitForTimeout(350)
  await page.screenshot({ path: out + '/dark.png', fullPage: true })
  await page.evaluate(async () => {
    const { useTheme } = await import('/src/composables/useTheme.ts')
    useTheme().setTheme('light')
  })
  await page.waitForTimeout(350)
  await page.setViewportSize({ width: 900, height: 1050 })
  if (
    !(await page.locator('.ops-workbench').evaluate((el) => el.scrollWidth <= el.clientWidth + 1))
  )
    throw new Error('queue overflows narrow workbench')
  await page.screenshot({ path: out + '/queue-compact.png', fullPage: true })
  await page.setViewportSize({ width: 1512, height: 1100 })

  await page.getByRole('button', { name: '查看组内案例', exact: true }).click()
  await page.getByText('同类企业额度是否需要单独审批？', { exact: true }).waitFor()
  if ((await page.locator('.queue-case').count()) !== 2)
    throw new Error('group members not listed independently')
  await page.goto(baseURL + '/platform/knowledge-ops?tab=queue&run=run-1')
  await page.waitForSelector('.ops-detail')
  await page.locator('.answer-disclosure > summary').click()
  await page.getByText('经营满两年即可申请固定额度。', { exact: true }).waitFor()
  await page.waitForTimeout(350)
  if (!(await page.locator('.diagnosis-first').isVisible())) throw new Error('diagnosis not first')
  await page.screenshot({ path: out + '/detail.png', fullPage: true })
  const forms = page.locator('.review-form')
  await forms.nth(1).locator('textarea').fill('需补充来源适用期核对')
  await forms.nth(1).locator('.review-action input').click()
  await page
    .locator('.t-select-option')
    .filter({ hasText: /^需补证$/ })
    .click()
  await forms.nth(0).locator('textarea').fill('已对照生成时片段，确认部分支持')
  await forms.nth(0).getByRole('button', { name: '保存本项复核' }).click()
  await page.getByText('复核已追加保存', { exact: true }).waitFor()
  if ((await forms.nth(1).locator('textarea').inputValue()) !== '需补充来源适用期核对')
    throw new Error('saving answer erased cause draft')
  await forms.nth(1).getByRole('button', { name: '保存本项复核' }).click()
  await page.waitForFunction(
    () => document.querySelectorAll('.review-form textarea')[1]?.value === '',
  )
  if (
    submitted.length !== 2 ||
    submitted[0].target !== 'answer' ||
    submitted[1].target !== 'cause' ||
    submitted[1].verdict !== 'unresolved'
  )
    throw new Error('wrong review payload')
  await page.locator('.review-grid').scrollIntoViewIfNeeded()
  await page.screenshot({ path: out + '/review.png', fullPage: true })
  simulateConflict = true
  await forms.nth(0).locator('textarea').fill('过时版本不得提交')
  await forms.nth(0).getByRole('button', { name: '保存本项复核' }).click()
  await forms.nth(0).getByRole('alert').filter({ hasText: '分析版本已变化' }).waitFor()
  simulateConflict = false
  historicalMode = true
  await page.goto(baseURL + '/platform/knowledge-ops?tab=records')
  await page.getByText('已对照生成时片段，确认部分支持', { exact: true }).waitFor()
  await page.screenshot({ path: out + '/records.png', fullPage: true })
  await page
    .locator('table tbody tr')
    .first()
    .getByRole('button', { name: '查看诊断', exact: true })
    .click()
  await page.locator('.diagnosis-first').getByText('历史版本 · 只读', { exact: true }).waitFor()
  if ((await page.locator('.review-form button[type=submit]:disabled').count()) !== 2)
    throw new Error('historical version permits review')
  if (!page.url().includes('analysis=analysis-1'))
    throw new Error('record did not link its bound version')
  await page.goto(baseURL + '/platform/knowledge-ops?tab=records')
  await page.getByRole('button', { name: '诊断设置', exact: true }).click()
  await page.locator('.ops-settings .model-select input').waitFor()
  await page.waitForTimeout(350)
  await page.screenshot({ path: out + '/settings.png', fullPage: true })
  if (
    !(await page.locator('.ops-settings .model-select input').inputValue()).includes(
      'UI 验收模拟模型',
    )
  )
    throw new Error('model selector empty')
  await page.locator('.ops-settings input[type=number]').nth(0).fill('25')
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  await page.getByText('设置已保存', { exact: true }).waitFor()
  if (settings.sample_percent !== 25) throw new Error('settings not saved')
  historicalMode = false
  const originalReport = analysis.report
  analysis.report = { ...originalReport, causes: [], source_check: 'unresolved' }
  analysis.cause_assessment = { candidates: [], unlinked: [] }
  runs[0].review_state = 'reviewed'
  runs[0].source_followup_pending = true
  await page.goto(baseURL + '/platform/knowledge-ops')
  const reviewCell = page.locator('.review-state-cell').first()
  await reviewCell.getByText('已复核', { exact: true }).waitFor()
  await reviewCell.getByText('来源待核验', { exact: true }).waitFor()
  if ((await reviewCell.innerText()).includes('部分复核')) throw new Error('source followup downgraded human review')
  await page.locator('.list-panel').getByRole('button', { name: '查看诊断', exact: true }).first().click()
  await page.locator('.run-meta').getByText('当前人工复核 · 已复核', { exact: true }).waitFor()
  await page.locator('.run-meta').getByText('来源待核验', { exact: true }).waitFor()
  await page.waitForFunction(() => {
    const rect = document.querySelector('.ops-detail')?.getBoundingClientRect()
    return rect && rect.left >= 0 && rect.right <= innerWidth + 1
  })
  await page.screenshot({ path: out + '/reviewed-source-pending.png' })
  await page.goto(baseURL + '/platform/knowledge-ops?tab=queue')
  await page.locator('.queue-case .source-followup').getByText('来源待核验', { exact: true }).first().waitFor()
  if (!(await page.locator('.queue-case').first().innerText()).includes('已复核')) throw new Error('queue lost human completion')
  runs[0].coverage = {
    content_status: 'complete', provenance_status: 'pending', content_gaps: [],
    provenance_gaps: ['wiki_linked_summary_versions_unknown'],
  }
  analysis.report.source_check = 'inconsistent'
  analysis.report.source_audit = {
    issues: ['source_linked_versions_unknown', 'source_lookup_failed'],
    evidence_ids: ['ev-2'], chars: 100, char_limit: 16000,
  }
  evidence.push({
    ...evidence[0], id: 'summary-unknown', kind: 'wiki_summary', page_id: 'neighbor-page',
    slug: '关联页/缺少版本', title: '未记录版本的关联摘要', version: 'unknown',
    content: '模型实际收到的关联摘要', truncated: true,
  })
  await page.goto(baseURL + '/platform/knowledge-ops')
  await page.locator('.list-panel .provenance').getByText('来源版本待核验', { exact: true }).waitFor()
  const versionSwitch = page.getByRole('switch', { name: '显示来源版本核验', exact: true })
  const beforeDisplayChange = await page.locator('.metrics').innerText()
  await versionSwitch.click()
  await page.locator('.list-panel .provenance').waitFor({ state: 'hidden' })
  if (await page.locator('.list-panel .source-followup').count()) throw new Error('source badge remains when display is off')
  if (await page.locator('.provenance-summary').count()) throw new Error('version stats remain when display is off')
  await page.locator('.source-panel').getByText('不一致', { exact: true }).waitFor()
  if (await page.locator('.metrics').innerText() !== beforeDisplayChange) throw new Error('display preference changed operational counts')
  if (await versionSwitch.getAttribute('aria-checked') !== 'false') throw new Error('version switch did not turn off')
  await page.reload()
  await page.locator('.list-panel').getByRole('button', { name: '查看诊断', exact: true }).first().waitFor()
  if (await versionSwitch.getAttribute('aria-checked') !== 'false') throw new Error('display preference was not restored on reload')
  await page.screenshot({ path: out + '/source-versions-hidden.png', fullPage: true })
  await page.locator('.list-panel').getByRole('button', { name: '查看诊断', exact: true }).first().click()
  await page.locator('.run-meta').getByText('当前人工复核 · 已复核', { exact: true }).waitFor()
  if (await page.locator('.run-meta').getByText('来源待核验', { exact: true }).count()) throw new Error('detail header ignores preference')
  await page.locator('#ops-source-check .section-heading').getByText('不一致', { exact: true }).waitFor()
  await page.locator('#ops-source-check').getByText('来源补查未能完成', { exact: true }).waitFor()
  const versionDetails = page.locator('.version-details')
  if (await versionDetails.getByText('Wiki 关联摘要版本未知', { exact: true }).isVisible()) throw new Error('version gap should be folded')
  await versionDetails.locator('summary').click()
  await versionDetails.getByText('Wiki 关联摘要版本未知', { exact: true }).waitFor()
  await versionDetails.getByRole('button', { name: '未记录版本的关联摘要', exact: true }).click()
  await page.locator('#ops-evidence-summary-unknown').getByText('模型实际收到的关联摘要', { exact: true }).waitFor()
  await page.locator('#ops-evidence-summary-unknown').getByText('Wiki 关联页摘要', { exact: true }).waitFor()
  await page.goto(baseURL + '/platform/knowledge-ops?tab=queue')
  await page.locator('.queue-case').first().waitFor()
  if (await page.locator('.queue-case .source-followup, .queue-case .provenance').count()) throw new Error('queue ignores display preference')
  await page.locator('.queue-case').first().getByText('已复核', { exact: false }).waitFor()
  await page.setViewportSize({ width: 800, height: 1000 })
  await page.screenshot({ path: out + '/source-versions-hidden-narrow.png', fullPage: true })
  await page.setViewportSize({ width: 1512, height: 1100 })
  await versionSwitch.focus()
  await versionSwitch.press('Space')
  await page.locator('.queue-case .source-followup').first().waitFor()
  await page.locator('.queue-case .provenance').first().waitFor()
  awaitingDiagnosis = true
  await page.goto(baseURL + '/platform/knowledge-ops?run=run-1')
  await page.locator('.ops-detail .capture-coverage').waitFor()
  if (await page.locator('.diagnosis-first').count()) throw new Error('fixture unexpectedly has a diagnosis')
  await page.locator('.ops-detail .provenance').getByText('来源版本待核验', { exact: true }).waitFor({ timeout: 5000 })
  await page.goto(baseURL + '/platform/knowledge-ops')
  await versionSwitch.click()
  await page.locator('.list-panel').getByRole('button', { name: '查看诊断', exact: true }).first().click()
  await page.locator('.ops-detail .capture-coverage').waitFor()
  if (await page.locator('.ops-detail .provenance').count()) throw new Error('undiagnosed detail ignores hidden version preference')
  await page.goto(baseURL + '/platform/knowledge-ops')
  await versionSwitch.click()
  awaitingDiagnosis = false
  delete runs[0].coverage
  evidence.pop()
  runs[0].review_state = 'unreviewed'
  runs[0].source_followup_pending = false
  const positiveCause = { ...originalReport.causes[0], summary: '模型准确提取要点，生成内容与原文高度一致，无幻觉或遗漏。' }
  analysis.report = { ...originalReport, claims: originalReport.claims.map((claim) => ({ ...claim, verdict: 'supported' })), causes: [positiveCause] }
  analysis.cause_assessment = { candidates: [], unlinked: [positiveCause] }
  await page.goto(baseURL + '/platform/knowledge-ops?run=run-1')
  await page.locator('.unlinked-causes').getByText('归因一致性待核实', { exact: true }).waitFor()
  if (await page.locator('.cause-panel').count()) throw new Error('unlinked attribution still appears in cause counts')
  if (await page.locator('#review-action-cause').count()) throw new Error('unlinked attribution offered for direct confirmation')
  if (await page.locator('.unlinked-causes').getByText(positiveCause.summary, { exact: true }).isVisible())
    throw new Error('raw unlinked attribution should start folded')
  await page.locator('.unlinked-causes summary').click()
  await page.locator('.unlinked-causes').getByText(positiveCause.summary, { exact: true }).waitFor()
  await page.locator('.unlinked-causes').scrollIntoViewIfNeeded()
  await page.screenshot({ path: out + '/unlinked-cause.png' })
  await page.setViewportSize({ width: 800, height: 1000 })
  await page.screenshot({ path: out + '/unlinked-cause-narrow.png' })
  await page.goto(baseURL + '/platform/knowledge-ops?tab=queue')
  await page.locator('.unlinked-cause-notice').first().waitFor()
  if (await page.locator('.diagnosis').filter({ hasText: positiveCause.summary }).count())
    throw new Error('queue still presents an excluded attribution as a root-cause diagnosis')
  analysis.report = originalReport
  analysis.cause_assessment = { candidates: originalReport.causes, unlinked: [] }
  runs.splice(0)
  await page.goto(baseURL + '/platform/knowledge-ops')
  await page.getByText('当前范围内暂无记录', { exact: true }).waitFor()
  await page.screenshot({ path: out + '/empty.png', fullPage: true })
  await page.setViewportSize({ width: 800, height: 1000 })
  await page.screenshot({ path: out + '/compact.png', fullPage: true })
  apiFailure = true
  await page.locator('.page-header').getByRole('button', { name: '刷新', exact: true }).click()
  await page.getByText('知识运营数据加载失败', { exact: true }).waitFor()
  apiFailure = false
  await page.evaluate(() =>
    localStorage.setItem('weknora_memberships', JSON.stringify([{ tenant_id: 1, role: 'viewer' }])),
  )
  memberships[0].role = 'viewer'
  await page.goto(baseURL + '/platform/knowledge-ops')
  await page.waitForURL('**/platform/knowledge-bases')
  if (await page.locator('.ops-workbench').count()) throw new Error('viewer accessed workbench')
  console.log(
    JSON.stringify(
      {
        checks: [
          'TDesign tab route state',
          'server pagination',
          'dark theme',
          'narrow queue layout',
          'compact filter bar',
          'advanced filters and dates',
          'remove applied filter',
          'overview',
          'cause category drilldown',
          'unlinked attribution excluded and preserved for audit',
          'queue excludes unlinked attribution',
          'completed human review with independent source followup',
          'source version display preference persists without changing counts',
          'hidden version gaps remain accessible while content conflicts stay visible',
          'version gap links to the exact consumed summary evidence',
          'undiagnosed detail follows both version display preferences',
          'grouped queue diagnosis',
          'group members',
          'diagnosis first',
          'bound historical review',
          'detail',
          'answer review',
          'cause draft preserved',
          'cause review',
          '409 conflict',
          'records',
          'settings save',
          'empty',
          'compact',
          '500 error state',
          'viewer route guard',
        ],
        submitted,
        errors,
      },
      null,
      2,
    ),
  )
  await browser.close()
  if (errors.length) throw new Error(`Browser runtime errors: ${errors.join('; ')}`)
})().catch((e) => {
  console.error(e)
  process.exit(1)
})
