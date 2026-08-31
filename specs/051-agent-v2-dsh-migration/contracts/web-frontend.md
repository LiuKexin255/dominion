# Contract: web 前端（侧栏优化 + preset 管理 + agent 物化 + 工具结果呈现）

**Feature**: [spec.md](spec.md) FR-001/002/003/004/005(web)/008/007(web 面) | **决策**: [research.md](../research.md) D10/D13 | **数据模型**: [data-model.md](../data-model.md) §2.10

049 契约 [web-frontend.md](../../049-agent-v2-dsh-init/contracts/web-frontend.md) 的演进：工程基座（vite + React 18 + dsh primitives、`vite_build`/`vitest_test` bazel 面、NDJSON 流消费、store reducer 不变量）不变；本文只定义变更面。

## 1. 侧栏交互（FR-001..004，US4）

| 项 | 契约 |
|---|---|
| 标题单行 | `Sessions (n)` 计数不换行：`.sidebar-title` `white-space: nowrap`；侧栏任意常规宽度下单行（渲染断言：标题元素高度 = 单行行高） |
| 图标按钮 | 新建 = 加号图标、刷新 = 圆环箭头图标（dsh primitives 图标或内联 SVG）；`aria-label`/tooltip（"新建会话"/"刷新"）；行为与 049 文字按钮一致（含 loading 禁用态延续）；保留 `data-testid="create-session"/"refresh-sessions"` |
| `···` 菜单 | 每 session 条目右侧 `···` 按钮（不依赖选中态）；点击弹出菜单含"删除"项（确认后执行删除编排）；删除按钮 disabled 条件从"无选中"改为"该条目删除进行中" |
| 长名虚化+悬停滚动 | 未悬停：超出部分右侧渐隐遮罩（无横向滚动条、无换行）；悬停：内容横向滚动可看全名（滚动条隐藏样式）；移出：`scrollLeft` 复位 |

**删除编排变更**（FR-007）：去掉 dispose 跳——仅 `DELETE /api/v1/{name}` → 本地移除；失败报错中止（049 两跳编排的简化）。

## 2. preset 管理视图（FR-005 web 面，US2）

- 侧栏底部视图切换 `Sessions | Presets`（单页 state 切换，无 router）。
- **PresetsView**：列表（name + 更新时间）；新建/编辑表单（名称 + `player_prompt` 多行文本——提示词编辑主体）；删除带确认；空态引导"先创建 preset 才能物化 agent"（无内置默认 persona，Q2 裁定）。
- API（`api/agent.ts`）：`listPresets` GET `/api/v2/templates/{t}/presets`、`createPreset` POST、`getPreset` GET、`updatePreset` PATCH（`update_mask: ["player_prompt"]`）、`deletePreset` DELETE。

## 3. agent 物化面板（FR-008 web 面，US2）

- ChatPanel 顶部"设置 agent"入口 + 面板：
  - preset 下拉（ListPresets；**必选**）；
  - model 下拉（ListModels + "默认"项；选项与提交校验同源——`/api/v2/models`）；
  - Apply = `UpdateAgent`（PATCH `/api/v2/{session}/agent`，body `{agent: {name, preset, model}}`）；成功后刷新 `agentStatus`。
- **未物化引导**：进入未物化 session（GetAgent 404）或 Send 前置错误（FAILED_PRECONDITION/NOT_FOUND）→ 对话区呈现引导态（"该会话尚未设置 agent"）+ 物化面板入口；完成物化后可发送（US2 场景 5）。
- 面板对已物化 session 同样可用（再次 Apply = 刷新：清空短期记忆 + 重读 preset，US2 场景 3/4——UI 提示该语义）。

## 4. 对话页事件模型扩展（D10）

- **store reducer** 新增分支：`tool_result` → 按 `tool_id` 找 live/history 中最近的 RUNNING ToolCallBlock，更新 `status`（SUCCEEDED/FAILED）与 `result`；找不到（如重启后残留流）忽略（forward-compat）。
- **ToolCard**：呈现 `result` 文本（棋盘等）与终态（已完成/失败）；049 既有 RUNNING/args 展示不变。
- **回填**：`ListAgentMessages`（`GET /api/v2/{parent=templates/*/sessions/*/agent}/messages`）替代 `:history`；历史中 ToolCallBlock 已带终态与 result，渲染规则同 live。
- 049 reducer 不变量（turn 序、block index 对齐、delta 拼接）延续；`index` 为回合全局单调（服务端重映射契约，[agent-api.md](agent-api.md) §2.4）。

## 5. desktop 退化清单（FR-016/017，对照面；详见 [desktop-bridge.md](desktop-bridge.md) §5）

**移除**：`SessionList.svelte` 管理操作（新建/删除/刷新）、`ProfileManagement.svelte`、`ProfileSelectDialog.svelte`、`ChatView.svelte`/`ChatMessage.svelte`/`ScreenshotModal.svelte`、chatstream 子系统（`internal/chatstream/`、`chat-stream.ts`/`stream-merge.ts`/`chat-fifo.ts`、`main.go` 的 SSE 注册）、Go 绑定 `GetTeam/UpdateTeam/RefreshTeam/ListMessages/*TeamProfile*`（`app.go:1039-1471`）、`CreateSession/DeleteSession` 绑定、对应 view_model 类型与测试。
**保留**：只读 session 选择（ListSessions）、连接（URL → `/api/v2`）+ 探测 + readLoop + 操作执行 + 确认抽屉 + debug、窗口枚举/绑定/截图、config/日志。
**Go 测试**：`app_test.go`/`view_model_test.go` 中对话/Profile/管理面用例随绑定移除；连接/执行/确认用例保留。

## 6. 测试义务（vitest 组件级，SC-003）

1. **SessionList**：标题单行（长 session 列表数据下断言行高/nowrap）；图标按钮 aria-label 与行为（含 loading 禁用）；`···` 菜单弹出与删除回调（不依赖选中）；长名虚化类与悬停滚动行为（jsdom 下断言类/style 切换与复位）。
2. **PresetsView**：CRUD 交互与 API 调用形状（fetch mock）；空态引导。
3. **物化面板**：下拉数据源（presets/models）、必选校验、Apply 请求形状、未物化引导态流转。
4. **store/ChatView/ToolCard**：`tool_result` 分支（live 终态化 + 找不到忽略）；回填渲染（tool_id 关联、result 展示）。
5. **App**：删除编排简化（无 dispose 调用断言）；049 既有对话/隔离/回填用例零回归（API 路径更名后）。

## 7. 实现期必读（间接引用显式列出）

- 049 web 契约基线：`specs/049-agent-v2-dsh-init/contracts/web-frontend.md`（§3.2 组件契约/§4 reducer 不变量/§5 API 客户端表——延续部分）
- 049 事件契约：`specs/049-agent-v2-dsh-init/contracts/conversation-api.md` §3/§4/§6（NDJSON 消费样例）
- 现状组件源：`projects/game/web/frontend/src/components/SessionList.tsx`、`App.tsx:89-221`、`store/chat.ts`、`api/conversation.ts`、`api/sessions.ts`
- dsh UI primitives 包：`node_modules/.pnpm/@deepseek-ai+dsh-client-ui-primitives@0.1.1-rc.2_*/…/`（图标/按钮组件面）
- 样式基线：`projects/game/web/frontend/src/theme.css`（侧栏 token 与布局）
- 050 构建面：`specs/050-vite-react-bazel/spec.md`（`vite_build`/`vitest_test` bazel 规则用法）
