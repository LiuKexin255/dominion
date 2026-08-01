# Specification Quality Checklist: Team Template Mode (StateGraph 升级)

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-07-29
**Feature**: [`specs/031-team-template-mode/spec.md`](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic (no implementation details)
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

- 需求方在描述中已对前期调研 `survey/agent-team-mode.md` §8 的 6 项待决策点给出明确澄清（见 spec.md「Clarifications」节），其中 survey Q5（gameEnded 标志生命周期）被需求方明确指定为「方案阶段决策」，已据此在 spec 中约束行为结果（FR-011/FR-017）而将实现细节留待 `plan.md`，未引入任何 [NEEDS CLARIFICATION] 标记。
- spec 初稿中的三处 API 契约级推断已由需求方在补充澄清中**全部确认采用**（见 spec.md「Clarifications — Session 2026-07-29 (补充澄清)」）：(1) 忽略历史数据兼容与迁移（clean break）；(2) Message 路径为会话级作用域 `templates/{template}/sessions/{session}/team/agents/{agent}/messages/{message}`；(3) 不需要短期记忆自动清空（仅 `RefreshTeam` 显式清空，已写入 FR-018）。三者均无歧义，不构成阻塞性问题。
- 本特性为大型架构升级（proto 全量重构 + 3 个 Go 服务 + TS agent 重写 + desktop 重构），建议 `plan.md` 按 US1（API 契约）→ US3（MCP sink）→ US2（team graph）→ US4/US5（desktop/profile）的依赖顺序拆分 phase，并为每 phase 设置独立验证门禁。
- 大型测试（FR-030）为宪法原则 VI 强制项；`tasks.md` 须为该验收单独分配 task（经 testplan skill 完整执行部署→测试→清理闭环，全部用例通过）。
- Items marked incomplete require spec updates before `/speckit.clarify` or `/speckit.plan`.
