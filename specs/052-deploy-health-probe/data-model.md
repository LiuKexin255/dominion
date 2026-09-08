# Data Model: Deploy Health 探针支持

**Feature**: `specs/052-deploy-health-probe/spec.md`

本特性无持久化数据。数据模型描述契约级实体：探针约定、探针参数集、health 服务生命周期。字段引用 `specs/052-deploy-health-probe/contracts/deploy-probe.md` 与 `contracts/bootstrap-health.md`。

## 实体

### HealthEndpointConvention（Health 端点约定）

固定约定（非配置、不校验，FR-008）。

| 属性 | 值 |
|----------|-----|
| port | `38080` |
| path | `/healthz` |
| method | HTTP GET |
| 成功判定 | 响应状态码 ∈ [200, 400)（k8s HTTP 探针语义） |
| 成功语义 | 所有组件已启动且进程存活（FR-007，不做深度检查） |
| 响应体 | 极小（`ok\n`） |

### ProbePair（探针参数集，deploy 生成侧）

附加到用户服务容器（Deployment 与 StatefulSet 同等），值为固定契约（无配置项）。

| 字段 | startupProbe | livenessProbe |
|------|--------------|---------------|
| httpGet.path | `/healthz` | `/healthz` |
| httpGet.port | `38080` | `38080` |
| periodSeconds | `10` | `10` |
| failureThreshold | `30` | `3` |
| initialDelaySeconds | 不设置（0） | 不设置（0） |
| timeoutSeconds | 默认（1） | 默认（1） |
| 生效语义 | 启动预算 `10 × 30 = 300s`；成功前 liveness 不执行 | startup 成功后接管；连续失败 3 次（≈30s）容器被杀并按 restartPolicy 重启 |

**不变式**：不声明 `containerPort: 38080`（探针直接按端口号寻址）；不进入 Service ports；deploy 工具状态检查/rollout 判定逻辑零修改。

### HealthServerLifecycle（health 服务生命周期）

Go bootstrap 核心行为与 JS helper 共同遵循的状态机：

```text
idle ──start(全部组件启动成功后)──▶ running ──stop(关闭序列首位)──▶ stopped
  │                                    │
  └─ start 失败（如 EADDRINUSE）        └─ 进程退出信号（SIGTERM/SIGINT/组件退出）
     → 视同组件启动失败                  → stop 先于任何组件 Stop 执行
       （回滚已启动组件并退出，FR-010）
```

**顺序不变式（先进先出，FR-004/FR-005/FR-006）**：
- `health.start` 严格晚于所有 `component.start`；
- `health.stop` 严格早于所有 `component.stop`；
- 组件启动失败回滚时 `health` 尚处 `idle`，不参与回滚、无残留监听。

### GoBootstrapRunner 扩展（`common/gopkg/bootstrap`）

`RunSignal` 编排步骤的终态序列（无新增公开 API，health 为内部实现）：

```text
sort → start(各组件，失败即回滚) → start health(失败即回滚+退出)
     → wait(信号/ctx/组件退出) → stop health → stop(各组件逆序)
```

### JS 实现形态（本特性：experimental 服务自行实现）

统一 JS bootstrap 公共库为后续独立工作（spec FR-006）。本特性中用于验证的 JS 服务（`experimental/`）在自身 `bootstrap.ts` 中按同一 `HealthServerLifecycle` 状态机自行实现：

- 启动链尾部启动 health 服务（`:38080`，`/healthz`）；
- 关闭链头部停止 health 服务；
- 启动失败按启动失败退出（FR-010 语义）。

## 验证规则（来自需求）

- 约定不校验：仓库内 MUST NOT 存在针对 38080 的冲突/声明/编译期校验代码（FR-008/SC-006）。
- deploy 生成侧唯一权威参数来源为 `contracts/deploy-probe.md`；修改参数即修改契约（需同步 builder 单测与 README）。
