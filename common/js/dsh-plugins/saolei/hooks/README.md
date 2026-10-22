# @dominion/dsh-saolei-hooks (`common/js/dsh-plugins/saolei/hooks`)

扫雷 **hook 引擎** cordis 插件（cordis 插件名 `saolei-hooks`，`inject = []`）：
由 saolei-loop 组合的 `config.plugins` 以裸包名挂载，`ctx.provide` 暴露
`saoleiHooks` 能力面。该能力面是 team 级工厂——编排在物化时构建一个 unit，
使 planner 侧工具面与 player 侧棋盘变动事件处理器共享同一份进程内存态注册表
（specs/078-saolei-js-hooks/data-model.md §3.1）。插件契约与验收面见
`specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md`。

## 依赖边界

- 只依赖游戏基础上游 `@dominion/dsh-saolei`（`GameState` / `GameLogEntry` 类型
  与棋盘变动事件 seam）与框架面（peer：`@deepseek-ai/cordis` /
  `@deepseek-ai/dsh-tools` / `@deepseek-ai/dsh-system-prompt`）。
- 与 `@dominion/dsh-saolei-tools` **零依赖边**（specs/078-saolei-js-hooks/spec.md
  FR-016）；不依赖 saolei-loop（播报经编排注入的 seam，
  specs/078-saolei-js-hooks/spec.md FR-017）。

## 能力面与单元

```ts
interface SaoleiHooksMount {
  buildHooksUnit(deps: { announce: (text: string) => void }): SaoleiHooksUnit;
}

interface SaoleiHooksUnit {
  readonly tools: readonly ToolDefinition[];              // [saolei_hook]（planner 侧）
  readonly sections: readonly PromptSection[];            // 恒为 []——零 prompt section
  readonly onBoardChange: (entry: GameLogEntry) => void;  // player 侧事件处理器
}
```

- **一个 unit = 一个 team 物化**：注册表、沙箱执行器、工具面与事件处理器共享同一
  闭包状态。`announce` 是编排注入的播报 seam（绑定编排持有的 announcer，惰性
  闭包），插件因此保持纯 provider（specs/078-saolei-js-hooks/contracts/
  saolei-hooks-plugin.md §1/§5）。
- **工具面**：单工具 `saolei_hook`（`action: set | remove | list`），全部行为引导
  经工具 describe 承载——`sections` 恒为空数组，**无任何 prompt section**
  （specs/078-saolei-js-hooks/spec.md FR-006）；describe 的唯一实现清单见
  specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §3.2。
- **事件处理器**：棋盘变动事件触发时按注册序执行全部 hook，每个非空输出逐条经
  `announce` 播报 `[hook <name>] …` 文本（空字符串输出不播报）；运行期错误/超时/
  输出不可序列化以播报形式反馈，失败隔离不上抛。播报文本格式的唯一权威来源是
  specs/078-saolei-js-hooks/contracts/hook-announcement.md §2。

## 挂载方式

- 经 `projects/game/agent_v2/cordis.yml` 的 saolei-loop 行 `config.plugins` 以裸包名
  挂载（无 config）：`- name: '@dominion/dsh-saolei-hooks'`；包声明进
  `projects/game/agent_v2/package.json` 闭包
  （specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §5）。
- **条件 fail-loud**：saolei-loop `apply` 记录 `config.plugins` 是否列出本包行——
  行在而装载后 `ctx.get("saoleiHooks")` 缺失 → 组合装载失败（与 `saoleiTools` /
  `memoryMount` 同型的装载期校验语义）；行缺席 = 可选能力缺席（编排读取面得
  undefined、零 hook 装配、现状行为不变，specs/078-saolei-js-hooks/spec.md
  FR-015）。
- **编排装配**：物化早期（player 成员构造前）经工厂构建 unit；planner setup 注册
  `unit.tools` 与 `unit.sections`（恒空），player setup 以
  `createGameRuntime(sessionName, desktopBridge, { onBoardChange })` 的第三参接入
  事件处理器，播报 seam 绑定编排持有的 announcer
  （specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §5）。

## 资源上限

`src/limits.ts` 的四个具名常量（`specs/078-saolei-js-hooks/data-model.md` §2.3）——
工程界而非接口契约，可调整不需契约变更（依据
`specs/078-saolei-js-hooks/research.md` D4）：

| 常量 | 值 | 语义 |
|---|---|---|
| `MAX_HOOKS` | 8 | 注册数量上限；`set` 超出以普通文本结果拒绝 |
| `MAX_SCRIPT_CHARS` | 16384 | 脚本源码长度上限；`set` 超出以普通文本结果拒绝 |
| `MAX_OUTPUT_CHARS` | 4096 | 单条播报输出上限；超出截断并附 `...[truncated]` 标识 |
| `EXECUTION_TIMEOUT_MS` | 500 | 逐 hook 执行预算；超出被中断并播报超时 |

全部 hook 串行同步执行于 `operate` 调用路径上，总开销上界 =
`MAX_HOOKS × EXECUTION_TIMEOUT_MS` = 4s（已接受上界，
`specs/078-saolei-js-hooks/spec.md` FR-011）。

## 无持久化语义

- 注册表是 team 物化生命周期的**进程内存态**：`buildHooksUnit` 每次构建新的
  `HookRegistry`，随 unit 存续；刷新（UpdateTeam 重建）与 dispose 丢弃 unit 即
  清空——无跨生命周期残留，无任何持久化存储
  （`specs/078-saolei-js-hooks/spec.md` FR-005）。
- 需要跨局保留的脚本由调用方（planner agent）自行保存并在新 team 重新注册；该
  说明经 `saolei_hook` describe 承载，**不与 memory 耦合、不指向任何特定存储
  工具**（`specs/078-saolei-js-hooks/spec.md` FR-006）。
