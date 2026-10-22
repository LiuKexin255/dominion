# @dominion/dsh-sts2 (`common/js/dsh-plugins/sts2/game`)

杀戮尖塔2（sts2）**场景骨架** cordis 插件（cordis 插件名 `sts2`，
`inject = []`）：组合清单的惰性占位行
（`- { id: sts2, name: '@dominion/dsh-sts2' }`）。当前实现为空 `apply`——不
provide 服务、不注册工具或 prompt section、不持有任何运行时状态。

## 包定位与边界

- 本包是 sts2 场景的**游戏基础**落位（`common/js/dsh-plugins/sts2/game/`），
  与 saolei 的 game foundation 同构；目录与包名映射见
  `specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §2。
- 惰性行的裁定与插件形态（name/inject/空 apply、组合清单行）见
  `specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §3 与
  `specs/079-sts2-template-scaffold/research.md` D3/D6。
- 本包随 agent_v2 部署闭包交付：workspace 依赖 + `:runtime_pkg` 进
  `server_pkg.runtime_deps`，不进 npm registry 依赖通道
  （`specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §4）。
- 测试骨架断言插件形态（name/inject 字段值与 apply 可调用），防形态漂移
  （`specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §5）。

## 演进入口

sts2 场景的正式接入由后续场景 feature 承载，调研结论见：

- `survey/sts2-game-control.md` §5——desktop 桥新增游戏 API 中继 kind、
  `@dominion/dsh-sts2(-tools/-loop)` 能力行、fake-sts2 替身。
- `survey/sts2-mod-poc.md` §5——raw API 经工具面封装（不直开给 agent）、
  `card_in_play` wait 原语、fake-sts2 设计输入与状态读取格式。

填充实现时按能力角色在本分组内扩展子包（`tools/`、`loop/` 等）：本包保持
游戏基础边界，不承载模型工具或编排逻辑
（`specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §1/§2）。
