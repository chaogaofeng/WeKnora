# 知识运营详情页调整计划

> 执行方式：superpowers:subagent-driven-development；后端证据与队列逻辑独立实现，前端在当前会话实施，完成后交叉审查。

**Goal:** 将诊断依据、来源缺口与复核操作串联，收起候选元数据和技术信息，保留审计可追溯性。

**Architecture:** 仅修改知识运营模块。新诊断将补查审计附加在现有 report JSON 内，无数据库迁移；既有报告不猜测版本关联。候选和技术明细默认折叠，论断与当前未决事项前移。

**Tech Stack:** Go/GORM、Vue 3、TDesign、现有 CSS tokens。

**Spec:** 本会话 2026-10-09 用户认可的详情页调整方案；docs/plans/知识运营模块方案.md 的只读诊断与复核边界。

## Global Constraints

- 延续 feature/generalKB 当前开发环境，不切换正在运行的应用目录。
- 不修改知识、原始问答或已保存报告；不补写历史数据，不推送或合并分支。
- 所有明细沿用现有权限检查；不能凭时间或列表顺序猜测历史证据版本。
- 回答支持性、来源追溯和人工复核是独立维度。

## Task 1: 后端来源审计与补证队列

Files: internal/knowledgeops/{model,service,runtime,source_check,http,workbench}.go 及相关测试；可新增模块内 source_audit.go。

Interfaces:

```typescript
interface SourceAudit {
  evidence_ids: string[] // 本次提供给诊断的事后来源材料，服务器填写
  issues: string[]       // 稳定原因码，不存数据库/提供商原始错误
  chars: number         // 补查正文字符数
  char_limit: number    // 本次补查上限，未知时为 0
}
// report.source_audit?: SourceAudit; run.source_followup_pending?: boolean
```

- [x] 先以测试复现：causes=[]、supported、source_check=unresolved，确认答案后仍保留待补证；来源确已核验的记录保持原关闭规则。
- [x] 来源未决采用独立函数，基于来源状态、已知来源缺口及本次补查限制；不把所有 unknowns 或业务裁决事项一概视为来源缺口。回答确认后整体复核仍为 partially_reviewed，needs_evidence 保留。
- [x] 来源审计由服务端填充，覆盖模型伪造的同名字段；记录各次实际补查材料与具体限制，保存失败诊断时也保留审计。不推断历史关联、不记录敏感原始错误。
- [x] 现有 SourceReader 签名保持兼容；用模块内结构化错误传递 budget、linkage、version、chunk coverage、lookup 等限制。新报告具体说明待补材料，原始模型文字保留，来源范围限制另行展示。
- [x] 测试新旧分析的独立证据关联、预算截断原因、模型伪造审计覆盖和权限回归；运行 go test ./internal/knowledgeops/...。

## Task 2: 详情页信息层级

Files: frontend/src/views/knowledge-ops/DetailDrawer.vue、可复用 EvidenceList.vue / detail.ts、locales/index.ts、frontend/src/api/knowledge-ops.ts、相关组件行为测试。

- [x] 添加失败测试：本版 source_audit 引用的补查材料可见且不混入其他版本；历史引用仍可定位，未绑定存档单独折叠；候选无正文不创建空正文面板；证据定位自动展开。
- [x] 顶部问答元信息和分析版本选择；摘要展示支持性、来源核验、风险、完成状态。
- [x] 问题与回答（使用现有安全 Markdown）、逐条主张与理由前移；引用跳转可展开目标证据。
- [x] 来源核验显示回答时缺口、本次补查范围、所需处理事项；旧报告明确关联缺失，现有存档不冒充本版输入。
- [x] 无根因候选默认展示说明；保留主动纠正入口，不能默认引导给不存在的根因补材料。
- [x] 候选记录默认折叠紧凑展示标题、类型、版本；去除“未送入模型”对整个来源的歧义。
- [x] 合并为默认折叠的“诊断记录与运行信息”，区分回答模型和诊断模型、各阶段时间与限制；复核历史独立保留。
- [x] 前端相关行为测试、type-check、构建校验；修复实际发现的问题。

## Task 3: 联调与审查

- [x] 用“什么是用户意图识别？”的只读数据验证正文、论断、历史补查存档及来源待办展示；不为验收生成虚假人工复核。
- [x] 后端测试（含 race）、vet、前端测试与类型检查；审查 git diff 和模块边界。
- [x] 独立审查本轮修改，修复重要问题；更新开发验收说明。
