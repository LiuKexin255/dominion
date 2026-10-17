# Contract: hooks 插件——能力面、注册表、工具与沙箱

**Feature**: [../spec.md](../spec.md)（FR-003–FR-007、FR-012–FR-017；US2/US3/US5）| **决策依据**: [../research.md](../research.md) D1–D4、D7–D9 | **数据模型**: [../data-model.md](../data-model.md) §2–§3、§5
**实现锚点**: `common/js/dsh-plugins/saolei-hooks/src/`（新包 `@dominion/dsh-saolei-hooks`）、`common/js/dsh-plugins/saolei-loop/src/{index,orchestrator}.ts`、`projects/game/agent_v2/{cordis.yml,package.json}`
**基线契约修订**: `specs/074-dsh-grpc-runtime/contracts/team-assembly.md`（能力插件行新增 hooks——首个双角色协作单元的装配形态）；`projects/game/agent_v2/README.md`（hook 能力说明）。

## 1. 插件定位与依赖边界

- 新包 `@dominion/dsh-saolei-hooks`（`common/js/dsh-plugins/saolei-hooks/`）：`name = "saolei-hooks"`，`inject: []`（**纯 provider**——无 cordis 服务依赖；播报 seam 经 build 参数注入，research D8）。
- plugin load 时 `ctx.provide("saoleiHooks", mount)`；`mount.buildHooksUnit({ announce }) → { tools, sections, onBoardChange }`（`sections` 恒为空数组——零 prompt section，与 `SaoleiToolsUnit` 同形以便编排统一注册；一个 unit = 一个 team 物化，registry/executor/handler 共享同一闭包）。
- **构建点**：`buildHooksUnit` 在物化**早期**调用（`createMember("player")` 之前——player setup 需要 `onBoardChange`），故 `announce` 传入的是**惰性闭包**（调用时读编排持有的 announcer，见 §5）。
- **零耦合**（spec FR-016）：依赖仅为 `@dominion/dsh-saolei`（`GameState`/`GameLogEntry` 类型）+ `@deepseek-ai/dsh-tools`（`defineTool`/`ToolDefinition`）+ `@deepseek-ai/dsh-system-prompt`（`PromptSection`——`sections` 恒空数组的类型来源）+ node 内建 `node:vm`；**不依赖** saolei-tools、saolei-loop、team、announcer 的任何类型或服务，也不被 saolei-tools 依赖。

## 2. 注册表契约

- 注册项身份 = `name`（字符串）；`set` 已存在名称 → **覆盖脚本**（重新过全部校验）并**保留注册序位置**（research D3）。
- 生命周期 = team 物化内存态：物化时创建、刷新（UpdateTeam 重建）/dispose 时丢弃；**无持久化**（spec FR-005）。
- 写读面仅 `saolei_hook` 工具（planner）与 `onBoardChange` handler（事件读）。
- 上限（research D4，具名常量）：数量 8、脚本 16384 字符、输出 4096 字符、单次执行 500ms。

## 3. 工具面契约（`saolei_hook`）

### 3.1 schema 与校验层次

```text
参数（根字段全部 optional；schema 受 dsh-tools enforced JSON Schema 子集约束）:
  action?: "set" | "remove" | "list"
  name?:   string
  script?: string
校验层次（全部在 execute 内，以普通文本结果拒绝——memory 工具同型，
schema 层不做互斥以保留模型可恢复性）:
  1. action 缺失/未知              → 文本拒绝
  2. 非适用动作的字段（`action:"list"` 带 name/script；
     `action:"remove"` 带 script）→ 文本拒绝（列出多余字段名）
  3. set: name/script 缺失         → 文本拒绝（含缺失字段名）
  4. set: 数量/长度超上限          → 文本拒绝（说明上限值）
  5. set: banned-pattern 命中      → 文本拒绝（说明命中模式）——research D7 清单
  6. set: 编译错误（vm.Script）    → 文本拒绝（含语法错误信息与行号）
  7. remove: name 不存在           → 文本告知不存在 + 当前注册名列表
  8. list: 恒成功
```

### 3.2 describe 契约（唯一说明面；spec FR-006/SC-008，research D9）

本清单是 spec FR-006/SC-008 的**实现级展开（唯一实现清单）**；与 spec 冲突时以 spec 为准。describe 为场景无关的自身功能陈述（memory 工具文案纪律，`specs/068-wiki-round-pause/spec.md` FR-012 先例），MUST 覆盖：

1. 三动作用法与参数形状（set = name+script；remove = name；list 无参）。
2. 脚本形态：函数体源码，输入变量 `board` = 棋盘 JSON（字段表：`width`/`height`/`grid[y][x]`（`[y][x]` 行主序；符号 `"INITIAL"|"0".."8"|"FLAG"|"HIT_MINE"|"MINE"|"UNKNOWN"`）/`mineCounter?`（`{decoded: true, value}` 或 `{decoded: false}`，未解码时缺席；`value` = 雷数 − 旗数，过度插旗时为负））；`return` 字符串或可 JSON 序列化对象。脚本拿到的是棋盘 JSON 快照——改写 `board` 不影响对局状态。
3. **空输出不播报**：返回空字符串的执行不产生任何播报——仅在有价值结论时返回内容，编写低噪音脚本；`undefined`（无 `return`）与不可序列化值不属于空输出，按 §2 格式表以 `error: output is not serializable` 播报。
4. **无持久化**：注册表随 team 刷新/重建清空；需要跨局保留的脚本由调用方自行保存并在新 team 重新注册——**不提及 memory、不指向任何特定存储工具**。
5. 同步纯函数要求；无宿主 API（无 require/process/fetch/timer/网络——沙箱内不可得）。
6. 资源上限：单次执行超时（500ms）中断并播报超时；输出超 4096 字符截断；脚本 ≤ 16384 字符；至多 8 个 hook。
7. 触发语义：每次有棋盘变动的批量操作后按注册序自动执行并逐条播报（player 在途时同回合下一 step 经广播可见）；**游戏开始与结束不触发**。
8. 同名 set = 更新脚本（保持执行顺序位置）。

### 3.3 输出契约

工具输出恒为 `{result: string}` 形态的普通结果文本；无 error outcome 路径（失败也是模型可据以修正的文本，saolei 双形式 operate 惯例）。

## 4. 沙箱执行契约（research D1/D7）

- 引擎：`node:vm`——注册期以**调用形态**编译：`new vm.Script("(function(board) {\n" + source + "\n})(board)")`（编译即语法校验；同一 `vm.Script` 可跨 context 复用）；执行期每次 `vm.createContext({ board: <该条目的 JSON 快照> }, { codeGeneration: { strings: false, wasm: false } })`——上下文的唯一注入绑定是数据参数 `board`（无任何宿主 API，其余可见面为 fresh context 自带的 ECMAScript 内建），随后 `script.runInContext(context, { timeout: EXECUTION_TIMEOUT_MS })`，返回值即脚本 `return` 值。
  - **超时覆盖范围**：函数体在 `runInContext` 内执行（调用表达式内联于被编译源码），故 `timeout` 中断窗口**覆盖 hook 函数体**；"宿主在 vm 之外调用编译产物返回的函数"不属本契约认可的形态——那种形态下死循环不会被打断。见 [../data-model.md](../data-model.md) §2.2。
  - **载荷快照隔离**：传入的 `board` 是 `entry.state` 的 JSON 快照（`structuredClone` / `JSON.parse(JSON.stringify(...))` 等价语义），脚本改写 `board` 不触达领域状态（spec FR-012 / US3 场景 6）。
- 每次执行新 context：无跨执行共享状态（spec US3 场景 5）。
- 防御纵深（best-effort，非安全边界）：banned-pattern 注册期校验（§3.1 第 5 层）+ codeGeneration 禁用 + 无宿主 API 注入（上下文只有数据参数 `board` 快照）+ 覆盖函数体的 timeout + 输出截断。
- 威胁模型披露：防第一方作者（planner LLM）的失控资源（死循环/超长输出），非对抗逃逸（spec Assumptions）；已知限制——vm timeout 中断检查在 JS 回边，灾变性正则回溯可能不被中断（research D1 第 5 点，演进位 worker 变体）。

## 5. 组合挂载与编排装配（spec FR-015/FR-017）

- `projects/game/agent_v2/cordis.yml` saolei-loop 行 `config.plugins` 追加 hooks 行；`projects/game/agent_v2/package.json` 闭包追加 workspace 依赖。
- saolei-loop `apply` 条件 fail-loud：`config.plugins` 中存在 `@dominion/dsh-saolei-hooks` 行且装载后 `ctx.get("saoleiHooks")` 缺失 → 组合装载失败（`saoleiTools`/`memoryMount` 同型的装载期校验语义）；**行缺席 = 可选能力缺席**——orchestrator 读取面得 undefined，planner/player setup 零 hook 装配，现状行为不变（US5 场景 5）。
- 编排装配（hooks 能力面在场时）：
  - **物化早期（`createMember("player")` 之前——player setup 需要 `onBoardChange`）**：`unit = mount.buildHooksUnit({ announce: (text) => this.announceHookResult(text) })`——`announce` 是**惰性闭包**：`announceHookResult` 在调用时经 `get announcer` 读取面取编排持有的 announcer；缺席时 warn 并跳过（不上抛；不得用非空断言）。
  - **planner setup**：注册 `unit.tools`，随后遍历 `unit.sections`（恒空数组 → 零 section；与 `SaoleiToolsUnit` 同型，按统一模式注册）。
  - **player setup**：`createGameRuntime(session, bridge, { onBoardChange: unit.onBoardChange })`——事件处理器接入游戏运行时构造（同一运行时实例，`saoleiGame` 句柄解析不变）。
  - 编排不感知 hook 内部语义，只做组装（074 分层不变量）。

## 6. 验收面（对应 spec SC-003/SC-004/SC-005/SC-008）

saolei-hooks 单测（新包）：

1. 工具校验层次 §3.1 八条逐一断言（含非适用动作多余字段的文本拒绝、上限值、banned-pattern、编译错误文本反馈）。
2. describe 内容 §3.2 八项要素齐备（断言关键子串：board 字段表（符号枚举 + `mineCounter` 形态）、空输出不播报、不持久化/自行保存、无宿主 API、超时/截断、注册序、游戏开始与结束不触发）；`unit.sections` 恒为空数组，且 saolei-loop 装配单测断言注册进 agent 的 sections 集不含 hook 项（SC-008 集成面断言）。
3. 沙箱：宿主 API 白名单外访问（`typeof process === "undefined"`、`typeof require === "undefined"`——`require(` 已被注册期 banned-pattern 拦截，"直接引用抛 ReferenceError"只能以绕过 banned-pattern 的裸 `vm.Script` 构造验证）、死循环在 `EXECUTION_TIMEOUT_MS` 内被中断（`runInContext` 的 timeout 覆盖函数体，播报超时文本）、输出截断标识、跨执行无状态（脚本侧变量不存活）、载荷快照隔离（脚本改写 `board` 不改变宿主状态）、对象 return 的确定性 JSON 转字符串、循环引用与 `undefined`（无 `return`）return → `error: output is not serializable` 播报（不视为空输出）。
4. 注册表：同名 set 覆盖保序、remove 后 list、刷新丢弃（unit 重建即空）。
5. 事件处理器：注册序执行、前序失败继续、空输出零播报、异常不上抛（handler 边界 try/catch + warn）。

saolei-loop 单测（增量）：hooks 行在 → 装配三面（tools 注册/onBoardChange 接线/announce 绑定）；hooks 行缺 → 既有用例全量回归不变；行在而 provide 缺失 → 装载失败。
