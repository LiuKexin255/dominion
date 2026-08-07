# Contract: Team Graph 重建（profile 变更）

**Feature**: `040-team-singleton-conformance` | **Spec**: [spec.md](spec.md) | **Research**: R7

> 定义 `UpdateTeam` 在 profile 变更时重建 team graph 的行为契约（agent 服务内部）。这是本特性最高风险项（FR-005/FR-006）。依据 `projects/game/agent/src/team/graph.ts` 现状。

---

## 1. 触发条件

`UpdateTeam` 在**已物化** Team 上、且 `team.profile` ≠ 既有 profile 时触发重建。

- 不触发：未物化（→ 物化路径，新建 graph）；已物化且 profile 相同（→ 幂等返回）。
- 重建仅在"无 turn in-flight"时进行；in-flight → FAILED_PRECONDITION（FR-006，复用 `RefreshTeam` 的 `team.isRunning()` 守卫，`projects/game/agent/src/handler.ts:231-238`）。

---

## 2. 状态保留（checkpointer 复用）

**必须保留**（按 `thread_id=sessionId` 从既有 MemorySaver 重建）：

| TeamState 通道 | 保留内容 | 来源 |
|---|---|---|
| `playerMessages` | player 对话历史（含 user/agent/tool 消息） | 既有 checkpointer |
| `plannerMessages` | planner 对话历史 | 既有 checkpointer |
| `gameEnded` | 当前 game-ended 状态（通常为 null） | 既有 checkpointer |
| `gameCounter` | 局数计数 | 既有 checkpointer |

**机制**：
- `TeamGraphHandle.checkpointer`（`projects/game/agent/src/team/graph.ts:146`）已 public 暴露。
- `buildTeamGraph(deps)`（`graph.ts:213`）增**可选** `checkpointer?: MemorySaver` 入参（建议加到 `TeamGraphDeps` 或作独立参数）；缺省 → `new MemorySaver()`（首建）；重建 → 传入既有 checkpointer。
- `TeamState`（`graph.ts:67-89`）是模块私有同一 `Annotation.Root` schema，recompiled graph 与既有 checkpointer schema 一致；`messagesStateReducer` 等 reducer 语义稳定 → 通道按 `thread_id` 正确重建。
- **不新建** MemorySaver（否则历史丢失，违反 FR-005）。

---

## 3. 复用不变（与 profile 无关）

重建时**复用既有实例**，不重建：

- `EphemeralGameBuffer`（`buffer`）—— 游戏态缓冲，profile 无关。
- `OperationBridge`（`bridge`）—— player 独占操作桥，profile 无关。
- `SaoleiEventSink`（`sink`）—— 绑定 buffer，profile 无关。
- MCP-host 的 per-session `McpServer`（`projects/game/agent/src/mcp-host.ts`，按 sessionId 缓存）—— 由 bridge/sink 构建，profile 无关，无需重连。
- `TurnLoop`（若有）—— 重建后随新 graph handle 生效于下一 turn。

---

## 4. 变更项（随新 profile）

重建时**按新 profile 重新解析并烘焙**：

| 项 | 来源 | 烘焙点 |
|---|---|---|
| player LLM model | `SaoleiProfile.player_model` | `player.ts` createAgentFn 闭包（编译时） |
| planner LLM model | `SaoleiProfile.planner_model` | `planner.ts` createAgentFn 闭包（编译时） |
| player base prompt | `SaoleiProfile.player_prompt`（空=模板默认） | `player.ts` systemPrompt（编译时） |
| planner base prompt | `SaoleiProfile.planner_prompt`（空=模板默认） | `planner.ts` systemPrompt（编译时） |
| player tools | saolei MCP tools（模板固定，FR-028，profile 无关但随 graph 重绑） | `player.ts` tools |

profile 经 `promptClient.getTeamProfile(template, profileName)`（`projects/game/agent/src/prompt-client.ts`）解析。

---

## 5. 重建流程（agent 侧）

```text
UpdateTeam(已物化, profile=P'≠P):
  1. team.isRunning()? → YES: FAILED_PRECONDITION（FR-006）
  2. 单飞：pending map 占位（避免并发重建）
  3. 解析 P' → getTeamProfile → 新 deps（models/prompts）
  4. buildTeamGraph(new deps, checkpointer=既有 handle.checkpointer)  // 复用，research.md §R7
  5. 替换 SessionTeam 的 graphHandle（由 readonly 改可替换，或加 rebuildProfile 方法）
  6. 更新 teams map 的 profileName = P'
  7. 返回 Team(profile=P')；既有 buffer/bridge/sink/MCP 复用
  异常：步骤 3-4 任一失败 → 既有 Team 不变，返回错误（不留半重建状态）；清理 pending
```

---

## 6. 不重跑 initInstruction

- profile 变更重建**不**触发 initInstruction（init 仅 graph **首建**触发，见 [../research.md](../research.md) R8）。
- initInstruction 属 spec 039（未实现）；本特性仅保证触发点随首建迁移，不在本特性实现 init。

---

## 7. 单测要点

- 重建后 `getTeamState()` 返回的 playerMessages/plannerMessages 计数与内容 = 重建前（零丢失/零重复，FR-005/SC-003）。
- 重建后下一 turn 使用新 model/prompt（可用 fake provider 断言 model 切换）。
- in-flight 重建 → FAILED_PRECONDITION，既有 graph 与 turn 不受影响（FR-006）。
- 重建失败 → 既有 Team profile 不变、仍可正常 turn。
- 并发重建同一 session → 单飞（仅一次 buildTeamGraph）。
- checkpointer 复用断言：重建前后 `handle.checkpointer` 引用相同（`toBe(既有)`）。

---

## 8. 风险与验证

- **风险**：checkpointer schema 漂移——若未来 `TeamState` 通道变更，重建读旧 checkpoint 可能不一致。缓解：TeamState 为模块私有单一 schema，重建用同一编译单元的 schema，无版本漂移；本特性不改 TeamState。
- **风险**：langgraph MemorySaver 跨 graph 实例读取的兼容性。缓解：MemorySaver 是按 thread_id 的 KV 存储，与 graph 实例解耦；须在单测显式验证（§7 checkpointer 引用断言 + 历史保留断言）。
- 验证门禁：`bazel test //projects/game/agent/...` 含上述单测全通过；大型测试（[../quickstart.md](../quickstart.md) 场景 3）端到端验证重建 + 历史保留。
