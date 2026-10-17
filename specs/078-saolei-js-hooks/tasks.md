# Tasks: 078 saolei JS hook（planner 固化策略脚本 + 棋盘变动后执行播报）

**Input**: Design documents from `/specs/078-saolei-js-hooks/`

**Prerequisites**: `specs/078-saolei-js-hooks/plan.md`（技术上下文/结构/门禁）、`specs/078-saolei-js-hooks/spec.md`（US1 P1/US2 P1/US3 P2/US4 P2/US5 P2 与 FR-001–FR-019/SC-001–SC-008，含 Clarifications 2026-10-17）、`specs/078-saolei-js-hooks/research.md`（D1–D9）、`specs/078-saolei-js-hooks/data-model.md`、`specs/078-saolei-js-hooks/contracts/`（board-change-event / saolei-hooks-plugin / hook-announcement）、`specs/078-saolei-js-hooks/quickstart.md`（均已存在）

**通用约定**（全部 phase 适用）：

- 编译 + 单测随**每次代码变更**执行（宪法 IV，不单列 task）：`bazel build` + `bazel test` 相关 target（各 phase 验证门禁列出）。
- 新包 `common/js/dsh-plugins/saolei-hooks/` 由 Phase 1 创建；后续各 phase 的 BUILD 变更经 `bazel run //:gazelle common/js/dsh-plugins/saolei-hooks`（或对应目录）刷新。
- 代码书写遵循各 phase 文档清单「代码规范文档」桶所列规范；本 feature 全部 TS 包适用 `style/javascript.md` 及其引用基准。
- 注释只表述终态与"为什么"（宪法 VII），引用带仓库相对路径或完整 URL。
- 测试一律 DI / `vi.fn()` seam（`style/javascript.md` §测试）；禁止新增 `vi.mock` 模块拦截。

---

## Phase 1: Setup（新包脚手架与共享常量）

**Goal**: `@dominion/dsh-saolei-hooks` 包骨架可构建可测试；四项资源上限常量落位（全部 US 的共享基座）。

**Independent Test**: `bazel build //common/js/dsh-plugins/saolei-hooks/...` 通过 + 包内冒烟单测 `bazel test //common/js/dsh-plugins/saolei-hooks/...` 通过（vitest 宏接线正确）。

### 文档清单（编码前必读；宪法 V 三分类）

- **代码规范文档**：`style/javascript.md`（模块系统/编译配置锁步/源码书写规则/`vitest_test` 宏 data 规则）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（`style/javascript.md` 引用的包级契约——新包 package.json/tsconfig/.swcrc/测试的机器可审计定义，间接引用显式列出）；`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（`style/javascript.md` §测试 引用的 shim 退出码契约——T001 的 `vitest_test` 接线相关，间接引用显式列出）；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的规范基准）
- **官方文档**：无（tsconfig/.swcrc 形态经 saolei-tools 原型与 esm 契约覆盖，无第三方 API 语义）
- **技术文章/技术参考文档**：`common/js/dsh-plugins/saolei-tools/package.json`、`common/js/dsh-plugins/saolei-tools/tsconfig.json`、`common/js/dsh-plugins/saolei-tools/.swcrc`、`common/js/dsh-plugins/saolei-tools/BUILD.bazel`（新包四件原型）；`specs/078-saolei-js-hooks/research.md` D4（上限值依据）；`specs/078-saolei-js-hooks/quickstart.md` §2（验证面）

### Implementation

- [X] T001 新包脚手架：创建 `common/js/dsh-plugins/saolei-hooks/package.json`（镜像 saolei-tools 形态：`"name": "@dominion/dsh-saolei-hooks"`、`"type": "module"`、exports `"."` 双条件指向 `./src/index.js`、`dependencies: {"@dominion/dsh-saolei": "workspace:*"}`、`peerDependencies` cordis/dsh-tools/dsh-system-prompt（`sections` 的类型来源，`data-model.md` §3.1）用 `catalog:`、devDependencies @types/node/typescript/vitest 用 `catalog:`）；复制 saolei-tools 的 `tsconfig.json`（paths 指向 `@dominion/dsh-saolei/src/index.js`）与 `.swcrc`（esm 契约 §2 锁步）；`src/index.ts` 最小占位导出（plugin entry 在 Phase 6 完成，本 phase 仅保证类型与构建通过）；`src/index.test.ts` 冒烟用例；`README.md` 骨架（插件定位/依赖边界——Phase 7 补全）。构建接线：根目录执行 pnpm 安装注册 workspace 包（`bazel run @pnpm -- --dir /mnt/code/dominion install`），在包目录执行 `bazel run //:gazelle common/js/dsh-plugins/saolei-hooks` 生成 BUILD 后补 `vitest_test` 宏（name `lib_test`，data = `glob(["src/**/*.ts"])` + `:node_modules/*` deps 镜像，`style/javascript.md` §测试 data 规则）；`bazel build` + `bazel test //common/js/dsh-plugins/saolei-hooks/...` 全绿
- [X] T002 [P] 资源上限常量：创建 `common/js/dsh-plugins/saolei-hooks/src/limits.ts`——`MAX_HOOKS = 8`、`MAX_SCRIPT_CHARS = 16384`、`MAX_OUTPUT_CHARS = 4096`、`EXECUTION_TIMEOUT_MS = 500`（`specs/078-saolei-js-hooks/data-model.md` §2.3 逐值引用，注释注明依据 `specs/078-saolei-js-hooks/research.md` D4 与调整口径：改常量不需契约变更）

**Checkpoint / 验证门禁**：`bazel build //common/js/dsh-plugins/saolei-hooks/... && bazel test //common/js/dsh-plugins/saolei-hooks/...` 全绿；esm 契约 §6 审计对新包零命中（`"type": "module"` 存在、无 require 直用）。

---

## Phase 2: User Story 4 - 游戏基础棋盘变动事件（领域事实推送 seam） (Priority: P2)

**Goal**: `GameRuntimeDeps.onBoardChange` 可选回调 + operate 内触发条件（`executed > 0` 且 `status === "playing"` 合取）——游戏开始与结束、无落账、全 skip 路径零回调；seam 缺省行为与现状一致。

**Independent Test**: `bazel test //common/js/dsh-plugins/saolei/...` 全绿（`specs/078-saolei-js-hooks/contracts/board-change-event.md` §5 七项断言）；既有用例全量回归不变。

### 文档清单（编码前必读；宪法 V 三分类）

- **代码规范文档**：`style/javascript.md`（ESM/测试书写规则）；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的规范基准）
- **官方文档**：无（纯实现类内部增量，无第三方 API 语义）
- **技术文章/技术参考文档**：`specs/078-saolei-js-hooks/contracts/board-change-event.md`（全部——语义/机械契约/异常边界/验收面）；`specs/078-saolei-js-hooks/data-model.md` §1（触发条件机械定义与回调异常边界）；`specs/078-saolei-js-hooks/research.md` D5（deps 回调选型）；`specs/078-saolei-js-hooks/quickstart.md` §2（验证面）；`specs/075-saolei-game-foundation/contracts/saolei-foundation.md` §2（被保护的基础服务面基线——FR-002 要求该契约面零变化）；`specs/075-saolei-game-foundation/contracts/team-loop-assembly.md` §2（构造接线基线——`createGameRuntime` 签名扩展对象）；`survey/saolei-hook-scripts.md` §2.1（K1 裁定：事件锚定游戏领域，不与工具耦合）

### Implementation

- [X] T003 [US4] 事件 seam：`common/js/dsh-plugins/saolei/src/game/runtime.ts`——`GameRuntimeDeps` 增可选字段 `readonly onBoardChange?: (entry: GameLogEntry) => void`（JSDoc 注明触发条件与"回调异常由订阅方边界处理、不影响 operate 返回值"——`specs/078-saolei-js-hooks/contracts/board-change-event.md` §3）；`operate()` 内 `saolei_operate` 条目 push 后、`endedStatus` 分支前求值触发条件（`deps.onBoardChange 存在 ∧ executed > 0 ∧ gameStatus(finalState) === "playing"`）满足则同步回调并传入该条目；`common/js/dsh-plugins/saolei/src/index.ts` 的 `createGameRuntime` 构造签名扩展为可选第三参（承载扩展 deps，既有两参调用语义不变；`GameLogEntry` 已在导出面）
- [X] T004 [US4] seam 单测：`common/js/dsh-plugins/saolei/src/game/runtime.test.ts` 增量——契约 §5 七项：①触发合取（有效批量 → 恰一次、载荷与 gameLog 尾条逐字段同值）；②全 skip 批量零回调；③终局批量（won 与 lost 各一）零回调、`(game-end)` 条目不触发；④`saolei_init` 零回调；⑤结构拒绝/识别失败/派发失败/空批量提前返回零回调；⑥seam 缺省（不传 `onBoardChange`）既有用例全量回归不变；⑦回调抛错 → `operate` 返回值与无订阅方时一致（异常不上抛）

**Checkpoint / 验证门禁**：`bazel test //common/js/dsh-plugins/saolei/...` 全绿（含既有用例回归）。

---

## Phase 3: User Story 3 - 沙箱化执行与失败隔离 (Priority: P2)

**Goal**: `node:vm` 沙箱执行器——注册期编译校验（banned-pattern + 包装编译）与执行期隔离（fresh context、`codeGeneration` 禁用、timeout 中断、输出截断、结果分类）。

**Independent Test**: `bazel test //common/js/dsh-plugins/saolei-hooks/...` 全绿（`specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md` §6 第 3 组沙箱断言）。

### 文档清单（编码前必读；宪法 V 三分类）

- **代码规范文档**：`style/javascript.md`（ESM/测试书写规则——DI/`vi.fn()` seam）；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的规范基准）
- **官方文档**：[Node.js vm API](https://nodejs.org/api/vm.html)（`vm.Script` 构造即编译不执行、`script.runInContext(contextifiedObject, { timeout })` 超时中断语义（"If execution is terminated, an Error will be thrown"）、`vm.createContext` 的 `contextCodeGeneration.strings/wasm` 选项、"The node:vm module is not a security mechanism" 边界声明）
- **技术文章/技术参考文档**：`specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md` §4（沙箱执行契约）+ §6（验收面第 3 组）；`specs/078-saolei-js-hooks/data-model.md` §2.2（HookRunResult 分类与执行语义）/§2.3（上限常量）/§2.4（banned-pattern 清单）；`specs/078-saolei-js-hooks/research.md` D1（node:vm 选型与已知限制披露）/D4/D7（防御纵深）；[ado-code-mode-mcp ADR-002](https://github.com/thijs-hakkenberg/ado-code-mode-mcp/blob/main/docs/adr/002-node-vm-sandbox.md)（同威胁模型防御纵深先例——banned-pattern/冻结/codeGeneration/timeout/最小 globals）；`specs/078-saolei-js-hooks/quickstart.md` §2（验证面）

### Implementation

- [X] T005 [US3] 沙箱执行器：创建 `common/js/dsh-plugins/saolei-hooks/src/sandbox.ts`——`compileHookScript(source: string): { ok: true; script: vm.Script } | { ok: false; rejection: string }`（长度 ≤ `MAX_SCRIPT_CHARS`；banned-pattern 子串校验：`require(`、`import `、`process.`、`globalThis.`、`global.`、`fetch(`、`child_process`、`eval(`、`Function(`，拒绝文本说明命中模式；包装为**调用形态** `"(function(board) {\n" + source + "\n})(board)"` 后 `new vm.Script`——同一 `vm.Script` 跨 context 复用；语法错误捕获并返回含错误信息/行号的拒绝文本）；`runHookScript(script: vm.Script, board: GameState): HookRunResult`（每次执行 `vm.createContext({ board: <board 的 JSON 快照> }, { codeGeneration: { strings: false, wasm: false } })`——上下文的唯一注入绑定是数据参数 `board`，其余为 fresh context 自带的 ECMAScript 内建、无任何宿主 API；`script.runInContext(context, { timeout: EXECUTION_TIMEOUT_MS })`——调用表达式内联于被编译源码，故 timeout 中断窗口**覆盖函数体**（不得改为"宿主在 vm 外调用返回的函数"）；返回值分类：字符串 → 空串 `{kind:"empty"}`、长度 ≤ `MAX_OUTPUT_CHARS` `{kind:"output"}`、超限 `{kind:"truncated"}` 截断；非字符串 → `JSON.stringify` 成功转 `{kind:"output"|"truncated"}`、`undefined`（无 `return`）与 `JSON.stringify` 抛错（循环引用）转 `{kind:"error", message: "output is not serializable"}`（**不视为 empty**）、其他抛错转 `{kind:"error", message: <原错误信息>}`；执行抛错（含 timeout Error）→ `{kind:"error", message}` 或 `{kind:"timeout"}`；`HookRunResult` 联合类型按 `specs/078-saolei-js-hooks/data-model.md` §2.2 定义并 `export type`）
- [X] T006 [US3] 沙箱单测：创建 `common/js/dsh-plugins/saolei-hooks/src/sandbox.test.ts`——契约 §6 第 3 组逐项：宿主 API 白名单外访问不可得（脚本 `typeof process === "undefined"`、`typeof require === "undefined"` 断言为字面量 true/undefined——通过 output 返回值验证）；`while(true){}` 死循环在 `EXECUTION_TIMEOUT_MS` 内被中断返回 `{kind:"timeout"}`（**证明 timeout 覆盖函数体**；用例内放宽断言容差防 flaky）；超长输出截断（`"a".repeat(MAX_OUTPUT_CHARS + 1)` → truncated 且截断文本长度 = `MAX_OUTPUT_CHARS`）；跨执行无状态（脚本向全局挂属性 → 第二次执行读不到）；载荷快照隔离（脚本内改写 `board.width`/`board.grid` → 传入的源对象不被改动）；对象 return 确定性 `JSON.stringify`；循环引用对象 return → `{kind:"error", message:"output is not serializable"}`；无 `return`（`undefined`）→ `{kind:"error", message:"output is not serializable"}`（**不得**归为 `empty`）；空串 return → `{kind:"empty"}`；banned-pattern 逐一命中拒绝 + 合法脚本通过（`Math`/`JSON`/`Array` 等内建可用断言）

**Checkpoint / 验证门禁**：`bazel test //common/js/dsh-plugins/saolei-hooks/...` 全绿。

---

## Phase 4: User Story 2 - planner 经 saolei_hook 工具固化与管理 JS 策略脚本 (Priority: P1)

**Goal**: 进程内存态注册表（set 编译校验链/同名覆盖保序/remove/list）+ planner 专属单 action 工具 `saolei_hook`（八层校验文本拒绝、describe 八要素、零 error outcome）。

**Independent Test**: `bazel test //common/js/dsh-plugins/saolei-hooks/...` 全绿（`specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md` §6 第 1/2/4 组断言）。

### 文档清单（编码前必读；宪法 V 三分类）

- **代码规范文档**：`style/javascript.md`（ESM/测试书写规则）；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的规范基准）
- **官方文档**：无（`@deepseek-ai/dsh-tools` 为仓库内闭包携带的私有包、无公开文档；`defineTool`/schema 子集语义以仓库先例与包源码锚点为唯一权威，源码锚点见本 phase「技术文章/技术参考文档」桶）
- **技术文章/技术参考文档**：`specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md` §2（注册表契约）/§3（工具契约——八层校验/describe 八要素/输出契约）+ §6（验收面第 1/2/4 组）；`specs/078-saolei-js-hooks/data-model.md` §2.1（HookRegistry 接口）/§2.3（上限）/§3.2（工具参数 schema）；`specs/078-saolei-js-hooks/research.md` D2（单 action 维持与被否决备选）/D3（同名覆盖保序）/D9（describe 文案纪律）；`survey/saolei-hook-scripts.md` §2.3.3（脚本形态）/§1.5（schema 子集硬边界）；`common/js/memory/src/tool.ts`（单 action 工具先例——根字段 optional/execute 内 XOR 文本拒绝/describe 场景无关文案纪律）；`common/js/dsh-plugins/saolei-tools/src/index.ts`（defineTool 用法与 refusal 惯例先例）；`projects/game/agent_v2/node_modules/@deepseek-ai/dsh-tools/lib/index.js`（安装后闭包内的 `CONSTRAINT_KEYWORDS`——schema 允许关键字只准 `type/oneOf/properties/required/additionalProperties/items/enum/const` + 注解）

### Implementation

- [X] T007 [US2] 注册表：创建 `common/js/dsh-plugins/saolei-hooks/src/registry.ts`——`HookRegistry` 类（`specs/078-saolei-js-hooks/data-model.md` §2.1）：`set(name, source)`（空名/超 `MAX_HOOKS`（同名覆盖不占新位）/超 `MAX_SCRIPT_CHARS` → `{ok:false, rejection}`；经 `compileHookScript` 全链校验；同名 set 覆盖脚本并**保留原注册序**）；`remove(name)`（不存在 → 拒绝文本含当前注册名列表）；`list()`（按注册序返回 `{name, order, length}`）；`entriesInOrder()`（只读 `HookEntry[]`——`{name, script: vm.Script, source, order}`）
- [X] T008 [US2] 工具面：创建 `common/js/dsh-plugins/saolei-hooks/src/tool.ts`——`defineTool` 单工具 `saolei_hook`：参数 schema 根字段全部 optional（`action: enum ["set","remove","list"]`、`name: string`、`script: string`，`additionalProperties: false`，仅用 `CONSTRAINT_KEYWORDS` 允许的关键字）；execute 内八层校验全以普通文本结果拒绝（`specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md` §3.1 层次：action 缺失/未知 → 非适用动作的多余字段 → set 缺 name/script → 数量/长度超限 → banned-pattern → 编译错误 → remove 不存在 → list 恒成功）；工具持有 `HookRegistry`（构造注入）；describe 按 §3.2 八要素书写（三动作用法；`board` 字段表含 `grid[y][x]` 行主序与符号枚举 `INITIAL|0..8|FLAG|HIT_MINE|MINE|UNKNOWN`、**`mineCounter` 形态**（`{decoded:true,value}` 或 `{decoded:false}`，未解码时缺席；`value` = 雷数 − 旗数，可为负）、函数体/return 契约、**空输出不播报与低噪音引导**、**不持久化与跨局自行保存（不提 memory、不指向任何特定存储工具）**、同步纯函数、无宿主 API、超时 500ms/输出 4096/脚本 16384/至多 8 个、触发语义含**游戏开始与结束不触发**、同名 set 更新保序）；导出 `SAOLEI_HOOK_TOOL_NAME = "saolei_hook"`
- [X] T009 [US2] 注册表与工具单测：创建 `common/js/dsh-plugins/saolei-hooks/src/registry.test.ts` 与 `src/tool.test.ts`——契约 §6 第 1/2/4 组：八层校验逐层断言（含非适用动作多余字段拒绝、拒绝文本含上限值/命中模式/编译错误信息）；同名 set 覆盖后 `entriesInOrder()` 顺序不变、脚本已更新；remove 后 list 不含；describe 八要素关键子串断言（board 字段表含符号枚举与 `mineCounter` 形态、空输出、不持久化/自行保存、无宿主 API、超时/截断、注册序、游戏开始与结束不触发）；工具结果恒 `{result: string}` 普通 text（无 isError 路径）

**Checkpoint / 验证门禁**：`bazel test //common/js/dsh-plugins/saolei-hooks/...` 全绿。

---

## Phase 5: User Story 1 - 棋盘批量操作变动后自动执行 hook 并统一播报（核心闭环） (Priority: P1) 🎯

**Goal**: 事件处理器 `onBoardChange`——注册序执行全部 hook、非空输出逐条 `[hook <name>] <text>` 播报（空输出零播报）、失败隔离不上抛；播报回流形态即契约格式表（快路/pending 由 077 交付承载，本 phase 只生产文本）。

**Independent Test**: `bazel test //common/js/dsh-plugins/saolei-hooks/...` 全绿（announce seam `vi.fn()` 断言——`specs/078-saolei-js-hooks/contracts/hook-announcement.md` §5 第 1 组）。

### 文档清单（编码前必读；宪法 V 三分类）

- **代码规范文档**：`style/javascript.md`（ESM/测试书写规则——DI/`vi.fn()` seam 与 mock 正向断言）；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的规范基准）
- **官方文档**：无（announce 为注入的 DI 面，无第三方 API 语义）
- **技术文章/技术参考文档**：`specs/078-saolei-js-hooks/contracts/hook-announcement.md`（全部——格式表/可见性语义/边缘继承/验收面）；`specs/078-saolei-js-hooks/data-model.md` §3.3（处理器伪码）/§4（播报回流四载体）；`specs/078-saolei-js-hooks/research.md` D6（前缀格式裁定）；`survey/saolei-hook-scripts.md` §2.2.4（统一播报裁定——announcer 单次生产）；`common/js/dsh-plugins/saolei-loop/src/announcer.ts`（`announce(text)` 既有生产协议——非空校验 fail-loud，T010 的 announce seam 上游）与 `specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md` §3（announcer 成员与产出通道——hook 播报为第二生产来源）；`specs/077-announcer-member-unification/contracts/inflight-broadcast-fastpath.md` §1（hook 播报=插入式产出的快路接入位——消费方语义背景，本 phase 零改动）；`specs/078-saolei-js-hooks/quickstart.md` §2（验证面）

### Implementation

- [X] T010 [US1] 事件处理器：创建 `common/js/dsh-plugins/saolei-hooks/src/handler.ts`——`createOnBoardChange(deps: { registry: HookRegistry; announce: (text: string) => void; logger?: { warn(msg: string): void } }): (entry: GameLogEntry) => void`（DI 构造，`style/javascript.md` §测试 mock 约定）：`entry` 到达时按 `registry.entriesInOrder()` 顺序执行 `runHookScript(hook.script, entry.state)`（`board` 由 `runHookScript` 转 JSON 快照传入），按 `HookRunResult` 分派播报文本（格式以 `specs/078-saolei-js-hooks/contracts/hook-announcement.md` §2 为唯一来源，逐行：output → `[hook <name>] <text>`；truncated → `[hook <name>] <text> ...[truncated]`；error → `[hook <name>] error: <message>`；timeout → `[hook <name>] timeout after <EXECUTION_TIMEOUT_MS>ms`；empty → 零播报）；整个循环 try/catch 包裹（`runHookScript` 自身已分类，防御 announce 抛错的边界）——异常 `logger.warn` 吞噬不上抛（`specs/078-saolei-js-hooks/contracts/board-change-event.md` §3 订阅方边界责任）；registry 为空 → 零执行零播报
- [X] T011 [US1] 处理器单测：创建 `common/js/dsh-plugins/saolei-hooks/src/handler.test.ts`——announce 用 `vi.fn()` 注入并**正向断言调用序列**：注册序逐条（两个 hook 按序各一条）；格式表四种文本逐行断言；空输出 hook 零播报且不影响后续 hook；前序 hook error/timeout 后续照常执行；announce 抛错 → 不上抛（handler 返回正常）且 `logger.warn` 被调用；空注册表 → announce 零调用；输入 `entry.state` 即脚本收到的 `board`（脚本 `return JSON.stringify(board.width)` 验证载荷直通）

**Checkpoint / 验证门禁**：`bazel test //common/js/dsh-plugins/saolei-hooks/...` 全绿（Phase 3–5 后新包单测覆盖契约 §6 全部验收面——SC-003/SC-004 的单测部分达成）。

---

## Phase 6: User Story 5 - 组合挂载与双角色装配（hooks 插件接入） (Priority: P2)

**Goal**: 插件 entry provide `saoleiHooks` 工厂（buildHooksUnit：tools + sections（恒空）+ onBoardChange 共享闭包）；saolei-loop 条件 fail-loud 与编排三面装配（含依赖边静态断言）；agent_v2 组合接线。

**Independent Test**: `bazel test //common/js/dsh-plugins/saolei-hooks/... //common/js/dsh-plugins/saolei-loop/...` 全绿（装配/缺席回归/装载失败断言）+ agent_v2 组合装载验证。

### 文档清单（编码前必读；宪法 V 三分类）

- **代码规范文档**：`style/javascript.md`（ESM/测试书写规则）；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的规范基准）
- **官方文档**：无（`@deepseek-ai/cordis` 为仓库内闭包携带的私有包、无公开文档；`ctx.plugin`/`ctx.provide` 语义以仓库先例为唯一权威——`common/js/dsh-plugins/saolei-loop/src/index.ts` 与 saolei-memory entry）
- **技术文章/技术参考文档**：`specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md` §1（插件定位与依赖边界）/§5（组合挂载与编排装配）+ §6（验收面第 5 组）；`specs/078-saolei-js-hooks/contracts/hook-announcement.md` §1（播报通道零新机制）/§2（格式表——唯一权威来源）/§5 第 2 组（announce 绑定到编排持有 announcer 的验收面，T014 断言依据）；`specs/078-saolei-js-hooks/data-model.md` §3.1（SaoleiHooksMount/SaoleiHooksUnit 接口）/§5（组合清单增量）；`specs/078-saolei-js-hooks/research.md` D8（工厂形态与被否决备选）；`specs/074-dsh-grpc-runtime/contracts/team-assembly.md` §1（机制）/§2（依赖声明）/§3（提供方插件契约——既有能力行的 provide/fail-loud 形态）/§4（编排消费）/§6（不变量——能力单元整体生效）；`specs/075-saolei-game-foundation/contracts/team-loop-assembly.md` §2（游戏运行时构造持有）/§3（成员 setup 注册序——planner/player 插入点）；`common/js/dsh-plugins/saolei-loop/src/index.ts`（apply 挂载与 fail-loud 先例——T013 改动对象）；`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`（`createMember` 的 player/planner setup、announcer 物化与 `get announcer` 读取面——T014 改动对象）；`common/js/dsh-plugins/saolei-loop/src/{index,orchestrator}.test.ts`（T013/T014 扩展对象——既有断言基线）；`common/js/dsh-plugins/saolei-tools/src/index.ts`（`SaoleiToolsUnit` 提供方先例——T012「同形」的对照面）；`common/js/dsh-plugins/saolei-loop/src/announcer.ts`（`announce(text)` 生产协议）与 `specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md` §3（announcer 产出通道）；`projects/game/agent_v2/cordis.yml`（saolei-loop 行 config.plugins——T015 接线对象）；`specs/078-saolei-js-hooks/quickstart.md` §2（验证面）

### Implementation

- [X] T012 [US5] 插件 entry：`common/js/dsh-plugins/saolei-hooks/src/index.ts`——`export const name = "saolei-hooks"`、`export const inject: string[] = []`、`export const SAOLEI_HOOKS_SERVICE = "saoleiHooks"`、`apply(ctx)` → `ctx.provide(SAOLEI_HOOKS_SERVICE, mount)`；`mount.buildHooksUnit({ announce }): SaoleiHooksUnit`——unit 构造时创建一个 `HookRegistry` 闭包，返回 `{ tools: [saoleiHookTool(registry)], sections: [], onBoardChange: createOnBoardChange({ registry, announce }) }`（tools 与 handler 共享同一 registry；`sections` 恒为空数组——零 prompt section，spec FR-006；与 `SaoleiToolsUnit` 同形供编排统一注册）。创建 `common/js/dsh-plugins/saolei-hooks/src/index.test.ts`：provide 面存在；同一 unit 的 tool set 后 `entriesInOrder()` 可见（双面共享 registry）；两次 `buildHooksUnit` 返回独立 registry（team 物化隔离）；unit 的 tools 长度 1、name `saolei_hook`、`sections` 为空数组；**依赖边静态断言**（SC-005）：`package.json` deps 仅 `@dominion/dsh-saolei`（peer 仅 cordis/dsh-tools/dsh-system-prompt），且 `src/` 无对 saolei-tools/saolei-loop/team/announcer 的 import
- [X] T013 [US5] 条件 fail-loud：`common/js/dsh-plugins/saolei-loop/src/index.ts` `apply()`——遍历 `config.plugins` 行时记录是否存在 `@dominion/dsh-saolei-hooks` 行；挂载循环后若该行存在而 `ctx.get("saoleiHooks") === undefined` → throw（与既有 `saoleiTools`/`memoryMount` 校验同型文案）；行缺席 → 不检查（可选能力缺席，spec FR-015）。扩展 `common/js/dsh-plugins/saolei-loop/src/index.test.ts`：行在面缺 → 装载 reject；行缺（仅 tools+memory）→ apply 成功（既有用例回归）
- [X] T014 [US5] 编排装配：`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`——物化路径读取可选能力 `ctx.get("saoleiHooks")`（新增私有读取方法，缺省 undefined）：在场 → **物化早期（`createMember("player")` 之前——player setup 需要 `onBoardChange`）**`unit = mount.buildHooksUnit({ announce: (text) => this.announceHookResult(text) })` 并持有 unit；`announceHookResult` 为私有方法——调用时经 `get announcer` 读取面取 announcer，缺席则 `warn` 并跳过（不上抛，禁用非空断言）；planner setup 注册 `unit.tools` 后遍历 `unit.sections`（恒空数组 → 零 section）；player setup 的 `foundation.createGameRuntime(options.session, this.desktopBridge())` 增第三参 `{ onBoardChange: unit.onBoardChange }`（unit 缺席 → 不传第三参，现状不变）；teardown/dispose 丢弃 unit 引用（registry 随之失效）。扩展 `common/js/dsh-plugins/saolei-loop/src/orchestrator.test.ts`：能力在场 → planner setup 收到 `saolei_hook` 工具且注册的 sections 集不含 hook 项（SC-008 集成面）、`createGameRuntime` 第三参含 `onBoardChange`、hook 播报经 announce 绑定到 orchestrator 持有的 announcer（mock announcer 断言转发）、saolei-tools 工具面与 `SAOLEI_GUIDANCE` 逐字不变（FR-007）；能力缺席 → 既有用例全量回归不变（setup 零 hook 装配、createGameRuntime 两参）
- [X] T015 [US5] 组合接线：`projects/game/agent_v2/cordis.yml` saolei-loop 行 `config.plugins` 追加 `- name: '@dominion/dsh-saolei-hooks'`（无 config）；`projects/game/agent_v2/package.json` dependencies 追加 `"@dominion/dsh-saolei-hooks": "workspace:*"`；根目录 pnpm 安装 + `bazel run //:gazelle projects/game/agent_v2` 刷新；`bazel build //projects/game/agent_v2/...` + `bazel test //projects/game/agent_v2/...` 全绿（组合装载与既有 session/history 单测回归）

**Checkpoint / 验证门禁**：`bazel test //common/js/dsh-plugins/saolei-hooks/... //common/js/dsh-plugins/saolei-loop/... //projects/game/agent_v2/...` 全绿。

---

## Phase 7: Polish & 大型测试验收（宪法 VI）

**Purpose**: 契约终态修订（FR-019）、README 同步、真实端到端大型测试（077 推迟项，FR-018/SC-001/SC-002/SC-006）、最终验收。

### 文档清单（编码前必读；宪法 V 三分类）

- **代码规范文档**：`style/golang.md`（§单元测试——命名/表驱动/禁止用例内塞断言，T018 go 用例必读；`style/large_test.md` 引用的规范基准）；[Google Go Style](https://google.github.io/styleguide/go/) 及其三份基准——[Style Guide](https://google.github.io/styleguide/go/guide)（normative/canonical，必读）、[Style Decisions](https://google.github.io/styleguide/go/decisions)（normative）、[Best Practices](https://google.github.io/styleguide/go/best-practices)（非规范但鼓励遵循；`style/golang.md` 引用的规范基准，间接引用显式列出）；`style/large_test.md`（测试计划组织/按模块归位/反模式——"不要为单个需求新建独立测试计划"、T019 执行约定 "FOR Agent: 使用 testplan SKILL"）
- **官方文档**：无（guitar/testplan 与 fake-llm 均为仓库内基建，无第三方 API 语义需外部文档；fake-llm 模板语义以 `specs/046-fake-llm-think-chunking/contracts/` 的仓库内契约为权威）
- **技术文章/技术参考文档**：`specs/078-saolei-js-hooks/quickstart.md` §3/§4（大型测试场景脚本与执行命令）；`specs/078-saolei-js-hooks/contracts/hook-announcement.md` §5（端到端断言面 ①②③④⑤⑥）；`specs/078-saolei-js-hooks/spec.md` FR-018/FR-019 与 SC-001–SC-008（验收基准）；被修订契约原文——`specs/075-saolei-game-foundation/contracts/saolei-foundation.md`、`specs/075-saolei-game-foundation/contracts/team-loop-assembly.md`、`specs/074-dsh-grpc-runtime/contracts/team-assembly.md`、`specs/076-agent-v2-optimize/contracts/flow-orchestration.md`、`specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md`、`specs/076-agent-v2-optimize/contracts/announcer-member.md`；`projects/game/fake-llm/README.md`（keyword/tool_call 模板机制与嵌入 testdata）；`specs/046-fake-llm-think-chunking/contracts/template-config.md`（模板 schema 的 author-facing 契约——README 明示的唯一权威引用，T018 新编模板必读，间接引用显式列出）；`specs/046-fake-llm-think-chunking/contracts/streaming-sequence.md`（模板的 SSE 序列语义——tool_call/多轮模板行为依据）；`projects/game/fake-llm/service/testdata/team_planner.yaml`（planner 模板锚定形态——persona 开场 + user message 关键词 + tool 规则）；`projects/game/fake-llm/service/testdata/team_player.yaml`（player 模板锚定形态——operate 链与广播关键词锚定先例）；`projects/game/testplan/README.md`（大型测试消费 fake-llm 的方式与套件约定）；`projects/game/testplan/system_test.yaml`（game-system suite 结构与四拓扑；本 feature 的 hook 场景落在 game-system suite）；`projects/game/testplan/deploy_agent_v2.yaml`（won 拓扑：fake-desktop 执行器为 `won` 终局场景——T018 据此判定不能经 fake-desktop 承载非终局 operate）；`projects/game/fake-desktop/README.md`（场景族 `won`/`lost`/`progressive` 与 env、两个拓扑的绑定——判定场景可用性的权威依据）；`projects/game/testplan/agent_v2_helpers_test.go`（**测试自带 desktop half**：`teamFlowScript`/`serveTeamFlowScript`/`openDesktopStream` 与 `agentV2Prog*` 文案常量——T018 场景的复用面）；`projects/game/testplan/saolei_fixtures_test.go`（内嵌扫雷棋盘 fixture 与共享纪律：`saoleiBoardInitPNG`（`saolei_1`，16×16 全 INITIAL）等——播种仍 `playing` 局面所需；本 feature 另从 `projects/game/fake-desktop/service/testdata/` 引入 `saolei_3`）；`projects/game/testplan/agent_v2_game_test.go`（game 模块用例归位与既有 helper）；`projects/game/agent_v2/README.md`（T017 修订对象——现有已知限制/能力说明基线）与 `common/js/dsh-plugins/saolei-hooks/README.md`（T001 骨架 → T017 补全对象）；`specs/078-saolei-js-hooks/research.md` D6（断言的播报文本格式）

### Implementation

- [X] T016 [P] 契约终态修订（宪法 VII：直接修订原文不留增量痕迹，修订处引用 `specs/078-saolei-js-hooks/spec.md` 与本 feature `contracts/`）：①`specs/075-saolei-game-foundation/contracts/saolei-foundation.md` §2 基础服务面增事件 seam（`onBoardChange` 可选回调、触发条件、订阅方异常边界）；②`specs/075-saolei-game-foundation/contracts/team-loop-assembly.md` §2 增 `createGameRuntime` 第三参与 hooks 事件接线表述；③`specs/074-dsh-grpc-runtime/contracts/team-assembly.md` §3 提供方插件契约增 `saoleiHooks` 行（纯 provider + `buildHooksUnit` 工厂 + 条件 fail-loud 语义）、§4 编排消费增双角色装配点（planner tools / player 事件接线 / announce seam 物化绑定）；④`specs/076-agent-v2-optimize/contracts/flow-orchestration.md` §3 增广播投递时机注记（hook 播报=插入式产出，快路 + pending 按 077 契约）；⑤`specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md` §3 产出通道增 hook 播报为第二生产来源（经编排装配的 announce seam，终态表述），`specs/076-agent-v2-optimize/contracts/announcer-member.md` §2 注明第二来源沿用同一消息形状（proto 面零变更）
- [X] T017 [P] `common/js/dsh-plugins/saolei-hooks/README.md` 补全（插件定位/依赖边界/挂载方式/资源上限/无持久化语义）与 `projects/game/agent_v2/README.md` 更新（hook 能力：planner 经 `saolei_hook` 固化脚本、有棋盘变动且非终局的批量操作后播报、注册表随 team 刷新清空、引用 `specs/078-saolei-js-hooks/`）
- [X] T018 [US1] 大型测试端到端（077 推迟项）：fake-llm 模板 + go 用例，按模块归位（`style/large_test.md`——**不新建测试计划、不新建 binary**；场景脚本与断言以 `specs/078-saolei-js-hooks/quickstart.md` §3 为准）：①`projects/game/fake-llm/service/testdata/team_planner.yaml` 增 hook-set 模板（锚定 planner persona 开场 + 测试用例控制的关键词，emit `saolei_hook` set tool_call：name 如 `safe-cells`、script 为确定性安全格扫描函数体；tool 结果后续播确认文本）；②`team_player.yaml` 增一条锚定 `[hook safe-cells]` 播报文本的后续模板，并使其 `tool_call` 成为 player 下一 step 的**唯一成功路径**（fake-llm 对无匹配请求回退随机文本模板，故"模板未命中"本身不是判据：缺播报时该 tool_call 不出现、后续断言即失败——可失败的间接断言）；③`projects/game/testplan/agent_v2_game_test.go` 增 hook 场景用例（独立 session 资源名隔离）：**非终局 operate 必须经测试自带 desktop half 播种**——`openDesktopStream` + `serveTeamFlowScript`（复用 `projects/game/testplan/agent_v2_helpers_test.go` 的 script 类型），配对固定为 16×16：`initBoards` = `saoleiBoardInitPNG`（`saolei_1`，16×16 全 INITIAL）→ `stepBoards` = `saolei_3` 的 16×16 部分翻开板（同尺寸、已翻开不回退）使 `executed > 0` 且条目 `status: "playing"`（**9×9 家族不可用**——其唯一既有后继 `saolei_10` 是终局 won 板，按 FR-001 不触发）；**不得用 won/lost 终局链路，也不得依赖 won 拓扑的 fake-desktop 执行器**（其 `won` 场景 init 即终局、无 cell 转换）；`saolei_3` 从 `projects/game/fake-desktop/service/testdata/saolei_3.png` 复制进 `projects/game/testplan/testdata/`（与既有内嵌 fixture 同型，`saolei_fixtures_test.go` 共享纪律——本场景只需 `saolei_3`）；断言 web `ListTeamMessages` 与实时流出现 `[hook safe-cells]` 播报（member=announcer）、同锚点恰一次、planner 侧下次驱动经锚定模板命中同一播报（drain）、跨成员发言级顺序一致；**player 的 `saolei_operate` 结果文本与基线逐字相等（不含 `[hook` 前缀——hook 分析不经工具结果回流，FR-010）**；全 skip 或终局批量后零新增 hook 播报；④`system_test.yaml` 无需变更（`agent_v2_game_test` case 已在 game-system suite）；本地 `bazel build //projects/game/testplan:agent_v2_game_test` 验证编译（构建仅是开发步骤，验收在 T019 实际执行）
- [ ] T019 最终验收：按 `specs/078-saolei-js-hooks/quickstart.md` 执行——§2 四包 `bazel build`/`bazel test` 全绿（saolei / saolei-hooks / saolei-loop / agent_v2）；§3 经 **testplan SKILL** 实际执行 `guitar run projects/game/testplan/system_test.yaml` 全套件（部署→测试→清理闭环，含既有四 suite 与新增 hook 用例），**全部用例通过**（宪法 VI：任何 failed/flaky 即验收未通过，修复后重跑至全绿）；对照 spec SC-001–SC-008 逐项确认

**Checkpoint / 验证门禁**：T016–T017 文档落盘（引用可追溯）；T018 编译通过；T019 全绿后 feature 达到 spec 全部 SC。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1（Setup）**：无依赖，立即开始；**阻塞全部后续 phase**（新包载体）。
- **Phase 2（US4 事件 seam）**：依赖 Phase 1 无关（改 `@dominion/dsh-saolei` 既有包）——**可与 Phase 1 并行**；阻塞 Phase 5（handler 的触发源）与 Phase 6（player setup 接线）。
- **Phase 3（US3 沙箱）**：依赖 Phase 1（limits.ts 与包载体）；阻塞 Phase 4（registry.set 消费 `compileHookScript`）与 Phase 5（handler 消费 `runHookScript`）。
- **Phase 4（US2 注册表与工具）**：依赖 Phase 3；阻塞 Phase 5（handler 消费 registry）与 Phase 6（unit.tools 组装）。
- **Phase 5（US1 处理器与播报闭环）**：依赖 Phase 2/3/4（触发源 + 执行器 + 注册表）。
- **Phase 6（US5 挂载与装配）**：依赖 Phase 4/5（unit 组装面）；T013/T014/T015 串行（同一装载链），T012 先行。
- **Phase 7（Polish + 验收）**：依赖 Phase 1–6 全部完成（契约修订描述终态、大型测试走真实链路）。

### User Story Dependencies（spec 优先级与实现的拓扑序说明）

- spec 优先级：US1 P1 / US2 P1 / US3 P2 / US4 P2 / US5 P2。phase 排序按**依赖拓扑**（US4 → US3 → US2 → US1 → US5）：US1 是集成闭环故事（其独立测试 = 单测级构造注册表 + 事件载荷），其输入构件由 US2/US3/US4 交付；优先级标注保留在各 phase 标题，映射不丢失。
- 各 US 的独立性：US4（游戏基础单测）、US3（沙箱单测）、US2（注册表/工具单测）在各自 phase 完成时即可独立验证；US1/US5 的端到端价值在 Phase 6 后经组合装载可用、Phase 7 大型测试验收。

### Within Each User Story

- 先实现后测试的仓库惯例不强制 TDD（模板默认测试可选；本仓库测试随实现任务同 task 交付——宪法 IV 编译+单测为变更一部分）；实现文件与对应 `*.test.ts` 同 task 或紧邻 task 完成。

### Parallel Opportunities

- Phase 1 T001 与 Phase 2 T003/T004（不同包，无依赖）可并行。
- Phase 2 与 Phase 3（不同包；Phase 3 仅依赖 Phase 1 的 T001/T002）可并行。
- Phase 4 的 T007 与 T008（registry 与 tool 不同文件，tool 依赖 registry 接口——接口先行时可并行书写，合入按 T007 → T008 序）。
- Phase 7 的 T016 与 T017 与 T018 前半（fake-llm 模板）可并行（不同文件）。

---

## Parallel Example: Phase 2 与 Phase 3

```bash
# 不同包、无依赖，可并行推进（各自门禁独立）：
Task: "T003 [US4] 事件 seam common/js/dsh-plugins/saolei/src/game/runtime.ts"
Task: "T005 [US3] 沙箱执行器 common/js/dsh-plugins/saolei-hooks/src/sandbox.ts"

# 同理 Phase 1 与 Phase 2：
Task: "T001 新包脚手架 common/js/dsh-plugins/saolei-hooks/"
Task: "T003 [US4] 事件 seam common/js/dsh-plugins/saolei/src/game/runtime.ts"
```

---

## Implementation Strategy

### MVP First（单 US 不可行的说明与本 feature 的 MVP 口径）

- 模板默认"MVP = User Story 1"不适用于本 feature：spec 的五个 US 是同一能力的**结构切片**（触发源/写入面/执行器/闭环/挂载），其独立性在单测级而非交付级（spec 各 US 的 Independent Test 均为单测面）。
- 本 feature 的 **MVP 口径 = Phase 1–6 完成**：四包单测全绿 + agent_v2 组合装载后 hook 能力真实可用（planner 可 set、player 对局触发播报、web 可见）。Phase 7 为验收与文档收口（宪法 VI 大型测试门禁在 T019 关账）。

### Incremental Delivery

1. Phase 1–2（载体 + 触发源）→ 游戏基础事件 seam 可独立回归验证。
2. Phase 3–4（执行器 + 写入面）→ 沙箱与工具单测闭环。
3. Phase 5（闭环）→ handler 播报单测闭环（SC-003/SC-004 单测部分达成）。
4. Phase 6（挂载）→ 组合级可用（装载/装配/缺席回归）。
5. Phase 7（验收）→ 大型测试全绿 + 契约/README 终态（SC-001–SC-008 全量达成）。

---

## Notes

- [P] 标记 = 不同文件且无未完成依赖；[US*] 映射 spec 用户故事。
- 每个 checkpoint 后提交（task 或逻辑组粒度）；原子变更组无需（本 feature 无跨包同批语义变更——077 的组 A 形态不适用）。
- 宪法 V：各 phase 文档清单编码前**完整阅读**；清单外的仓库文档按引用需要自行检索。
- 禁止事项提醒（契约红线）：不得给 hook 增加任何 prompt section（FR-006/SC-008 有代码面断言）；不得在 describe 中提及 memory 或任何特定存储工具（FR-003）；`saolei_operate` 工具结果不得混入 hook 输出（FR-010）；saolei-tools 与 hooks 插件间不得新增依赖边（FR-016）。
