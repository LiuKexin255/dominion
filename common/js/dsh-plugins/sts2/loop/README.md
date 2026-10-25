# @dominion/dsh-sts2-loop (`common/js/dsh-plugins/sts2/loop`)

sts2 场景的**最小编排**包（cordis 插件名 `sts2-loop`）：官方 agent-loop 直驱
单主 agent，作为主子形态的 host 侧验收载体与后续 sts2-loop 能力的落地板。

## 包定位与边界

- 本包是 sts2 场景的编排落位（`common/js/dsh-plugins/sts2/loop/`），与
  `saolei/loop/` 同层级；目录与包名映射见
  `specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §1/§2。
- 公开面、物化/驱动/事件/快照行为与测试义务由
  `specs/080-host-main-sub-model/contracts/sts2-loop-minimal.md` 定义。
- 编排实例由 host 经场景注册按会话构造（非组合单例），与 saolei-loop 的
  所有权模型一致（同上契约 §1）。
- 不承载 team 注册、announcer、memory 挂载、hooks、游戏运行时、desktop flow
  与 preset 池模板内容；这些边界见
  `specs/080-host-main-sub-model/contracts/sts2-loop-minimal.md` §3 与
  `specs/080-host-main-sub-model/spec.md` FR-010。
- 本包随 agent_v2 部署闭包交付：workspace 依赖 + `:runtime_pkg` 进
  `server_pkg.runtime_deps`，不进 npm registry 依赖通道
  （`specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §4）。

## 演进入口

sts2 场景的完整能力（房间子代理派发、主/子能力与 section 分配、房间类型
preset 池模板）由后续 sts2-loop feature 在本包内填充，调研结论见
`survey/sts2-agent-model.md` §5。
