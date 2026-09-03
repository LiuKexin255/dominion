# Revision: proxy Cancel 转发缺口补充设计（phase2-proxy-cancel）

**Feature**: [spec.md](../spec.md) | **日期**: 2026-09-03 | **性质**: 执行期缺口补充设计（Phase 2 开工前代码核实的发现；本文为设计产物，不含代码变更）

**状态**: 本文是 proxy 侧 `Cancel` 转发缺口的落地方案权威描述。tasks.md 修订文本见 §6（由执行者应用，本文不直接修改 tasks.md）；契约补行见 §5（随 T013b 落地）。与既有设计文档冲突的表述（[plan.md](../plan.md) 源码树括注"gateway/proxy：/api/v2 透传，无面变更预期"）按 §4 同步。

---

## 0. 缺口定性

### 0.1 发现（已核实）

- gateway 的 AgentService HTTP 面注册在 proxy 连接上：`projects/game/gateway/cmd/main.go:129`（`game.RegisterAgentServiceHandler(ctx, gwmux, teamConn)`，teamConn = proxy gRPC conn）——owner affinity 路由，`/api/v2/` 会话面 RPC 全部经 proxy 转发（[specs/051-agent-v2-dsh-migration/contracts/agent-api.md](../../051-agent-v2-dsh-migration/contracts/agent-api.md) §4）。
- proxy 的 `AgentHandler`（`projects/game/proxy/handler/agent.go`）为每个 RPC 显式实现转发方法（UpdateAgent/GetAgent/ListAgentMessages/Send），嵌入 `game.UnimplementedAgentServiceServer`（`projects/game/proxy/handler/agent.go:52`）。proto 新增 `Cancel` RPC 后**生产代码编译通过**（嵌入兜底），但运行时 `:cancel` 返回 `Unimplemented`——**功能不可用**。
- tasks.md（[tasks.md](../tasks.md)）无任何 task 覆盖 proxy 侧 Cancel 转发：T002 只做 proto+codegen+编译确认，其"gateway/proxy 透传预期零代码改动"的表述对**字段扩展**成立（`desktop_connected` 经透传自动生效），对**新增 RPC** 不成立；T012/T013/T014 是 agent_v2 session.ts / agent_v2 server.ts / 前端。T024 的 testplan `:cancel` 用例走公网 gateway → proxy → agent_v2，proxy 无转发必然失败。

### 0.2 "零改动"预期不成立的完整清单（三层，均已在仓库现状核实）

1. **运行时缺口（proxy 生产代码）**：如上，`AgentHandler` 缺 `Cancel` 转发方法，经 gateway 调用 `:cancel` 得 `Unimplemented`。这是本文要补的核心缺口（§1–§3）。
2. **Go 编译缺口（proxy 测试代码）**：`fakeAgentClient`（`projects/game/proxy/handler/agent_test.go:60`）以 Go 结构化类型充当 `game.AgentServiceClient`（`agent_test.go:128` 处作为该接口返回）。codegen 后接口新增 `Cancel` 方法，fake 缺方法即不再满足接口 → `agent_test.go` 编译失败。T002 的编译门禁 `bazel build //projects/game/proxy/...` 构建 `go_test` target（`go_unittest` 即 `go_test`，`tools/dev/go/defs.bzl:17`；Bazel 通配 build 包含测试 target 的编译链接），**T002 不补 fake 的 `Cancel` 方法则自身门禁不过**。
3. **TS 编译缺口（agent_v2 生产代码）**：`agent_v2_types` 由 `proto-loader-gen-types` 生成（`tools/dev/js/ts_proto_library.bzl`），其 handler 接口的每个方法是**必填属性**（生成器源码 `export interface XHandlers extends grpc.UntypedServiceImplementation { M: grpc.handleUnaryCall<...>; }`，无 `?`；https://github.com/grpc/grpc-node/tree/master/packages/proto-loader ）。codegen 后 `AgentServiceHandlers` 新增必填 `Cancel`，`buildAgentHandlers`（`projects/game/agent_v2/src/server.ts:278`）返回的对象字面量缺该属性 → `server_lib` ts_project 类型检查失败，**T002 不补条目则 `bazel build //projects/game/agent_v2/...` 不过**。

**gateway 侧确为真零改动**：`RegisterAgentServiceHandler` 为生成代码，codegen 后自动携带 `POST /api/v2/{name=templates/*/sessions/*/agent}:cancel` 路由并拨 proxy 的 `Cancel`；gateway 无任何 `AgentServiceClient/Server` 手写实现（全仓 grep 核实，仅 `projects/game/gateway/cmd/main_test.go` 使用生成的注册函数），WS 派发不触及非 upgrade 的 POST。

### 0.3 Phase 2 → Phase 6 之间 `Unimplemented` 窗口的可接受性

**可接受**，依据：

- 该窗口内无消费者：前端 `cancelAgent` 在 T014（Phase 6 内、按 phase 顺序位于服务端任务之后），testplan `:cancel` 用例在 T024（Phase 11）。
- 即使 proxy 提前转发，agent_v2 侧在 T013 注册 handler 前对未实现方法同样返回 `UNIMPLEMENTED`（grpc-js `addService` 对缺 handler 的方法套用 `getDefaultHandler`，https://github.com/grpc/grpc-node/tree/master/packages/grpc-js ）——链路在该窗口本就 inert，proxy 转发早落不产生任何可用性差异。
- Phase 3/4/5 的 Independent Test 均不触及 `:cancel`，不存在被窗口误伤的验证。

---

## 1. 归属决定：新增 T013b（Phase 6）

**决定**：proxy `Cancel` 转发方法及其单测归属 **Phase 6（US5），新增独立 task `T013b`**（插入于 T013 与 T014 之间；字母后缀编号沿用 051 先例 `T014b`，见 [specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md](../../051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md) §8）。

**理由**：

1. **消费者时序**：转发只需在 T014/T024 之前就绪；放 Phase 6 使 US5 的服务端链（T012 session 语义 → T013 agent_v2 handler → T013b proxy 转发）与前端消费（T014）同 phase 闭环，Phase 6 checkpoint"终止全链可用"名实相符。
2. **Phase 2 检查点保持成立**：Phase 2 checkpoint 是"无行为变更"。T002 仅做 §0.2-2/3 的两处**编译适配**（行为等价：grpc-js 缺省 handler 本就返回 UNIMPLEMENTED，显式条目与之等价；fake 方法仅测试可达），`Unimplemented` 链路行为不变。若把生产转发塞进 T002，则该 phase 产生行为变更（proxy 跳由行为改变），checkpoint 被迫改写。
3. **评审颗粒度**：转发是 Go 代码 + Go 单测 + Go 规范文档，与 T013（TS，`server.ts`）分属不同文件/语言/测试体系；独立 task 使 Phase 6 的文档清单映射与 review 单元清晰。
4. **依赖准确**：T013b 仅依赖 Phase 2（proto codegen），不依赖 T012/T013（转发无本地业务语义，测试经 fake 驱动）——标记 `[P]` 可与 T012/T013 并行。

**否决的备选**：

- 并入 T002（Phase 2）：违反 Phase 2"无行为变更"检查点（理由 2）；且 Phase 2 文档清单为纯协议方向，被迫并入 Go/TS 双语言规范，评审面膨胀。
- 并入 T013（扩描述）：可行但使单 task 跨 TS+Go 两套文件/测试/规范，颗粒度劣于独立 task；且 T013 的依赖表述（依赖 T012 的 session 语义）会错误地捆绑本无依赖的 proxy 转发。
- 新开 phase：无必要——US5 story 内聚于 Phase 6，依赖与并行关系可用一个 task 表达。

---

## 2. 转发方法设计

### 2.1 方法实现（GetAgent 同构，`projects/game/proxy/handler/agent.go`）

```go
// Cancel forwards the cancel request to the agent_v2 instance owning the
// session. The owner is looked up, never allocated (lookup-only family:
// GetAgent/ListAgentMessages/Send): no owner → NOT_FOUND — for routing
// purposes there is no agent to cancel. All cancel semantics (in-flight
// turn termination, queue landing, idempotent no-op) live in agent_v2
// (specs/054-agent-v2-bugfixes/contracts/agent-api-changes.md §3); the
// proxy is a pure routing layer.
func (h *AgentHandler) Cancel(ctx context.Context, req *game.CancelRequest) (*game.CancelResponse, error) {
	name, err := parseAgentResourceName(req.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	owner, err := lookupAgentOwner(ctx, h.ownerStore, name.TemplateID, name.SessionID)
	if err != nil {
		return nil, err
	}

	connRef, err := agentV2Conn(ctx, h.manager, owner)
	if err != nil {
		return nil, err
	}

	resp, err := newAgentClient(connRef.Conn).Cancel(ctx, req)
	if err != nil {
		logs.Error(ctx, "cancel agent: downstream call failed",
			event.String("session_id", name.SessionID),
			event.Err(err),
		)
		return nil, propagateAgentError(err, "cancel agent")
	}
	return resp, nil
}
```

文件内位置：ListAgentMessages 之后、Send 之前（unary 方法归组）。复用的既有件零改动：`parseAgentResourceName`（`agent.go:266`，含 5 段形状与 known-template 校验）、`lookupAgentOwner`（`agent.go:280`，只查不分配）、`agentV2Conn`（`agent.go:378`，实例不可达 → UNAVAILABLE/503）、`propagateAgentError`（`agent.go:393`，下游状态原码透传）。

### 2.2 校验面分析（对照 Send）

`CancelRequest` 仅有 `name` 路径参数（[contracts/agent-api-changes.md](../contracts/agent-api-changes.md) §3），**无 body 字段** → 不存在 Send 的 empty-text 类前置校验。前置校验只有两层，与 Send 前置错误族完全同构（051 [contracts/agent-api.md](../../051-agent-v2-dsh-migration/contracts/agent-api.md) §2.4/§3）：

| 层 | 位置 | 错误 |
|---|---|---|
| 资源名形状/未知 template | proxy（`parseAgentResourceName`） | INVALID_ARGUMENT / 400 |
| 无 owner（路由视角未物化） | proxy（`lookupAgentOwner` → `mapDomainError`） | NOT_FOUND / 404 |
| owner 在而 agent 未物化 | agent_v2（T013 handler，代理只透传） | FAILED_PRECONDITION / 400 |

### 2.3 注释同步（constitution VII 终态）

`agent.go` 的 package 注释（`agent.go:1-9`，列举面方法）与 `AgentHandler` 类型注释（`agent.go:37-50`，列举 lookup-only 家族）在 T013b 中把 `Cancel` 并入对应清单——交付态注释与代码一致，不留"待补"痕迹。

---

## 3. 测试要求

测试文件：`projects/game/proxy/handler/agent_test.go`（既有 fake/harness 全复用：`newAgentHarness`、`seedAgentOwner`、`agentResource` 常量、`setFakeAgentClient`——`agent_test.go:125-166`）。

fake 扩展（T002 已落，此处驱动）：`fakeAgentClient` 增加 `cancelReq *game.CancelRequest` 与 `cancelErr error` 字段及方法——记录请求、配置错误优先、默认返回 `&game.CancelResponse{}`（ListAgentMessages 同构的最小 unary 形态；响应为空对象无需 result 字段）。

用例（命名/结构对照既有 GetAgent/Send 用例，given-when-then + 表驱动，`style/golang.md`）：

| # | 用例 | 断言要点 | 对照先例 |
|---|---|---|---|
| 1 | `TestAgentHandler_Cancel_SuccessForwardsName` | seed owner(3)：err=nil、resp 非空、`manager.getCalls==[3]`、`fake.cancelReq.GetName()==agentResource`、`store.createCalls==0`（只查不分配） | `TestAgentHandler_GetAgent_SuccessForwardsName` |
| 2 | `TestAgentHandler_Cancel_InvalidNameReturnsInvalidArgument` | 表驱动：malformed（`projects/p1`）/缺 agent 段（`agentSession`）/未知 template（`templates/unknown/sessions/s1/agent`）→ INVALID_ARGUMENT，无 owner 分配 | `TestAgentHandler_UpdateAgent_InvalidNameRejectedWithoutAllocation`（表含未知 template） |
| 3 | `TestAgentHandler_Cancel_NoOwnerReturnsNotFoundWithoutAllocation` | 空 store → NOT_FOUND、`createCalls==0`（cancel 不做物化入口） | `TestAgentHandler_GetAgent_NoOwnerReturnsNotFoundWithoutAllocation` |
| 4 | `TestAgentHandler_Cancel_InstanceUnreachable` | seed owner + `manager.getErr` → UNAVAILABLE（proxy→agent_v2 跳断路 503，051 §3 两跳错误表） | `TestAgentHandler_Send_InstanceUnreachable` |
| 5 | `TestAgentHandler_Cancel_DownstreamErrorPropagates` | `cancelErr = status.Error(codes.FailedPrecondition, "agent not materialized; ...")` → 原码透传 FAILED_PRECONDITION（前端见 400 而非 500/503） | `TestAgentHandler_UpdateAgent_DownstreamErrorPropagates` |

编译+单测（`bazel build //projects/game/proxy/...` + `bazel test //projects/game/proxy/...`）为 T013b 交付的一部分（constitution IV），不单列 task。端到端 `:cancel` 断言由 T024 testplan 用例承载（公网 gateway → proxy → agent_v2），不在本层重复。

---

## 4. 既有文档同步（plan.md 括注）

[plan.md](../plan.md) Project Structure 源码树中 `(gateway/proxy：/api/v2 透传，无面变更预期)` 的括注按本文结论失准（gateway 仍零改动；proxy 有 T013b 面变更 + T002 测试替身编译适配）。应用 tasks.md 修订时同步将该括注改为：`(gateway：/api/v2 透传零改动；proxy：Cancel 转发见 revisions/phase2-proxy-cancel.md)`。此为应用者的一次性文本同步，不改变 plan 其余内容。

---

## 5. 契约更新（需要，一行）

**结论：需要。** [contracts/agent-api-changes.md](../contracts/agent-api-changes.md) §3 当前只定义 `:cancel` 的方法行为与错误码，未记录两跳路由（对照：§4 GetAgent 明确写了"gateway/proxy 预期零改动"的路由说明——新增 RPC 的路由事实与之不同，契约与实现一致要求补记，constitution I/III）。错误语义不变（仍经"与 Send 前置错误同族"延引 051 §3 两跳表），仅补路由事实。

在 §3 错误码行（`错误码：路径非法/未物化 → 400/FAILED_PRECONDITION（与 Send 前置错误同族，051 FR-007 语义）。`）之后追加一行：

```markdown
- 路由：会话面两跳（gateway→proxy→agent_v2，051 §4 拓扑）——gateway 的 grpc-gateway 注册随 codegen 自动携带 `:cancel` 路由（零代码改动）；proxy `AgentHandler` 以 owner 亲和显式转发（GetAgent 同构，无本地语义），设计见 [revisions/phase2-proxy-cancel.md](../revisions/phase2-proxy-cancel.md)。
```

该契约补行随 T013b 同变更落地（先于/伴随实现，接口优先）。

---

## 6. tasks.md 修订文本（最小修订，共 6 处；由执行者应用）

以下行号对应当前 `specs/054-agent-v2-bugfixes/tasks.md`（应用时以引文锚点为准）。

1. **Phase 2 文档清单**（tasks.md:43，行尾追加）：

   > ；`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（server.ts 的 Cancel 编译适配）；`style/golang.md`；[Google Go Style Guide](https://google.github.io/styleguide/go/guide)（proxy 测试替身的 Cancel 编译适配）

2. **T002 描述**（tasks.md:47）——将尾部括注

   > （生成类型进 `projects/game/agent_v2/agent_v2_types/`，gateway/proxy 透传预期零代码改动，编译确认）

   替换为：

   > （生成类型进 `projects/game/agent_v2/agent_v2_types/`；gateway 零改动——grpc-gateway 注册随 codegen 自动携带 `:cancel` 路由；新增 RPC 另需两处编译适配，行为不变（`:cancel` 端到端仍 Unimplemented，语义落地在 Phase 6）：`projects/game/agent_v2/src/server.ts` 的 `buildAgentHandlers` 补显式 UNIMPLEMENTED `Cancel` 条目（proto-loader-gen-types 的 handler 接口方法为必填属性）、`projects/game/proxy/handler/agent_test.go` 的 `fakeAgentClient` 补 `Cancel` 方法（记录请求/配置错误/默认成功，ListAgentMessages 同构）——依据 `specs/054-agent-v2-bugfixes/revisions/phase2-proxy-cancel.md` §0.2）

3. **Phase 6 Independent Test**（tasks.md:117，替换该行）：

   > **Independent Test**: `bazel test //projects/game/agent_v2/... //projects/game/proxy/... //projects/game/web/frontend/...`——cancel 服务端全语义（agent_v2 + proxy 转发）与前端编排用例全绿

4. **Phase 6 文档清单**（tasks.md:121 与 :123）：
   - 代码规范文档行尾追加：

     > ；`style/golang.md`（T013b proxy Go 转发）；[Google Go Style Guide](https://google.github.io/styleguide/go/guide)（`style/golang.md` 引用基准）

   - 技术文章/技术参考文档行尾追加：

     > 、`specs/054-agent-v2-bugfixes/revisions/phase2-proxy-cancel.md`（T013b 设计）、`specs/051-agent-v2-dsh-migration/contracts/agent-api.md` §2/§3（两跳错误表与 NOT_FOUND/FAILED_PRECONDITION 分层基线）

5. **新增 task**（插于 tasks.md:126 T013 行之后、T014 行之前）：

   > - [ ] T013b [P] [US5] `projects/game/proxy/handler/agent.go`：新增 `Cancel` 转发方法——GetAgent 同构（`parseAgentResourceName` → `lookupAgentOwner`（只查不分配）→ `agentV2Conn` → `newAgentClient.Cancel` → `propagateAgentError`），无本地业务语义（cancel 语义在 T012 的 agent_v2），并同步 agent.go 包/类型注释的方法清单；`projects/game/proxy/handler/agent_test.go`：Cancel 用例（正常转发/INVALID_ARGUMENT 表驱动含未知 template/无 owner NOT_FOUND 且不分配/owner 实例不可达 UNAVAILABLE/下游状态原码传播）；按 `specs/054-agent-v2-bugfixes/revisions/phase2-proxy-cancel.md` §5 在 `specs/054-agent-v2-bugfixes/contracts/agent-api-changes.md` §3 补 proxy 路由说明一行；仅依赖 Phase 2，可与 T012/T013 并行

6. **Parallel Opportunities**（tasks.md:272，该条目行尾追加）：

   > ；Phase 6 内 T013b（proxy Cancel 转发，仅依赖 Phase 2）可与 T012/T013 并行

不改动：Phase Dependencies 的 Phase 6 行（"依赖 Phase 2 + Phase 4"对 phase 整体仍成立，T013b 的更弱依赖已由 `[P]` 标记与第 6 处并行说明表达）；Phase 2/Phase 6 的 Checkpoint（T002 编译适配行为等价，"无行为变更"仍成立；Phase 6"终止全链可用"因 T013b 名实更符）。

---

## 7. 下游执行指引（分步可恢复）

1. 应用 §6 六处 tasks.md 修订 + §4 plan.md 括注同步（纯文档步）。
2. T002 执行时按修订后的描述落地两处编译适配（§0.2-2/3），跑 `bazel build //projects/game/agent_v2/... //projects/game/gateway/... //projects/game/proxy/...`。
3. Phase 6 执行到 T013b 时按 §2/§3 实现与测试，按 §5 补契约行，跑 `bazel build //projects/game/proxy/...` + `bazel test //projects/game/proxy/...`。
4. T014/T024 消费链验证无需额外动作（转发已在链上）。
