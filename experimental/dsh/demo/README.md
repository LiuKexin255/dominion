# dsh Chat Demo

grpc-js 服务进程内嵌入 dsh（DeepSeek Harness，B1 模式）的最小 chat 链路实证，feature 定义见
`specs/047-dsh-chat-demo/spec.md`（chat 链路与框架底座）与 `specs/058-dsh-preset-roster-demo/spec.md`
（preset roster 机制验证与 preset 扩展实践）。三个服务组成一条确定性聊天链：调用方经公共 HTTP 入口
显式创建会话（preset 必选——绑定 store preset）并发送消息，gateway 将请求转码为 gRPC 交给 agent，
agent 驱动进程内 dsh 组合的 agent 会话，LLM 适配指向 fake-llm 的脚本化模板，确定性回复沿原路
返回——链路运行时零外部 LLM/网络依赖。

demo 同时是 `third_party/dsh/core` 框架核心底座（零插件）的第一个消费者：agent 镜像即该底座
可用性的活证据（`specs/047-dsh-chat-demo/spec.md` US3：底座第一个消费者）。

## 拓扑

```text
client
  │ POST https://apitest.liukexin.com/experimental/dsh-demo/conversations            （创建会话，可绑定 preset）
  │ POST https://apitest.liukexin.com/experimental/dsh-demo/conversations/{id}:sendMessage
  │ GET/POST/PATCH/DELETE https://apitest.liukexin.com/experimental/dsh-demo/presets  （preset 配置面）
  ▼
gateway — Go grpc-gateway，http :80（唯一公共入口，ingress 配置见 experimental/dsh/demo/testplan/deploy.yaml）
  │ gRPC Chat.* 与 PresetService.*，经 solver.URI("dsh-demo/agent:grpc") 服务发现拨号
  ▼
agent — grpc-js/TS，grpc :50051，进程内嵌入 dsh（boot() 加载直组组合清单，见下节）
  │ HTTP POST /v1/chat/completions；baseURL 运行期解析注入：
  │ createResolver().resolve("dominion:///dsh-demo/fake-llm:8080") → FAKE_LLM_BASE_URL = http://<endpoint>/v1
  ▼
fake-llm — Go，http :8080，OpenAI chat-completions 兼容的确定性模板服务（内部服务，无公共入口）
```

对外契约：`experimental/dsh/demo/chat.proto`（`Chat` 与 `PresetService` 两服务 + `google.api.http`
注解经 gateway 透出），HTTP/gRPC 行为定义见 `specs/047-dsh-chat-demo/contracts/chat-api.md`（消息面）
与 `specs/058-dsh-preset-roster-demo/contracts/chat-api.md`（会话面与 preset 配置面）：

- **Chat**：`SendMessage` 发送单条消息；`CreateConversation` 显式创建会话并绑定 **store preset**
  （preset 必选——缺省即 `INVALID_ARGUMENT`；同 id 同 preset 幂等返回，同 id 异 preset 重建）。
  未创建会话的 SendMessage 以 `FAILED_PRECONDITION` 拒绝，无懒创建。
- **PresetService**：`CreatePreset`/`GetPreset`/`ListPresets`/`UpdatePreset`/`DeletePreset` 五个
  AIP 风格资源 CRUD RPC——创作 preset 指定模板与动态字段（persona 文本、可选展示名），CRUD 只写
  store（唯一事实源）；成员组合在会话创建时从 store 记录派生为纯临时文件并挂载，磁盘上不存在需要
  维护的副本。persona 更新后已加入会话保持旧组合、新会话生效新组合；删除不影响已加入会话；模板为
  部署数据不可删。

## agent 组合清单

组合清单 `experimental/dsh/demo/agent/cordis.yml` 为直组形态，行清单契约见
`specs/058-dsh-preset-roster-demo/contracts/composition-manifest.md` §1，按行来源分三层：

- **基线行**（`timer`/`invariants`/`system-prompt`）：经 `//third_party/dsh/core:runtime_pkg` 随
  agent 镜像 `runtime_deps` 物化，不进运行时 dependencies（package.json devDependencies 仅供
  `composition.test.ts` 真直组 boot）；
- **服务声明行**（`llm`/`session`/`tools`/`agents`/`agent-loop`/`llm-deepseek`）：经 demo agent
  package.json 依赖与 BUILD `npm_deps` 显式声明；
- **preset 扩展行**：`agent-presets`——官方 roster 插件，只扫描模板 system root、`default` 为空 id
  （preset 必选语义）、禁用机器用户目录根（确定性）；`preset-authoring`——自研 authoring 插件，
  承载 store-only preset CRUD 与使用时派生挂载，经 `ctx.presetAuthoring` 服务暴露（service 层零
  roster/文件系统引用）。

**roster roots 与环境变量**（契约：`specs/060-agent-v2-team-optimize/contracts/preset-derivation.md`
§2/§3；由 `experimental/dsh/demo/testplan/deploy.yaml` 注入）：

| env | 值 | 语义 |
|---|---|---|
| `PRESET_TEMPLATES_ROOT` | `/dominion/dsh-demo/agent/presets-templates` | 镜像内模板数据目录（artifact data 随包分发，system 信任根、派生来源） |

roster 不再声明可写 root：用户 preset 的 CRUD 只写内存 store，成员组合在会话创建时从 store 记录派生
为系统临时目录下的纯临时文件（不维护、删除无副作用、重建幂等）。模板 root 为镜像内部署数据，不受
进程/容器生命周期影响。

## 服务与底座 target 一览

| 组件 | bazel target | 端口 | 寻址 |
|---|---|---|---|
| gateway（Go grpc-gateway） | `//experimental/dsh/demo/gateway:cmd_image` | http 80 | 公共入口 `https://apitest.liukexin.com/experimental/dsh-demo`；内部经 `solver.URI("dsh-demo/agent:grpc")` 拨号 agent（`experimental/dsh/demo/gateway/main.go`） |
| agent（grpc-js/TS，嵌入 dsh） | `//experimental/dsh/demo/agent:cmd_image` | grpc 50051 | 被 gateway 服务发现拨号；自身经 `createResolver().resolve("dominion:///dsh-demo/fake-llm:8080")` 解析 fake-llm 并注入 `FAKE_LLM_BASE_URL`（`experimental/dsh/demo/agent/src/dsh.ts`） |
| fake-llm（Go mock LLM） | `//experimental/dsh/demo/fake-llm:cmd_image` | http 8080 | 仅被 agent 内部寻址（无公共 ingress）；接口与模板匹配语义见 `experimental/dsh/demo/fake-llm/README.md` |
| dsh 框架核心底座 | `//third_party/dsh/core:runtime_pkg` | —（构建期） | agent 镜像 `runtime_deps` 引用；仅物化框架核心闭包（11 包、零插件）；demo 侧插件经服务显式声明进入闭包——registry 包走 `npm_deps`、workspace 包走 `runtime_deps`（`experimental/dsh/demo/agent/BUILD.bazel`） |

## 构建 / 单测 / 审计 / 大型测试

与 `specs/058-dsh-preset-roster-demo/quickstart.md` §2/§3 对齐：

```bash
# 构建与单测（Constitution IV：每次代码变更必跑）
bazel build //experimental/dsh/demo/... //common/js/dsh-plugins/... //third_party/dsh/core/...
bazel test  //experimental/dsh/demo/... //common/js/dsh-plugins/... //third_party/dsh/core/...

# 依赖闭包审计（specs/047-dsh-chat-demo/spec.md SC-004 及审计不变量：底座零插件、服务声明可溯源、同名包版本唯一）
bazel test //experimental/dsh/demo/testplan:closure_audit_test

# 大型测试（Constitution VI 验收：校验 → 部署 → 用例 → 清理闭环）
# guitar / deploy 工具未安装时先执行（说明见 tools/test/guitar/README.md）：
bazel run //:guitar_install
bazel run //:deploy_install

guitar run experimental/dsh/demo/testplan/interface_test.yaml
```

大型测试部署三服务与 ingress 后执行三套用例（`experimental/dsh/demo/testplan/interface_test.yaml`）：
`testplan_test`（单轮命中模板逐字一致/重复确定性/兜底/非法请求拒绝）、`multiturn_test`（多轮分支/
会话隔离/并发交错）与 `preset_test`（会话绑定 store preset 的组合差异/无 preset 拒绝/同 preset
行为一致/幂等重建/未创建拒绝，以及 store-only preset 创作-更新-删除闭环与拒绝路径），全部通过即
验收。手动冒烟与排障指引见 `specs/058-dsh-preset-roster-demo/quickstart.md` §4/§6。

## 已知限制

（`specs/047-dsh-chat-demo/spec.md` Assumptions 与 `specs/058-dsh-preset-roster-demo/spec.md` Edge Cases）

- **无上下文压缩**：chat 组合无 compaction 行，长会话上下文单调增长，demo 范围内接受此限制。
- **无会话与创作 preset 持久化**：会话为 agent 进程内内存态，随进程销毁；创作 preset 同为进程态——
  store 记录在内存，成员组合在使用时派生为系统临时文件，进程重启或 Pod 重建后一并丢失，使用前需
  重新创作；模板 preset 为镜像部署数据，不受影响。
- **dsh 0.x-rc 漂移成本**：dsh 全家桶按 0.1.1-rc 线同线精确 pin（dist-tag 不可信），升级以
  lockfile PR 方式整体进行；0.x-rc 的破坏性变更风险由实验性 demo 接受。
- **非流式 v1 / 实验性质量线**：聊天入口为非流式请求/响应，SSE/流式输出不在范围；demo 无
  auth/secrets/生产化运维。

## fake-llm 大型测试豁免

fake-llm 定位为测试基建而非被测交付服务，按 `.specify/memory/constitution.md` 原则 VI 在其 README
声明大型测试豁免：它随 testplan 作为依赖服务部署，端到端行为由 demo 大型测试传递覆盖。豁免声明见
`experimental/dsh/demo/fake-llm/README.md` §Large-test exemption（先例：
`projects/game/fake-llm/README.md`）。
