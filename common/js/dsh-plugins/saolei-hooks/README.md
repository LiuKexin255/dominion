# @dominion/dsh-saolei-hooks (`common/js/dsh-plugins/saolei-hooks`)

扫雷 **hook 引擎** cordis 插件（cordis 插件名 `saolei-hooks`，`inject = []`）：
由 saolei-loop 组合的 `config.plugins` 以裸包名挂载，`ctx.provide` 暴露
`saoleiHooks` 能力面。该能力面是 team 级工厂——编排在物化时构建一个 unit，
使 planner 侧工具面与 player 侧棋盘变动事件处理器共享同一份进程内存态注册表
（specs/078-saolei-js-hooks/data-model.md §3.1）。

## 依赖边界

- 只依赖游戏基础上游 `@dominion/dsh-saolei`（`GameState` / `GameLogEntry` 类型
  与棋盘变动事件 seam）与框架面（peer：`@deepseek-ai/cordis` /
  `@deepseek-ai/dsh-tools` / `@deepseek-ai/dsh-system-prompt`）。
- 与 `@dominion/dsh-saolei-tools` **零依赖边**（specs/078-saolei-js-hooks/spec.md
  FR-016）；不依赖 saolei-loop（播报经编排注入的 seam，
  specs/078-saolei-js-hooks/spec.md FR-017）。

<!-- 待补全（`specs/078-saolei-js-hooks/tasks.md` T017）：挂载方式、资源上限、
     无持久化语义、`saolei_hook` 工具 describe 说明面 -->
