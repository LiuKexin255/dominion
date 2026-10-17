# @dominion/dsh-saolei-tools (`common/js/dsh-plugins/saolei-tools`)

扫雷**工具** cordis 插件（cordis 插件名 `saolei-tools`，`inject = []`）：saolei-loop
组合的 `saoleiTools` 静态能力单元提供方，作为 saolei-loop 行的 `config.plugins`
子插件挂载（裸包名动态 import，`specs/074-dsh-grpc-runtime/contracts/
team-assembly.md` §2）。`apply` 内 `ctx.provide("saoleiTools", { tools, sections })`
暴露单元内容，由 saolei-loop 编排在 player 成员 setup 内整单元注册到成员上下文
（不在组合作用域全局注册——那会把工具与 section 泄漏给全员）。单元只含工具与其
配套守则：玩法 section `saolei:game` 归游戏基础 `@dominion/dsh-saolei`，由编排对
player 与 planner 注册。插件契约见
`specs/075-saolei-game-foundation/contracts/saolei-tools-plugin.md`。

## saoleiTools 静态单元

```ts
interface SaoleiToolsUnit {
  readonly tools: readonly ToolDefinition[];    // [saolei_init, saolei_operate, saolei_remain]
  readonly sections: readonly PromptSection[];  // [{ name: "saolei:guidance", order: 100, text: SAOLEI_GUIDANCE }]
}
```

- **三工具**：`saolei_init`（无参，开局/重开）、`saolei_operate`（双形式：single
  `{type,x,y}` XOR batch `{operations:[...]}`）、`saolei_remain`（无参只读）——
  定义/schema/描述文案为冻结契约值，工具面契约见
  `specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §3。
- **校验资产**：`AMBIGUOUS_ARGS_TEXT`/`MISSING_ARGS_TEXT`/`INCOMPLETE_ARGS_TEXT`
  （v1 拒绝字面量，`specs/064-memory-split-fold-remain/contracts/saolei-plugins.md`
  §2 为冻结权威）、`normalizeOperateArgs`（双形式互斥校验）、`RESULT_OUTPUT_SCHEMA`
  与 `resultOutput()`（`{result: string}` canonical 输出）。
- **`SAOLEI_GUIDANCE`**：`saolei:guidance`（order 100）工具守则 section 文本，内容
  边界 = 仅工具用法（符号表/坐标/结果三层结构/校验拒绝语义/示例/纪律），不含玩法
  解释（`specs/060-agent-v2-team-optimize/contracts/prompt-sections.md` §2）。
- **执行语义**：`resolveRuntime` 经调用者 agent ctx 的 `saoleiGame` 句柄解析（编排
  在 player setup 注册的运行时实例；`exec.agent` 缺失或 scope 无服务 = fail-loud，
  不静默成功）→ `executeOutcome`（isError → throw 为模型可见失败；
  `concludesTurn === true` → 先 `exec.concludeTurn()` 再返回，
  `specs/062-team-game-end-handoff/contracts/saolei-turn-conclude.md` §2）。

工具执行经 `saoleiGame` 句柄转发到 `GameRuntime`，运行时 API 与游戏状态归
`@dominion/dsh-saolei`；本包对 saolei-loop 编排零知识（运行时经 agent scope 解析，
不经编排面）。

## 依赖 pin 决策

- dsh 家族 peer（dsh-tools/dsh-system-prompt）经 catalog 解析到精确版本
  `0.2.0-rc.2`：dsh 家族按 0.2.0-rc.2 线锁定是仓库既定决策
  （`third_party/dsh/core` 同线），依据 `specs/073-dsh-0.2-upgrade/research.md` D1。
- `@deepseek-ai/cordis` peer 经 catalog 解析到 `~4.0.4`（线内互锁：解析必须落线内值，
  否则可能物化两份 cordis；依据 `specs/073-dsh-0.2-upgrade/research.md` D1）。
- `@dominion/dsh-saolei` 经 `workspace:*` 依赖：消费游戏类型与运行时类型面
  （`OperateInput`/`SaoleiGame`/`ToolOutcome`，
  `specs/075-saolei-game-foundation/contracts/saolei-tools-plugin.md` §4）。
- `@deepseek-ai/dsh-tools` peer：工具定义与执行上下文的 `defineTool`/`ToolRunContext`
  类型面（运行期由 dsh 提供）。
- 不依赖 `@dominion/dsh-saolei-loop`（工具对编排零知识，
  `specs/075-saolei-game-foundation/contracts/saolei-tools-plugin.md` §4）。
