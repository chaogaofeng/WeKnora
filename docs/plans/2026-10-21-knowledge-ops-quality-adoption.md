# 人工回答质量复核与 FAQ 采纳实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement and review each task. 用户已于 2026-10-21 批准设计并授权实施，于 2026-10-22 授权完成 Review 后提交并重启开发服务；不推送或合并其他分支。

**Goal:** 人工确认整条回答可用后，将经过预览、整理和明确批准的标准问答发布至 FAQ 知识库，供后续 RAG / 混合问答召回。

**Architecture:** 人工业务结论与机器证据支持性并列保存。采纳为独立持久化快照，经当前版本、最新通过记录和来源/目标权限验证后调用既有 FAQ 服务，索引成功才记录已入库；诊断器没有写知识的能力。

**Tech Stack:** Go / Gin / GORM、PostgreSQL / SQLite、Vue 3 / TypeScript / TDesign。

**Spec:** [知识运营模块方案](知识运营模块方案.md)，以其中 2026-10-21 的 v1.3 人工采纳扩展替代早期“确认机器结论”及全部只读范围。

## 全局约束

- 业务逻辑集中于 `internal/knowledgeops` 和 `frontend/src/views/knowledge-ops`；可恢复的 FAQ 写入单独放在 `internal/application/service/knowledge_faq_adoption.go`，已有文件只增加 DB 字段和容器接线。普通 FAQ Create/Update、公共服务接口和检索器行为不变。
- 不修改原聊天、机器诊断、Wiki 或原文；本次唯一新增知识写入是用户明确批准的 FAQ 创建或完全一致条目的复用。
- 不做历史问答回填。旧 `confirmed/corrected` 不推定为“回答可用”，不自动转成发布授权。
- 管理员/Owner 还须拥有来源访问权限和目标 FAQ 库写权限；知识库不因来源问答可见而获得写授权。
- 使用新迁移 PostgreSQL `000118` / SQLite `000037`；不改已应用的迁移。
- 人工通过不覆盖机器 `supported/partial/unverifiable`，发布失败不撤销已保存的人工结论。
- 不直接对真实知识发布测试内容；测试使用隔离数据库和 FAQ 边界替身。

## Task 1: 人工业务质量结论

**Files:** `internal/knowledgeops/{review_outcome,current_work,repository,model}.go` 与 `review_outcome_test.go`。

**Interfaces:** 新动作/结论严格匹配：`accepted/usable`、`revision_required/revision_required`、`excluded/excluded`、`needs_more_evidence/unverifiable`。`currentAnswerOutcome(Detail) string` 派生 `Run.answer_outcome`。旧动作接口继续兼容。

- [x] 测试人工接受不可核验回答、理由要求、非法组合、机器报告保持、旧记录不自动变可用、当前版本和独立待办。
- [x] 红测后实现；通过、需修订和排除完成本次回答复核，补证继续挂起。
- [x] 回归旧复核、抽检和来源/技术待办；不把新业务结论计为旧支持性纠正。

## Task 2: 可恢复的 FAQ 采纳

**Files:** `internal/knowledgeops/adoption*.go`、`internal/container/knowledge_ops.go`、模块服务/路由/详情小接点、PG118 / SQLite37 新迁移及相关测试。

**Interfaces:**

```text
GET  /knowledge-ops/adoption-targets
  -> {items:[{id,name}]}
POST /knowledge-ops/runs/:id/adoptions
  {analysis_id,review_id,kb_id,question,answer} -> Adoption
POST /knowledge-ops/runs/:id/adoptions/:adoptionID/retry
  -> Adoption
GET  /knowledge-ops/runs/:id
  -> Detail.adoptions[]
```

`Adoption` 保存原运行/会话/消息/分析/人工通过记录、发布人、目标库、最终问题/答案、状态、FAQ 编号、复用标志、安全错误码、租约和时间。

- [x] 先持久化采纳快照，再调用 FAQ；`tenant/run/analysis` 唯一，不在数据库长事务中调用索引服务。
- [x] 当前版本最新 `accepted + usable` 才可开始发布；重新诊断任务、复核变化及权限撤销不能沿用旧授权。
- [x] 45 秒租约与令牌隔离发布者，20 秒发布预算；中断后可以原采纳记录重试。
- [x] 同标准问且单答案完全一致、启用且确已索引的 FAQ 可明确复用；不同内容、禁用或未索引条目不能覆盖或误报成功。
- [x] 数据库失败和客户端断连不能丢失采纳意图；重试查询既有条目恢复结果。
- [x] SQLite up/down/reapply、唯一约束、旧授权、跨租户、失权、并发及不确定结果的业务测试。
- [x] PostgreSQL 隔离 schema 的迁移、回滚、重放及版本行锁验证。

**Ruling:** 单诊断版本只允许一份发布快照，提交后不静默更改目标或内容。更改同版本请求返回版本冲突，以免网络重试把已批准内容替换成另一份知识。

**恢复复查：** 既有 FAQ 创建在索引成功但状态保存失败时会留下普通列表不可见的 Stored 条目，因此增加独立 `knowledge_faq_adoption.go` 能力，不修改普通 Create/Update。只有自有创建标识匹配的条目才可恢复；重建前清理本条旧向量，明确缺失集合视为无内容可删，网络/权限故障仍停止。Weaviate 删除的 404/422 还需独立 Schema GET 明确返回 404，不能单凭删除错误忽略清理失败。自有草稿用既有 `processing` 门禁阻止召回，状态、召回可见性、计账和审计事务一次提交；复用也排除仍在处理或失败的索引。初始和最终共用索引配置快照，配置变化拒绝完成；SQL TEXT 配置按原始 JSON 读取，公共 Scanner 不变。

## Task 3: 复核和采纳界面

**Files:** 模块 `ReviewForm.vue` / `state.ts` / `AdoptionPanel.vue` / `DetailDrawer.vue` / 列表与状态文案、`frontend/src/api/knowledge-ops.ts`、相关模块测试。

- [x] 主表单改为通过、需修订、待补证、不纳入评估；通过/排除说明选填，另两项必填。
- [x] 新人工质量结论单独展示；旧记录标明历史支持性复核，不改变机器分布口径。
- [x] 通过后提供采纳入口；先显示可编辑完整问题和答案及可写 FAQ 库，用户明确点击发布。
- [x] 多轮问优先用实际已记录的改写作为草稿，人工确认其能脱离上下文理解；不事后冒充实际改写。
- [x] 发布、成功、失败分别展示；失败和悬挂发布可刷新/重试，不重复触发新快照。
- [x] 问答、分析版本、人工复核和工作空间切换后隔离迟到响应；失败保留编辑内容，复核与发布错误分别显示。
- [x] 中英文文案、管理员权限、目标库空态、真实 TDesign 渲染和状态行为回归。

## Task 4: 集成、独立复查与验收

- [x] 后端模块与采集包完整测试、竞态检查、容器接线编译及模块 go vet。
- [x] 前端 68 项模块测试、类型检查、生产构建及实际组件交互检查；同身份刷新保留草稿与失败信息，复核顺序及发布快照合并保留完整时间精度。
- [x] 独立后端和前端 Review；Stored 恢复、向量幂等、缺失集合及索引配置边界已修复并复验。
- [x] 同步 v1.3 方案、实施计划和验收说明，明确 FAQ 的召回范围与纯 Wiki 限制。
- [x] 核对最终改动边界、迁移号段及工作区状态；未提交、推送、合并 main 或发布真实 FAQ。开发服务已自动应用 118，健康检查通过，采纳记录仍为 0。

## 验收要点

1. 机器“不可核验”的回答可经人工判为“回答可用”，两者同时保留且不会相互覆盖。
2. 未通过、旧支持性确认、旧版本或来源/目标失权的请求不能创建 FAQ。
3. 通过后完成标准问答预览，显式发布才调用知识写服务；成功之前不显示已参与召回。
4. 断连/重试只恢复同一份采纳，不重复新建；冲突答案不覆盖既有知识。
5. FAQ 已入库仍须在后续问答的检索范围内；RAG/混合模式可以使用，纯 Wiki 模式默认不调用 FAQ 检索。

**操作边界：** 发布开始后或成功后更改人工结论，不会自动撤回 FAQ；已发布知识后续通过 FAQ 管理维护。同诊断版本的失败快照也不可替换通过记录、目标或内容；需要重新整理发布时先创建新诊断版本。
