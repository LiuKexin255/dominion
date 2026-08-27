# Feature Specification: vite + React Bazel 打包支持与验证 Demo

**Feature Branch**: `050-vite-react-bazel`

**Created**: 2026-08-27

**Status**: Draft

**Input**: User description: "插入一个新 spec，为 vite + React 增加 bazel 打包支持，并在 @experimental/js/ 目录增加一个 demo 用于验证。为 agent 迁移做基建准备。"

## Motivation

game agent 迁移至 dsh 框架的 step 1（`specs/049-agent-v2-dsh-init`）已确定 web 服务前端采用 React 并组件级复用 dsh 前端组件库（`@deepseek-ai/dsh-client-ui-primitives`，peer 依赖 react ^18.2.0）。但仓库当前**没有任何 React 项目先例**：依赖目录（`pnpm-workspace.yaml` catalog）中不存在 react 及其工具链依赖，前端 bazel 打包的唯一消费方是 desktop 的 Svelte 前端（`projects/game/desktop/frontend/`，经 `tools/dev/js/vite.bzl` 的 `vite_build` 规则构建）。

若 049 直接开工，将同时引入"新框架接入 + 新服务交付"两类风险。本 feature 把**前端构建基建**前置独立交付：让仓库标准构建链路（pnpm workspace + bazel）能够构建 vite + React 项目，并用 `experimental/js/` 下的一个最小 demo 实证整条链路可用——049 的 web 前端届时只需按同一模式建项目，无基建改动。

**现状与目标差距**：
1. 依赖面：React 相关依赖（react/react-dom 及 vite 的 React 插件、类型声明）需纳入 catalog 统一管理（`AGENTS.md` 依赖规则：TS/JS 依赖版本统一在 `pnpm-workspace.yaml` catalog）。
2. 构建面：现有 `vite_build` 规则（`tools/dev/js/vite.bzl`）按 Svelte 消费方塑形（含 `svelte_config` 专属属性）；需确认/调整其对 React 项目的适用性，且调整不得破坏存量 Svelte 构建。
3. 实证面：`experimental/js/` 目录不存在，需新建并放入 demo 项目（workspace 成员 + bazel target + 自动化验证）。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 用仓库标准构建命令构建 React 前端项目 (Priority: P1)

开发者（或 CI）在仓库内新建一个 vite + React 前端项目（workspace 成员、依赖来自 catalog），以仓库标准构建命令（bazel）构建它，得到一个完整的静态产物目录（tree artifact）：含入口 HTML 与带内容哈希命名的资源文件，入口正确引用资源——与现有 Svelte 前端构建产物同构、同样可被下游规则消费。

**Why this priority**: 这是本 feature 的核心价值与 049 的前置条件：没有"bazel 能构建 React 项目"这条链路，049 的 web 前端无从交付。

**Independent Test**: 对 demo 项目执行标准构建命令，断言产物目录存在、含入口 HTML 与至少一个 hash 命名资源、入口引用的资源均可解析到产物内。

**Acceptance Scenarios**:

1. **Given** 仓库完成依赖与规则准备，**When** 以 bazel 构建 demo 的前端 target，**Then** 构建成功并产出 dist tree artifact。
2. **Given** 构建产物，**When** 检查其结构，**Then** 含入口 HTML，HTML 引用的 JS/CSS 资源以内容哈希命名且全部存在于产物目录中。
3. **Given** 下游规则（如静态资源打包/嵌入），**When** 以与现有前端产物相同的方式消费该 tree artifact，**Then** 消费成功（产物形态与现有 `vite_build` 输出兼容）。

---

### User Story 2 - demo 实证 React 真实参与构建且可自动化验证 (Priority: P1)

`experimental/js/` 下的 demo 是一个真实的最小 React 应用（入口 HTML + 至少一个 React 组件），其构建产物**包含 demo 组件的渲染逻辑**（React 真实编译进 bundle，而非空壳页面）；产物正确性与组件逻辑均有自动化测试随仓库标准测试命令执行。

**Why this priority**: "能构建"不等于"构建对了"——demo 的存在意义是端到端实证（React 组件、JSX 转换、依赖解析全部真实生效），且这种实证可重复执行（进 CI），为 049 提供可信的基建信号。

**Independent Test**: 构建产物后运行产物断言测试（bundle 内含 demo 组件的渲染产物特征）与组件单测（复用仓库现有 JS 测试设施），全部通过。

**Acceptance Scenarios**:

1. **Given** demo 构建产物，**When** 产物断言测试检查 bundle 内容，**Then** 断言 demo React 组件的渲染逻辑在产物 JS 中（而非空 HTML）。
2. **Given** demo 的组件单测，**When** 以仓库标准测试命令执行，**Then** 全部通过。
3. **Given** 干净环境（无历史构建缓存），**When** 重复执行构建 + 断言 + 单测，**Then** 结果一致（可重复、确定性）。

---

### User Story 3 - 存量前端构建零回归 (Priority: P2)

为支持 React 而对构建规则/依赖面做的任何调整，不改变现有 Svelte 前端（desktop）的构建与测试行为；仓库既有构建与测试保持可用。

**Why this priority**: 基建变更的底线是不破坏存量；desktop 前端是 `vite_build` 的唯一存量消费方，是其回归对照。

**Independent Test**: 对 desktop 前端执行既有构建与测试 target，结果与变更前一致（成功且产物正常）。

**Acceptance Scenarios**:

1. **Given** 本 feature 的规则/依赖变更已合入，**When** 构建 desktop 前端的 dist target，**Then** 构建成功、产物结构不变。
2. **When** 执行仓库既有测试命令，**Then** 无因本变更引入的失败。

---

### Edge Cases

- **依赖版本冲突**：React 工具链（如 vite 的 React 插件）与 catalog 现有 vite 版本不兼容时，构建配置阶段即暴露并解决（升级或固定兼容组合），不得以项目级版本覆盖绕过 catalog。
- **gazelle 误生成**：新目录的 BUILD 生成/更新遵循仓库 gazelle 流程；若自动生成结果与 vite 项目形态冲突（如误将 src 当 Go/TS 库处理），以手工调整 target 的既有惯例处理（`AGENTS.md`：BUILD.bazel 通常由 gazelle 生成，特殊 target 生成后追加）。
- **本地执行环境差异**：前端构建规则按现有模式本地执行（依赖源码树 node_modules）；demo 构建在缺少 node_modules 的环境失败时给出可理解的失败原因（与现有前端构建一致的行为，不额外引入新的环境假设）。
- **产物为空/不完整**：断言测试必须能捕获"构建成功但产物缺资源/入口引用悬空"的情况（而非只检查目录存在）。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 仓库构建系统 MUST 支持构建 vite + React 前端项目：以 bazel target 形式驱动 vite 构建 React 项目并产出静态产物 tree artifact；产物入口 HTML 引用的资源完整存在、以内容哈希命名——与现有 `vite_build` 产物形态兼容（可被下游以相同方式消费）。
- **FR-002**: React 相关依赖（react、react-dom、React 类型声明、vite 的 React 插件）MUST 纳入 `pnpm-workspace.yaml` catalog 统一管理；项目 manifest MUST NOT 声明直接版本（`AGENTS.md` TS/JS 依赖规则）；React 主版本 MUST 兼容 049 将复用的 dsh 前端组件库 peer 要求（react ^18.2.0 线）。
- **FR-003**: 系统 MUST 在 `experimental/js/` 目录下提供 demo React 前端项目：最小 React 应用（入口 HTML + 至少一个交互组件），作为 pnpm workspace 成员与 bazel 构建图成员纳入仓库（新增 workspace packages 条目、BUILD target）。
- **FR-004**: demo MUST 附带自动化验证并随仓库标准测试命令执行：(a) 构建产物断言测试——入口 HTML、hash 资源存在、引用完整、React 组件渲染逻辑真实在 bundle 中；(b) 组件级单测——复用仓库现有 JS 测试设施（`tools/dev/js/vitest_test.bzl`）。
- **FR-005**: 对构建规则的调整 MUST 保持框架中立：现有 Svelte 前端（`projects/game/desktop/frontend/`）的构建与测试行为不变；React 支持不得以破坏或特化排除其他框架消费方的方式实现。
- **FR-006**: 范围边界：本 feature MUST NOT 交付服务、dev server/HMR、SSR、路由/状态管理/UI 组件库（含 dsh 组件库接入——属 049 范围）；demo 保持最小验证形态。

### Key Entities

- **React Frontend Package（React 前端包）**: pnpm workspace 成员包，含入口 HTML、React 组件源码、vite 配置；依赖全部解析到 catalog。
- **Frontend Build Target（前端构建 target）**: bazel target，输入前端包源文件，输出静态产物 tree artifact（dist）。
- **Dist Tree Artifact（静态产物目录）**: vite 构建输出的完整目录产物：入口 HTML + 内容哈希命名的 JS/CSS 资源；与现有 `vite_build` 输出同构。
- **Catalog 依赖项**: `pnpm-workspace.yaml` catalog 中统一管理版本的 React 工具链依赖。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: demo 的标准构建（bazel build）与全部验证测试（产物断言 + 组件单测）100% 通过，且可重复执行（含无缓存环境）。
- **SC-002**: 产物质量可验证：入口 HTML 引用的所有资源均存在于产物中；React 组件渲染逻辑在产物 JS 中可被断言检出。
- **SC-003**: 存量零回归：desktop 前端构建与测试在本变更前后行为一致；仓库既有构建/测试无新增失败。
- **SC-004**: 依赖治理合规：React 相关依赖全部来自 catalog；demo（及任何后续 React 项目）的 manifest 中零直接版本声明。
- **SC-005**: 基建可复用：React 构建支持以通用规则/宏 + catalog 依赖形态交付，不与 demo 目录耦合——049 的 web 前端可按同一模式新建项目而无需改动构建规则。

## Assumptions

- **本 feature 非服务型交付**：交付物为构建规则 + 依赖 + demo（无对外服务），宪法 VI 的大型测试不适用；验收以 bazel build + bazel test（产物断言与单测）完成，豁免说明随 demo README 交付（`.specify/memory/constitution.md` 原则 VI 豁免条款）。
- **vite 沿用 catalog 现有线**（^6.x）：React 插件选择与 vite 6 兼容的版本组合；如存在不兼容，以升级 catalog 内版本解决而非绕开。
- **`experimental/js/` 为新建目录**：与既有 `experimental/ts/`（服务侧 TS 实验）并存，定位前端构建实验区（用户指定路径）；demo 项目命名与目录内布局由 plan 阶段确定。
- **React 版本锚定 18.x**：对齐 049 前端将复用的 `@deepseek-ai/dsh-client-ui-primitives`（0.1.1-rc.2 线）peer 依赖 react ^18.2.0，避免 049 落地时被迫二次升级。
- **demo 复用现有构建/测试设施**：构建走现有 vite 构建规则形态（必要时中性化扩展），测试走 `vitest_test` 宏；不引入第二套前端构建机制。
- **工具链版本演进**：catalog 升级（vite/react 大版本）不在本 feature 范围；本 feature 落地时以"当前 catalog 线 + 兼容 React 组合"为基准。
