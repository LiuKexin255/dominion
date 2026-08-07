# Specification Quality Checklist: saolei Team 模板优化

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-08-05
**Feature**: [`spec.md`](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs) — 本特性为已实现系统（031）的优化/修复，引用既有契约实体（`playerMessages`/`plannerMessages` 通道、`streamEvents`、`update_strategy` 工具等）作为溯源指针（宪法原则 I），属 refinement spec 的精确性需要，非新引入的实现细节。
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders — 面向已知 031 系统的利益相关者，引用既有词汇便于可追溯。
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain — 全部以合理默认（Assumptions）覆盖，无歧义阻塞项。
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic — 部分引用既有契约实体（如 `update_strategy`），为 refinement spec 的精确验收所需，可验证。
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified — 8 个 edge case 覆盖压缩失败、空通道、RefreshTeam 交互、去重、计数并发等。
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows — 5 个 US（实时可见性修复、上下文压缩、工具描述注入、消息上限、游戏统计数据）覆盖全部需求 + bug。
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification — 引用既有契约为溯源，非新实现细节。

## Notes

- 本特性为对 `specs/031-team-template-mode` 已实现行为的优化与缺陷修复，非新架构。引用既有契约实体（通道名、工具名、streamEvents、CellStatus/MineCounter 等）是为满足宪法原则 I（引用溯源）与需求可验证性，非引入新实现细节。
- US1（planner 游戏历史实时可见性）建立的"非模型产出通道消息→实时帧"机制是 US2（压缩摘要实时可见）的前提；二者共享同一根因（`streamEvents` 不产出 createAgent 输入 HumanMessage 事件）与同一修复方向。此依赖关系已在 Clarifications 与 Assumptions 显式记录。
- 用户确认项（"压缩内容 desktop 可见、需确认无需改动"）的结论：经分析，压缩摘要在重载（ListMessages）天然可见；实时可见复用 US1 帧发射机制，desktop 无需额外改动（前提 US1 落地）。已写入 Clarifications。
- **新增特性（游戏统计数据）**：saolei MCP game end 事件增加三项统计（操作次数、正确标记地雷数、每雷平均操作数）。经调研确认统计数据可由 MCP 第一手计算：correctFlags = 总地雷数（取自开局 mineCounter）− 终局 MINE/HIT_MINE 格数；operationCount = 本局 onMove 触发次数。统计数据经 onGameEnd → buffer → planner 复盘输入流转。除零（y=0）与 counter 不可解码情形有降级约束（FR-029/FR-033）。扩展 sink 接口携带游戏统计不违背 FR-019（统计数据为游戏概念，非 team mode 耦合）。
- 所有模糊项（压缩语义=整体替换、压缩模型=复用各自模型、desktop 上限默认值、压缩失败=直接 abort、统计数据除零表示）均有明确决策或合理默认并记录于 Clarifications/Assumptions，无阻塞澄清项。
- Items marked incomplete require spec updates before `/speckit.clarify` or `/speckit.plan` — 全部通过，可进入下一阶段。
- **2026-08-05 复审精化（/speckit.analyze → /speckit.specify）**：基于分析报告修订以下条目并复验全部通过：
  - **D1**：FR-020 与 FR-022 语义重复——FR-020 收敛为"每 agent tab 独立的消息数量上限"，FR-022 聚焦"移除仅作用于该 tab、其他 tab 不受影响"（消除"独立计数"重复表述）。
  - **B1**：FR-012 "有意义的摘要"增加可测判据——摘要文本 trim 后长度 > 0，空/纯空白视为压缩失败按 FR-013 abort。
  - **B2**：US2 AS9 "planner → player 边不变"改为"路由行为不变：仍回到 player"（与条件路由实现一致，消除字面冲突）。
  - **D2**：Edge Case "压缩失败 abort"精简为 FR-013 指针，删除重复规范性正文。
  - **D3/F6**：SC-007 大型测试覆盖清单改为引用 FR-034 所列范围（消除与 FR-034 的重复枚举，并修正与 tasks.md T024 实际用例不一致的问题）。
  - 复验：无 [NEEDS CLARIFICATION] 残留、无占位符、全部 22 项清单条目通过。
