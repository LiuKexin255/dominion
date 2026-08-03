# Tasks: Tool Bubble Rendering & Saolei Coordinate Accuracy

**Input**: Design documents from `/specs/024-tool-render-coord-fix/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/coordinate-space-contract.md, quickstart.md.

**Tests**: 单元测试 (`*.test.ts`) 是每个代码任务的一部分（宪章原则 IV — 编译 `bazel build` + 单测 `bazel test` 在每次代码变更时执行，**不单独分配 task**）。下方每个代码 task 的描述包含"更新相邻测试"的要求。大型测试（agent 服务验收）单独分配为 Phase 3 的验收 task（宪章原则 VI）。

**Organization**: 按 spec.md 的用户故事组织（US1/US2 均 P1，相互独立、可并行）。US1 修桌面对话 tool 气泡渲染 + 状态；US2 修 saolei 网格→客户坐标的 chrome 补偿。无 proto 变更、无新增文件、无新依赖，故**无独立 Setup/Foundation 阶段**（既有工程直接进入用户故事）。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、依赖已完成）
- **[Story]**: 用户故事归属（US1/US2）
- 所有 task 描述包含确切文件路径

## Path Conventions

多工程变更，根于 `projects/game/`：
- agent (TypeScript): `projects/game/agent/src/...`
- desktop frontend (Svelte/TS): `projects/game/desktop/frontend/src/...`
- 大型测试 (Go): `projects/game/testplan/...`

---

## 全局约定（宪章原则 I / IV / V）

- **引用溯源**：代码注释引用 specs/契约时写明相对路径（如 `specs/024-tool-render-coord-fix/contracts/coordinate-space-contract.md §Compensation scope`）。
- **编译+单测门禁**：每个代码 task 完成后 MUST 运行 `bazel build //projects/game/...` 与 `bazel test //projects/game/...`（覆盖整个 game 目录树：agent/desktop/testplan 等，**不得**只 build 子树），作为该 task 的一部分。
- **无需 gazelle / proto regen**：本 feature 仅编辑既有文件常量与 CSS/逻辑，无新增/删除文件、无 proto 变更。
- **本 feature 的 plan/spec/research/data-model/contracts/quickstart 为必读**（宪章原则 V 注：无需在下方各 phase 重复列出；AGENTS.md 同为必读）。
- **大型测试验收**：agent 是服务型应用，大型测试 MUST 实际 `guitar run`（宪章原则 VI v1.3.0），仅构建不构成验收。

---

## Phase 1: User Story 1 — Tool Call Renders as a Styled Bubble with Accurate Status (Priority: P1)

**Goal**: 对话中的 tool call 渲染为有样式的气泡（bordered box，含 name + args + 结果），且 neutral/缺失状态的 tool 结果显示为 neutral（绝不显示 "failed"）。修复 Defect 1a（`.tool-*` CSS 缺失）与 1b（absent status 被当 failed）。

**Independent Test** (quickstart Scenario 1/2/3): 跑一轮含 saolei tool 调用的 turn；确认每个 tool call 显示为带边框气泡（name + args），`saolei_init` 成功后显示 neutral（非 `✗ failed`）；native(mouse) tool 成功仍 `✓ succeeded`、失败仍 `✗ failed`；离开并重进 session 气泡一致。

**Depends on**: 无（与 US2 相互独立，可并行）。

### 文档清单（本 phase 编码前必读，宪章原则 V）

- **代码规范文档**：
  - `style/javascript.md`（TS 规范基准；注意：前端包 `projects/game/desktop/frontend` 仅有 `vite_build`、**无 `vitest_test`**，本 phase 不新增前端单测）。
  - Google TypeScript Style Guide — https://google.github.io/styleguide/tsguide.html
- **官方文档**：
  - ProtoJSON Format — *Presence and default-values*（enum-zero 字段在 protojson 中被省略 → 解释为何 `status` 缺失时按 neutral 处理）— https://protobuf.dev/programming-guides/json/
  - Svelte 5 官方文档 — https://svelte.dev/docs/svelte/overview（本 phase 复用既有 `$derived`/`$props`/`<style>` 模式，不引入新 rune；组件 `<style>` 参考 — https://svelte.dev/docs/svelte/components#style）。
- **技术文章**：无。

### 任务

- [ ] T001 [P] [US1] 在 `projects/game/desktop/frontend/src/api.ts` 新增纯函数 `classifyToolResultStatus(status)`：返回 `"succeeded" | "failed" | "neutral"`。分类规则（依据 `data-model.md` §1）：`undefined`/`null`/`""`/`0`/`"TOOL_RESULT_STATUS_UNSPECIFIED"` → `"neutral"`；`1`/`"TOOL_RESULT_STATUS_SUCCEEDED"` → `"succeeded"`；`2`/`"TOOL_RESULT_STATUS_FAILED"` → `"failed"`（同时接受 protojson 枚举名字符串与数值形式）。导出类型 `ToolResultStatusClass`。这是 status 判断的单一来源（research.md D3），供 ChatView 与未来测试共用。
- [ ] T002 [US1] 更新 `projects/game/desktop/frontend/src/components/ChatView.svelte`（依赖 T001）：
  - **状态逻辑（Defect 1b）**：移除内联 `isToolResultSucceeded` + 行 ~274 的 status-text 三元判断，改用 `classifyToolResultStatus(item.result?.status)`。`neutral` 渲染为 neutral 图标（如 `›`）+ 标签 `done` + neutral/muted 色（research.md D4），区别于 `succeeded`（`✓` 绿）/`failed`（`✗` 红）；未到达结果的 `tool-pending`（`running…`）保持不变。
  - **气泡样式（Defect 1a）**：在 `<style>` 补齐模板已使用但缺失定义的 CSS 规则——`.tool-bubble`、`.tool-head`、`.tool-name`、`.tool-args`、`.tool-result`、`.tool-pending`，以及 `.tool-resolved-success` / `.tool-resolved-failure` / `.tool-resolved-neutral`，复用既有 `.op-card` / `.op-result-card` / `.op-result-success` / `.op-result-failure` 的视觉语言（bordered box、monospace args、按状态着色）。依据 `data-model.md` §1/§2、`research.md` D4/D5。
   - 门禁：按全局约定执行 `bazel build //projects/game/...` 与 `bazel test //projects/game/...`（前端包仅有 `vite_build`，无单测 target；构建通过即前端门禁通过）。

**Checkpoint**: tool call 渲染为带样式气泡；saolei neutral 结果不再显示 failed；native tool 状态不回归；live≡history。

---

## Phase 2: User Story 2 — A Saolei Cell Click Lands on the Intended Cell (Priority: P1)

**Goal**: `saolei_click`/`saolei_flag`/`saolei_chord_click` 的网格 `(x,y)` 点击落到真实棋盘的 `(x,y)` 格。在 agent 的 `geometry.ts` 做截图→客户空间的 chrome 补偿（`CHROME_OFFSET_Y_PX=96`，实测；客户棋盘顶 `BOARD_ORIGIN_Y_PX=104`），`center(x,y)` 产出客户坐标，desktop 原样投递。

**Independent Test** (quickstart Scenario 4): 绑定扫雷窗口；`saolei_init` 后 `saolei_click(4,4)`；从返回截图确认点到的是 `(4,4)`（非第 7-8 行）。`center(4,4)` 单测断言为 `(168, 248)`。

**Depends on**: 无（与 US1 相互独立，可并行）。**不依赖** desktop 改动（desktop WINDOW_MESSAGE 路径已原样投递，research.md D8）。

### 文档清单（本 phase 编码前必读，宪章原则 V）

- **代码规范文档**：
  - `style/javascript.md`（§测试：`vitest_test` 宏、Mock/DI 约定、禁止跨包 `vi.mock`、验证 mock 生效）。
  - Google TypeScript Style Guide — https://google.github.io/styleguide/tsguide.html
  - `specs/018-saolei-mcp/research.md` D6（geometry `24/200/32` 的来源——screenshot-space 标定，解释为何需 chrome 补偿）。
  - `specs/018-saolei-mcp/contracts/proto-operation-contract.md`（desktop-facing `MouseMoveAndClickPart{WINDOW_MESSAGE}` 契约**不变**）。
- **官方文档**：
  - Win32 `WM_LBUTTONDOWN` — `lParam` 携带**客户坐标**（chrome-excluded）— https://learn.microsoft.com/en-us/windows/win32/inputdev/wm-lbuttondown
  - `DwmGetWindowAttribute`（`DWMWA_EXTENDED_FRAME_BOUNDS` 返回**整窗口** bounds，含非客户 chrome；对比 `GetClientRect` 客户区）— https://learn.microsoft.com/en-us/windows/win32/api/dwmapi/nf-dwmapi-dwmgetwindowattribute
- **技术文章**：无。

### 任务

- [ ] T003 [US2] 更新 `projects/game/agent/src/mcp/saolei/geometry.ts` 并同步其单测 `projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts`（宪章 IV——单测更新是本 task 一部分）：
  - **geometry.ts**：将 `BOARD_ORIGIN_Y_PX = 200` 改为截图→客户的显式 chrome 补偿——新增 `BOARD_ORIGIN_Y_PX_SCREENSHOT = 200`（截图空间棋盘顶，与截图/视觉一致，018 D6）、`CHROME_OFFSET_Y_PX = 96`（非客户 chrome 高度，实测）、`BOARD_ORIGIN_Y_PX = BOARD_ORIGIN_Y_PX_SCREENSHOT - CHROME_OFFSET_Y_PX`（= `104`，客户空间）。`BOARD_ORIGIN_X_PX = 24` 保留（X 无 chrome 补偿：左边框 sub-cell）。`CELL_SIZE_PX = 32` 不变。`center(x,y)` 签名不变，产出**客户坐标**（`centerY(y) = BOARD_ORIGIN_Y_PX + y*32 + 16`）。更新文件顶部 doc comment 说明：坐标为 `WM_*` 客户空间、chrome 补偿、**WINDOW_MESSAGE-only 不变量**（SIMULATED 消费截图空间坐标，不补偿）——依据 `contracts/coordinate-space-contract.md` §2/§3/Compensation scope、`data-model.md` §3、`research.md` D1/D2。
  - **saolei-mcp.test.ts**：把 dispatch 坐标断言更新为客户空间值（`center(4,4) → (168, 248)`；其余网格示例同步重算，如 `center(3,4) → (136, 248)`）。依据 `contracts/coordinate-space-contract.md` §6、`data-model.md` §3 worked example。
  - 门禁：`bazel test //projects/game/agent:lib_test` 通过（含 saolei-mcp.test.ts）。

**Checkpoint**: `center(x,y)` 产出客户坐标；三个 cell 工具的 dispatch 坐标为 `WINDOW_MESSAGE` 客户空间值；单测通过。

---

## Phase 3: 大型测试验收 + 手动 Windows 门禁（宪章原则 VI）

**Purpose**: agent 是服务型应用，大型测试 MUST 实际执行（`guitar run`）且全部通过；US2 的 click-landing（点击是否落到目标格）需 Windows 实机验证（CI 无扫雷窗口，类 023 T020 的手动集成门禁）。

### 文档清单（本 phase 编码前必读，宪章原则 V）

- **代码规范文档**：
  - `style/large_test.md`（测试组织按**模块**、复用既有 `system_test.yaml` 不得新建独立 YAML、`guitar run` 闭环、`pkg/testtool` 读环境变量、修改既有功能更新既有测试）。执行大型测试前 MUST 加载 `testplan` SKILL（`.opencode/skills/testplan/SKILL.md`）。
  - `style/golang.md`（`*_test.go` 表驱动 / given-when-then、命名 `TestFuncName`、禁止用例内塞断言）。
- **官方文档**：无（`guitar` / `testplan` SKILL 提供执行指引）。
- **技术文章**：无。

### 任务

- [ ] T004 [US2] 更新 `projects/game/testplan/agent_saolei_test.go`（既有模块测试，**不新建文件/不新建 YAML**——`style/large_test.md`）：把断言的 dispatched `MouseMoveAndClickPart` 坐标更新为新的客户空间值（`center(4,4) → (168, 248)`，对应 `CHROME_OFFSET_Y_PX = 96` / `BOARD_ORIGIN_Y_PX = 104`）。复用 `helpers_test.go` 既有构造/断言 helper，不复制。依据 `contracts/coordinate-space-contract.md` §6、`quickstart.md` Scenario 4a。
- [ ] T005 通过 `testplan` SKILL 执行大型测试验收：`guitar run projects/game/testplan/system_test.yaml`，完成部署→测试→清理闭环；**所有用例 MUST 全部通过**（failed/flaky 即验收未通过，修复后重跑至全绿）。仅 `bazel build` 测试 target 不构成验收（宪章原则 VI v1.3.0）。
- [ ] T006 [P] 手动 Windows 集成门禁（US2 click-landing）：在 Windows 主机绑定 Microsoft Minesweeper 窗口，`saolei_init`（F2 新局）后 `saolei_click(4,4)`，从返回截图确认点到的是 `(4,4)`（非第 7-8 行）；并对若干 `(x,y)` 与 `saolei_flag`/`saolei_chord_click` 复验均落到目标格；将结果与最终 `BOARD_ORIGIN_*`/`CHROME_OFFSET_Y_PX` 常量记入测试记录。若无可用的 Windows 大型测试环境，记为手动集成门禁（类 023 T020）。依据 `quickstart.md` Scenario 4b。

**Checkpoint**: agent 大型测试全绿；Windows 实机确认点击落到预期格。

---

## Dependencies & Execution Order

### Phase 依赖

- **Phase 1 (US1)**: 无依赖，可立即开始。内部 T001 → T002（T002 用 T001 的 helper）。
- **Phase 2 (US2)**: 无依赖，与 US1 **相互独立、可并行**。T003 单 task（geometry + 其单测）。
- **Phase 3 (验收)**: 依赖 US2（T003 完成，坐标常量确定）。T004 → T005；T006 可与 T005 并行准备（不同验证维度：自动化 vs 实机）。

### User Story 依赖

- **US1 (P1)**: 独立（前端），不依赖 US2。内部 T001 → T002。
- **US2 (P1)**: 独立（agent geometry），不依赖 US1。T003 → (Phase 3) T004 → T005；T006 并行。

### Parallel Opportunities

- US1 与 US2 **跨故事并行**（不同工程/文件：前端 vs agent）。
- Within US1: T001 (`api.ts`) 可与 US2 的 T003 (`geometry.ts`) 并行（不同文件）；T002 依赖 T001。
- Phase 3: T006（手动 Windows）可与 T005（`guitar run`）并行。

---

## Implementation Strategy

### MVP First

1. **US1（前端）** 或 **US2（agent）** 任一可先做——两者独立。建议先 US1（对话显示是每轮可见的破损；US2 使 saolei 可用）。
2. 完成 US1 → 跑 quickstart Scenario 1/2/3（build + 手动）独立验证。
3. 完成 US2 (T003) → 跑 `saolei-mcp.test.ts` 单测 + Phase 3 大型测试 (T004/T005) + 手动 Windows (T006)。

### Incremental Delivery

1. US1 → 对话 tool 气泡正确显示 + 状态准确 → 验证（前端 build+手动）。
2. US2 → saolei 点击落到正确格 → 单测 + 大型测试 + Windows 实机验证。
3. 每个 story 独立交付价值、不破坏另一个。

### Notes

- 本 feature 不改 proto、不加文件、不加依赖——所有变更是既有文件的常量/CSS/逻辑修正。
- chrome 补偿是 **WINDOW_MESSAGE-only**（`contracts/coordinate-space-contract.md` §Compensation scope）：不得挪到 desktop 共享 `runMouseMoveAndClick`、不得让通用 mouse 工具走 `center()`、不得把 saolei 工具改 `SIMULATED`。
- [P] = 不同文件、依赖已完成。[Story] 标签映射到 spec.md 用户故事。
- 每个代码 task 包含其编译 (`bazel build //projects/game/...`) + 单测 (`bazel test //projects/game/...`)（宪章原则 IV）。
- 大型测试验收 MUST 实际 `guitar run`（宪章原则 VI v1.3.0），仅构建不构成验收。
- 每个 phase 编码前 MUST 完整阅读该 phase "文档清单" 全部文档（宪章原则 V）。
- 前端包 `projects/game/desktop/frontend` 无单测基础设施（仅 `vite_build` 编译）；SC-001（气泡样式全覆盖）与 SC-004（live≡history 一致性）当前仅通过 `bazel build` + quickstart 手动验证，自动化回归依赖未来引入 `vitest_test` target。
