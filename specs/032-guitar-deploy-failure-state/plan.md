# Implementation Plan: Guitar Deploy Failure Environment Diagnostics

**Branch**: `032-guitar-deploy-failure-state` | **Date**: 2026-08-02 (revised 2026-08-03) | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/032-guitar-deploy-failure-state/spec.md`

## Summary

When `guitar run` 执行某 suite 的部署步骤不成功时，当前仅返回一句 `deploy apply <path>: <error>`，无法直观看出环境最终状态、失败原因（哪个 service 失败）或涉及哪些服务。

初版方案（已落地：`describe` 命令 + guitar 失败诊断 hook）依赖 `EnvironmentStatus.message` 自由文本承载 per-service 信息。实测（`--timeout=5s`）暴露该数据源**不稳定**：`message` 由 reconciler 在 `checkRollout`（`projects/infra/deploy/service/reconcile.go:147`）每 5s 轮询一次后填充（`runtime/k8s/rollout.go:263` 的 `CheckRollout` 将各 workload 的状态 `strings.Join(..., "; ")` 折叠为单一环境级字符串），首次轮询前 `message` 为空 → describe 无法指认哪个 service。

**修订方案**（详见 [research.md](./research.md)）：根因是 Environment **缺乏结构化的 per-service 状态**。因此：
1. **deploy service 侧**：为 `EnvironmentStatus` 新增 `repeated ServiceStatus services` 字段（proto + domain + runtime + reconcile + storage + handler），reconciler 在 `applyAndWait`（资源已提交、进入 `WAITING_ROLLOUT`）即写入每个服务的初始状态（`PENDING`），并在每次 `checkRollout` 用真实的 k8s rollout 状态（ready/waiting/failed + 可读原因 + 副本数）更新之——**不再折叠为单一 message**。
2. **deploy `describe` CLI 侧**：输出改为以 per-service 状态为主线（每个服务一行，标注其 rollout 状态/原因），结构化数据成为稳定主诊断；环境级 `message` 降级为次要补充（仅对非 rollout 原因如 apply 失败、retry-exhausted 有意义时显示）。
3. **guitar 侧**：**无需改动**——guitar 经 shell-out 调 `deploy describe`（既有链路），describe 输出的 per-service 状态自然流入控制台。

该方案直接服务于 spec 用户故事"判断具体哪个 service 部署失败或者超时"，并以"环境进入 WAITING_ROLLOUT 即有 per-service 数据"消除了初版的时序空窗。

## Technical Context

**Language/Version**: Go（仓库 toolchain；`deploy` service、`deploy` CLI v3、`guitar` 均为 Go）

**Primary Dependencies**:
- **deploy service**（`projects/infra/deploy`）：proto3 + gRPC + grpc-gateway（REST transcoding）；存储 MongoDB（schemaless）；reconcile 经 `domain.EnvironmentRuntime`（k8s 实现 `runtime/k8s`）。
- **deploy CLI v3**（`tools/release/deploy/v3`）：`pflag`；经 v2 HTTP client（`tools/release/deploy/v2/client`）消费 deploy service。
- **guitar**（`tools/test/guitar`）：`pflag`；shell-out 编排 `deploy` 二进制。

**Storage**: MongoDB（`projects/infra/deploy/storage/mongo.go`，schemaless——新增字段无需 migration，旧文档缺字段解码为零值）。

**Testing**: `bazel test`（Go 单测）。deploy service 各层有既有单测（domain/service/runtime/handler）；deploy v3 用 `httptest` 桩；guitar `run` 用可替换 `runCommand` 桩。大型测试经 testplan skill（`tools/test/guitar`）执行——guitar e2e（如 `experimental/ts/grpc_hello_world/testplan/interface_test.yaml`）端到端覆盖 deploy service。

**Target Platform**: Linux/macOS（CLI 工具 + 服务端 deploy service 部署于 infra.liukexin.com）。

**Project Type**: 服务型应用（deploy service）+ CLI 工具（deploy v3 / guitar）。

**Performance Goals**: per-service 状态随 reconcile 正常流转写入（无额外查询）；describe 仍是单次 `GetEnvironment`（HTTP GET，`--timeout=10s`）。

**Constraints**:
- proto 变更必须**向后兼容**（proto3 additive：新增字段/枚举，不删改已有编号）。
- reconciler 的乐观并发模型不变（`TransitionStatus` 的 generation+fromState 前置条件）。
- 诊断调用须用非取消上下文（部署失败常为 ctx 已取消），失败降级不掩盖原始错误（既有设计，guitar 侧不变）。
- guitar 经 shell-out 消费 describe，**不直接 import deploy service client**（保持既有架构）。

**Scale/Scope**:
- deploy service：proto 新增 1 message + 2 enum + 1 字段；domain 新增对应类型 + `EnvironmentStatus.Services` + `RolloutStatus.Services`；runtime `CheckRollout` 重构为产出 per-service 列表（不再折叠）；reconcile `applyAndWait`/`checkRollout`/`retainWaitingRollout`/`markFailedFromRollout`/`markReadyFromRollout` 持久化 Services；storage `mongoStatus` + `TransitionStatus` 增列；handler `toProtoStatus` 增映射；配套单测。
- deploy CLI：`describe.go` 输出改为 per-service 主线 + message 次要；配套单测。
- guitar：无代码改动（描述变更已在初版落地，describe 输出增强自动生效）。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

依据 `.specify/memory/constitution.md`（v1.3.0）：

- **原则 I（引用溯源）**：本 plan 及产物中的仓库内引用使用相对路径，外部引用使用完整 URL。✅ 合规。
- **原则 II（重构式变更）**：现有 `EnvironmentStatus` 仅环境级 `message`，`CheckRollout` 将 per-workload 状态折叠为单一字符串——该设计相对新需求"判断具体哪个 service"**不足**。本方案在数据模型层面**重构**：新增结构化 `ServiceStatus`、reconciler 不再折叠而是逐服务持久化、describe 以 per-service 为主线。属设计层面的恰当重构，而非打补丁。✅ 合规。
- **原则 III（接口优先）**：先定 proto `ServiceStatus`/`ServiceKind`/`ServiceRolloutState` + `EnvironmentStatus.services` 契约与 describe 输出契约（见 [contracts/](./contracts/)），再实现。✅ 合规。
- **原则 IV（测试颗粒度）**：编译 + 单测作为开发任务的一部分（proto 映射、reconcile、storage、runtime、describe 各层单测），不单列 task。大型测试单独验收（见 VI）。✅ 合规。
- **原则 V（编码前阅读文档）**：由后续 `/speckit.tasks` 在 tasks.md 中按 phase 声明文档清单。✅ 预留。
- **原则 VI（大型测试验收）**：本次改动包含 deploy service（服务型应用），原则上 VI 适用。但 `projects/infra/deploy/README.md:28` 明确声明 deploy service **不进行大型测试**——根因是技术性的无法自举：deploy service 是 `deploy`/guitar/testplan 部署链路的后端（`README.md:24`："本服务禁止使用 `deploy` 工具部署"），不能用自己部署自己，故无 testplan 适用于它（全仓库各 `testplan` 目录无 deploy service 用例，已核实）。这是仓库既定的、基于技术约束的例外。据此：deploy service 的代码变更（新增 `services` 字段 + reconcile + 持久化）以**单测**为验收门禁（proto 映射、reconcile 各转移路径持久化 Services、storage 往返、`CheckRollout` 产出 per-service、handler 返回 services、describe 输出 per-service）。端到端价值验证（per-service 状态在真实 describe 中可见）依赖 deploy service 新版本经其独立 k8s 部署流程上线 infra.liukexin.com 后，可用 guitar e2e（`experimental/ts/grpc_hello_world/testplan/interface_test.yaml`）做**冒烟验证**（非强制 testplan 门禁，因受限于 deploy service 的独立部署周期，超出本特性 scope）——见 [quickstart.md](./quickstart.md)。guitar 自身改动为零（初版已落地），其大型测试行为不受影响。✅ 合规（遵循 deploy service 既定例外）。

**结论**：无违规。Complexity Tracking：

| 复杂项 | 说明 | 缓解 |
|--------|------|------|
| reconcile 流为乐观并发状态机 | 新增 Services 字段须随状态转移正确持久化/清空 | TransitionStatus 的"非零字段写入"语义需为 Services 显式处理；单测覆盖每条转移路径 |
| CheckRollout 行为变更（不再 early-return on first failed） | 影响既有 rollout_test.go 断言 | 同步更新 k8s rollout 单测；保留 env-level State/Message 派生以维持 reconcile 语义不变 |

## Project Structure

### Documentation (this feature)

```text
specs/032-guitar-deploy-failure-state/
├── plan.md                       # 本文件
├── research.md                   # Phase 0：per-service 状态方案决策与备选
├── data-model.md                 # Phase 1：ServiceStatus 实体 + describe 输出模型
├── quickstart.md                 # Phase 1：端到端验证指南（含失败场景）
├── contracts/
│   ├── environment-status.md     # NEW: proto/domain per-service 状态 API 契约
│   ├── deploy-describe.md        # REVISED: describe 输出契约（per-service 主线）
│   └── guitar-integration.md     # guitar 集成契约（无代码改动，仅引用 describe 输出）
└── tasks.md                      # Phase 2（/speckit.tasks 生成）
```

### Source Code (repository root)

```text
projects/infra/deploy/
├── deploy.proto                  # 新增 ServiceStatus message + ServiceKind/ServiceRolloutState enum + EnvironmentStatus.services(=5)
├── domain/
│   ├── environment.go            # EnvironmentStatus 增 Services；cloneStatus 深拷贝；新 buildInitialServiceStatuses helper
│   ├── reconcile_types.go        # RolloutStatus 增 Services；新增 ServiceStatus/ServiceKind/ServiceRolloutState 类型
│   └── *_test.go                 # 单测：ServiceStatus 构建/克隆/状态
├── service/
│   ├── reconcile.go              # applyAndWait 写初始 Services；checkRollout 路径持久化 Services（retain/ready/failed）
│   └── reconcile_test.go         # 单测：per-service 状态在各转移路径的持久化
├── runtime/k8s/
│   ├── rollout.go                # CheckRollout 重构：产出 per-service 列表 + 派生 env-level state/message（不再折叠/early-return）
│   └── rollout_test.go           # 单测：断言 got.Services（含 failed 时其余服务状态仍上报）
├── storage/
│   ├── mongo.go                  # mongoStatus 增 Services；TransitionStatus 写入；statusToMongo/statusFromMongo 映射
│   └── mongo_test.go             # 单测：Services 持久化往返
├── handler.go                    # toProtoStatus 增 services 映射；新增 toProtoServices + ServiceKind/State 映射
└── handler_test.go               # 单测：GetEnvironment 返回 services

tools/release/deploy/v3/
├── describe.go                   # printEnvironmentDetail 改为 per-service 主线（每服务一行状态/原因）+ message 次要
└── describe_test.go              # 单测：per-service 状态输出断言

tools/test/guitar/pkg/run/        # 无改动（初版已落地：diagnoseDeployFailure shell-out describe）
```

**Structure Decision**:
- deploy service 侧遵循既有分层（proto → domain → service → runtime → storage → handler），per-service 状态沿同一链路贯穿，每层既有单测同步扩展。
- proto codegen 由 bazel `go_proto_library`（`projects/infra/deploy/BUILD.bazel:58`）驱动，编辑 `deploy.proto` 后 `bazel build`/`bazel test` 自动重新生成 gRPC + gateway + AIP 桩——无需手动 codegen 步骤。
- v2 client（`tools/release/deploy/v2/client/client.go:94`）直接返回生成的 proto `*deploy.Environment`（`protojson` 解码），**proto 新字段自动流经、无需改 client**。
- describe 与 guitar 沿用初版 shell-out 链路（guitar → describe → GetEnvironment），describe 输出增强后 guitar 无需改动。
