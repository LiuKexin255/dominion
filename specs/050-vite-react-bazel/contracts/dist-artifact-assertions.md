# Contract: Dist 产物断言（sh_test）

**Feature**: [050-vite-react-bazel](../spec.md) | 宪法 §III（接口优先设计）

`sh_test` 产物断言测试的接口契约：输入 `vite_build` 的 dist tree artifact，输出进程退出码（0 = 断言全过，非 0 = 至少一条断言失败）。载体先例：`experimental/ts/grpc_hello_world/BUILD.bazel:115` + `smoke_test.sh`（rules_shell `sh_test` + `$(location)` + runfiles 定位）。

## 接口

**Target 声明形态**（demo 内）：

```python
sh_test(
    name = "dist_assert_test",
    srcs = ["dist_assert.sh"],
    args = ["$(location :dist)"],
    data = [":dist"],
    deps = ["@bazel_tools//tools/bash/runfiles"],
)
```

**输入**：argv 中的 dist 目录路径（经 `$(location :dist)` 展开为 tree artifact 路径）。

**输出**：退出码（bash `set -euo pipefail`；任一断言失败即非 0，错误信息打印到 stdout/stderr 指明失败断言编号）。

## 断言清单（binding）

| 编号 | 断言 | 对应 spec |
|------|------|-----------|
| A1 | 产物根存在 `index.html`，且为非空文件 | FR-001、US1 场景 2 |
| A2 | `index.html` 内引用的每个本地资源（`src=`/`href=` 指向产物内相对路径）均解析到产物目录中的既有文件（零悬空引用） | FR-001、US2 边界（"产物为空/不完整"必须被捕获） |
| A3 | 产物中存在内容哈希命名的 JS 资源（`assets/*-<hash>.js` 形态）：`<hash>` 是 rollup 内容哈希的 url-safe base64 段——字符集 `[0-9A-Za-z_-]`（依据 [rollup `output.hashCharacters` 默认值 `base64`](https://rollupjs.org/configuration-options/#output-hashcharacters)），默认长度 8（demo 产物实测，如 `index-CZHFTCDt.js`）——**非**十六进制，断言匹配勿用 `[0-9a-f]` | FR-001 |
| A4 | bundle 内容检出 demo 组件特征字符串 `dominion-vite-react-demo`（证明 React 组件真实编译进产物，非空壳页面）与 React 运行时痕迹（react-dom 产物的 `@license` banner 字面量 `react-dom.production.min.js`——vite 默认 `esbuild.legalComments: "eof"` 保留 license 注释） | FR-004(a)、US2 场景 1 |

**行为要求**：

1. 断言逐条编号输出（PASS/FAIL 行），失败时终止并返回非 0——任何断言失败 MUST 使 bazel test FAILED（fail-loud，不静默降级为警告）。
2. 脚本 MUST 对"目录存在但内容缺资源"的情况失败（A2 捕获悬空引用），而非只检查目录存在。
3. 脚本自身无外部依赖（bash + coreutils：grep/sed/find），可在 bazel 沙盒内运行。
4. 特征字符串常量与 demo 组件源码中的渲染文本保持单一来源定义（组件渲染 `dominion-vite-react-demo`，脚本 grep 同一常量字面量；修改时两处同步）。
5. A4 的 React 运行时痕迹字面量 `react-dom.production.min.js` 依赖 vite 默认 license 注释保留行为（demo 产物已核实该 banner 存在）；若未来 minify/legalComments 配置变更导致该 banner 消失，以产物中可稳定检出的 React 字面量替换，并同步更新本契约与 `dist_assert.sh`（单一来源）。

## 消费方

- demo 的 `:dist_assert_test`（本 feature 交付）
- 后续 React 前端包（如 049 web 前端）可复制同形态断言脚本、替换特征字符串常量
