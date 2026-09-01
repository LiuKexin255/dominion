# Contract: bootstrap health 服务（Go / JS）

**Feature**: `specs/052-deploy-health-probe/spec.md`（FR-004/FR-005/FR-006/FR-007/FR-010）

## 1. 共同约定（两语言一致）

- **端点**：监听 `:38080`（非 loopback——kubelet 经 Pod IP 探测），`GET /healthz` 返回 HTTP 200、响应体 `ok\n`；其余路径 404。
- **语义**：200 表示"所有组件已启动且进程存活"。不做组件级深度检查（FR-007）。
- **生命周期（先进先出）**：
  - start 时机：所有组件启动成功**之后**；
  - stop 时机：任何组件停止**之前**（关闭序列首位）。
- **启动失败语义（FR-010）**：health 启动失败（如端口 38080 被占用）视同组件启动失败——回滚已启动组件并退出进程，不得以无 health 端点的状态继续运行。
- **回滚场景**：组件启动失败时 health 尚未启动（idle），无残留监听。
- **无校验**：不检查端口冲突/声明（FR-008）；无配置项、无开关。

## 2. Go — `common/gopkg/bootstrap`（核心行为，无新增公开 API）

`RunSignal`（`common/gopkg/bootstrap/bootstrap.go`）的终态编排序列：

```text
1. 组件排序并依次启动（失败 → 回滚已启动组件并返回错误）      // 既有行为
2. 启动 health（失败 → 回滚已启动组件并返回错误，FR-010）     // 新增
3. 等待退出触发（信号 / ctx 取消 / 组件退出）                 // 既有行为
4. 停止 health（首位）                                        // 新增
5. 逆序停止全部组件（统一 deadline）                          // 既有行为
```

- health 实现为包内私有（新文件 `health.go`）：`net/http` 服务器，`:38080`，`/healthz` handler。
- 步骤 2 的失败日志须包含失败原因（如绑定错误），语义与组件启动失败日志一致。
- 步骤 4 使用与组件停止相同的关闭超时预算。
- 日志：health 启动/停止使用包内既有 `logs.Info` 风格，字段含端口。
- 使用方（各服务 `main.go`）**零改动**自动获得（FR-004/SC-004 通过单测验证顺序）。

## 3. JS — 服务自行实现（本特性）

统一的 JS bootstrap 公共库（提供 health 能力）为后续独立工作，不在本特性范围。本特性中用于验证的 JS 服务在自身 `bootstrap.ts` 中自行实现，MUST 满足 §1 的全部共同约定（端点、语义、先进先出生命周期、失败语义、无校验）：

- 实现：`node:http` 服务器，监听 `:38080`，`GET /healthz` → 200 `ok\n`。
- 接入位置：启动链尾部（服务组件启动完成后）启动；关闭链头部（先于 server/otel 等既有停止步骤）停止。
- 启动失败（如 `EADDRINUSE`）：按启动失败处理（记录原因并退出，对齐 FR-010）。

**实现范围（FR-009）**：仅 `experimental/` JS 服务——`experimental/grpc_chain/mid/src/bootstrap.ts`、`experimental/ts/grpc_hello_world/src/bootstrap.ts`。`projects/game/agent`、`projects/game/agent_v2` 不实现。

## 4. 验证

- Go 单测（`common/gopkg/bootstrap`）：启动顺序（health 晚于全部组件）、停止顺序（health 早于全部组件）、`/healthz` 200、绑定失败 → 回滚 + 错误返回（FR-010）。
- JS 侧无共享包单测（本特性不交付公共库）；experimental 服务的自行实现由大型测试端到端覆盖。
- 端到端：见 `specs/052-deploy-health-probe/contracts/verification-testplan.md`。
