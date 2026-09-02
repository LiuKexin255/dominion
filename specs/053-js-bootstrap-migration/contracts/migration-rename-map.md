# Contract: experimental ts→js 更名映射与审计（migration-rename-map）

**Feature**: [spec.md](../spec.md) | **Date**: 2026-09-01 | 依据: [research.md](../research.md) D10

本契约为迁移的**权威映射表**与**终态审计判据**（宪法原则 I/VII：引用可追溯、只表述终态）。实现任务 MUST 按本表逐项变更，MUST NOT 遗漏；表外发现的新引用按同一规则处理并回填本表。

## 1. 标识符映射（old → new）

| # | 类别 | old | new |
|---|--------|-----|-----|
| M1 | 目录 | `experimental/ts/grpc_hello_world` | `experimental/js/grpc_hello_world` |
| M2 | 目录 | `experimental/ts/hello_world` | `experimental/js/hello_world` |
| M3 | 目录 | `experimental/ts/team_graph_spike` | `experimental/js/team_graph_spike` |
| M4 | proto 包名 | `experimental.ts.grpc_hello_world` | `experimental.js.grpc_hello_world` |
| M5 | HTTP 路径 | `/experimental/ts/grpc-hello-world/say-hello`（annotation 前缀） | `/experimental/js/grpc-hello-world/say-hello` |
| M6 | Go importpath | `dominion/experimental/ts/grpc_hello_world` | `dominion/experimental/js/grpc_hello_world` |
| M7 | 服务工件/app 名 | `grpc-hello-world-ts` | `grpc-hello-world-js` |
| M8 | 生成 TS 类型路径 | `../greeter_types/experimental/ts/grpc_hello_world/Greeter.js` | `../greeter_types/experimental/js/grpc_hello_world/Greeter.js`（proto 包名派生，随 M4 自动变化，消费侧同步） |
| M9 | proto 运行时路径 | `experimental/ts/grpc_hello_world/greeter.proto`（server.ts 内拼路径） | `experimental/js/grpc_hello_world/greeter.proto` |
| M10 | OTel reporter service 名 | `grpc-hello-world-ts/service` | `grpc-hello-world-js/service`（M7 派生） |
| M11 | solver URI | `grpc-hello-world-ts/service:grpc` | `grpc-hello-world-js/service:grpc`（M7 派生） |
| M12 | bazel npm label | `@npm//experimental/ts/grpc_hello_world:...` | `@npm//experimental/js/grpc_hello_world:...` |
| M13 | workspace glob | `experimental/ts/*`（pnpm-workspace.yaml） | 删除该条目（`experimental/js/*` 已存在） |
| M14 | bazelignore | `experimental/ts/{grpc_hello_world,hello_world,team_graph_spike}/node_modules` | 对应 `experimental/js/...` 路径 |
| M15 | gateway HTTP 路径前缀 | `/experimental/ts/grpc-hello-world`（deploy yaml `http.matches[].path.value`） | `/experimental/js/grpc-hello-world` |

## 2. 受影响文件清单（按映射项）

| 文件 | 涉及映射 |
|--------|--------|
| `experimental/ts/grpc_hello_world/greeter.proto` | M4、M5 |
| `experimental/ts/grpc_hello_world/BUILD.bazel` | M1（target 路径随目录移动）、M6（`go_proto_library.importpath`、`go_library.importpath`）、M7（`artifact_pkg_js.app`、`artifact_image.app`） |
| `experimental/ts/grpc_hello_world/src/server.ts` | M4（`proto.experimental.js.grpc_hello_world.Greeter.service` 运行时命名空间）、M8、M9（+ 目录移动后 `protoRoot` 相对定位验证） |
| `experimental/ts/grpc_hello_world/src/bootstrap.ts` | M7（`info("service starting", { service: ... })` 日志字段的 app 名）、M10；接入共享 bootstrap 时整体重写（见 [bootstrap-js-api.md](bootstrap-js-api.md) §8） |
| `experimental/ts/grpc_hello_world/smoke_test.sh` | M1、M7（如引用 app 名/路径） |
| `experimental/ts/grpc_hello_world/service.yaml` | M7（`app` 字段）；**greeting 配置值 `"hello from ts config"` 为测试数据，不改** |
| `experimental/ts/grpc_hello_world/testplan/BUILD.bazel` | M1（`binary` label 指向 gateway）、M7（`app` 字段 ×2） |
| `experimental/ts/grpc_hello_world/testplan/deploy.yaml` | M1（artifact `path:` 值）、M15（gateway `http.matches[].path.value` 前缀） |
| `experimental/ts/grpc_hello_world/testplan/deploy_selfheal.yaml` | M1、M15；**`GREETING_SUFFIX: ts-suffix` 为测试数据，不改** |
| `experimental/ts/grpc_hello_world/testplan/interface_test.yaml` | M1（`deploy:`/`cases:` label 路径） |
| `experimental/ts/grpc_hello_world/testplan/interface_test.go` | M5（`pathPrefix`） |
| `experimental/ts/grpc_hello_world/testplan/health_test.go` | M5（`healthPathPrefix`） |
| `experimental/ts/grpc_hello_world/testplan/gateway/main.go` | M6（import）、M11（solver URI）、M7（`phttp.Handler` name `grpc-hello-world-js-gateway` 与日志前缀字符串） |
| `experimental/ts/grpc_hello_world/testplan/gateway/BUILD.bazel` | M1（importpath/deps 路径随移动） |
| `experimental/ts/grpc_hello_world/testplan/gateway/service.yaml` | M1（`artifacts[].target` 路径）、M7（`app` 字段） |
| `experimental/ts/hello_world/**` | 仅 M2（目录移动；BUILD 由 gazelle 重生成） |
| `experimental/ts/team_graph_spike/**` | M3；`src/bootstrap.ts` 接入共享 bootstrap；`FINDINGS.md` 内路径引用同步 |
| `pnpm-workspace.yaml` | M13 |
| `.bazelignore` | M14 |
| `tools/dev/js/BUILD.bazel` | M12 |
| `projects/game/agent/src/context-middleware.ts` | 注释内 M1 路径引用 |
| `projects/game/agent/src/team/graph.test.ts` | 注释内 M3 路径引用 |
| `experimental/js/vite_react_demo/BUILD.bazel`、`experimental/js/vite_react_demo/dist_assert.sh` | 注释内 M1 参考路径引用 |

**不改清单（终态排除项）**：`specs/` 目录全部历史文档（历史记录）；测试数据值（§2 已注明两处）；`git` 历史。

## 3. 终态审计判据（零命中）

```bash
# 路径与 proto 包残留（历史 spec 文档除外）
rg -l 'experimental/ts|experimental\.ts\.' \
  -g '!specs/**' -g '!node_modules' -g '!pnpm-lock.yaml' \
  common projects experimental tools third_party
# 工件名残留
rg -l 'grpc-hello-world-ts' \
  -g '!specs/**' -g '!node_modules' -g '!pnpm-lock.yaml' \
  common projects experimental tools third_party
# 目录实体消失
test ! -d experimental/ts
# workspace 无 ts glob
! rg -q 'experimental/ts' pnpm-workspace.yaml
```

## 4. 迁移后必做的再生成步骤

1. `pnpm` 刷新 lockfile（workspace 包路径变化；`bazel run @pnpm -- --dir <abs-repo-root> install` 或按 `AGENTS.md` 的 `pnpm` 流程）。
2. `bazel run //:gazelle experimental/js common/js/bootstrap`（移动/新增目录的 BUILD 重生成）。
3. `bazel mod tidy` + `bazel build //...` + `bazel test //...` 门禁（原则 IV）。
