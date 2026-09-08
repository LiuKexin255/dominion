# Contract: 静态页面服务载体与部署声明

**Feature**: [050-vite-react-bazel](../spec.md) | 宪法 §III（接口优先设计）

本契约固定两套对外接口：**(1) 静态页面服务载体的 bazel target 形态**（前端 dist tree artifact → Go embed → 服务镜像），**(2) deploy 工具的部署消费方式**（`service.yaml`/`deploy.yaml` 字段与部署/清理/访问入口）。demo（`experimental/js/vite_react_demo/`）是该契约的活样板；`specs/049-agent-v2-dsh-init` 的 web 服务（自身 HTTP 直接 serve 前端页面，其 spec FR-013）是第一个计划中的同形态后续消费方。

全部机制均为仓库既有设施，本契约零新建构建规则（`tools/dev/js/vite.bzl`/`vitest_test.bzl` 零修改红线不受影响）。

## 接口 1：静态页面服务载体（bazel targets）

### 1.1 embed 资产库（消费 `:dist`）

```python
# experimental/js/vite_react_demo/server/assets/BUILD.bazel
load("//tools/release/wails:defs.bzl", "wails_asset_library")

wails_asset_library(
    name = "assets",
    src = "//experimental/js/vite_react_demo:dist",   # vite_build 产物 tree artifact
    importpath = "dominion/experimental/js/vite_react_demo/server/assets",
    visibility = ["//experimental/js/vite_react_demo/server:__pkg__"],
)
```

| 约定 | 内容 | 依据 |
|------|------|------|
| 机制 | `wails_asset_library`：stage dist 进包 → 生成 `//go:embed all:frontend_dist` 的 `assets.go` → `go_library` | `tools/release/wails/private/assets.bzl`；消费先例 `projects/game/desktop/assets/BUILD.bazel` |
| 产物变量 | 包级 `FrontendDist embed.FS`，内容位于 `frontend_dist/` 前缀下（规则默认 `out`） | `tools/release/wails/helpers/generate_assets_go.go`（模板 `//go:embed all:{dir}` + `var {Var} embed.FS`） |
| visibility | 包级授权 MUST 使用 `:__pkg__` 后缀：`["//experimental/js/vite_react_demo/server:__pkg__"]`——省略 `:__pkg__` 的 `//pkg` 短格式会被 bazel 解析为同名 target 而非包，报 `does not refer to a package group` | `experimental/dsh/demo/agent/BUILD.bazel:25`（`//pkg:__pkg__` 先例） |
| 独立子目录 | embed 库 MUST 位于独立 BUILD 包（`server/assets/`），importpath 与物理目录一致 | `//go:embed` 模式不允许 `..`（[Go embed §Directives](https://pkg.go.dev/embed)），跨包 dist 必须经 stage 进包；包路径须真实存在以供消费方 resolve 指令映射（§1.2） |

### 1.2 服务 target（Go 二进制 + 镜像）

```python
# experimental/js/vite_react_demo/server/BUILD.bazel
# gazelle:resolve go dominion/experimental/js/vite_react_demo/server/assets //experimental/js/vite_react_demo/server/assets:assets

load("@rules_go//go:def.bzl", "go_binary", "go_library")
load("//tools/dev/go:defs.bzl", "go_unittest")
load("//tools/release:defs.bzl", "artifact_image", "artifact_pkg_go")

go_library(                # gazelle 生成；deps 含 embed 库与 common/gopkg 基建
    name = "server_lib",
    srcs = ["main.go"],
    importpath = "dominion/experimental/js/vite_react_demo/server",
    visibility = ["//visibility:private"],
    deps = [
        "//common/gopkg/bootstrap",
        "//common/gopkg/http",
        "//common/gopkg/otel",
        "//experimental/js/vite_react_demo/server/assets",
    ],
)

go_binary(
    name = "server",
    embed = [":server_lib"],
    visibility = ["//visibility:public"],
)

go_unittest(               # 仓库单测惯例 wrapper（非裸 go_test），表驱动断言静态托管行为
    name = "server_test",  # gazelle 默认名（{package_name}_test）
    srcs = ["main_test.go"],
    embed = [":server_lib"],
    deps = ["//experimental/js/vite_react_demo/server/assets"],
)

artifact_pkg_go(     # 打包为 tar 层（/dominion/{app}/{service}/bin/server）
    name = "server_pkg",
    app = "vite-react-demo",
    binary = ":server",
    service = "server",
)
artifact_image(      # OCI 镜像（distroless base，ENTRYPOINT 即二进制）
    name = "cmd_image",
    app = "vite-react-demo",
    pkg = ":server_pkg",
    service = "server",
)
```

| 约定 | 内容 | 依据 |
|------|------|------|
| gazelle 依赖解析 | 消费方 BUILD 顶部 MUST 声明 `# gazelle:resolve go <importpath> <label>` 将 embed 库映射到 target——gazelle 无法自动解析生成型 embed 库（wails_asset_library 产出的 go_library）的 importpath | 同构先例 `projects/game/desktop/BUILD.bazel:6`（wails assets 库 resolve）；本 demo 落点 `experimental/js/vite_react_demo/server/BUILD.bazel:1` |
| 单测 target | `go_unittest`（`go_test` 的仓库 wrapper：注入 `-test.v`、默认 `size = "small"`），MUST NOT 使用裸 `go_test`；target 名用 gazelle 默认名 `{package_name}_test`（防 gazelle 重复生成 `go_unittest`） | `tools/dev/go/defs.bzl:17`；`style/golang.md` §单元测试 |
| 服务形态 | 最小静态托管：`fs.Sub(FrontendDist, "frontend_dist")` → `http.FileServerFS`；无 API/路由/持久化 | fake-llm 样板 `experimental/dsh/demo/fake-llm/cmd/main.go`；`http.FileServerFS` 接受 `fs.FS`（[embed.FS 实现 fs.FS](https://pkg.go.dev/embed)） |
| 进程基建 | `phttp.Handler`（otelhttp 包装）+ `bootstrap.HTTPServer` + `otel.Component()`；监听端口 `:8080`（flag 默认） | `common/gopkg/http/default.go`、`common/gopkg/bootstrap/http.go`；`style/golang.md` §可观测 |
| 无健康端点 | deploy 生成的 artifact 服务 Deployment 无探针（进程监听即就绪），MUST NOT 添加 `/health` 之外的机制 | `projects/infra/deploy/runtime/k8s/builder.go`（BuildDeployment 无 probe）；原则 II 最小化 |
| 打包命名 | `app = "vite-react-demo"`、`service = "server"`（镜像仓库 `registry.liukexin.com/vite-react-demo/server`） | `tools/release/defs.bzl` §服务镜像构建 |

## 接口 2：部署声明（deploy 工具消费）

### 2.1 `service.yaml`（服务产物声明）

```yaml
# experimental/js/vite_react_demo/server/service.yaml
version: "3.0"
name: server
app: vite-react-demo
kind: stateless
desc: vite react demo static web server
ports:
  - name: http        # deploy.yaml http.matches.backend 引用此端口名
    port: 8080
artifacts:
  - name: server      # deploy.yaml artifact.name 引用此产物名
    target: :cmd_image
```

deploy CLI 校验 `service.yaml` 的 `app`/`name` 与 `artifact_image` 声明一致（`tools/release/deploy/README.md` §Go 服务）。

### 2.2 `deploy.yaml`（环境部署声明，demo 根）

```yaml
# experimental/js/vite_react_demo/deploy.yaml
version: "3.0"
name: vite.demo       # 固定环境名（无 {{run}}）；{scope}.{env} 各段 ^[a-z][a-z0-9]{0,7}$
type: prod            # prod = hostname+path 直接访问（免 header）；见下方访问语义
desc: "vite react demo 静态页面环境（浏览器直连人工验证入口）"
services:
  - artifact:
      path: //experimental/js/vite_react_demo/server/service.yaml
      name: server
    http:
      hostnames:
        - vite-react-demo.liukexin.com
      matches:
        - backend: http
          path:
            type: PathPrefix
            value: /
```

### 2.3 部署 / 访问 / 清理

| 操作 | 命令 | 语义 |
|------|------|------|
| 部署 | `deploy apply //experimental/js/vite_react_demo/deploy.yaml` | 自动构建并推送镜像（registry.liukexin.com）→ 提交环境 → 等待就绪；**部署后保持运行**。前置：`bazel run //:deploy_install` 安装 deploy CLI；registry 推送凭证可用 |
| 访问 | `https://vite-react-demo.liukexin.com/` | 浏览器免 header 直连（见下） |
| 清理 | `deploy del vite.demo` | 删除环境与路由；人工验证完成后执行（保持页面可访问时可暂缓） |

**访问语义（type 选择依据）**：deploy service 对 `test`/`dev` 型环境的 HTTPRoute 强制注入 `env` header 精确匹配（值 = 完整环境名，`projects/infra/deploy/runtime/k8s/builder.go:669-676`），浏览器无法携带；`prod` 型按 hostname+path 直接访问（`tools/release/deploy/README.md` §环境类型）。本 demo 环境的 `type: prod` 仅表示免 header 直连路由模式，`vite.demo` 为 demo 性质环境，非业务生产环境。独立 demo hostname 依赖 `*.liukexin.com` 通配解析（先例 `hello.liukexin.com`、`mongo-demo.liukexin.com`）。

## 与 guitar / testplan 的关系

- **本 feature 不创建 guitar 测试计划**：web E2E 用例暂缓（用户决策 2026-08-27，[spec.md](../spec.md) FR-009）；guitar run 为"校验→部署→用例→清理"闭环（suite 用例必填 `tools/test/guitar/pkg/validate/validate.go:87`、执行后强制 `deploy del` `tools/test/guitar/pkg/run/run.go:137-149`），不承载"仅部署供人工访问"形态，故部署入口为同源工具链的 deploy CLI（调研依据 [research.md](../research.md) D8）。
- **未来 web E2E 接入方式（不改服务形态）**：新建 `testplan/deploy.yaml`（`type: test`、`name: {scope}.{{run}}`，引用同一 `service.yaml`）+ guitar plan（`testplan/` 目录），测试代码经 `env` header 访问测试路由——`service.yaml` 与服务镜像零变更。

## 消费方

- demo：`experimental/js/vite_react_demo/`（本 feature 交付，作为契约实证）
- 计划中：`specs/049-agent-v2-dsh-init` web 服务（自身 HTTP serve 前端页面，按本契约形态扩展 API 能力）
