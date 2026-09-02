# game agent-v2

agent-v2 是 game 域的 agent 服务（服务发现名 `agent-v2`，deploy API 命名
约束要求连字符——`specs/049-agent-v2-dsh-init/research.md` D13；项目目录与
bazel target 保留 `agent_v2`）：以 **dsh 进程内嵌入（B1 模式）**宿主
[deepseek-harness](https://github.com/deepseek-ai/deepseek-harness)（版本线
0.1.1-rc.2，与 `third_party/dsh/core` 同线），通过自研 LLM 插件
`@dominion/dsh-llm-glm`（`common/js/dsh-plugins/llm-glm`）接入 GLM codingplan
的 OpenAI **Responses** 协议端点（`https://open.bigmodel.cn/api/v1`，
https://docs.bigmodel.cn/cn/coding-plan/tool/others）。承载 agent 对话与游戏
驱动（saolei-loop + 桌面桥接 + saolei 工具插件）、preset 配置面与桌面
flow 控制流。需求与验收锚点见
`specs/051-agent-v2-dsh-migration/spec.md` 与
`specs/049-agent-v2-dsh-init/spec.md`。

## 服务形态与拓扑

- **`kind: stateful`**：会话注册表、每会话 FIFO 队列、内存对话历史与游戏
  状态都驻留进程内存，因此会话面**必须**经 proxy 的 owner 亲和路由定向到
  同一实例（`specs/049-agent-v2-dsh-init/research.md` D4）。
- gRPC 监听 `0.0.0.0:50051`，三个服务（`projects/game/agent_v2.proto`，接口
  契约 `specs/051-agent-v2-dsh-migration/contracts/agent-api.md`）：
  - **AgentService**（agent 是 session 的单例资源，AIP-156，
    `templates/{template}/sessions/{session}/agent`）：`Send`（server-streaming
    ChatEvent，经 gateway 以 NDJSON 输出）、`UpdateAgent`（显式物化，见下节）、
    `GetAgent`、`ListAgentMessages`（历史回填）。
  - **PresetService**（无状态配置面）：preset 标准 CRUD + `ListModels`。
  - **DesktopBridgeService**：desktop 的 flow 控制流 WebSocket 入口
    （`/api/v2/templates/{template}/sessions/{session}/connect`）。
- gateway 的 `/api/v2` 路由按 RPC 状态归属拆分
  （`specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md` §3）：
  会话面（AgentService + DesktopBridgeService）走
  `gateway → proxy（owner 亲和，Mongo `game_proxy.agent_v2_owners`）→ agent-v2
  实例` 两跳；配置面（PresetService：状态在 Mongo、模型目录静态）由 gateway
  直连任一实例。
- 进程内组件生命周期（`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md`
  §6）：preset Mongo 存储最先启动、最后停止；dsh 组合居中；gRPC server 先排空，
  再逐会话 dispose（在途回合 abort）；38080 `/healthz` 由 Bootstrap 内建，仅在
  全部组件启动完成后开始服务（`specs/052-deploy-health-probe/contracts/deploy-probe.md`）。

## 组合清单（cordis.yml）

`cordis.yml` 是启用插件集的唯一事实源（`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md`
§5）——**直组 dsh 核心件，无 spine、无官方 agent-loop**（spine 硬挂官方
AgentLoop 且 agent 注册表禁止二次 factory，FR-012）：

- dsh 核心件：`llm`/`session`/`system-prompt`/`tools`/`agents`/`invariants`/
  `llm-retry`（`system-prompt` 关闭 harness 身份与运行时上下文注入——部署自有
  完整 system prompt）；`timer`/`invariants`/`system-prompt` 经框架基线
  `@dominion/dsh-core` 进入。
- 一致性防护伴生：`dsh-session`/`dsh-agent`/`dsh-scope` 三个包的 `/invariant`
  subpath 插件（自研 loop 的高风险面防护）。
- `llm-glm`（`@dominion/dsh-llm-glm`）：GLM Responses 端点接入；`models[]`
  是部署模型目录的唯一来源（`ListModels` 与物化校验同源）。
- Dominion 游戏三件：`desktop-bridge`（桌面 flow 控制流）、`saolei-loop`
  （player 单角色 agent 工厂 + 每 session 游戏 loop）、`saolei`（init/operate/
  remain 工具面 + 配套 prompt section，`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md`）。

## agent 物化（UpdateAgent）

agent 不经 Send 懒创建，必须经 `UpdateAgent`（AIP-134 create-or-update，
`allow_missing=true`）**显式物化**：

- **preset 引用必填**（不预置默认 preset 资源）；模型可选，缺省用进程默认
  （`cordis.yml` `models[]` 首项）；模型 id 与 `ListModels` 目录同源校验，
  未知 id fail-fast 拒绝，不产生半物化状态。
- agent 已存在时 Update 即**刷新**：重读所引用 preset 的当前内容并清空短期
  记忆（对话历史、排队消息、游戏状态；在途回合终止），无论配置是否变化；
  重复 Update 结果幂等。
- preset 提示词为空时 persona 回退模板默认 base。
- 未物化 session 的 `Send` 以明确错误拒绝（提示先物化）。

## preset 持久化与环境变量 MONGO_URI

preset 数据持久化在 Mongo `game_agent_v2.presets`，agent-v2 重启不丢
（FR-005；agent 本体——历史/物化配置/游戏状态——为内存态，重启后需重新
物化）。Mongo 连接解析（`src/presets.ts`）：

1. 环境变量 `MONGO_URI` 已设则直连使用（本地/测试直连形态）。
2. 否则经 Dominion 服务发现解析 `dominion:///game/mongo:27017`，并按部署
   环境派生 admin 凭据拼出 credentialed URI——派生算法与 Go 服务
   （`dominion/common/gopkg/mongo/credentials.go`）字节一致，同一实例同一
   认证（`DOMINION_ENVIRONMENT` 参与派生，缺省 `default`）。

## 模型端点与凭据配置

启动时的解析顺序（`src/dsh.ts`，`specs/049-agent-v2-dsh-init/research.md` D9）：

1. 模型端点：`GLM_BASE_URL` 直用 > `GLM_LLM_TARGET` 经 Dominion 服务发现解析
   （追加 `/v1` 路径）> 默认 `https://open.bigmodel.cn/api/v1`。
2. API token（`GLM_API_KEY`，三级解析，`specs/049-agent-v2-dsh-init/research.md`
   D9）：环境变量已设直用（trim 非空，空白视为未设置）→ 否则读取
   `$DOMINION_SECRET_DIR/glm-api-token` 文件 → 皆缺失/为空则**保持未设**并记录
   一条 warning（含 env 名与 secret 文件路径，不含任何 key 内容——049 SC-004）
   后继续 boot。token 缺失不阻塞启动：插件对空 key 的模型请求**不携带
   Authorization header**（`specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md`
   §3 义务 6）——fake 端点对凭据容忍；真实端点对无 Authorization 请求返回
   401，以首轮 `turn_end{ERROR}` 明确呈现。

### 运维预置（k8s secret，仅生产需要）

生产部署前，运维需在集群中预置 secret：**`llm-secrets` 增加 key
`glm-codingplan`**（GLM codingplan API Key，在
https://docs.bigmodel.cn/cn/coding-plan/quick-start 套餐页新建）。`service.yaml`
的生产 artifact `agent-v2` 声明逻辑 secret `glm-api-token`，
`projects/game/deploy.yaml` 将其绑定到 `llm-secrets/glm-codingplan`（deploy 工具
的 secret 双向校验仅对所选 artifact 生效，`tools/release/deploy/README.md`
§服务类型）；运行期经 projected volume 挂载到
`$DOMINION_SECRET_DIR/glm-api-token`
（`specs/002-deploy-secret-config/contracts/secret-config.md`）。

测试 artifact **`agent-v2-test`**（同 target/tls、无 secrets 声明）承载零 secret
测试部署（`projects/game/testplan/deploy_agent_v2.yaml` 选用），测试集群无需为
agent-v2 预置任何 secret。

## 大型测试

`projects/game/testplan/system_test.yaml` 的 agent-v2 面套件
（agent-v2-conversation / agent-v2-preset / agent-v2-game / desktop-flow，部署
`projects/game/testplan/deploy_agent_v2.yaml`；agent-v2-game-disconnect 部署
`projects/game/testplan/deploy_agent_v2_drop.yaml`——同拓扑、fake-desktop 以
progressive 场景 + 断连故障 env 运行，fake-llm `/v1/responses` 替换
真实端点、fake-desktop 替换真实桌面、零外部网络）覆盖对话面、preset
CRUD/持久化/物化、游戏闭环与断连分支、desktop flow 面；套件-场景对照见
`specs/051-agent-v2-dsh-migration/quickstart.md` §2。

## 已知限制

- **多标签页无实时推送**：未发送消息的标签页看不到其他页面的回合进展，可刷新经
  `ListAgentMessages` 查询（`specs/051-agent-v2-dsh-migration/contracts/agent-api.md`）。
- **agent 为进程内存态**：对话历史、游戏状态与物化配置（preset/model 绑定）
  随重启丢失；重启后再次对话前必须重新经 UpdateAgent 物化（spec Edge Cases
  与 Assumptions A2 明示接受）。仅 preset 资源数据持久化（见上节）。
- **session 删除不联动 agent 清理**：删除 session 仅移除元数据，进程内残留
  agent（含历史与 flow 连接）不受影响；以相同资源名重建 session 会命中残留
  agent（已接受限制，spec Edge Cases；`Dispose` 已随 FR-007 移除）。
