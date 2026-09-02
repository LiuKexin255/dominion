# Contract: DesktopBridgeService（desktop flow 控制流桥接）

**Feature**: [spec.md](spec.md) FR-009/016/017/018 | **决策**: [research.md](../research.md) D8/D9/D12 | **数据模型**: [data-model.md](../data-model.md) §2.6/§2.9

desktop ↔ agent-v2 的双向 flow 控制流。链路（Q3 裁定 A）：

```text
desktop（wails，既有 WSClient）
   │ wss://{GatewayURL}/api/v2/templates/{template}/sessions/{session}/connect
   │      binary proto：UserFrame ↑ / TeamFrame ↓（帧类型复用 projects/game/game.proto）
▼
gateway（新 /api/v2 WS 入口；v1 connect 模式移植：URL→template/session 注入）
   │ gRPC bidi（proxy conn）
▼
proxy DesktopBridgeHandler（owner 亲和 get-or-create → 定向实例）
   │ gRPC bidi
▼
agent-v2 desktop-bridge 插件 gRPC 面（Connect）
   └─ ctx.desktopBridge 服务（连接注册表 + dispatch API）← saolei-loop 消费
```

**与对话流独立**（FR-009/US1 场景 6）：flow 流（本链路）与对话流（`/api/v2/:send` NDJSON）为两条独立连接、独立 gRPC 流、独立故障域——任一断开/故障不影响另一条（agent-v2 侧连接对象互不引用）。

## 1. gRPC 面（agent_v2.proto 新增）

```proto
import "projects/game/game.proto";

service DesktopBridgeService {
  // bidi；无 REST 绑定（WS 由 gateway 自定义路径承载，对齐 v1 TeamService.Connect 形态）
  rpc Connect(stream projects.game.UserFrame) returns (stream projects.game.TeamFrame);
}
```

**帧语义**（全部延续 v1 wire 语义——desktop 执行端零语义变化）：

| 方向 | 帧 | 语义 |
|---|---|---|
| ↑ 首帧 | `UserFrame{flowParts:[StatusSignal{ACTIVE}]}` | 连接探测（desktop `App.Connect` 应用层探测，10s 超时） |
| ↓ | `TeamFrame{flowParts:[StatusSignal]}` | 探测回应（status 枚举名回传） |
| ↑ | `UserFrame{flowParts:[FlowResultPart{tool_id, status, message, screenshot}]}` | 操作结果回执（按 tool_id 匹配在途 dispatch） |
| ↓ | `TeamFrame{flowParts:[FlowPart]}` | 操作下发（mouseMoveAndClick / keyboardPress 等，game.proto FlowPart oneof） |
| ↑/↓ | wait/warn/status 信号帧 | v1 readLoop 路由语义延续（desktop 侧） |

**身份绑定**：gateway 从 URL 路径提取 `{template}/{session}` 注入首帧（覆写客户端值）——v1 `wsStream.Recv` 行为移植（`projects/game/gateway/cmd/main.go:221-235`）。首帧之后连接即绑定该 session；消息帧（messageParts）在 v2 面不产生（desktop 无对话能力）。

## 2. 插件服务接口（`ctx.desktopBridge`）

`@dominion/dsh-desktop-bridge`（`common/js/dsh-plugins/desktop-bridge/`）导出 cordis Service 插件：

```ts
export const name = "desktop-bridge";
// 无 inject（自足；gRPC handlers 由宿主 server.ts 在 50051 单 server 上注册）

export interface DesktopBridgeService /* ctx.desktopBridge */ {
  /** bidi 流接入：首帧绑定 session；新连接接管（旧连接关闭）；流断开清理。 */
  attach(sessionName: string, stream: BidiStream): void;
  /** 下发一个操作部件并等待回执；无连接 → FAILED "desktop disconnected"；
   *  超时 backstop 20min；abort → FAILED "aborted"。 */
  dispatch(sessionName: string, part: FlowPart, signal?: AbortSignal): Promise<OperationResult>;
  /** 供宿主 server.ts 注册 gRPC 服务用的 handler 实现。 */
  handlers(): DesktopBridgeServiceHandlers;
}
```

- `OperationResult = {status: SUCCEEDED|FAILED|UNSPECIFIED, message, screenshot?: {data(base64 PNG), widthPx, heightPx}}`（v1 `projects/game/agent/src/operation-bridge.ts:84-95` 类型随迁）。
- `dispatch` 铸造 UUID `tool_id` 盖入 FlowPart（解耦对话 tool_call.id，v1 `:232-233` 语义）；
- 连接对象只持写回调不持流引用（断线重连 in-flight 不丢，v1 `:10-12`）；stale 回执（未知/过期 tool_id）记日志忽略（v1 `handleResult` 语义）。
- 回执状态映射：FAILED 结果原样上抛；UNSPECIFIED 保持中性（拒绝类结果不在桥上发生——拒绝在 GameRuntime 校验层，永不下发）。

## 3. gateway WS 入口（`/api/v2/.../connect`）

- 路径匹配 `/api/v2/templates/{template}/sessions/{session}/connect`（7 段，v1 `isWebSocketConnectPath` 模式移植）；`/api/v2/` 子树先查 WS 分支再落 gwmux（对齐 v1 `/api/v1/` 子树结构，`projects/game/gateway/cmd/main.go:160-173`）。
- `websocket.Accept`（OriginPatterns `["*"]`、10MB 读限）、binary proto 帧双向泵、关闭分类（normal/clean/protocol/internal → websocket status 映射）——v1 `handleWebSocketConnect` 移植（`main.go:266-339`）。
- 后端：`game.NewDesktopBridgeServiceClient(teamConn).Connect(ctx)` bidi。
- **v1 面移除**（FR-019）：`/api/v1` 的 WS connect 分支与路径匹配、TeamService/PromptService 的 handler 注册，以及 promptConn（prompt 服务随部署下线）；teamConn 承载 AgentService（会话面）HTTP + DesktopBridgeService bidi；PresetService 经 gateway 直连 agent-v2（presetConn），不经 proxy（[research.md](../research.md) D9、[revisions/directive-2026-09-01.md](../revisions/directive-2026-09-01.md) §3）。

## 4. proxy 转发（DesktopBridgeHandler）

- implements `game.DesktopBridgeServiceServer`（`projects/game/proxy/`，049 ConversationHandler 同层新增）。
- `Connect`：等待首帧 → 解析 `template_id/session_id` → **owner get-or-create**（复用 `assignConversationOwner` 语义与 `agent_v2_owners` 池——desktop 可先于对话/物化连接；owner 保证后续对话与游戏落在同实例）→ 目标实例 conn → `bind.WithFirstFrame` + bidi pump（v1 TeamHandler.Connect 模式，`projects/game/proxy/handler/handler.go:179-226`）。
- 错误映射：无 owner 可建（Mongo 故障）→ INTERNAL；无活实例 → UNAVAILABLE；资源名形状非法 → INVALID_ARGUMENT（首帧注入值校验）。

## 5. desktop 侧（退化后保留面，FR-016/017/018）

- **改向**：`internal/api/websocket.go` URL 模板 `/api/v1/.../connect` → `/api/v2/.../connect`（GatewayURL 配置与探测/接管/readLoop 语义零改动）。
- **保留**：连接探测（StatusSignal 首帧 + 10s 超时）、新连接接管、`readLoop` 操作路由（FlowParts → `handleInboundOperation`，信号帧转发，recv 错误合成 wait 帧退出）、`executeAgentOperation`（窗口解析 → 执行器分发 → 500ms 后截图回传 5MiB 上限）、确认抽屉（hold/release、15min 自动放行、`game:debug:result-held/released` 事件）、debug 模式、窗口枚举/绑定/截图、config/logs。
- **移除**（对话/管理面）：见 [web-frontend.md](web-frontend.md) §5 清单（chatstream 子系统、Profile/Chat/Session 管理 UI 与绑定）。
- **session 选择只读**（A4）：`ListSessions`（`GET /api/v1/templates/{t}/sessions`）拉取列表供选择连接目标；无新建/删除/切换管理操作。

## 6. 验收锚点（US1/US3 场景 ↔ 断言）

1. US1-1/US3-3：fake desktop 连接 + 绑定后，`saolei_init` 的 F2 FlowPart 到达 fake desktop，回执截图被识别为棋盘（工具结果含 `board size` 行）。
2. US1-4：desktop 未连接时 `dispatch` 立即 FAILED（工具错误结果、模型可见、回合/进程存活）。
3. US1-5：mid-game 断连在途 FAILED、重连后游戏继续（棋盘状态在 GameRuntime 保留）。
4. US1-6：对话流收流式回复的同时 desktop 执行操作；任一断开另一条不中断（两流独立）。
5. US3-2/Edge：同 session 二次 Connect → 第一个连接被关闭（接管）。

## 7. 实现期必读（间接引用显式列出）

- v1 桥接语义源：`projects/game/agent/src/operation-bridge.ts`（dispatch/回执/超时/接管全语义）
- v1 gateway WS 实现源：`projects/game/gateway/cmd/main.go:178-350`（路径匹配/wsStream/handleWebSocketConnect/关闭分类）
- v1 proxy bidi 转发源：`projects/game/proxy/handler/handler.go:179-226` + `pkg/bind/`（WithFirstFrame/Binder）
- 帧与信号类型：`projects/game/game.proto`（UserFrame/TeamFrame/FlowPart/FlowResultPart/StatusSignal）
- desktop 连接实现：`projects/game/desktop/internal/api/websocket.go`、`projects/game/desktop/app.go:1619-1821`（Connect/CloseAgent）、`app.go:638-777`（readLoop/handleInboundOperation）
- dsh 插件形态：`survey/deepseek-harness-agent-loop-prereq.md` §5.1/§5.4（Service/inject；插件桥接先例）
