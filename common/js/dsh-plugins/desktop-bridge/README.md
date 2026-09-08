# @dominion/dsh-desktop-bridge (`common/js/dsh-plugins/desktop-bridge`)

desktop 操作桥接 dsh 插件（cordis 插件名 `desktop-bridge`）：将 desktop 连接绑定到
agent session，并向下发放操作、回收截图与操作回执。服务接口与行为契约见
`specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md` §2；包形态（无 inject、
无运行时 deps）见
`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §1。

## 依赖形态

- `@dominion/common-js-logs`（`workspace:*`）：连接接管/断连/超时/stale 回执的结构化
  日志（v1 `projects/game/agent/src/operation-bridge.ts` 同型日志面）。
- `@deepseek-ai/cordis`（peer `^4.0.1`）：类形式 Service 插件的框架基类。
- 桥接的 gRPC handler 类型不消费 dsh 服务面与 agent_v2 生成类型：`src/wire.ts`
  以本地最小接口声明镜像 proto-loader 产物形状（camelCase、enums 为字符串联合、
  oneof 为 `kind` 判别联合，`specs/051-agent-v2-dsh-migration/contracts/
  saolei-plugins.md` §1"本地最小接口声明"）。包契约形态对齐
  `specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md` §1（ESM、exports、
  tsconfig/.swcrc 锁步）。
