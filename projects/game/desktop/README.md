# projects/game/desktop

本目录包含 Dominion 游戏 desktop 的 Wails 应用（Go 后端 + Svelte/Vite 前端）。desktop 是
游戏 flow 的**控制终端**：它把 WebSocket flow 通道（`/api/v2`）下发的鼠标/键盘操作落到
本机窗口上执行，并回传操作结果与截图。

## 能力清单

| 能力 | 说明 |
|---|---|
| 连接 | 经 gateway 的 `/api/v2/templates/{template}/sessions/{session}/connect` flow 流建立 WebSocket 连接；连接后做应用层探测（StatusSignal 首帧 + 10s 超时）；同 session 二次连接接管旧连接 |
| 绑定 | 窗口枚举（`ListWindows`）与选择（`SetSelectedWindow`/`GetSelectedWindow`）；所选窗口是操作与截图的唯一目标 |
| 执行 | `readLoop` 路由入站 FlowPart 操作（mouse/keyboard）→ `executeAgentOperation` 执行 → 500ms 后截图随 `FlowResultPart` 回传（5 MiB 上限） |
| 确认 | debug 模式下操作结果先挂起（hold），经确认抽屉放行；15 分钟自动放行（`game:debug:result-held/released` 事件） |
| 配置 | GatewayURL / Env / Template（Template 为本地常量控制平面） |
| 日志 | 应用内日志面板（applog + `game:log` 事件）；debug 开关控制 DEBUG 级输出 |

session 选择为**只读**：数据源为 `ListSessions`（`GET /api/v1/templates/{t}/sessions`），
本终端不做 session 新建/删除等管理操作（`specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md` §5）。

## 测试与大型测试豁免

- 单元测试：`bazel test //projects/game/desktop/...`（连接探测/接管/readLoop 执行/确认
  抽屉/窗口选择/截图/WS 传输等保留面均有用例）。
- **真 desktop 大型测试豁免（constitution 原则 VI 登记项）**：desktop 为 Windows GUI
  应用，无法进 CI 大型测试环境执行。flow 链路的端到端行为由 `desktop-flow` 套件以
  fake-desktop（`projects/game/fake-desktop/`）覆盖——fake-desktop 以 WS 客户端身份连接
  同一 `/api/v2` 入口，验证连接/探测/接管/操作回执（`projects/game/testplan/system_test.yaml`
  的 `desktop-flow` suite）。
- 真机冒烟需 Windows 环境（按 `specs/051-agent-v2-dsh-migration/quickstart.md` §3），
  当前无可用 Windows 环境未执行，无冒烟记录。

## 构建方式

**必须使用 Bazel 构建，禁止直接调用前端构建工具生成 `dist/`。**

正确命令：

```bash
bazel build //projects/game/desktop/frontend:dist
bazel build //projects/game/desktop
```

## 禁止手动生成 `frontend/dist/`

### 出现原因

`frontend/package.json` 中声明了 `"build": "vite build"` 脚本。Vite 默认将构建产物输出到 `frontend/dist/`，且会对 JS/CSS 文件名加入内容哈希（例如 `index-DFjPDZE6.js`）。

当开发者直接在 `frontend/` 目录下执行 `pnpm build`、`npm run build` 或 `vite build` 时，这些带哈希的文件会被写入源码树。由于每次构建产生的哈希不同，会导致：

- `git status` 中出现大量未跟踪文件；
- 已跟踪的 `dist/` 文件被标记为删除或修改；
- 源码与构建产物耦合，污染 diff。

### 正确做法

前端构建由 Bazel 的 `vite_build` 规则接管（见 `frontend/BUILD.bazel`）：

```bash
bazel build //projects/game/desktop/frontend:dist
```

产物会输出到 Bazel 的输出目录（`bazel-bin/...`），由 `wails_app` 规则消费，不会污染源码树。`wails.json` 中的 `frontend:build` 也已置空，避免 Wails CLI 触发额外的 npm 构建。

### 禁止事项

- 禁止在 `frontend/` 内手动运行 `pnpm build`、`npm run build` 或 `vite build`；
- 禁止将 `frontend/dist/` 下的任何文件提交到 Git；
- 禁止修改 `vite.config.ts` 将 `outDir` 指回源码树。

### 已生成的 `dist/` 如何处理

如果工作区中已经存在 `frontend/dist/`，请删除并确保不再提交：

```bash
rm -rf projects/game/desktop/frontend/dist
# 若文件曾被 Git 跟踪，则使用：
git rm -r projects/game/desktop/frontend/dist
```

`frontend/.gitignore` 已配置忽略 `dist/`，防止再次误提交。