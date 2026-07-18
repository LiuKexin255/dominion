<!--
==============================================================================
Sync Impact Report
==============================================================================
Version change: 1.0.0 → 1.1.0
Rationale: Principle V (Read Before Code) materially expanded — added a
  concrete example to the "不做引用传递" (no citation transitivity) rule and
  added a new mandatory rule "规划即阅读" (planner must actually read listed
  docs before assigning them). Both strengthen planning discipline without
  removing or redefining any principle → MINOR 1.1.0.

Modified principles:
  - V. 编码前阅读文档 (Read Before Code) — 流程: expanded with example +
    new mandatory rule (planner-must-read-before-listing).

Added sections: none (rule-level expansion within an existing principle).

Removed sections: none.

Templates requiring updates:
  - .specify/templates/plan-template.md   — ✅ no change (Constitution Check
        reads this file dynamically; gates are derived per-feature).
  - .specify/templates/spec-template.md   — ✅ no change (scope/requirements
        unaffected).
  - .specify/templates/tasks-template.md  — ✅ no change (task structure
        unaffected; the document-reading rule constrains how tasks.md is
        authored, not its template layout).
  - .specify/workflows/speckit/workflow.yml — ✅ no change (integration-agnostic).

Follow-up TODOs:
  - Previous 1.0.0 follow-up (tasks-template test-framing adjustment) remains
    open/pending manual review; no new TODOs introduced by this amendment.
==============================================================================
-->

# Dominion Constitution

## Core Principles

本宪章按三类组织原则：**通用**（跨领域约束）、**设计**（架构与方案）、**流程**（开发与测试纪律）。所有原则为声明式、可校验的强制规则。

### I. 引用溯源 (Citation & Provenance) — 通用

代码或文档中引用任何内容（无论来自本仓库还是外部）MUST 包含可追溯的引用来源：

- **仓库内引用**：MUST 使用相对于仓库根目录的路径（如 `src/foo/bar.go:42`、`style/golang.md`）。
- **仓库外引用**：MUST 使用完整 URL 链接（官方文档、GitHub 仓库 README、技术文章等）。
- 不允许"裸引用"（仅描述而无来源指针）。

**Rationale**：可追溯性是协作与审计的基础；新成员或 agent 凭引用即可定位原始依据，避免知识断层与误读。

### II. 重构式变更 (Refactoring Over Patching) — 设计

进行代码变更时 MUST 评估现有架构设计与代码分层是否仍符合新的目标/需求：

- 当现有架构相对新需求**过度设计**时，MUST 简化、收缩 scope 以保持简洁；不能仅堆叠代码。
- 当现有架构**无法满足**需求时，MUST 扩展或重构，在设计层面满足需要，而非通过打"补丁"绕过。
- 架构调整 MUST 与功能变更同步进行，二者作为同一变更的产物交付。

**Rationale**：单纯堆叠代码导致架构腐化；打补丁掩盖问题并积累技术债。让设计随需求演化才能维持长期可维护性。

### III. 接口优先设计 (Interface-First Design) — 设计

技术方案设计 MUST 包含接口设计，包括但不限于 RPC、HTTP（REST/gRPC）、内部模块 API：

- 接口设计 MUST 在实现前明确：契约、输入/输出 schema、错误码与语义、版本化策略。
- 服务间或模块间通信 MUST 以接口契约为先——先定契约，再实现。

**Rationale**：接口是协作边界与变更隔离点；先定接口使并行开发、契约测试与向后兼容成为可能。

### IV. 测试颗粒度与执行频率 (Test Granularity & Cadence) — 流程

测试遵循"先小颗粒度、后大规模"的执行顺序；测试颗粒度越小，执行频率越高：

- 编译与单测属于小规模测试，MUST 在每次代码变更时执行，并作为代码开发任务的一部分——**不单独分配 task**。
- 大型测试（如有）作为功能/需求验收，在功能或需求完成后进行验证；大型测试 MAY 单独分配 task 作为验收环节。

**Rationale**：编译+单测构成快速反馈循环，回归捕获成本最低；大型测试验证端到端价值但成本高，应低频执行。

### V. 编码前阅读文档 (Read Before Code) — 流程

tasks.md MUST 为每个 phase 显式声明该 phase 需要阅读的文档：

- 仓库内文档用相对路径，仓库外文档用完整 URL；所列文档地址 MUST 直接包含实际内容，无需二次跳转。
- agent 编码前 MUST 完整阅读所有声明文档，然后再编码。
- 不做引用传递：所有文档（含间接引用的文件）MUST 在 tasks.md 规划时一次性明确列出，避免不确定性。
  - **示例**：若 `a.md` 引用了 [b](URL1) 与 [c](URL2)，而某 phase 需要阅读 b 的内容，则 MUST 显式列出 `a.md` 与 [b](URL1)，不能只列出 `a.md`。
- 规划即阅读：规划 tasks.md 确定需阅读的文档列表时，规划者 MUST 实际阅读列表中每个文档，确认其包含该 phase 所需的实际内容（部分文档可能只是引用索引，仅有链接而无具体内容）。禁止在未阅读的情况下，凭"理解"或"惰性思维"分配文档列表。
- 文档分类：**代码规范文档**、**官方文档**（第三方组件/依赖的官方文档或 GitHub 仓库 README）、**技术文章**；所列文档 MUST 与该 phase 开发任务相关或作为参考。
- AGENTS.md 与 spec 相关文件是代码开发必读内容，无需在 tasks.md 中重复列出。

**Rationale**：明确、完整的文档清单消除"该读什么"的不确定性，避免基于错误假设编码；规划阶段即验证文档实际内容，防止列出空索引或无关文档导致下游 agent 阅读无效。

### VI. 服务型应用大型测试验收 (Large Test Acceptance for Services) — 流程

服务类应用代码 MUST 同时具备单测与大型测试（large test），大型测试通过作为验证标准之一：

- 大型测试 MUST 覆盖关键服务行为（接口契约、端到端流程、跨服务通信等）。
- 大型测试通过 testplan skill（`tools/test/guitar`）执行，相关规范见 `style/large_test.md`。

**Rationale**：单测验证内部逻辑正确性；大型测试验证服务在真实集成环境下的行为，两者互补才能构成服务型应用的完整验收。

## 技术约束与规范 (Additional Constraints)

- 本仓库采用 **SDD 架构**，以 **speckit** 作为 SDD 框架；需求规划、方案设计、计划制定与代码开发 MUST 遵守本宪章。
- 编译与测试入口为 **bazel**（`bazel build //...` / `bazel test //...`）；大型测试通过 **testplan** skill 执行。具体操作命令见 `AGENTS.md`。
- 代码规范文档位于 `style/` 目录（`style/golang.md`、`style/javascript.md`、`style/api.md`、`style/large_test.md` 等）；编辑对应语言代码前 MUST 先阅读相关规范。
- 依赖管理：TS/JS 依赖版本统一在 `pnpm-workspace.yaml` 的 catalog 中管理；Go/Python/Bazel 依赖通过各自锁文件管理（详见 `AGENTS.md`）。
- 排查服务问题时 MUST 优先查看 tracing 与 log（提供 trace id 时尤其如此），通过 signoz skill 查询。

## 开发流程与质量门禁 (Development Workflow & Quality Gates)

按以下顺序执行质量门禁（各门禁对应的声明式原则见上文）：

1. **文档阅读门禁**（原则 V）：每个 phase 开始前，MUST 完整阅读 tasks.md 声明的全部文档。
2. **实现门禁**（原则 II / III）：变更以重构式进行；服务/模块变更 MUST 先有接口设计。
3. **编译 + 单测门禁**（原则 IV）：每次代码变更 MUST 通过 `bazel build` + `bazel test`（相关 target），作为开发任务的一部分，不单列 task。
4. **引用门禁**（原则 I）：产出的代码与文档 MUST 包含引用来源。
5. **大型测试验收门禁**（原则 VI）：服务型应用在功能/需求完成后，MUST 通过大型测试作为验收；该步骤 MAY 单独分配 task。

## Governance

- 本宪章是最高治理文档，supersedes 其他实践；当 `AGENTS.md` 或其他规范与本宪章冲突时，以本宪章为准（除非本宪章明确让位）。
- **修订程序**：修订 MUST 记录变更内容、说明版本号变更依据；涉及原则移除或语义变更时 MUST 提供迁移说明。
- **版本化**：`MAJOR.MINOR.PATCH` 语义化版本——MAJOR（移除/重定义原则或向后不兼容的治理变更）、MINOR（新增原则或实质性扩展）、PATCH（措辞澄清、typo、非语义细化）。
- **合规审查**：所有 PR / review MUST 校验本宪章合规性；任何复杂度 MUST 可被论证（对齐原则 II 的简化要求）。
- 运行时开发指引见 `AGENTS.md`；本宪章文件位置：`.specify/memory/constitution.md`。

**Version**: 1.1.0 | **Ratified**: 2026-07-16 | **Last Amended**: 2026-07-17
