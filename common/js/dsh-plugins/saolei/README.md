# @dominion/dsh-saolei (`common/js/dsh-plugins/saolei`)

扫雷工具 dsh 插件（cordis 插件名 `saolei`，`inject = ['tools', 'systemPrompt',
'saoleiGame']`）：全局注册 `saolei_init`/`saolei_operate`/`saolei_remain` 三工具与
`saolei:guidance` prompt section，执行经 `saoleiGame` 服务（由 saolei-loop 插件提供）
转发到调用者 agent 的 GameRuntime。插件全契约（工具 schema、参数互斥校验文本、
section 注册）见 `specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §3；
包契约形态对齐 `specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md` §1。

`saolei_remain` 的三处语义表述（工具 description、`saolei:game` 玩法行、结果体
legend 行）以 `specs/064-memory-split-fold-remain/contracts/saolei-plugins.md` 为准：
主语义 = 每个数字格周围剩余未标记雷数（数字 − 相邻已标旗数，可为 0/负），显式排除
旗子数量读法；结果体 legend 行位于 `board size` 行与网格之间，并锚定列 = x、行 = y
（与 `saolei_operate` 参数一致）。`saolei:guidance` 的 remain 条目同语义改写；按 060
所有权边界（`specs/060-agent-v2-team-optimize/contracts/prompt-sections.md` §2）该
条目不含 `over-flagged` 玩法解释，保留 `may be 0 or NEGATIVE` 值域表述。

## 依赖 pin 决策

- dsh 家族 peer（dsh-tools/dsh-system-prompt）按精确版本 `0.1.1-rc.2` pin（无前缀）：
  dsh 家族按 0.1.1-rc.2 线锁定是仓库既定决策
  （`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md`，A8）。
- `@deepseek-ai/cordis` peer `^4.0.1`（插件框架，与 dsh 版本线解耦）。
- `@dominion/dsh-saolei-loop` 经 `workspace:*` 依赖：消费 `saoleiGame` 服务类型
  （`SaoleiGameService`/`GameRuntime`，`contracts/saolei-plugins.md` §2 服务面）。
