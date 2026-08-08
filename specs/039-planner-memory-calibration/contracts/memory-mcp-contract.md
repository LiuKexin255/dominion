# Contract: memory mcp server（agent 上，planner 记忆工具）

**Feature**: `039-planner-memory-calibration` | **Spec**: [`spec.md`](../spec.md) | **Research**: D3/D9/D11

> 在 agent 服务上新建 memory mcp server（`projects/game/agent/src/mcp/memory/memory-mcp.ts`），向 planner 暴露**一个** hermes 风格 `memory` 工具（Session 2026-08-08）。工具参数与 hermes [`memory` schema](https://github.com/NousResearch/hermes-agent/blob/main/tools/memory_tool.py) 对齐：`action`/`content`/`old_text`/`operations`（无 `memory_id`/无 `target`）。template/session 经 mcp server path 闭包注入（同 saolei mcp，FR-012）。工具经 `memory-client.ts`（gRPC）转发到 MemoryService——**mcp server 不直连 memory 服务**（FR-007）。**memory 服务存储 API 不变**（memory_id 式资源）；agent 负责将 hermes 式调用转换为服务的 memory_id 式 RPC（D9）。

---

## 1. 工具（MCP，planner 专属，单一 `memory` 工具）

```ts
memory(
  action?: "add" | "replace" | "remove",   // 单次操作的动作
  content?: string,                         // 条目正文（add/replace 用）
  old_text?: string,                        // 定位既有条目的短子串（replace/remove 用）
  operations?: MemoryOp[],                  // 批量原子操作（每项 {action, content?, old_text?}）
): string   // 单一 MCP 文本内容块
```

| 调用形态 | 用到的参数 | agent 转换 → MemoryService RPC | 匹配/冲突语义 |
|---|---|---|---|
| `action=add, content` | `content` | agent 内部生成 memory_id → `CreateMemory(template, session, <gen_id>, content)` | 等价 content 已存在 → 成功（去重，同 hermes "no duplicate added"） |
| `action=replace, old_text, content` | `old_text`, `content` | `listMemories` → 按 `old_text` 子串匹配定位唯一条目得其 memory_id → `updateMemory(template, session, <id>, content)` | **0 命中 → 错误文本（含当前条目助 LLM 重选）**；**多个不同条目命中 → 错误文本（要求更具体子串）**；全相同 → 作用首条 |
| `action=remove, old_text` | `old_text` | 同上定位 memory_id → `deleteMemory(template, session, <id>)` | 同上 0/多命中语义 |
| `operations=[...]`（批量） | `operations` | 数组每项按上述单 op 转换，**原子**应用（全成功才提交） | 任一 op 失败 → 整批不提交，返回错误 + 当前条目 |

- **无 `memory_id` 参数**：`memory_id` 为 memory 服务内部存储键（资源 id `{memory}` 段），agent 在 `add` 时内部生成（非 LLM 提供），对 LLM 不可见。`replace`/`remove` 经 `old_text` 子串定位（D9）。
- **无 `target` 参数**：dominion planner 仅有单一记忆存储（无 hermes 的 memory/user 双存储，D9）。
- 单次操作（直接 `action`/`content`/`old_text`）与批量（`operations` 数组）二选一，同 hermes。
- 工具返回单一 MCP 文本内容块（成功/错误文本，LLM 据此决策；错误非 tool 异常，031 C15 neutral status）。
- **改存储不刷新冻结快照**（FR-010 冻结语义；快照在压缩/初始化边界刷新，见 team-graph-contract §3）。

### 1.1 `old_text` 子串匹配语义（同 hermes）

- 匹配规则：`old_text` 为条目正文的**子串**（`old_text in entry`，子串包含；大小写敏感，同 hermes [`memory_tool.py`](https://github.com/NousResearch/hermes-agent/blob/main/tools/memory_tool.py) `replace`/`remove`）。
- 0 命中：返回错误文本 + 当前全部条目列表（助 LLM 重选更具体的 `old_text`）。
- 多个**不同**条目命中：返回错误文本 + 命中条目预览（要求更具体子串）。
- 多条目内容**完全相同**：作用于首条（去重语义）。
- `old_text` 为空 / 缺失（replace/remove）：返回错误文本 + 当前条目（同 hermes `_missing_old_text_error`，助 LLM 重发）。

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
// memory 工具实现（示意，单次 add 形态）
server.registerTool("memory", {
  description: "...(see memory-skill-contract / FR-020 for guidance)...",
  inputSchema: {
    action: z.enum(["add", "replace", "remove"]).optional(),
    content: z.string().optional(),
    old_text: z.string().optional(),
    operations: z.array(z.object({
      action: z.enum(["add", "replace", "remove"]),
      content: z.string().optional(),
      old_text: z.string().optional(),
    })).optional(),
  },
}, async ({ action, content, old_text, operations }) => {
  try {
    if (operations) {
      return textResult(await applyBatch(memoryClient, template, session, operations));
    }
    if (action === "add") {
      const id = generateMemoryId(content);              // agent 内部生成（D9 待 plan）
      await memoryClient.createMemory(template, session, id, content);
      return textResult(`memory added`);
    }
    if (action === "replace" || action === "remove") {
      const entries = await memoryClient.listMemories(template, session);  // 全量
      const matched = matchBySubstring(entries, old_text);                 // 0/多命中处理
      if ("error" in matched) return textResult(matched.error);            // 0/多命中 → 错误文本
      const id = matched.id;
      if (action === "replace") await memoryClient.updateMemory(template, session, id, content);
      else await memoryClient.deleteMemory(template, session, id);
      return textResult(`memory ${action}d`);
    }
    return textResult(`memory: invalid call`);
  } catch (err) {
    return textResult(`memory failed: ${describeErr(err)}`);   // 错误文本，非异常
  }
});
```

> 工具 `description` 应简短（参数/行为）；**富引导**（何时记/跳过什么/冻结快照模型）由 memory skill 承载（FR-020，[`memory-skill-contract.md`](./memory-skill-contract.md)），注入 planner 系统提示词。

---

## 3. memory-client（agent gRPC client，仿 `prompt-client.ts`）

```ts
// projects/game/agent/src/memory-client.ts
export const MEMORY_SERVICE_TARGET = "dominion:///game/memory:50051";
export class MemoryClient {
  createMemory(template, session, memoryId, content): Promise<void>;
  updateMemory(template, session, memoryId, content): Promise<void>;
  deleteMemory(template, session, memoryId): Promise<void>;
  listMemories(template, session): Promise<{memory_id: string; content: string}[]>;
}
```

- 仿 `projects/game/agent/src/prompt-client.ts`：`registerDominionResolver` + proto-loader + keepalive/round_robin/TLS channel options（同款 `KEEPALIVE_OPTIONS`/`ROUND_ROBIN_SERVICE_CONFIG`）。
- 资源名构造：`templates/${template}/sessions/${session}/memories/${memoryId}`。
- `listMemories` 供：① 冻结快照烘焙（`memory-snapshot.ts`，见 team-graph-contract §3）；② `memory` 工具 `replace`/`remove` 的 `old_text` 子串定位（§1.1）。返回 `memory_id` + `content`（memory_id 供工具内部定位，不暴露给 LLM）。
- DI seam：构造器可选注入 `grpc.Client`（测试用 fake，无模块 mock，同 prompt-client）。
- **memory 服务存储 API 不变**（D9）：本契约的转换逻辑全在 agent 侧 memory-mcp.ts，memory-client 仅是直通 gRPC client。

---

## 4. mcp-host 装配（每 mcp 独立 path，template-scoped）

- **每个 mcp 一个独立 path**（不共用单 path）：memory mcp 与 saolei mcp 各自一个 path，path **须包含 `template` 字段**（template-scoped，team 改造前的路径风格），形如：
  - saolei mcp：`/internal/mcp/{template}/{session}/saolei`（player 连）
  - memory mcp：`/internal/mcp/{template}/{session}/memory`（planner 连）
- mcp-host（`projects/game/agent/src/mcp-host.ts`）按 **(template, session, mcpKind)** 路由到对应 McpServer 实例，懒创建。
- 两者独立 McpServer 实例（player 工具集 = saolei_operate 等；planner 工具集 = `memory` + `instruct_player`）。
- `SessionBridgeLookup` 扩展为同时提供 saolei `{bridge, sink}` 与 memory `{memoryClient, template, session}`（由 SessionTeam/server 装配）。
- **注意**：既有 saolei mcp path 为 `/internal/mcp/{sessionId}`（无 template）；本特性将 mcp path 统一为 template-scoped 多 path 方案——既有 saolei mcp 接线（`buildSaoleiMcpTools` 的连接 URL）须同步迁移到新 path 方案（tasks 落实，clean break）。

> 精确 path 段命名（`saolei`/`memory` 字面量 vs 枚举）由 plan 落实；约束：每 mcp 独立 path、path 含 template、planner 与 player 连各自 mcp、工具集互不交叉（FR-009/FR-010）。

---

## 5. 错误反馈与降级（FR：memory 服务不可用不阻断 gameplay）

- **`old_text` 0/多命中**：工具返回错误文本（含当前条目列表/命中预览，助 LLM 重选），非异常（§1.1）。
- memory 服务不可用 → 工具返回错误文本（如 "memory service unavailable"），LLM 据此决策（不抛 tool 异常）。
- 冻结快照读（`listMemories`，压缩/初始化边界）失败 → 保持上一次冻结快照（或空），不阻断 team 运行（调研 D4，spec Edge Case "memory 服务不可用"）。
- 具体重试/退避由 plan 决定（约束：不因 memory 不可用阻断 player gameplay）。

---

## 6. 验证要点

- planner 持**单一** `memory` 工具（hermes 式 `action`/`content`/`old_text`/`operations`），无 `memory_id`/无 `target` 参数；player 不持记忆工具。
- 工具参数不含 template/session（path 闭包注入）。
- 工具经 memory-client 转发到 MemoryService；mcp server 不直连 memory 服务（连接拓扑审查）。
- **agent 转换正确**：`add`→agent 生成 memory_id+Create；`replace`/`remove`→listMemories+`old_text` 子串定位 memory_id+Update/Delete；**0 命中/多不同命中 → 错误文本（含当前条目）**；全相同→作用首条；add 等价内容→成功（去重）。
- memory 服务存储 API 不变（Create/Update/Delete/List，memory_id 式资源）。
- memory 工具改存储不刷新冻结快照（冻结语义）。
