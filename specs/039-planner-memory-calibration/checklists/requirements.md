# Specification Quality Checklist: Planner 长期记忆与校准指令

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-08-07
**Feature**: [`spec.md`](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

> 评估：spec 聚焦 WHAT/WHY（行为与契约语义），未绑定具体实现语言/框架细节；grpc-go/memory mcp 等表述来自需求方明确 directive（属需求约束而非实现选择），与 031 既有 spec 风格一致。所有 mandatory section（User Scenarios & Testing、Requirements、Success Criteria）均已完整。

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic (no implementation details)
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

> 评估：无 [NEEDS CLARIFICATION]——需求方在描述中对 5 项改进均已给出明确决策，剩余歧义（批量中途失败语义、memory_id 冲突处理、指令投递存储机制、记忆上限/清理）均在 Edge Cases / Assumptions 中以"留待 plan 决定 + 本 spec 约束行为结果"的方式界定，具备合理默认。FR-001..FR-018 均可测试（含验收场景与 SC-001..SC-008 可度量指标）。SC 为用户/行为视角的可验证结果。Edge Cases 覆盖批量中途结束/拒绝、空列表、指令时序、压缩时序、id 冲突、记忆上限、session 清理、旧数据。Assumptions 记录 clean break、memories 复数、memory_add id 来源、冻结刷新边界、指令存储机制、批量失败细节、记忆上限/清理、player 装配不变、参考资料。

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

> 评估：每条 FR 均映射到对应的 Acceptance Scenario（US1 #1-6 ↔ FR-001..005；US2 #1-8 ↔ FR-006..012；US3 #1-7 ↔ FR-013..017；FR-018 大型测试 ↔ SC-008）。三个 P1 用户故事覆盖主流程（批量落子 / planner 长期记忆+memory 服务 / 校准指令+共享记忆废弃）。SC-001..008 对应可度量结果。实现细节（具体存储机制、proto 字段号、mongo 集合名等）未泄漏，留待 plan。

## Notes

- 本 spec 为对 `specs/031-team-template-mode/` 核心记忆契约的破坏性改进，独立为新 spec 目录 `039-planner-memory-calibration`（Assumptions 已说明产出形态决策依据：调研 `survey/planner-memory-and-agent-communication.md` §7.1）。
- "memories vs memory" 集合段命名：需求方描述中提问"memory 有复数形式吗？"，本 spec 按仓库 AIP-122 约定（复数集合/单数 id）采用 `memories/{memory}`，并已在 Assumptions 记录该决策与需求方原描述的差异。
- `memory_add` 含 `memory_id` 为需求方明确要求（非常规的服务端生成 id），已在 Assumptions 记录其语义。
- 调研引用（hermes 冻结快照/记忆工具、openclaw HumanMessage、LangGraph 通道）指向 `survey/planner-memory-and-agent-communication.md`，该调研文档为本次 spec 的核心依据；下游 plan 阶段须据此细化冻结缓存实现（调研 D5）、压缩刷新（D4）、指令投递通道（D6）。
- Items marked incomplete require spec updates before `/speckit.clarify` or `/speckit.plan`。当前无 incomplete 项。
