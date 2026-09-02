# Contract: 验证测试计划（testplan）

**Feature**: `specs/052-deploy-health-probe/spec.md`（SC-002/SC-003/SC-007，宪法原则 VI）

验证载体全部来自 `experimental/`（FR-009）。执行入口为 testplan skill：`guitar run <plan.yaml>`，完成部署→测试→清理闭环，全部 case 通过为验收标准。

## 1. Suite: 探针就绪路径（grpc_chain，双语言覆盖）

- **部署**：复用 `experimental/grpc_chain/testplan/deploy.yaml`（backend = Go 自动 health；mid = TS 自行实现 health 端点；gateway 暴露 `apitest.liukexin.com/experimental/grpc-chain`）。
- **断言方式**：部署达到 READY 本身即证明 startupProbe 通过（Go 与 TS 两类副本均被探针门控）；既有 interface case 继续通过证明流量路径不受影响。
- **case**：复用/扩展 `experimental/grpc_chain/testplan/interface_test.go`（`go_largetest`，遵循 `style/large_test.md`）。
- **对应 SC**：SC-002（READY 依赖探针）、SC-007（experimental 载体验证）。

## 2. Suite: 自愈路径（grpc_hello_world + 定时停 health）

- **部署**：`experimental/ts/grpc_hello_world/testplan/` 下新增 deploy yaml（基于既有 `deploy.yaml`），为 service 工件注入 test-only 环境变量 `HEALTH_STOP_AFTER_MS`；该服务（experimental，允许修改）在延时后停止其自行实现的 health 服务，模拟进程挂死。
- **case（Go，新 case binary）**：轮询公共端点（`https://apitest.liukexin.com/experimental/ts/grpc-hello-world/...`）：
  1. 初始阶段请求成功（部署 READY）；
  2. 到达 `HEALTH_STOP_AFTER_MS` 后，观测到至少一次失败（liveness 判死 ≈30s + 容器重启期间端点不可用/网关错误）；
  3. 随后观测到恢复成功（容器重启完成、探针重新通过）。
- **时序约束**：`HEALTH_STOP_AFTER_MS` 与 case 的轮询窗口需覆盖"判死 + 重启"全周期（> 60s 余量）；case 对步骤 2 的观测采用"在时间窗内出现 ≥1 次失败"而非精确时刻（容忍调度抖动）。
- **对应 SC**：SC-003（分钟级自动重启，无需人工干预）。
- **清理**：test-only 环境变量与停 health 逻辑仅存在于 `experimental/ts/grpc_hello_world` 的自身代码，不进入任何共享包（本特性不交付 JS 公共库）。

## 3. 负向路径（未适配服务不 READY，不进 guitar）

guitar 将部署失败视为 suite 失败，无法作为断言载体。负向验证为 quickstart 手动步骤（`deploy apply` 一个未适配服务 → `deploy describe` 观察 WAITING/最终 FAILED）：见 `specs/052-deploy-health-probe/quickstart.md` §4。

## 4. 单元级验证（每次代码变更必跑，宪法原则 IV）

- builder 单测（`projects/infra/deploy/runtime/k8s`）：`BuildDeployment`/`BuildStatefulSet` 容器携带契约探针（SC-001）。
- bootstrap 单测（`common/gopkg/bootstrap`）：生命周期顺序、`/healthz` 200、绑定失败回滚（SC-004/FR-010）。
- JS 侧无共享库单测（本特性不交付 JS 公共库，见 `contracts/bootstrap-health.md` §3）；experimental 服务的自行实现由大型测试端到端覆盖。

## 5. 已知范围外影响（不属本特性验收，见 research.md D9）

`projects/game/testplan/system_test.yaml` 全部 suite 在本特性合入后部署失败（其部署的 JS agent 服务未适配），由后续适配工作恢复。
