# Implementation Plan: 078 saolei JS hook（planner 固化策略脚本 + 棋盘变动后执行播报）

**Branch**: `078-saolei-js-hooks` | **Date**: 2026-10-17 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/078-saolei-js-hooks/spec.md` + 用户 plan 阶段指令（2026-10-17：①热门 coding agent JS 实现调研；②无持久化/空输出不播报入 describe；③单工具零 section；④游戏开始与结束不触发——已并入 spec Clarifications）

## Summary

为 agent_v2 落地 JS hook 能力（survey `survey/saolei-hook-scripts.md` 全部裁定，K4 与在途广播快路已由 077 交付）：① 游戏基础 `@dominion/dsh-saolei` 增加棋盘变动事件 seam（`GameRuntimeDeps.onBoardChange` 可选回调——operate 落账且 `executed > 0` 且 `playing` 时同步触发；init/终局/无落账路径不触发）；② 新插件 `@dominion/dsh-saolei-hooks`：team 级工厂 `buildHooksUnit({announce}) → {tools, sections（恒空）, onBoardChange}`——进程内存态注册表（set/remove/list，同名覆盖保序，无持久化）、`node:vm` 沙箱执行器（无宿主 API 注入（上下文仅数据参数 `board` 快照）+ `codeGeneration: { strings: false, wasm: false }` + 调用形态内联编译（`(function(board){…})(board)`，timeout 覆盖函数体）+ 逐 hook 500ms timeout + 输出截断 + banned-pattern）、planner 侧单 action 工具 `saolei_hook`（全部说明经 describe 承载，含空输出不播报的低噪音引导与"跨局保留自行保存"且不指向任何存储工具；零 prompt section）、事件处理器（注册序执行 → 非空输出逐条 `[hook <name>] <text>` 播报，空输出零播报，失败隔离不上抛）；③ 播报回流零新机制——announcer 单次生产 + 077 快路（在途 player 同回合下一 step 可见、锚点闭包恰好一次）+ pending drain（静止成员）；④ saolei-loop 条件 fail-loud 挂载与双角色装配（planner tools / player 事件接线 / announce seam 绑定编排持有的 announcer），hooks 与 saolei-tools 零耦合；⑤ 大型测试携带 077 推迟的真实端到端快路场景（fake-llm 驱动 planner set → player operate → 三面投影 + 下一 step 携带断言；play 侧经测试自带 desktop half 播种仍 `playing` 的棋盘）。

技术方向（用户指令①调研后裁定，[research.md](research.md)）：JS 引擎 D1 = **`node:vm`**（三先例收敛：OpenClaw Node executor 默认 node:vm [docs.openclaw.ai](https://docs.openclaw.ai/tools/code-mode/executors)、ado-code-mode-mcp ADR-002 同威胁模型、Claude Code Mods 第一方 JS 无沙箱——本方案严格更保守；quickjs/worker 为演进位）；工具形态 D2 = 单 action 工具维持（本仓库 memory 先例 + describe 内聚）；其余决策 D3–D9 见 research.md。

## Technical Context

**Language/Version**: TypeScript（ESM，Node 运行时；`common/js/dsh-plugins/` 插件 + `projects/game/agent_v2` 宿主）

**Primary Dependencies**: `@dominion/dsh-saolei`（`GameState`/`GameLogEntry` 类型 + 事件 seam）、`@deepseek-ai/dsh-tools`（`defineTool`/`ToolDefinition`/schema 子集）、`@deepseek-ai/dsh-system-prompt`（`PromptSection`——`sections` 恒空数组的类型来源）、Node 内建 `node:vm`；**零新增运行时第三方包**（`peerDependencies` 沿用 `saolei-tools` 先例的 cordis/dsh-tools/dsh-system-prompt `catalog:`；quickjs/isolated-vm 均为演进位）。

**Storage**: N/A（注册表为 team 物化进程内存态，无持久化——spec FR-005）

**Testing**: bazel（`bazel build`/`bazel test`，单测随每次变更）；大型测试经 testplan skill（`guitar run projects/game/testplan/system_test.yaml`，宪法 VI 实际执行闭环，新增 hook 端到端场景）

**Target Platform**: Linux 服务（agent_v2 gRPC 宿主进程内）

**Project Type**: library（dsh 插件，新包）+ web-service（宿主组合增量，对外 API 零变更）

**Performance Goals**: hook 执行 ≤500ms/个（预算常量）；总执行上界 = `MAX_HOOKS × EXECUTION_TIMEOUT_MS` = 4s（同步路径的已接受上界，spec FR-011）；事件回调同步、零新增序列化/拷贝（载荷即既有 gameLog 条目对象）；播报为既有 announce 路径（077 交付，零新增开销面）。

**Constraints**: 对外 proto/RPC/ChatEvent 帧形状与 web 前端零变更；saolei-tools 与 `SAOLEI_GUIDANCE` 零改动；077 快路/统一流机制零语义修订（纯消费）；工具参数 schema 受 `@deepseek-ai/dsh-tools` enforced JSON Schema 子集约束（互斥在 execute 内文本拒绝）。

**Scale/Scope**: 新包 1（`common/js/dsh-plugins/saolei-hooks/`）+ 三包增量（saolei 事件 seam / saolei-loop 装配 / agent_v2 组合清单）+ 六份既有契约修订 + README + 大型测试场景。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 门禁 | 状态 | 说明 |
|---|---|---|
| 1. 文档阅读门禁（原则 V） | ✅ | tasks.md 按 phase 声明三分类文档清单（已产出）；本 plan 已锚定全部参考文档（survey、077 spec/契约、实现锚点、research.md 外部引用） |
| 2. 实现门禁（原则 II/III） | ✅ | 重构式变更：架构补缺（K4 分层 + 快路）已由 077 作为独立 feature 先行交付，本 feature 在其上只做最小功能增量，无补丁式绕行；接口优先：[contracts/](contracts/) 三份契约（board-change-event / saolei-hooks-plugin / hook-announcement）先于实现定义全部接口面（seam 回调、provide 工厂、工具 schema/校验层次、播报格式表） |
| 3. 编译 + 单测门禁（原则 IV） | ✅ | 每次变更 `bazel build` + `bazel test`（四包相关 target），不单列 task；大型测试单独验收（SC-006） |
| 4. 引用、图表与终态门禁（原则 I/VII/VIII） | ✅ | 引用全部带仓库相对路径/URL（research.md §1/§4、契约实现锚点）；spec/契约终态表述（被否决备选——additionalContexts、三工具分立、quickjs/worker——仅记录为演进位/否决理由，不留中间态）；全部图表为 Mermaid 代码块（spec 时序图、hook-announcement 流图） |
| 5. 大型测试验收门禁（原则 VI） | ✅ | testplan skill 实际执行 system_test.yaml 全套件（部署→测试→清理闭环），全部用例通过为验收（SC-006）；077 推迟的真实端到端快路场景由本 feature 携带（SC-002/FR-018）——不存在豁免情形 |

Phase 1 设计后复核：新增接口（`GameRuntimeDeps.onBoardChange` 可选回调、`SaoleiHooksMount.buildHooksUnit` 工厂、`saolei_hook` 工具 schema、`createGameRuntime` 可选第三参）均有契约定义与验收面；新包为 D6 用户裁定的独立插件（零耦合要求结构性需要），无过度设计——通过。

## Project Structure

### Documentation (this feature)

```text
specs/078-saolei-js-hooks/
├── plan.md              # This file
├── research.md          # Phase 0 output（D1–D9 决策记录 + 热门 agent 调研盘点）
├── data-model.md        # Phase 1 output（类型/注册表状态迁移/上限常量/装配）
├── quickstart.md        # Phase 1 output（验证指南）
├── contracts/           # Phase 1 output
│   ├── board-change-event.md     # 游戏基础事件 seam 契约
│   ├── saolei-hooks-plugin.md    # hooks 插件能力面/工具/沙箱/挂载契约
│   └── hook-announcement.md      # 播报回流契约（快路复用/格式表/可见性）
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
common/js/dsh-plugins/saolei/src/game/
├── runtime.ts        # GameRuntimeDeps.onBoardChange 可选回调 + operate 内触发条件
│                     # （executed>0 ∧ playing）；createGameRuntime 可选第三参
└── runtime.test.ts   # seam 七项断言（触发条件/载荷同值/seam 缺省回归/异常边界）

common/js/dsh-plugins/saolei-hooks/            # 新包 @dominion/dsh-saolei-hooks（survey D6）
├── package.json      # workspace 协议：deps 仅 dsh-saolei；cordis/dsh-tools/dsh-system-prompt 为 catalog: peer（saolei-tools 先例）
├── BUILD.bazel       # gazelle 生成
├── README.md         # 插件定位/依赖边界/挂载说明
└── src/
    ├── index.ts      # cordis plugin entry（name="saolei-hooks"，inject=[]，
    │                 # ctx.provide("saoleiHooks", mount)——buildHooksUnit 工厂）
    ├── limits.ts     # MAX_HOOKS=8 / MAX_SCRIPT_CHARS=16384 / MAX_OUTPUT_CHARS=4096
    │                 # / EXECUTION_TIMEOUT_MS=500（research D4）
    ├── registry.ts   # HookRegistry（set 覆盖保序 / remove / list / entriesInOrder）
    ├── sandbox.ts    # node:vm 执行器（注册期调用形态编译 + banned-pattern；执行期
    │                 # fresh context + 数据参数 board（JSON 快照）+ codeGeneration:{strings:false,wasm:false}
    │                 # + runInContext(timeout 覆盖函数体) + 截断；HookRunResult 分类）
    ├── tool.ts       # saolei_hook 工具（根字段 optional + execute 内 XOR 八层
    │                 # 校验（含非适用动作多余字段拒绝）文本拒绝；describe 八要素，research D9）
    ├── handler.ts    # onBoardChange（注册序执行 → 播报格式表 → 空输出零播报；
    │                 # 异常边界 try/catch + warn，不上抛）
    └── *.test.ts     # 契约 §6 验收面逐项

common/js/dsh-plugins/saolei-loop/src/
├── index.ts          # apply 条件 fail-loud（hooks 行存在而 provide 缺失 → 装载失败）
├── orchestrator.ts   # hooks unit 装配：物化早期（player 成员构造前）buildHooksUnit
│                     # （{announce: 惰性闭包 → get announcer}）；planner setup 注册
│                     # unit.tools 并遍历 unit.sections（恒空）；player setup
│                     # createGameRuntime(..., {onBoardChange: unit.onBoardChange})
└── *.test.ts         # 装配三面/行缺席回归/装载失败断言

projects/game/agent_v2/
├── cordis.yml        # saolei-loop 行 config.plugins 追加 @dominion/dsh-saolei-hooks 行
├── package.json      # 闭包追加 @dominion/dsh-saolei-hooks
└── src/              # 零改动（077 统一流已承载播报投影）

# 契约/文档修订（FR-019）
specs/075-saolei-game-foundation/contracts/saolei-foundation.md      # 事件 seam 增量
specs/075-saolei-game-foundation/contracts/team-loop-assembly.md     # §2 构造事件接线
specs/074-dsh-grpc-runtime/contracts/team-assembly.md                # hooks 能力行/双角色单元
specs/076-agent-v2-optimize/contracts/flow-orchestration.md          # 广播投递时机注记
specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md     # §3 announcer 产出通道：hook 播报第二生产来源
specs/076-agent-v2-optimize/contracts/announcer-member.md            # §2 proto 面：第二来源沿用同一消息形状（零变更）
projects/game/agent_v2/README.md                                     # hook 能力说明

# 测试基建
projects/game/fake-llm/service/testdata/    # hook 场景 keyword/tool_call 模板（team_planner / team_player）
projects/game/testplan/             # agent_v2_game_test.go 增 hook 端到端用例 + testdata/saolei_3.png
                                    # （system_test.yaml 无需变更——agent_v2_game_test case 已在 game-system suite）
```

**Structure Decision**: 新增一个插件包（survey D6 用户裁定——与 saolei-tools 零耦合的结构性载体），其余改动落在既有包内；无新对外 API、无 schema/存储变更；契约修订按各契约"基线契约修订"节列出的映射执行。

## Complexity Tracking

无宪法违规需论证（全部门禁通过）。新增面均为裁定的最小接口：deps 一个可选回调、provide 一个工厂、组合一行、编排三处装配点；`node:vm` 为 Node 内建零依赖。
