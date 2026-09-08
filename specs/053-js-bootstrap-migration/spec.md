# Feature Specification: JS bootstrap 组件与 experimental 目录统一为 js

**Feature Branch**: `053-js-bootstrap-migration`

**Created**: 2026-09-01

**Status**: Draft

**Input**: User description: "为 js 增加 bootstrap 组件（参考 golang）。另外将 experimental/ts/ 目录迁移到 experimental/js 目录，统一为 js"

## Clarifications

### Session 2026-09-01

- Q: JS bootstrap 组件的能力范围是否与 Go bootstrap（`common/gopkg/bootstrap/`）完全对齐？ → A: 全量对齐。除生命周期编排、健康端点、信号处理、关停预算、gRPC server 适配器外，还包含 HTTP server 适配器、gRPC client 连接适配器、Daemon worker 重启监督器、组件意外退出监测。
- Q: 目录迁移是否连带将 proto 包名、HTTP 路径、Go importpath 中的 `ts` 一并更名为 `js`？ → A: 一并更名。目录、proto 包名（`experimental.js.grpc_hello_world`）、HTTP 路径（`/experimental/js/grpc-hello-world/say-hello`）、Go importpath（`dominion/experimental/js/grpc_hello_world`）全部统一为 js；gateway 转发路径与测试计划路径前缀同步更新。
- Q: 生产 JS 服务（`projects/game/` 下 agent/agent_v2 等）是否在本特性内接入新 bootstrap？ → A: 不接入。迁移与接入仅涉及 `experimental/` 目录下的 JS 服务，其他 JS 服务（生产服务及其他位置的 JS 项目）一律不动（沿袭 specs/052-deploy-health-probe 的范围先例），生产服务接入为后续工作。
- Q: team_graph_spike 接入共享 bootstrap 后自动新增的约定健康端点（38080/healthz）是预期新行为，还是外部行为必须与改造前完全一致？ → A: 预期新行为。接入即按 FR-002 无条件获得健康端点；FR-013 的"外部行为保持不变"限定为既有接口契约与既有测试（含大型测试）不回归，新增约定健康端点不计为行为变化。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - JS 服务开发者获得与 Go 一致的标准化生命周期管理 (Priority: P1)

JS 服务开发者使用共享的 JS bootstrap 公共组件管理服务生命周期：注册各组件后，bootstrap 按生命周期阶段顺序启动组件；任一组件启动失败时自动回滚已启动组件；全部组件启动成功后自动提供健康端点；收到退出信号后按预算优雅停止（健康端点最先停止）。行为语义与现有 Go bootstrap（`common/gopkg/bootstrap/`）保持一致，消除 JS 服务各自手写启动/健康/退出样板代码的重复与漂移。

**Why this priority**: 这是本特性的核心价值——specs/052-deploy-health-probe 明确将"统一的 JS bootstrap 公共库"列为后续独立工作，本特性即完成该项；当前 `experimental/ts/grpc_hello_world` 与 `team_graph_spike` 各自维护手写 bootstrap，行为已出现不一致（健康端点、关停顺序实现各异）。

**Independent Test**: 将一个 experimental 下的 JS 服务接入共享 bootstrap，本地启动后访问约定健康端点得到成功响应；发送 SIGTERM 后确认健康端点先于业务组件停止、进程优雅退出。

**Acceptance Scenarios**:

1. **Given** 一个使用共享 JS bootstrap 的服务注册了多个组件，**When** bootstrap 启动，**Then** 组件按阶段+名称顺序依次启动，全部成功后健康端点（约定端口 38080、路径 /healthz）开始返回成功响应。
2. **Given** 某个组件启动失败，**When** bootstrap 执行启动流程，**Then** 已启动的组件按相反顺序停止，进程以非零状态退出且错误原因清晰（health 端点不残留监听）。
3. **Given** 一个使用共享 JS bootstrap 的服务收到 SIGTERM/SIGINT，**When** 执行退出流程，**Then** 健康端点最先停止，其余组件在关停预算内反向顺序停止，进程正常退出。
4. **Given** 健康端点启动失败（如端口 38080 被占用），**When** bootstrap 启动，**Then** 视同组件启动失败：回滚已启动组件并退出，不得以无健康端点的状态继续运行（沿袭 specs/052-deploy-health-probe/spec.md FR-010 语义）。

---

### User Story 2 - experimental 目录统一为 js，消除 ts/js 双命名 (Priority: P2)

仓库维护者将 `experimental/ts/` 下的全部内容（grpc_hello_world、hello_world、team_graph_spike）迁移至 `experimental/js/`，与既有 `experimental/js/vite_react_demo` 合并：仓库内不再存在 `experimental/ts` 路径，工作区配置、构建目标、代码引用、测试计划与注释全部统一指向 `experimental/js`。

**Why this priority**: 命名统一降低目录检索与认知成本；JS/TS 项目归一为 "js" 命名与 `common/js`、`tools/dev/js` 的既有约定一致。

**Independent Test**: 迁移后全仓库检索 `experimental/ts` 无任何活跃代码/配置命中（历史 spec 文档除外）；`bazel build //...` 与 `bazel test //...` 全绿。

**Acceptance Scenarios**:

1. **Given** 迁移完成，**When** 在仓库内检索 `experimental/ts`（代码、构建文件、工作区配置、测试计划、注释），**Then** 无命中（历史 spec 文档除外）。
2. **Given** 迁移后的三个子项目，**When** 执行构建与单测（含 smoke test），**Then** 全部通过。
3. **Given** 迁移后的 grpc_hello_world 服务，**When** 通过 testplan 执行大型测试（部署→接口验证→自愈→清理），**Then** 全部用例通过。

---

### User Story 3 - experimental JS 服务去除手写 bootstrap，改用共享组件 (Priority: P3)

`experimental/js/grpc_hello_world` 与 `experimental/js/team_graph_spike` 删除各自手写的 bootstrap 样板（OTel 初始化后动态导入 server、健康端点自建、信号处理样板），统一接入共享 JS bootstrap；接入后服务的既有接口契约、优雅退出语义与大型测试结果保持不变，约定健康端点（38080/healthz）由 bootstrap 自动提供（grpc_hello_world 为既有能力，team_graph_spike 为接入新增的预期行为）。这些服务同时作为共享 bootstrap 的验证载体。

**Why this priority**: 以真实服务验证共享组件的可用性，同时消除样板重复；external 行为不变使验证可完全对照既有大型测试基线。

**Independent Test**: 接入改造后运行 grpc_hello_world 既有 testplan（interface_test、health/deploy 自愈），结果与改造前基线一致（全部通过）。

**Acceptance Scenarios**:

1. **Given** grpc_hello_world 已接入共享 bootstrap，**When** 执行其既有大型测试计划，**Then** 接口契约、健康探针、自愈等全部用例通过。
2. **Given** team_graph_spike 已接入共享 bootstrap，**When** 构建并启动该服务，**Then** 服务正常启动、自动提供约定健康端点（38080/healthz）并按 bootstrap 语义优雅退出，既有单测通过。

---

### Edge Cases

- **重复注册同名组件 / 启动后再注册**：bootstrap 必须明确拒绝（与 Go bootstrap 语义一致），返回清晰的错误。
- **组件启动失败回滚**：health 在全部组件启动成功后才会启动，回滚场景下 health 尚未存在，不产生残留监听。
- **健康端口 38080 被占用（本地同主机多服务）**：端口是约定而非强制校验，不新增校验代码；绑定失败按 FR-004 处理（语义沿袭 specs/052-deploy-health-probe/spec.md FR-008/FR-010）。
- **优雅退出超时**：关停预算耗尽时，未在预算内完成的组件停止按失败记录，进程仍退出（与 Go bootstrap 统一预算语义一致）。
- **服务组件意外退出（如 server 崩溃）**：bootstrap 通过意外退出监测感知并记录错误，触发全局优雅关停（与 Go bootstrap 语义一致）；关停预算与错误聚合规则同样适用。
- **Daemon worker 反复失败**：重启次数达到上限或错误分类为致命时，按致命错误上报并触发全局关停，不得无限重启。
- **迁移导致 bazel target 路径变化**：所有仓库内对 `experimental/ts/...` 目标的引用（如 `tools/dev/js/BUILD.bazel` 的 proto-loader bin 引用）必须同步更新，否则构建断裂。
- **proto/HTTP 路径更名的外部兼容性**：proto 包名与 HTTP 路径随目录一并更名（FR-009），gateway 转发路径与测试计划中的路径前缀必须同步更新，保证大型测试验证的是新路径。
- **迁移期间其他开发者并行修改 experimental/ts**：迁移作为原子变更合入，避免跨目录半迁移状态。

## Requirements *(mandatory)*

### Functional Requirements

**共享 JS bootstrap 组件**

- **FR-001**: 仓库 MUST 在 JS 公共包目录下新增 bootstrap 公共组件（与 `common/gopkg/bootstrap/` 对应），提供组件生命周期编排：组件注册（拒绝重名、拒绝启动后注册）、按阶段（Stage）+名称（Name）顺序依次启动、任一组件启动失败时按相反顺序回滚已启动组件并退出。
- **FR-002**: bootstrap MUST 在所有组件启动成功后自动启动健康端点（约定端口 38080、路径 /healthz，成功响应表示"所有组件已启动且进程存活"）；bootstrap 停止/退出时 MUST 先停止健康端点再停止其他组件（先进先出），完全遵循 specs/052-deploy-health-probe/spec.md 已确立的 health 约定。
- **FR-003**: bootstrap MUST 以 SIGTERM/SIGINT 为默认退出信号触发优雅停止，并提供统一的关停预算（默认值与 Go bootstrap 一致为 5 秒，可配置）；预算内未完成的组件停止按失败记录，错误信息聚合返回。
- **FR-004**: 健康端点启动失败 MUST 视同组件启动失败：回滚已启动组件并以非零状态退出，不得以无健康端点的状态继续运行。
- **FR-005**: bootstrap 公共组件 MUST 提供 gRPC server 组件适配器（包装 grpc-js Server），支持预算内优雅停止与预算耗尽后的强制停止两阶段语义。
- **FR-006**: JS bootstrap 公共组件 MUST 与 Go bootstrap（`common/gopkg/bootstrap/`）全量能力对齐：在 FR-001~FR-005 之外，还 MUST 提供 HTTP server 组件适配器、gRPC client 连接适配器（注册进生命周期、停止时关闭连接）、Daemon worker 监督器（按可配置的重启策略管理 worker 的构建/启动/重启，含指数退避、重启上限与错误分类，不可恢复错误按致命错误触发全局关停）以及组件意外退出监测（server 类组件意外退出时记录错误并触发全局优雅关停）。
- **FR-007**: bootstrap 公共组件 MUST 具备单元测试，覆盖启动排序、失败回滚、健康端点 FIFO 生命周期、信号退出与关停预算、Daemon 重启/退避/错误分类策略、组件意外退出监测等核心语义。

**目录迁移**

- **FR-008**: `experimental/ts/` 下的全部内容（grpc_hello_world、hello_world、team_graph_spike）MUST 迁移至 `experimental/js/` 目录下；迁移完成后仓库内 MUST NOT 存在 `experimental/ts` 路径（历史 spec 文档除外）。`experimental/` 下其他位置的 JS 项目（`grpc_chain/mid`、`openai_llm/client`、`dsh/demo/agent`）MUST NOT 被移动。
- **FR-009**: 迁移 MUST 将所有携带 `ts` 的标识符随目录一并统一为 `js`：proto 包名更名为 `experimental.js.grpc_hello_world`，HTTP 路径更名为 `/experimental/js/grpc-hello-world/say-hello`，Go importpath 更名为 `dominion/experimental/js/grpc_hello_world`；gateway 转发路径与测试计划中的路径前缀 MUST 同步更新，保证大型测试验证的是新路径。
- **FR-010**: 迁移 MUST 同步更新所有仓库内引用：工作区配置（`pnpm-workspace.yaml` 仅保留 `experimental/js/*`）、构建文件（含 `tools/dev/js/BUILD.bazel` 的 proto-loader 引用）、源码内 proto/类型路径引用、测试计划（testplan deploy/interface/自愈用例与 gateway 路径前缀）、代码注释中的目录引用。
- **FR-011**: 迁移后的三个子项目 MUST 保持构建（含 smoke test）、单元测试、大型测试（testplan 全流程：部署→测试→清理）全部通过。

**服务接入**

- **FR-012**: 接入共享 bootstrap 的范围 MUST 限定为 `experimental/` 目录下的 JS 服务（grpc_hello_world、team_graph_spike）；`experimental/` 之外的 JS 服务（`projects/game/` 下 agent/agent_v2、各前端等）及 `experimental/` 下未迁移位置的 JS 项目（`grpc_chain/mid`、`openai_llm/client`、`dsh/demo/agent`）MUST NOT 在本特性内修改，其接入为后续工作。
- **FR-013**: `experimental/js/grpc_hello_world` 与 `experimental/js/team_graph_spike` MUST 接入共享 bootstrap 并删除各自手写的 bootstrap 样板（健康端点自建、信号处理样板等）；接入后服务的既有接口契约、优雅退出语义与既有大型测试结果 MUST 保持不变。接入使服务按 FR-002 自动新增约定健康端点（38080/healthz）属预期行为，不计为行为变化（team_graph_spike 原仅有业务端口上的 /health，接入后新增约定端点）。
- **FR-014**: grpc_hello_world 用于大型测试的测试钩子（`HEALTH_STOP_AFTER_MS` 自愈模拟，specs/052-deploy-health-probe/contracts/verification-testplan.md §2）MUST 保留其"仅存在于实验服务、不进入共享包"的边界，接入改造不得将其移入 bootstrap 公共组件。

### Key Entities

- **Component（组件）**：bootstrap 管理的基本单元，契约包含名称（唯一标识）、生命周期阶段（Stage）、启动（Start）与停止（Stop）。
- **Stage（生命周期阶段）**：组件的启动顺序分层，顺序语义与 Go bootstrap 一致（基础层→客户端层→后台任务层→服务层）。
- **健康端点（约定）**：固定端口 38080、路径 /healthz；成功响应表示"所有组件已启动且进程存活"；生命周期先进先出（最后启动、最先停止）。约定属性源自 specs/052-deploy-health-probe/spec.md。
- **关停预算（shutdown timeout）**：优雅停止的统一时间预算，超时后未完成的停止按失败记录；默认 5 秒，可配置。
- **组件适配器（Adapter）**：将既有服务对象（gRPC server、HTTP server、gRPC client 连接）包装为 Component 契约的桥接件；server 类适配器同时暴露意外退出信号供监测。
- **Daemon 监督器**：管理后台 worker 的构建、启动与按策略重启（指数退避、重启上限、错误分类）；不可恢复错误按致命错误上报，触发全局优雅关停。
- **意外退出监测（exit watcher）**：bootstrap 对 server 类组件意外退出信号的监听机制；收到信号后触发全局优雅关停。
- **验证载体服务**：`experimental/js/grpc_hello_world`（gRPC 服务，含完整大型测试）与 `experimental/js/team_graph_spike`（spike 服务），用于验证共享 bootstrap 的真实可用性。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 接入共享 JS bootstrap 的服务启动完成后，约定健康端点（38080/healthz）返回成功响应；发送退出信号后健康端点先于所有业务组件停止，顺序可被测试观测验证。
- **SC-002**: 任一组件启动失败（含健康端点启动失败）时，已启动组件按相反顺序全部停止、无残留监听，进程以非零状态退出且错误信息包含失败组件标识。
- **SC-003**: 迁移完成后，全仓库检索 `experimental/ts` 在活跃代码、构建文件、工作区配置、测试计划与注释中零命中（历史 spec 文档除外）。
- **SC-004**: 迁移与接入改造后，受影响子项目的构建、单测、smoke test 及既有大型测试（testplan 全流程）通过率 100%，无行为回归。
- **SC-005**: experimental JS 服务中不再存在重复手写的启动/健康/退出样板代码（健康端点、信号处理、关停顺序均由共享组件提供）。

## Assumptions

- JS bootstrap 公共组件放置于 `common/js/` 下并遵循既有包命名与管理约定（如 `@dominion/common-js-*`、catalog 依赖管理、BUILD 由 gazelle 生成），具体 API 形态（类/函数签名、模块划分）在方案阶段确定。
- 健康端点完全复用 specs/052-deploy-health-probe/spec.md 已确立的约定（端口 38080、路径 /healthz、先进先出、启动失败即退出、端口不作校验），本特性不引入新的健康语义。
- OTel 初始化先于 grpc-js 加载的既有时序实践（`experimental/ts/grpc_hello_world/src/bootstrap.ts` 中 init→动态 import 的顺序）在接入改造后保持等价，具体由服务入口还是 bootstrap 承载在方案阶段决定。
- 目录迁移采用保留 git 历史的方式（目录移动与内容更名分开提交），便于追溯。
- `experimental/` 下其他语言/用途目录（`golang/`、`grpc_chain/`、`openai_llm/`、`dsh/`）不在本特性范围内。
- 历史 spec 文档（`specs/` 目录）中出现的旧路径 `experimental/ts` 属于历史记录，不修改。
- grpc_hello_world 接入共享 bootstrap 后，`HEALTH_STOP_AFTER_MS` 自愈钩子仍必须可行：bootstrap 公共组件需为服务侧保留可模拟健康端点失效的集成缝隙（如暴露受控的 health 生命周期句柄或可注入的实现），具体机制在方案阶段确定；测试专用逻辑不得进入共享包（FR-014）。
- `hello_world` 子项目虽无手写 bootstrap（纯脚本 demo），仅做目录迁移不做接入改造。
