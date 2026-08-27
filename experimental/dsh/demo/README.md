# dsh Chat Demo

grpc-js 服务进程内嵌入 dsh（DeepSeek Harness，B1 模式）的最小 chat 链路实证，feature 定义见
`specs/047-dsh-chat-demo/spec.md`。三个服务组成一条确定性聊天链：调用方经公共 HTTP 入口发送消息，
gateway 将请求转码为 gRPC 交给 agent，agent 驱动进程内 dsh 组合的 agent 会话，LLM 适配指向
fake-llm 的脚本化模板，确定性回复沿原路返回——链路运行时零外部 LLM/网络依赖。

demo 同时是 `third_party/dsh/core` 框架核心底座（零插件）的第一个消费者：agent 镜像即该底座
可用性的活证据（`specs/047-dsh-chat-demo/spec.md` US3：底座第一个消费者）。

## 拓扑

```text
client
  │ POST https://apitest.liukexin.com/experimental/dsh-demo/conversations/{id}:sendMessage
  ▼
gateway — Go grpc-gateway，http :80（唯一公共入口，ingress 配置见 experimental/dsh/demo/testplan/deploy.yaml）
  │ gRPC Chat.SendMessage，经 solver.URI("dsh-demo/agent:grpc") 服务发现拨号
  ▼
agent — grpc-js/TS，grpc :50051，进程内嵌入 dsh（boot() 组合 agent-spine + llm-deepseek 两行清单）
  │ HTTP POST /v1/chat/completions；baseURL 运行期解析注入：
  │ createResolver().resolve("dominion:///dsh-demo/fake-llm:8080") → FAKE_LLM_BASE_URL = http://<endpoint>/v1
  ▼
fake-llm — Go，http :8080，OpenAI chat-completions 兼容的确定性模板服务（内部服务，无公共入口）
```

对外契约：`experimental/dsh/demo/chat.proto`（`Chat` 服务 + `google.api.http` 注解），HTTP/gRPC
行为定义见 `specs/047-dsh-chat-demo/contracts/chat-api.md`。

## 服务与底座 target 一览

| 组件 | bazel target | 端口 | 寻址 |
|---|---|---|---|
| gateway（Go grpc-gateway） | `//experimental/dsh/demo/gateway:cmd_image` | http 80 | 公共入口 `https://apitest.liukexin.com/experimental/dsh-demo`；内部经 `solver.URI("dsh-demo/agent:grpc")` 拨号 agent（`experimental/dsh/demo/gateway/main.go`） |
| agent（grpc-js/TS，嵌入 dsh） | `//experimental/dsh/demo/agent:cmd_image` | grpc 50051 | 被 gateway 服务发现拨号；自身经 `createResolver().resolve("dominion:///dsh-demo/fake-llm:8080")` 解析 fake-llm 并注入 `FAKE_LLM_BASE_URL`（`experimental/dsh/demo/agent/src/dsh.ts`） |
| fake-llm（Go mock LLM） | `//experimental/dsh/demo/fake-llm:cmd_image` | http 8080 | 仅被 agent 内部寻址（无公共 ingress）；接口与模板匹配语义见 `experimental/dsh/demo/fake-llm/README.md` |
| dsh 框架核心底座 | `//third_party/dsh/core:runtime_pkg` | —（构建期） | agent 镜像 `runtime_deps` 引用；仅物化框架核心闭包（11 包、零插件），插件一律由服务侧 `npm_deps` 显式声明（`experimental/dsh/demo/agent/BUILD.bazel`） |

## 构建 / 单测 / 审计 / 大型测试

与 `specs/047-dsh-chat-demo/quickstart.md` 对齐：

```bash
# 构建与单测（Constitution IV：每次代码变更必跑）
bazel build //experimental/dsh/demo/... //third_party/dsh/core/...
bazel test  //experimental/dsh/demo/... //third_party/dsh/core/...

# 依赖闭包审计（SC-004：底座零插件、服务闭包可溯源、同名包版本唯一）
bazel test //experimental/dsh/demo/testplan:closure_audit_test

# 大型测试（Constitution VI 验收：校验 → 部署 → 用例 → 清理闭环）
# guitar / deploy 工具未安装时先执行（说明见 tools/test/guitar/README.md）：
bazel run //:guitar_install
bazel run //:deploy_install

guitar run experimental/dsh/demo/testplan/interface_test.yaml
```

大型测试部署三服务与 ingress 后执行两套用例（`experimental/dsh/demo/testplan/interface_test.yaml`）：
`testplan_test`（单轮命中模板逐字一致/重复确定性/兜底/非法请求拒绝）与 `multiturn_test`
（多轮分支/会话隔离/并发交错），全部通过即验收。手动冒烟与排障指引见
`specs/047-dsh-chat-demo/quickstart.md` §4/§5。

## 已知限制

（`specs/047-dsh-chat-demo/spec.md` Assumptions）

- **无上下文压缩**：纯 chat 组合（agent-spine + llm-deepseek）无 compaction 能力，长会话上下文
  单调增长，demo 范围内接受此限制。
- **无会话持久化**：会话为 agent 进程内内存态，随进程销毁。
- **dsh 0.x-rc 漂移成本**：dsh 全家桶按 0.1.1-rc 线同线精确 pin（dist-tag 不可信），升级以
  lockfile PR 方式整体进行；0.x-rc 的破坏性变更风险由实验性 demo 接受。
- **非流式 v1 / 实验性质量线**：聊天入口为非流式请求/响应，SSE/流式输出不在范围；demo 无
  auth/secrets/生产化运维。

## fake-llm 大型测试豁免

fake-llm 定位为测试基建而非被测交付服务，按 `.specify/memory/constitution.md` 原则 VI 在其 README
声明大型测试豁免：它随 testplan 作为依赖服务部署，端到端行为由 demo 大型测试传递覆盖。豁免声明见
`experimental/dsh/demo/fake-llm/README.md` §Large-test exemption（先例：
`projects/game/fake-llm/README.md`）。
