# @dominion/dsh-saolei-loop (`common/js/dsh-plugins/saolei-loop`)

扫雷 agent 驱动 dsh 插件（cordis 插件名 `saolei-loop`）：以自研 Agent 工厂/驱动器替换
官方 agent-loop（"抄设计"而非继承代码），并提供 host 级 `saoleiGame` 服务面
（GameRuntime 的 init/operate/remain，v1 游戏契约语义迁移）。插件全契约（工厂与驱动、
GameRuntime 行为、依赖 pin）见
`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §2；包契约形态对齐
`specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md` §1。

## 依赖 pin 决策

- dsh 家族 peer（dsh-agent/dsh-session/dsh-llm/dsh-system-prompt/dsh-tools/dsh-scope）
  按精确版本 `0.1.1-rc.2` pin（无前缀）：dsh 家族按 0.1.1-rc.2 线锁定是仓库既定决策
  （`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md`，A8）。peer 划分对齐
  官方 `@deepseek-ai/dsh-agent-loop`（同型工厂插件全 peer 形态）。
- `@deepseek-ai/cordis` peer `^4.0.1`（插件框架，与 dsh 版本线解耦）。
- `@dominion/game-saolei-board` 经 `workspace:*` 依赖：棋盘识别
  （`SaoleiBoard.init/updateFromScreenshot`）与坐标几何复用（FR-015）。
