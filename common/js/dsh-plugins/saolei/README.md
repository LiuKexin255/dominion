# @dominion/dsh-saolei (`common/js/dsh-plugins/saolei`)

扫雷工具 cordis 插件（cordis 插件名 `saolei`，`inject = []`）：作为 saolei-loop
组合的静态能力单元提供方挂载（`config.plugins` 子插件），`apply` 内
`ctx.provide("saoleiTools", { tools, sections })` 暴露
`saolei_init`/`saolei_operate`/`saolei_remain` 三工具定义与
`saolei:guidance`/`saolei:game` 两个 prompt section，由 saolei-loop 编排在
player 成员 setup 内整单元注册到成员上下文（不在组合作用域全局注册——那会把
工具与 section 泄漏给全员）。工具执行经 `saoleiGame` 服务（由 saolei-loop 插件
提供）转发到调用者 agent 的 GameRuntime。插件全契约（工具 schema、参数互斥校验
文本、section 名/order/文本）见
`specs/074-dsh-grpc-runtime/contracts/team-assembly.md` §3.1/§6；工具面契约见
`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §3；
包契约形态对齐 `specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md` §1。

`saolei_remain` 的三处语义表述（工具 description、`saolei:game` 玩法行、结果体
legend 行）以 `specs/064-memory-split-fold-remain/contracts/saolei-plugins.md` 为准：
主语义 = 每个数字格周围剩余未标记雷数（数字 − 相邻已标旗数，可为 0/负），显式排除
旗子数量读法；结果体 legend 行位于 `board size` 行与网格之间，并锚定列 = x、行 = y
（与 `saolei_operate` 参数一致）。`saolei:guidance` 的 remain 条目同语义改写；按 060
所有权边界（`specs/060-agent-v2-team-optimize/contracts/prompt-sections.md` §2）该
条目不含 `over-flagged` 玩法解释，保留 `may be 0 or NEGATIVE` 值域表述。

## 依赖 pin 决策

- dsh 家族 peer（dsh-tools/dsh-system-prompt）经 catalog 解析到精确版本
  `0.2.0-rc.2`：dsh 家族按 0.2.0-rc.2 线锁定是仓库既定决策
  （`third_party/dsh/core` 同线），依据 `specs/073-dsh-0.2-upgrade/research.md` D1。
- `@deepseek-ai/cordis` peer 经 catalog 解析到 `~4.0.4`（线内互锁：解析必须
  落线内值，否则可能物化两份 cordis；依据 `specs/073-dsh-0.2-upgrade/research.md` D1）。
- `@dominion/dsh-saolei-loop` 经 `workspace:*` 依赖：消费 `saoleiGame` 服务类型
  （`SaoleiGameService`/`GameRuntime`，`contracts/saolei-plugins.md` §2 服务面）。
