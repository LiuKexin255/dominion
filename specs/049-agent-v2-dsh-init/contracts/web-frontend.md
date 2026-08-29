# Contract: web 前端（`projects/game/web/frontend`）与 web 服务（`projects/game/web/server`）

**Feature**: [spec.md](spec.md) FR-001/FR-004/FR-005/FR-009/FR-012/FR-013/FR-014 | **决策**: [research.md](../research.md) D5/D8

## 1. 工程契约

| 项 | 值 |
|---|---|
| 框架 | vite + React 18 + TypeScript（050 基建，`specs/050-vite-react-bazel/`） |
| workspace | pnpm-workspace.yaml 增条目 `projects/game/web/frontend`；React 栈走 catalog（050 已落） |
| dsh 组件库 | `@deepseek-ai/dsh-client-ui-primitives@0.1.1-rc.2` **精确 pin**（package.json 直接版本；dsh 家族锁定决策的 catalog 例外，先例 `third_party/dsh/core`） |
| 构建 | `vite_build`（`tools/dev/js/vite.bzl`）；组件测试 `vitest_test` + `@testing-library/react`（用法照 `experimental/js/vite_react_demo/BUILD.bazel`） |
| 模块格式 | ESM（`"type":"module"`；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`） |
| 托管 | `projects/game/web/server`：Go + `embed.FS(dist)` + `http.FileServerFS`（逐行照 `experimental/js/vite_react_demo/server/main.go`，050 US4 实证） |
| 暴露 | `game.liukexin.com` `/`（同主机名路径分流，[research.md](../research.md) D5）；API 全相对路径，零 CORS |
| 许可 | ui-primitives 为 BSD-3-Clause；自建组件参照上游源码改造，包 README 记录 attribution 与来源链接 |

## 2. 页面结构（单页双区，行为基线 = desktop `SessionList/ChatView/ChatMessage`）

```text
┌─────────────┬──────────────────────────────────┐
│ SessionList │  ChatView（当前选中 session）      │
│ (侧栏)       │  ├─ 消息区（滚动）                 │
│  列表+时间    │  │   user 气泡 / agent 消息        │
│  Refresh     │  │   agent 消息 = blocks 序列渲染   │
│  Create     │  │     TEXT → MessageText          │
│  Delete     │  │     THINK → ReasoningRow(自建)   │
│  选择→进入    │  │     TOOL_CALL → ToolCard(自建)  │
│             │  ├─ 排队指示区（pending 消息 chip）  │
│             │  └─ 输入区（Input+发送）            │
└─────────────┴──────────────────────────────────┘
```

无路由库：视图切换 = 选中 session 状态驱动（`selected` ↔ 会话详情）。空态（未选中）显示引导文案。

## 3. 组件契约

### 3.1 复用（ui-primitives，运行时零 cordis 依赖——0.1.1-rc.2 tarball 实测）

`MessageText`（markdown 正文）、`CodeBlock`/`JsonBlock`（代码/JSON 块）、`DisclosureRow`（折叠行外壳）、`IconThinkOutline14`、`Button`/`Input`/`StateDot`。

### 3.2 自建（交互参照 dsh-web 源码，剥离 slot/connection 系统）

| 组件 | Props 契约 | 参照 |
|---|---|---|
| `ReasoningRow` | `{ text: string; running: boolean }` — 默认折叠；折叠态显示摘要行（running 时跟随最新行滚动、完成态显示首行）；展开态渲染完整思考文本；`data-state="running\|ok"` 供样式区分 | [ReasoningRow.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ReasoningRow.tsx)（DisclosureRow + latest/first line + 节流滚动） |
| `ToolCard` | `{ toolId: string; name: string; argsJson: string; status: 'RUNNING'\|'SUCCEEDED'\|'FAILED'; result?: string }` — 名称/参数（JSON 折叠展示）/状态（StateDot）/结果关联展示于同一卡片 | [ToolCallTree.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-tool/src/client/tool/ToolCallTree.tsx)（剥离 renderSlot/子调用树，保留单卡形态） |
| `SessionList` | `{ sessions, selected, loading, error, onSelect, onCreate, onDelete }` — 列表（名称+创建时间）、新建（`saolei` 固定 template）、删除（D6 编排）、切换 | desktop `SessionList.svelte` 行为基线 |
| `ChatView` | `{ session, history, live, queue, onSend }` — 历史回填 + 实时流合并渲染 + 排队指示 | desktop `ChatView.svelte`（parts 保序、tool_id 关联、pending 标记、流式跟随滚动） |

### 3.3 主题 token

自建 `theme.css` 定义 ui-primitives 消费的 11 个 `--dsw-*` 变量（0.1.1-rc.2 `lib/index.js` grep 实测完整清单）：`--dsw-alias-label-primary`、`--dsw-alias-label-primary-inverted`、`--dsw-alias-label-tertiary`、`--dsw-alias-state-business-primary`、`--dsw-alias-state-error-primary`、`--dsw-alias-state-error-secondary`、`--dsw-alias-state-success-primary`、`--dsw-alias-state-success-secondary`、`--dsw-alias-state-warn-primary`、`--dsw-alias-state-warn-secondary`、`--dsw-static-blue-400`；+ 自有布局样式；单一主题（深色基调，对齐 dsh-web 观感）。

## 4. 状态与流模型（store 契约）

```ts
// 会话状态（useSyncExternalStore 消费的事件驱动 store，无框架依赖）
interface ChatState {
  history: HistoryMessage[];        // :history 回填 + 事件合并产物
  live: LiveTurn | null;            // 进行中回合：{ turnId, blocks: BlockDraft[] }
  queue: QueuedMsg[];               // { text, position }（本端发出的排队消息）
  error: string | null;             // 回合错误（turn_end{ERROR} 呈现）
}
type BlockDraft =
  | { index: number; type: 'TEXT' | 'THINK'; text: string }   // delta 拼接
  | { index: number; type: 'TOOL_CALL'; toolId: string; name: string; args: string; status: string; result?: string };
```

**事件归约（reducer 不变式，与 [conversation-api.md](conversation-api.md) §3 对齐）**：

- `queued` → 追加 queue 项（chip 展示"排队中 #N"）。
- `turn_start` → 本消息 queue 项转为 live（排队指示消除）。
- `block_start/delta/block_end` → live.blocks 按 index 归并（delta 拼接、end 覆盖终态）。
- `turn_end{COMPLETED}` → live 终结、合并入 history（THINK/TEXT/TOOL_CALL 分类保序渲染）；输入恢复可用。
- `turn_end{ERROR}` → error 呈现（本轮失败明确提示，进程/会话不崩）；输入恢复可重试。
- `turn_end{ABORTED}` → 会话已删除：清空并提示，返回列表页。
- 刷新/切换会话 → `GET :history` 全量重建 state（FR-014 回填）。

## 5. API 客户端面（`src/api/`）

| 函数 | 后端 | 说明 |
|---|---|---|
| `listSessions/createSession/deleteSession` | `/api/v1/templates/saolei/sessions`（既有 REST，零改动） | US4 管理闭环 |
| `sendStream(session, text)` | `POST /api/v2/...:send` | NDJSON 流读取（[conversation-api.md](conversation-api.md) §6 样例） |
| `listHistory(session)` | `GET /api/v2/...:history` | 回填 |
| `disposeSession(session)` | `POST /api/v2/...:dispose` | 删除编排第二跳（D6；DELETE /api/v1 成功后调用，幂等容错） |

删除编排：`deleteSession` 成功 → `disposeSession`（失败仅记录不阻断——资源随 agent-v2 重启释放，[research.md](../research.md) D6）。

## 6. 测试义务（US3 验证载体 + 行为回归）

1. **ToolCard 构造数据测试**（US3 三场景）：RUNNING 态名称/参数呈现；SUCCEEDED/FAILED 结果关联；"text+think+多次工具调用"混合分类保序渲染。
2. **ReasoningRow 测试**：折叠默认、展开切换、running 摘要跟随语义、无思考不渲染（上游组件级）。
3. **reducer 测试**：事件序归约（queued→turn_start→deltas→turn_end）、ERROR/ABORTED 分支、history 回填与流式合并一致性（FR-014 前端侧）。
4. **NDJSON 解析测试**：分片边界（半行/粘包）正确重组。
5. **SessionList 行为测试**：新建/删除/切换状态流转（mock API）。

以上均为 vitest + testing-library（进 `bazel test`，constitution 原则 IV 小颗粒度门禁）；US3 的端到端（真实工具调用）验证推迟至后续第一个工具 step（FR-005）。
