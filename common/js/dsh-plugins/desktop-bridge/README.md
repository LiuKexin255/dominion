# @dominion/dsh-desktop-bridge (`common/js/dsh-plugins/desktop-bridge`)

desktop 操作桥接 dsh 插件（cordis 插件名 `desktop-bridge`）：将 desktop 连接绑定到
agent session，并向下发放操作、回收截图与操作回执。服务接口与行为契约见
`specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md` §2；包形态（无 inject、
无运行时 deps）见
`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §1。

## 依赖形态

无 `dependencies`/`peerDependencies`：桥接的 gRPC handler 类型不消费 dsh 服务面
（grpc handler 类型经 agent_v2 生成类型对齐或本地最小接口声明，
`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §1）。包契约形态对齐
`specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md` §1（ESM、exports、
tsconfig/.swcrc 锁步）。
