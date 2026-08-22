# Contract: dsh Agent 服务（组合清单 / 寻址 / 闭包）

**Feature**: [spec.md](spec.md) FR-003/FR-004/FR-005/FR-007/FR-009/FR-011 | **Date**: 2026-08-22

agent 服务（`experimental/dsh/demo/agent`，grpc-js/TS）的内部契约：dsh 嵌入方式、组合清单内容、寻址注入、依赖闭包边界与 fail-loud 语义。决策依据：[research.md](../research.md) D1–D8。

## 1. 进程生命周期（bootstrap 契约）

```
init(otel) → resolver.resolve("dominion:///dsh-demo/fake-llm:8080")
  → process.env.FAKE_LLM_BASE_URL = `http://<endpoints[0]>/v1`
  → boot("dsh-demo-agent", <产物内 cordis.yml 绝对路径>, undefined, undefined, pathToFileURL(__filename).href)
  → await ctx.get('loader')?.await() 由 boot 内完成（fail-loud 审计）
  → import("./server.js") 启动 gRPC server
  → SIGTERM/SIGINT → server 优雅停止 → 逐 agent dispose → ctx.fiber.dispose() → exit 0
```

- `boot()` 签名勘误：第 5 参为 `bareModuleBaseUrl`（非 anchor），传 `pathToFileURL(__filename).href`（`node:url` 的 `pathToFileURL` + CJS 全局 `__filename`——CJS 入口锚点，与 ESM 的 `import.meta.url` 等价，`survey/deepseek-harness-b1-bazel-packaging.md` §2.4）锚定裸包名解析于服务根 node_modules（[research.md](../research.md) D10-1、D8 修订版）。
- 配置路径：产物内静态 `cordis.yml`（`data_files` 携带，路径相对入口推导）；无 `$DSH_CORDIS_CONFIG` 需求（demo 无外部覆写场景）。
- **fail-loud（FR-009）**：boot 任何步骤失败（行解析失败、peer 缺失、激活失败）→ 进程非零退出并携带诊断；**不存在半启动状态**。错误经 bootstrap 的 catch 输出后 `process.exit(1)`。

## 2. 组合清单（cordis.yml，启用面唯一事实源）

两行（[research.md](../research.md) D4；spine 配置 schema 实证：[packages/examples/agent-spine-demo/src/index.ts:92-129](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/examples/agent-spine-demo/src/index.ts)）：

```yaml
- id: agent-spine
  name: '@deepseek-ai/dsh-agent-spine-demo'
  config:
    persona: 'You are a helpful demo chat assistant.'
    workspaceContext: false        # 唯一必填键：显式关闭 runtime-context 注入
    includeRuntimeContext: false
    includeHarnessIdentity: false
    skills: { enabled: false }
    toolBash: false
    toolJobs: false
- id: llm-deepseek
  name: '@deepseek-ai/dsh-llm-deepseek'
  config:
    apiKeyEnv: FAKE_LLM_API_KEY   # dummy 值（deploy env 注入）；免 credentials 插件
    baseURL: !!js process.env.FAKE_LLM_BASE_URL   # 运行期 Dominion 服务发现注入（FR-011）
    models:
      - id: fake-chat-v1
        contextWindow: 100000
```

**不变量**:

- 启用行 ⊆ 物化 node_modules（违反 = 启动失败，fail-loud）；
- 不含 `sdk-jsonrpc-server` 行（stdio 服务面，与宿主进程主权冲突）与任何 persistence 行（会话内存态，spec Assumptions）；
- 除 LLM 适配行外全部为官方插件（FR-005；本 demo 实际零自研插件，[research.md](../research.md) D1）。

## 3. 对话驱动契约（gRPC handler 内部）

| 环节 | 契约 |
|---|---|
| 会话映射 | 请求 `name`（`conversations/{id}`，[chat-api.md](chat-api.md)）提取 id → dsh `SessionId` 直映射（宿主自选 ID，上游显式支持） |
| get-or-create | `ctx.agents.get(id)` 命中复用；未命中 `create({sessionId, agentOptions: {provider: 'deepseek-official', model: 'fake-chat-v1'}})`；同 id 并发首建经单一创建 promise 去重（官方 server 模式） |
| 发消息 | `agent.followup(createUserMessage({content: [{type:'text', text}], source: {kind:'user'}}))`（fire-and-forget） |
| 回复判定 | 订阅 `session/event`（按 sessionId 路由）收集本轮事件；以 `agent/status → idle`（或 `await agent.whenIdle()`，二选一，实现自定）为回合终止；reply = 末条 `assistant/message` 的 text blocks 拼接（无则空串） |
| agentOptions 路由 | `deepseek-official` 为官方适配器注册的路由名；model `fake-chat-v1` 与 cordis.yml `models[]` 目录对齐 |

事件生命周期依据：[docs/agent-lifecycle.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/agent-lifecycle.md)（`followup → running → turn/start → assistant/chunk* → assistant/message → turn/end → idle`）。

## 4. 依赖闭包契约（FR-004 / US3 / SC-004）

**分层**:

| 层 | 内容 | 归属 |
|---|---|---|
| 框架核心底座 | ≈11 包（app-boot + cordis 家族 loader/include/group/timer + node-addon-require-builtin + home-paths/invariants/system-prompt/launch-environment），精确 pin 0.1.1-rc.2 线 | `third_party/dsh/core:runtime_pkg`（**零插件**，catalog 例外） |
| 服务直接声明 | 服务代码直接 import 的包：`dsh-app-boot`（boot）、`dsh-llm`（createUserMessage）、`dsh-agent`（类型）、`dsh-agent-spine-demo` + `dsh-llm-deepseek`（启用行物理在场）+ grpc-js 公共包 | agent 服务 package.json / BUILD `npm_deps` |
| 传递闭包 | 上述声明的 peer 闭包（spine 26 peers + adapter 12 peers 等） | 经 link target files 物化（`survey/deepseek-harness-b1-bazel-packaging.md` §3.3） |

**审计断言（SC-004，`closure_audit_test` 承载）**:

1. 底座 target 物化集合内无任何 `@deepseek-ai/dsh-*` 插件包（插件 = spine 依赖图中除核心外的 dsh 包；以 BUILD `npm_deps` 枚举静态审计）；
2. 产物 tar 的 node_modules 中每个 `@deepseek-ai/*` 包 ∈ {核心清单 ∪ 服务声明的 peer 闭包到不动点}——即无第三来源；
3. 物化集合内同名包版本唯一（`name@version` 查重）。

## 5. 打包与运行形态

- `artifact_pkg_js`：`ts_project = :server_lib`、`entrypoint = src/bootstrap.js`、`runtime_protos = ["//experimental/dsh/demo:chat_proto"]`、`runtime_deps = [third_party/dsh/core:runtime_pkg, common/js/* runtime_pkgs]`、`npm_deps = [服务直接声明的 link targets]`、`data_files = [cordis.yml]`。
- **模块格式（CJS）**：服务入口以 CJS 编译（仓库 TS 包统一格式），服务根无需任何 package.json data 文件——tar 内无服务根 package.json 时 `.js` 默认按 CJS 解析即所需；dsh 上游 ESM-only 包经 require(esm) 消费，Node 22.12+ 默认启用（[research.md](../research.md) D8 修订版）。
- 镜像：`artifact_image`（distroless_nodejs24-debian12，`/nodejs/bin/node /dominion/dsh-demo/agent/src/bootstrap.js`）——native addon（`node-addon-require-builtin` linux-x64-gnu）在该 base 内实测加载为验收一部分（survey §7 风险 2）。
- `service.yaml`：`app: dsh-demo`、`name: agent`、`kind: stateless`、port `grpc: 50051`、artifact tls。
