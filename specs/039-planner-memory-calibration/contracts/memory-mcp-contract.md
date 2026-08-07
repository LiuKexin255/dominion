# Contract: memory mcp server（agent 上，planner 记忆工具）

**Feature**: `039-planner-memory-calibration` | **Spec**: [`spec.md`](../spec.md) | **Research**: D3/D9

> 在 agent 服务上新建 memory mcp server（`projects/game/agent/src/mcp/memory/memory-mcp.ts`），向 planner 暴露 `memory_add`/`memory_update`/`memory_remove` 工具（均含 `memory_id`）。template/session 经 mcp server path 闭包注入（同 saolei mcp，FR-012）。工具经 `memory-client.ts`（gRPC）转发到 MemoryService——**mcp server 不直连 memory 服务**（FR-007）。

---

## 1. 工具（MCP，planner 专属）

```ts
memory_add(memory_id: string, content: string): string
memory_update(memory_id: string, content: string): string
memory_remove(memory_id: string): string
```

| 工具 | 参数 | MemoryService RPC | 行为 |
|---|---|---|---|
| `memory_add` | `memory_id`, `content` | `CreateMemory` | 新建；`memory_id` 已存在 → 返回错误文本（ALREADY_EXISTS），LLM 改用 update |
| `memory_update` | `memory_id`, `content` | `UpdateMemory` | 改既有；不存在 → 返回错误文本 |
| `memory_remove` | `memory_id` | `DeleteMemory` | 删既有；不存在 → 返回错误文本 |

- 三个工具 MUST 均含 `memory_id` 参数（FR-008）。
- 其余参数（`content`）参考 hermes 既有记忆工具 add/replace/remove（调研 §3.2.2）。
- 工具返回单一 MCP 文本内容块（成功/错误文本，LLM 据此决策；错误非 tool 异常，是正常 tool result，031 C15 neutral status 模式）。
- **改存储不刷新冻结快照**（FR-010 冻结语义；快照在压缩/初始化边界刷新，见 team-graph-contract）。

---

## 2. 工厂与 path 闭包注入（`createMemoryMcpServer`）

```ts
export function createMemoryMcpServer(
  memoryClient: MemoryClient,
  template: string,
  session: string,
): McpServer;
```

- `template`/`session` 闭包绑定（FR-012）：工具实现用闭包的 template/session 构造 Memory 资源名（`templates/${template}/sessions/${session}/memories/${memory_id}`），**工具参数不含 template/session**（同 saolei mcp `createSaoleiMcpServer(bridge, boardApi, sink)` 闭包模式）。
- per-session：mcp host 为每个 session 创建一个 memory mcp server（与 saolei mcp 同一 per-session 生命周期）。
- DI seam（`style/javascript.md` §测试）：`memoryClient` 注入，测试用 fake（无 `vi.mock`）。

```ts
// memory_add 实现（示意）
server.registerTool("memory_add", { description: "...", inputSchema: { memory_id: z.string(), content: z.string() } },
  async ({ memory_id, content }) => {
    try {
      await memoryClient.createMemory(template, session, memory_id, content);
      return textResult(`memory added: ${memory_id}`);
    } catch (err) {
      // ALREADY_EXISTS 等 → 文本反馈，非异常
      return textResult(`memory_add failed: ${describeErr(err)}`);
    }
  });
```

---

## 3. memory-client（agent gRPC client，仿 `prompt-client.ts`）

```ts
// projects/game/agent/src/memory-client.ts
export const MEMORY_SERVICE_TARGET = "dominion:///game/memory:50051";
export class MemoryClient {
  createMemory(template, session, memoryId, content): Promise<void>;
  updateMemory(template, session, memoryId, content): Promise<void>;
  deleteMemory(template, session, memoryId): Promise<void>;
  listMemories(template, session): Promise<MemoryEntry[]>;
}
```

- 仿 `projects/game/agent/src/prompt-client.ts`：`registerDominionResolver` + proto-loader + keepalive/round_robin/TLS channel options（同款 `KEEPALIVE_OPTIONS`/`ROUND_ROBIN_SERVICE_CONFIG`）。
- 资源名构造：`templates/${template}/sessions/${session}/memories/${memoryId}`。
- `listMemories` 供冻结快照烘焙（`memory-snapshot.ts`，见 team-graph-contract）。
- DI seam：构造器可选注入 `grpc.Client`（测试用 fake，无模块 mock，同 prompt-client）。

---

## 4. mcp-host 装配（每 mcp 独立 path，template-scoped）

- **每个 mcp 一个独立 path**（不共用单 path）：memory mcp 与 saolei mcp 各自一个 path，path **须包含 `template` 字段**（template-scoped，team 改造前的路径风格），形如：
  - saolei mcp：`/internal/mcp/{template}/{session}/saolei`（player 连）
  - memory mcp：`/internal/mcp/{template}/{session}/memory`（planner 连）
- mcp-host（`projects/game/agent/src/mcp-host.ts`）按 **(template, session, mcpKind)** 路由到对应 McpServer 实例，懒创建。
- 两者独立 McpServer 实例（player 工具集 = saolei_operate 等；planner 工具集 = memory_*+instruct_player）。
- `SessionBridgeLookup` 扩展为同时提供 saolei `{bridge, sink}` 与 memory `{memoryClient, template, session}`（由 SessionTeam/server 装配）。
- **注意**：既有 saolei mcp path 为 `/internal/mcp/{sessionId}`（无 template）；本特性将 mcp path 统一为 template-scoped 多 path 方案——既有 saolei mcp 接线（`buildSaoleiMcpTools` 的连接 URL）须同步迁移到新 path 方案（tasks 落实，clean break）。

> 精确 path 段命名（`saolei`/`memory` 字面量 vs 枚举）由 plan 落实；约束：每 mcp 独立 path、path 含 template、planner 与 player 连各自 mcp、工具集互不交叉（FR-009/FR-010）。

---

## 5. 错误反馈与降级（FR：memory 服务不可用不阻断 gameplay）

- memory 服务不可用 → 工具返回错误文本（如 "memory service unavailable"），LLM 据此决策（不抛 tool 异常）。
- 冻结快照读（`listMemories`，压缩/初始化边界）失败 → 保持上一次冻结快照（或空），不阻断 team 运行（调研 D4，spec Edge Case "memory 服务不可用"）。
- 具体重试/退避由 plan 决定（约束：不因 memory 不可用阻断 player gameplay）。

---

## 6. 验证要点

- 三工具均含 `memory_id`；工具参数不含 template/session（path 闭包注入）。
- 工具经 memory-client 转发到 MemoryService；mcp server 不直连 memory 服务（连接拓扑审查）。
- memory_add memory_id 重复 → 错误文本反馈（非异常）；update/remove 不存在 → 错误文本。
- memory 工具改存储不刷新冻结快照（冻结语义）。
- 仅 planner 持有 memory 工具（player 不持有）。
