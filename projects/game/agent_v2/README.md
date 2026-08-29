# game agent-v2

agent-v2 是 game 域的对话 agent 服务（服务发现名 `agent-v2`，deploy API 命名
约束要求连字符——`specs/049-agent-v2-dsh-init/research.md` D13；项目目录与
bazel target 保留 `agent_v2`）：以 **dsh 进程内嵌入（B1 模式）**宿主
[deepseek-harness](https://github.com/deepseek-ai/deepseek-harness)（版本线
0.1.1-rc.2，与 `third_party/dsh/core` 同线），通过自研 LLM 插件
`@dominion/dsh-llm-glm`（`common/js/dsh-plugins/llm-glm`）接入 GLM codingplan
的 OpenAI **Responses** 协议端点（`https://open.bigmodel.cn/api/v1`，
https://docs.bigmodel.cn/cn/coding-plan/tool/others）。本阶段为零工具的纯对话
agent（组合清单 `cordis.yml` 裁剪全部工具面，spec FR-006），需求与验收锚点见
`specs/049-agent-v2-dsh-init/spec.md` 与 `specs/049-agent-v2-dsh-init/quickstart.md`。

## 服务形态与拓扑

- **`kind: stateful`**：会话注册表、每会话 FIFO 队列与内存对话历史都驻留进程
  内存，因此**必须**经 proxy 的 owner 亲和路由定向到同一实例
  （`specs/049-agent-v2-dsh-init/research.md` D4），不允许 gateway 直连。
- 浏览器请求路径（两跳）：`web 页面 → gateway（/api/v2）→ proxy（owner 亲和，
  Mongo `game_proxy.agent_v2_owners`）→ agent-v2 实例`。gRPC 服务面为
  ConversationService（`projects/game/agent_v2.proto`，proto 与 game.app 根目录
  同级，D12）：`Send`（server-streaming ChatEvent，经 gateway 以 NDJSON 输出）、
  `ListHistory`（刷新回填）、`Dispose`（幂等释放）。接口契约：
  `specs/049-agent-v2-dsh-init/contracts/conversation-api.md`。
- 宿主代码仅保留"嵌入并服务 built-in dsh"的最小集：`src/bootstrap.ts`（OTel →
  env/secret 注入 → boot → 优雅退出）、`src/dsh.ts`（模型端点解析与 token 注入；
  boot/resolver 失败 fail-loud，token 缺失容忍——见下节三级解析）、
  `src/server.ts`（gRPC 面）、`src/session.ts`（get-or-create 注册表
  + 队列 + dispose）、`src/history.ts`（dsh 事件 → ChatEvent/HistoryMessage 映射，
  `contracts/conversation-api.md` §4）。

## 模型端点与凭据配置

启动时的解析顺序（`src/dsh.ts`，`specs/049-agent-v2-dsh-init/research.md` D9）：

1. 模型端点：`GLM_BASE_URL` 直用 > `GLM_LLM_TARGET` 经 Dominion 服务发现解析
   （追加 `/v1` 路径）> 默认 `https://open.bigmodel.cn/api/v1`。
2. API token（`GLM_API_KEY`，三级解析，`specs/049-agent-v2-dsh-init/research.md`
   D9）：环境变量已设直用（trim 非空，空白视为未设置）→ 否则读取
   `$DOMINION_SECRET_DIR/glm-api-token` 文件 → 皆缺失/为空则**保持未设**并记录
   一条 warning（含 env 名与 secret 文件路径，不含任何 key 内容——SC-004）后继续
   boot。token 缺失不阻塞启动：插件对空 key 的模型请求**不携带 Authorization
   header**（`specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md` §3 义务 6）
   ——fake 端点对凭据容忍；真实端点对无 Authorization 请求返回 401，以首轮
   `turn_end{ERROR}` 明确呈现（生产正确性由真实端点冒烟覆盖，SC-003）。

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

`projects/game/testplan/system_test.yaml` 的 `agent-v2-conversation` suite（部署
`projects/game/testplan/deploy_agent_v2.yaml`，fake-llm `/v1/responses` 替换真实
端点、零外部网络）覆盖对话面全部关键行为，用例-场景对照见
`specs/049-agent-v2-dsh-init/quickstart.md` §2。

## 已知限制

记录于 `specs/049-agent-v2-dsh-init/research.md`「已知限制」节：

- **多标签页无实时推送**：未发送消息的标签页看不到其他页面的回合进展，可刷新经
  history 查询（`contracts/conversation-api.md` §5）。
- **desktop 删除同名 session 不联动**：desktop 侧删除不会触发 agent-v2 dispose
  （跨客户端编排留待后续 step）。
- **历史随进程重启丢失**：对话历史为内存态（spec Assumptions 明示接受）；session
  列表因复用 session 服务而持久。
