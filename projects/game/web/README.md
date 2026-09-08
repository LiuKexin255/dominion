# game web

web 是 game 域的网页服务：为 React 前端（`projects/game/web/frontend`，vite 构建）
提供静态托管。页面包含 session 管理列表与 session 对话页；全部 API 经存量 gateway
以相对路径访问（session 管理走 `/api/v1`，对话走 `/api/v2`），零 CORS
（`specs/049-agent-v2-dsh-init/contracts/web-frontend.md`）。

## 结构

- **frontend**（`frontend/`）：vite + React workspace 包，组件级复用
  `@deepseek-ai/dsh-client-ui-primitives`（0.1.1-rc.2 精确 pin，attribution 见
  `frontend/README.md`）；`src/api/`（session CRUD + NDJSON 流客户端）、
  `src/components/`（SessionList/ChatView/ReasoningRow/ToolCard）、`src/store/`
  （事件 reducer）。契约：`specs/049-agent-v2-dsh-init/contracts/web-frontend.md`。
- **server**（`server/`）：Go 静态服务。`vite_build` 产物（`frontend:dist`）经
  `wails_asset_library` 打包、`//go:embed` 嵌入二进制，以
  `http.FileServerFS` 原样托管（样板 `experimental/js/vite_react_demo/server`）。
  无业务逻辑、无持久化。

## 构建与部署

- `service.yaml` 声明 http 8080；`projects/game/deploy.yaml` 将其暴露在
  `game.liukexin.com` 的 `/` 路径（同主机名按最长 PathPrefix 分流：`/api/v1/`、
  `/api/v2/` 归 gateway，`/` 归 web——`specs/049-agent-v2-dsh-init/research.md` D5）。
- 前端构建：bazel `vite_build` 目标 `//projects/game/web/frontend:dist`；单测
  `//projects/game/web/frontend:lib_test`（vitest 组件测试）。

## 大型测试

`projects/game/testplan/system_test.yaml` 的 `agent-v2-conversation` suite 中的
`web_test` 覆盖托管面（`GET /` 入口 HTML、`/assets` 静态资源可解析）与页面的
管理闭环 smoke（新建 → 列表可见 → 对话一轮 → 删除，经 `/api/v1` + `/api/v2`，
无需浏览器）。

## 已知限制

与 agent-v2 共享同一组对话面限制，记录于
`specs/049-agent-v2-dsh-init/research.md`「已知限制」节：

- **多标签页无实时推送**：未发送消息的标签页看不到其他页面的回合进展，可刷新经
  history 查询（`specs/049-agent-v2-dsh-init/contracts/conversation-api.md` §5）。
- **desktop 删除同名 session 不联动**：desktop 侧删除不会触发 agent-v2 dispose
  （跨客户端编排留待后续 step）。
- **历史随 agent-v2 进程重启丢失**：对话历史为内存态（spec Assumptions 明示
  接受）；session 列表因复用 session 服务而持久。
