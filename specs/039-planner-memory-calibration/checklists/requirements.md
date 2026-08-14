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

> 评估：无 [NEEDS CLARIFICATION]——需求方在描述与 Session 2026-08-08 修订中对各改进均已给出明确决策，剩余歧义（批量中途失败语义、saolei_operate 双形态非法组合/空列表、指令投递存储机制、记忆上限/清理）均在 Edge Cases / Assumptions 中以"留待 plan 决定 + 本 spec 约束行为结果"的方式界定，具备合理默认。FR-001..FR-020 均可测试（含验收场景与 SC-001..SC-009 可度量指标）。SC 为用户/行为视角的可验证结果。Edge Cases 覆盖批量中途结束/拒绝、双形态非法组合、指令时序、压缩时序、old_text 0/多命中、记忆上限、session 清理、旧数据。Assumptions 记录 clean break、memories 复数、memory_id 决策 supersede、target 裁剪、双形态等价性、冻结刷新边界、指令存储机制、批量失败细节、记忆上限/清理、player 装配不变、参考资料。

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

> 评估：每条 FR 均映射到对应的 Acceptance Scenario（US1 #1-8 ↔ FR-001..005；US2 #1-9 ↔ FR-006..012 + FR-020；US3 #1-7 ↔ FR-013..017；FR-018 大型测试 ↔ SC-008；FR-019 两场景拆分）。三个 P1 用户故事覆盖主流程（双形态落子 / planner 长期记忆+memory 服务+memory skill / 校准指令+共享记忆废弃）。SC-001..009 对应可度量结果。实现细节（具体存储机制、proto 字段号、mongo 集合名、agent 内 memory_id 生成策略等）未泄漏，留待 plan。

## Notes

- 本 spec 为对 `specs/031-team-template-mode/` 核心记忆契约的破坏性改进，独立为新 spec 目录 `039-planner-memory-calibration`（Assumptions 已说明产出形态决策依据：调研 `survey/planner-memory-and-agent-communication.md` §7.1）。
- "memories vs memory" 集合段命名：需求方描述中提问"memory 有复数形式吗？"，本 spec 按仓库 AIP-122 约定（复数集合/单数 id）采用 `memories/{memory}`，并已在 Assumptions 记录该决策与需求方原描述的差异。
- **Session 2026-08-08 修订（memory 工具 / 落子工具 / skill）**：基于 hermes 记忆工具参数调研（`research.md` D11），需求方作出 5 项修订并已写入 spec（Clarifications Session 2026-08-08）：① memory 工具由 3 个独立工具（含 `memory_id`）改为 **1 个 hermes 风格 `memory` 工具**（`action`/`content`/`old_text`/`operations`，无 `memory_id`/无 `target`）；② memory 服务存储 API 不变，agent 负责转换（`old_text` 子串定位，0/多命中报错）；③ planner 注入由 `memory_id: 内容` 改为**纯内容**（hermes 风格）；④ `saolei_operate` 改为 hermes 式**双形态参数**（普通参数 `type`/`x`/`y` 或 `operations` 数组）；⑤ 新增 **memory skill**（FR-020，planner 专属）。原 2026-08-07 的 `memory_id` 参数/注入决策已被显式 supersede（spec 内以 supersede 标注 + Assumptions 删除线记录）。本次为 Draft 未实现 spec 的就地修订（宪法原则 II），不新建 spec 目录。
- 调研引用（hermes 冻结快照/记忆工具、openclaw HumanMessage、LangGraph 通道）指向 `survey/planner-memory-and-agent-communication.md`；hermes 记忆工具参数/引导细节见 `research.md` D11（来源 [hermes `tools/memory_tool.py`](https://github.com/NousResearch/hermes-agent/blob/main/tools/memory_tool.py)）。下游 plan 阶段须据此细化：hermes 式 `memory` 工具的 agent 转换（`old_text`→memory_id 匹配）、memory skill 内容与注入（FR-020）、冻结缓存实现（调研 D5）、压缩刷新（D4）、指令投递通道（D6）、saolei_operate 双形态参数的拒绝/优先规则。
- Items marked incomplete require spec updates before `/speckit.clarify` or `/speckit.plan`。当前无 incomplete 项。
