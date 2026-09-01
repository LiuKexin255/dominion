# Research: Deploy Health 探针支持

**Feature**: `specs/052-deploy-health-probe/spec.md` | **Date**: 2026-09-01

本文档记录方案决策（Decision / Rationale / Alternatives）。引用来源：仓库内使用相对路径，仓库外使用完整 URL。

## 背景与现状

- deploy 工具链：CLI（`tools/release/deploy/v3/`）→ deploy 服务（`projects/infra/deploy/`）→ k8s 运行时（`projects/infra/deploy/runtime/k8s/`）。用户服务的 Deployment/StatefulSet 由 `builder.go` 生成；当前**无任何探针**（仅 Mongo 基础设施有 TCP 探针，`builder.go:764-768`、`builder.go:804-809`）。
- rollout 判定（`projects/infra/deploy/runtime/k8s/rollout.go`）基于 k8s 原生 `AvailableReplicas` 等状态；无探针时"容器 Running 即就绪"。
- Go bootstrap（`common/gopkg/bootstrap/bootstrap.go`）：`RunSignal` 按 Stage+Name 排序启动组件、失败回滚、逆序停止（`bootstrap.go:91-156`）。
- JS 无共享 bootstrap 运行时：各服务自带 `bootstrap.ts`（如 `projects/game/agent_v2/src/bootstrap.ts`），共享包（`common/js/*`）仅提供工具函数。
- 端口 38080 与路径 /healthz 在仓库中无任何占用。

## D1: 探针落点 — 仅在 k8s builder 附加，不动 proto/config/compiler/schema

**Decision**: 在 `projects/infra/deploy/runtime/k8s/builder.go` 的 `BuildDeployment`（容器字面量 `builder.go:273-279`）与 `BuildStatefulSet`（`builder.go:441-447`）中为用户服务容器附加 `StartupProbe` 与 `LivenessProbe`（HTTP GET，端口 38080、路径 /healthz）。不改 `deploy.proto`、不改 service.yaml/deploy.yaml schema、不改 compiler/converter、不改 rollout/executor。

**Rationale**: 端口与路径是固定约定（spec FR-001/FR-002），无需任何配置面；builder 是唯一生成容器定义的位置（Mongo 探针先例即在此）。rollout 判定零修改（FR-003）：k8s 在有 startupProbe 时以探针成功作为就绪门槛，`AvailableReplicas` 自然被探针门控（见 D4）。executor 的 apply 为"全量期望对象更新"（`executor.go:955-1007`），探针字段自动随 apply/update 生效。

**Alternatives considered**:
- 经 proto/schema/config 全链路增加可配置探针字段（参照 `specs/045-deploy-config/contracts/proto.md` 的 `config_blocks = 12` 先例）——否决：违反"固定约定、无服务侧声明、无开关"（FR-002/FR-008），且引入大量无消费者 的配置面。
- CLI/deploy.yaml 级开关——否决：同上。

## D2: 探针参数 — startup 预算 300s，liveness 用 k8s 默认

**Decision**:
- `startupProbe`: `httpGet { path: /healthz, port: 38080 }`，`periodSeconds: 10`，`failureThreshold: 30` → 启动预算 300s。
- `livenessProbe`: `httpGet { path: /healthz, port: 38080 }`，`periodSeconds: 10`，`failureThreshold: 3`（≈30s 判死）。`initialDelaySeconds` 不设（startupProbe 成功前 liveness 不开始计时）。

**Rationale**: 300s 预算 = k8s 官方文档的范例配置（"the application will have a maximum of 5 minutes (30 * 10 = 300s) to finish its startup"，[Configure Liveness, Readiness and Startup Probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/)）；liveness 使用接近默认值的保守参数，遵循官方对 liveness 的级联失败告诫（"Liveness probes must be configured carefully…"，[Probe concepts](https://kubernetes.io/docs/concepts/workloads/pods/probes/)）。参数显式写出的目的是契约清晰（见 `contracts/deploy-probe.md`）。

**Alternatives considered**: 更紧的 liveness（period 5s×3）——否决：检测窗口已足够（分钟级，SC-003），更紧易在高负载下误杀；更大的 startup 预算（如 600s）——否决：300s 已覆盖仓库现有最慢启动（agent_v2 DSH init 为秒级），预算过大延长失败反馈。

## D3: 不声明 38080 为 containerPort

**Decision**: 探针直接以端口号 38080 为目标，不在容器的 `ports` 列表中声明 38080。

**Rationale**: k8s HTTP 探针的 `port` 接受端口号且**不要求**在 `containerPorts` 声明（[Probe concepts — HTTP probes](https://kubernetes.io/docs/concepts/workloads/pods/probes/#http-probes)："port: Name or number of the port to access on the container"）；`buildContainerPorts`（`builder.go:1068`）同时供 Service ports 使用，声明 38080 会污染 Service 定义。且 FR-008 禁止端口相关校验——不声明即无需处理与业务端口声明的冲突。

**Alternatives considered**: 声明具名 containerPort `health`——否决：会进入 Service ports 生成逻辑，需区分"仅容器可见"，复杂度无收益。

## D4: 不引入 readinessProbe（含语义论证）

**Decision**: 仅 startupProbe + livenessProbe（用户明确要求）。rollout 判定依据的变化来自 k8s 原生语义：

- "If a startup probe is configured, Kubernetes does not execute liveness or readiness probes until the startup probe succeeds"（[Probe concepts](https://kubernetes.io/docs/concepts/workloads/pods/probes/)）——启动期就绪被 startupProbe 门控。
- "If a container does not provide a particular probe, the kubelet always considers the result as Success"——无 readinessProbe 时，startup 成功后容器即 Ready，`AvailableReplicas` 增长；运行期的可用性由 liveness 失败→重启（期间 Not Ready）间接保障。

**Rationale**: 与 deploy 现有 rollout 逻辑（`rollout.go` 的 `AvailableReplicas == replicas` 判定）完全兼容，实现 FR-003"检查逻辑零修改"。

**Alternatives considered**: 同时加 readinessProbe——否决：超出用户明确范围；对"持续就绪"的额外收益在本仓库流量模型下有限（Service 仅 gateway 暴露）。

## D5: Go health — bootstrap 核心行为（非 Component）

**Decision**: 在 `common/gopkg/bootstrap` 新增 health 服务器（新文件 `health.go`），作为 `RunSignal` 的核心生命周期步骤：
1. 全部组件启动成功后（`bootstrap.go:112-125` 的启动循环之后、进入等待之前）启动 health（`:38080`，`/healthz` → 200）；
2. 启动失败（如端口占用）视同组件启动失败：执行 `b.shutdown(started)` 回滚并返回错误（FR-010）；
3. 关闭路径（`bootstrap.go:145-149`）先停 health，再执行 `b.shutdown(started)`（FR-005，先进先出）；
4. 端口/路径硬编码，不提供 Option（宪法 II 简洁性；FR-004 自动生效）。

**Rationale**: health 必须是"所有组件之后启动、所有组件之前停止"。若作为普通 Component 注册，排序受 Stage+Name 约束，无法保证绝对最后/最前（其他 `StageServer` 组件按名字排序可能排在后面）。作为 `RunSignal` 内嵌步骤是唯一能结构性保证顺序的位置。监听 `":38080"`（非 loopback）——kubelet 通过 Pod IP 探测（[HTTP probes](https://kubernetes.io/docs/concepts/workloads/pods/probes/#http-probes)）。

**Alternatives considered**:
- 注册为 `StageServer` 组件——否决：顺序无结构保证（见上）；
- 提供 `WithHealth…` Option——否决：增加配置面违背固定约定与自动生效；
- import 副作用启动——否决：无法感知"全部组件启动完成"时刻。

**测试注意**: bootstrap 包的单测顺序执行（同包测试串行），每次 Run 绑定/释放 38080 不冲突；跨进程并行的测试目标不运行 `RunSignal`。

## D6: JS 侧不交付公共库 — 验证服务自行实现（共享库延后）

**Decision**: 本特性**不**新建 `common/js/health` 共享包。统一的 JS bootstrap 公共库（提供 health 等生命周期能力）为后续独立工作。用于验证的 `experimental/` JS 服务（`experimental/grpc_chain/mid`、`experimental/ts/grpc_hello_world`）在其自身代码（`bootstrap.ts`）中按 `contracts/bootstrap-health.md` §3 的约定**自行实现** health 端点（`node:http`，`:38080`，`/healthz`；服务组件启动完成后启动、关闭链首位停止、启动失败即退出）。

**Rationale**: 用户在 plan 阶段决策——避免本特性产出将被统一 JS bootstrap 公共库取代的一次性公共 API（宪法 II：面向终态设计，减少未来收敛成本）；experimental 服务的自行实现仅为验证载体，后续随公共库落地由其取代。`projects/game/agent`、`agent_v2` 不实现（FR-009，接受 `system_test.yaml` 全部 suite 部署失败为已知范围外后果）。

**Alternatives considered**:
- 新建共享包 `@dominion/common-js-health`——否决（用户决策：公共库统一后续建设）；
- 新建共享 JS bootstrap 运行时（对齐 Go 的 New/Register/Run）并迁移服务——否决：同属后续统一工作。

## D7: 验证载体与大型测试设计

**Decision**:
- **主载体** `experimental/grpc_chain`（`testplan/deploy.yaml` 一份部署同时覆盖 backend（Go，自动 health）+ mid（TS，自行实现 health 端点）+ gateway），复用现有 `interface_test.yaml` 的 suite 形态。
- **自愈验证** `experimental/ts/grpc_hello_world`：新增 test-only 环境变量（如 `HEALTH_STOP_AFTER_MS`）在指定延时后停止 health 服务（实验性服务允许修改，FR-009），新增 case 轮询公共端点，观测"故障窗口 → 恢复"（liveness 判死 ≈30s + 重启秒级）证明端到端自愈（SC-003）。
- **SC-001（清单 100% 携带探针）**：builder 单测断言 Deployment/StatefulSet 容器包含探针字段与参数。
- **未适配负向路径**（Story 3 场景 2）：quickstart 手动步骤（`deploy apply` 未适配服务 → `deploy describe` 观察 WAITING/FAILED），不进 guitar（guitar 将部署失败视为 suite 失败，无法作为断言）。
- 宪法 VI 验收：实际执行 `guitar run <plan>`（部署→测试→清理闭环），全部 case 通过。

**Rationale**: 用户指定用 `experimental/` 服务验证；grpc_chain 是唯一同时覆盖双语言 health 的既有部署；自愈的黑盒可观测信号是公共端点的瞬时失败与恢复。

**Alternatives considered**: 在 guitar 内断言"未适配服务不 READY"——否决（机制冲突，见上）；用 `projects/game` 服务验证——否决（FR-009）。

## D8: 文档 — deploy README 增加"健康探针约定"章节

**Decision**: 在 `tools/release/deploy/README.md` 打包规范章节（`README.md:142-156`）之后新增"健康探针约定"：端口 38080、路径 /healthz、bootstrap 自动提供、未适配服务不就绪的风险、参数值。约定为文档性质，不进入 schema（FR-008）。

**Rationale**: 约定的唯一权威落点应与容器行为约定（打包规范、保留环境变量 `README.md:436-451`）同处。

## D9: 已知范围外后果（用户决策记录）

**Decision**: `projects/game/agent`、`agent_v2` 不实现 health 端点（不修改 service 代码）；`experimental/` 之外的服务代码零修改（FR-009）。探针上线后：
- `projects/game/testplan/system_test.yaml` 全部 13 个 suite（`deploy_agent.yaml`/`deploy_agent_stall.yaml`/`deploy_agent_v2.yaml` 均部署 JS agent 服务）在部署阶段失败。

**Rationale**: 用户在 plan 阶段明确选择（见 spec Clarifications 2026-09-01 会话）。后续适配为独立工作。Go 服务（session/gateway/memory/prompt/proxy/fake-llm/web/deploy-manager 及 experimental Go 服务）经 D5 自动覆盖，不受影响；其中 `specs/050-vite-react-bazel` 的静态文件服务（`experimental/js/vite_react_demo/server`）同为 Go + 共享 bootstrap，自动获得约定端点，部署仍可就绪，无需适配。

## D10: health 端点响应语义

**Decision**: `/healthz` 返回 HTTP 200、极小响应体（如 `ok\n`）。不做组件级深度检查（FR-007）。

**Rationale**: health 语义为"所有组件已启动且进程存活"（由生命周期顺序保证，D5/D6）。k8s 文档建议探针端点返回最小响应体（kubelet 仅读 10KiB，[HTTP probes](https://kubernetes.io/docs/concepts/workloads/pods/probes/#http-probes) 的 Caution）。

## 阅读清单（供 tasks 阶段引用）

- k8s 官方：[Configure Liveness, Readiness and Startup Probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/)、[Probe concepts](https://kubernetes.io/docs/concepts/workloads/pods/probes/)
- 仓库内：`common/gopkg/bootstrap/bootstrap.go`、`common/gopkg/bootstrap/http.go`、`projects/infra/deploy/runtime/k8s/builder.go`、`experimental/grpc_chain/testplan/`、`experimental/ts/grpc_hello_world/testplan/`、`style/large_test.md`、`tools/release/deploy/README.md`
