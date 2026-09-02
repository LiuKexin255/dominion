# Contract: deploy 探针生成

**Feature**: `specs/052-deploy-health-probe/spec.md`（FR-001/FR-002/FR-003/FR-008）

## 1. 范围

deploy 工具为**用户服务**（artifact）生成的工作负载附加探针：

- `BuildDeployment`（`projects/infra/deploy/runtime/k8s/builder.go`，stateless）
- `BuildStatefulSet`（同文件，stateful）

不涉及：Mongo 等基础设施组件（维持既有 TCP 探针）、deploy 服务自身部署清单（`projects/infra/deploy/k8s.yaml`）。

## 2. 生成内容（固定契约，无配置面）

每个用户服务容器的容器定义中附加：

```yaml
startupProbe:
  httpGet:
    path: /healthz
    port: 38080
  periodSeconds: 10
  failureThreshold: 30
livenessProbe:
  httpGet:
    path: /healthz
    port: 38080
  periodSeconds: 10
  failureThreshold: 3
```

- `initialDelaySeconds` 不设置（startupProbe 成功前 liveness/readiness 不执行，无需初始延迟）。
- `timeoutSeconds`、`successThreshold` 使用 k8s 默认（1/1）。
- **不**在 `containerPorts` 声明 38080，不进入 Service ports。
- 参数值唯一权威来源为本契约；修改参数 = 修改契约，须同步 builder 单测与 `tools/release/deploy/README.md` 健康探针约定章节。

## 3. 行为语义

- **就绪判定**：startupProbe 成功前容器不计入 Ready（k8s 语义：startup 成功前其他探针不执行、无 readinessProbe 时 Ready 以 startup 成绩为准）；startup 成功后 Running 即 Ready。`AvailableReplicas` 因此被探针门控。
- **启动预算**：`10s × 30 = 300s`。超时容器被杀并按 restartPolicy 重启。
- **运行期自愈**：liveness 连续失败 3 次（≈30s）容器被杀并重启，重启期间该副本 Not Ready。
- **未适配服务**：不提供 `/healthz:38080` 的服务（如 `projects/game/agent`、`agent_v2`——见 spec FR-009）startupProbe 持续失败 → rollout 不 READY（WAITING，按既有语义最终 FAILED）。

## 4. 零修改边界（FR-003/SC-005）

以下路径**不得**因本特性修改：`deploy.proto`、service.yaml/deploy.yaml schema 与解析（`tools/release/deploy/pkg/config/`）、compiler（`tools/release/deploy/v2/compiler/`）、converter/model、`projects/infra/deploy/runtime/k8s/rollout.go`、executor apply 逻辑、CLI（`tools/release/deploy/v3/`）。

## 5. 校验禁止（FR-008/SC-006）

MUST NOT 新增任何端口相关校验：不检查 38080 冲突、不要求服务声明 health 端口、不在 schema/编译期/CLI 中出现 38080 相关校验逻辑。约定仅记录于 `tools/release/deploy/README.md`（健康探针约定章节，位于打包规范之后）。

## 6. 验证

- builder 单测：`BuildDeployment`/`BuildStatefulSet` 产出的容器包含 §2 的探针字段与参数（SC-001）。
- 大型测试（就绪路径）：见 `specs/052-deploy-health-probe/contracts/verification-testplan.md`。
