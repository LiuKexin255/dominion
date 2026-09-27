# Specification Quality Checklist: dsh 升级到 0.1.7 线（core 底座 + agent_v2 官方插件 + preset 模型重构）

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-09-27
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs) — 版本线/包名/preset 模型为需求本体（WHAT）；构建工具仅出现在验收口径
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders — preset 产品语义五项独立于模型表述
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain — Q1（目标线 0.1.7-rc.2 + preset 重构）、Q2（全仓库范围）已由用户决策并回填（FR-002/FR-003/FR-008）
- [x] Requirements are testable and unambiguous — 每个 FR 有可验证口径（构建/审计/单测/大型测试）
- [x] Success criteria are measurable — 零混装/测试全绿/审计通过/语义用例覆盖
- [x] Success criteria are technology-agnostic (no implementation details)
- [x] All acceptance scenarios are defined — 5 个 user story 均含 Given/When/Then
- [x] Edge cases are identified — peer 互锁、旧包移除、preset 模型语义差、持久化映射、新增 peer、原生 addon、API 破坏、回退规则
- [x] Scope is clearly bounded — desktop/Go 侧明确不在迁移面；preset 持久化映射方案划归 plan
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows — 底座、插件升级、preset 重构、生态同线、前端对齐
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

- 2026-09-27 用户决策：Q1 = B（0.1.7-rc.2 线；preset 按 0.1.7 新模型重构——`dsh-agent-preset-registry` + `dsh-agent-preset`，旧 `dsh-agent-presets` 移除）；Q2 = A（全仓库同线迁移）。已回填 spec。
- 调研事实依据（registry + 上游仓库实测，2026-09-27）：0.1.7-rc.2 线 peer 互锁 cordis ~4.0.4 / loader ~1.0.5 / include ~1.0.9 / group ~1.0.4 / timer 1.1.6 / node-addon-require-builtin 0.1.6；新 preset 包 0.1.7-rc.2 已发布；旧 presets 包最高 0.1.5-rc.3 且 peer 与 0.1.7 线互斥；preset 新模型语义见上游架构注记（declarative-agent-presets，2026-09-18）。
- Spec ready for `/speckit.clarify` 或 `/speckit.plan`。
