# Quickstart: Deploy Health 探针支持

**Feature**: `specs/052-deploy-health-probe/spec.md`

端到端验证指南。契约细节见 `specs/052-deploy-health-probe/contracts/deploy-probe.md`、`contracts/bootstrap-health.md`、`contracts/verification-testplan.md`，不在此重复。

## 0. 前置

- bazel 构建环境可用（`bazel build //...` / `bazel test //...`）。
- testplan skill（`tools/test/guitar`）可用；大型测试规范见 `style/large_test.md`。
- deploy CLI 指向默认 endpoint（`--endpoint http://infra.liukexin.com` 或既有配置）。

## 1. 单元验证（每次代码变更必跑）

```bash
bazel test //common/gopkg/bootstrap/...          # Go 生命周期顺序 / /healthz 200 / 绑定失败回滚
bazel test //projects/infra/deploy/runtime/k8s/...  # builder 探针字段断言（SC-001）
```

期望：全部通过。构建门禁：`bazel build //...`。（JS 侧无共享库单测——本特性不交付 JS 公共库，experimental 服务的自行实现由 §3 大型测试覆盖。）

## 2. 本地快速体验（可选）

任一使用 Go bootstrap 的服务本地运行（如 `bazel run //projects/game/fake-llm`）：

```bash
curl -i http://localhost:38080/healthz    # HTTP/1.1 200 OK, body: ok
```

向进程发送 SIGTERM，日志顺序为：health 停止 → 各组件逆序停止（先进先出，SC-004）。

## 3. 大型测试（宪法 VI 验收，必须实际执行）

加载 testplan skill 后执行（完整部署→测试→清理闭环，全部 case 通过为验收标准）：

```bash
# Suite: 探针就绪路径（grpc_chain：backend Go 自动 health + mid TS 自行实现）
guitar run experimental/grpc_chain/testplan/interface_test.yaml

# Suite: 自愈路径（grpc_hello_world + HEALTH_STOP_AFTER_MS）
guitar run experimental/ts/grpc_hello_world/testplan/<自愈测试计划>.yaml
```

期望：
- 两个 plan 的部署均达 READY（证明 startupProbe 门控下 Go/TS 双侧 health 生效，SC-002/SC-007）；
- 自愈 case 观测到"成功 → 失败窗口（liveness 判死 + 重启）→ 恢复"（SC-003）；
- 清理阶段环境删除成功。

## 4. 负向验证（手动，证明判定依据切换为探针）

部署一个未适配约定的服务（如 `projects/game/agent` 的服务工件）：

```bash
bazel run //tools/release/deploy/v3 -- apply <引用 agent service.yaml 的 deploy.yaml> --run negative-check
bazel run //tools/release/deploy/v3 -- describe <env> --run negative-check
```

期望：`describe` 显示该服务持续 WAITING（startupProbe 失败），环境不 READY；验证后 `deploy del` 清理。此路径不进 guitar（guitar 将部署失败视为 suite 失败）。

## 5. 清单检查（SC-001 抽查，可选）

部署后检查生成的 Deployment YAML 中容器携带契约探针（参数见 `contracts/deploy-probe.md` §2）：

```bash
kubectl -n <env-namespace> get deploy <workload> -o yaml | grep -A6 -E "startupProbe|livenessProbe"
```

期望：startupProbe/livenessProbe 均为 `httpGet: {path: /healthz, port: 38080}` 及契约参数。

## 6. 已知范围外影响（预期行为，非缺陷）

- `projects/game/testplan/system_test.yaml` 全部 suite 部署失败（JS agent 服务未适配，spec FR-009 / research.md D9），由后续适配工作恢复。
