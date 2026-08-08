# Contract: memory skill（planner 专属，memory mcp 配套引导）

**Feature**: `039-planner-memory-calibration` | **Spec**: [`spec.md`](../spec.md) FR-020 | **Research**: D11/D12.2

> 参考 hermes 记忆工具引导（[`research.md`](../research.md) D11，来源 [hermes `tools/memory_tool.py`](https://github.com/NousResearch/hermes-agent/blob/main/tools/memory_tool.py) `MEMORY_SCHEMA.description` 的 HOW/WHEN/SKIP），为 memory mcp 编写配套 `SKILL.md`，注入 planner 系统提示词（与 saolei skill 注入 player 对称）。skill 是**静态指令**（随 createAgent systemPrompt 稳定，保 prefix cache），与冻结快照（**数据**，input SystemMessage）分离。实现于 `projects/game/agent/src/skill/memory/SKILL.md`。

---

## 1. 文件与格式契约

- **路径**：`projects/game/agent/src/skill/memory/SKILL.md`（folder name `memory` === frontmatter `name`）。
- **格式**：遵循 [`specs/020-agent-resources-layout/contracts/skill-md-format.md`](../../020-agent-resources-layout/contracts/skill-md-format.md)（agentskills.io 开放标准 + OpenCode 识别子集）。
- **data_files**：`artifact_pkg_js` target 须含 `src/skill/memory/SKILL.md`（同 saolei skill 既有 data_files 模式；tasks 落实）。
- **篇幅**：<500 行 / <5000 tokens（soft limit）。

### Frontmatter（最小可行 + 仓库约定）

```yaml
---
name: memory
description: Guides the planner on how and when to use the memory tool (action/content/old_text/operations) to maintain its own long-term review memory, and on the frozen-snapshot model. Use this skill when the session profile has the memory MCP enabled and the planner must record, update, or remove a long-term memory entry.
compatibility: opencode
metadata:
  audience: dominion
  scope: memory-mcp
---
```

---

## 2. 注入装配（与 saolei skill 对称，D12.2）

```text
src/skill-loader.ts
  BUILTIN_SKILL_NAMES = ["saolei", "memory"]   // 新增 "memory"

src/team/planner.ts  (createPlannerNode 组装 systemPrompt)
  const systemPrompt = appendSkillBodyToPrompt(base, ["memory"]) + buildToolDescriptionSection(...);
                                                        ↑ 当前 planner 不调；T020 需补
```

- **注入对象**：仅 planner（player 不持记忆工具，**不**注入 memory skill——player 仅注入 saolei skill，FR-009）。
- **注入位置**：烘焙进 createAgent 的**静态 systemPrompt**（template-fixed，031 FR-028），不随复盘变化（保 prefix cache，D11 Alternatives）。
- **与冻结快照分离**：systemPrompt = base + memory skill body + 工具描述（静态）；冻结快照 = input SystemMessage（数据，每条纯内容，team-graph-contract §3）。

> 先例：`projects/game/agent/src/team/player.ts:154` 已 `appendSkillBodyToPrompt(base, ["saolei"])`；planner 的 memory skill 注入与之完全对称。

---

## 3. skill body 内容规范（适配 dominion 复盘域 + 冻结快照）

skill body（Markdown，frontmatter 之后）MUST 覆盖以下主题。措辞由 tasks 落实（参考 hermes HOW/WHEN/SKIP + dominion 特有语义）；以下为**必须覆盖的要点**（非逐字文案）：

### 3.1 工具用法（HOW）

- `memory` 工具为**单一工具**，参数 `action`∈{add/replace/remove}、`content`、`old_text`、`operations`。
- `add(content)`：记录新条目。
- `replace(old_text, content)` / `remove(old_text)`：用 **`old_text` 短子串**定位既有条目（无需全文、无需 id）。
- **子串匹配自纠错**：0 命中 → 工具回传当前全部条目，重选更具体的 `old_text`；多不同条目命中 → 回传命中预览，要求更具体子串。
- 批量 `operations` 可一次原子改多条（dominion v1 无硬上限，单次操作即够，批量可选）。

### 3.2 何时记录（WHEN，适配复盘域）

参考 hermes "save proactively" 但改写为 dominion planner 复盘语义——**复盘后**主动记录跨局累积的认知：
- player **重复犯的错误模式**（如"player 在边角频繁误标地雷"）。
- **被验证有效的策略/技巧**（如"开局先点中心区域效率更高"）。
- planner 自身**复盘方法论演化**（如"评估 player 应优先看操作次数与正确标记比")。
- 优先级：重复模式 > 验证有效策略 > 方法论。

### 3.3 跳过什么（SKIP，适配复盘域）

- **单局偶发失误**（不构成模式的孤立错误）。
- **具体坐标/棋盘瞬态**（如"(3,5) 是地雷"——局特定，无跨局价值）。
- **易重新推导的事实**（扫雷通用规则）。
- **指令内容**（发给 player 的指令在 playerMessages 里，不进 planner 长期记忆——两者分工，调研 §6.5）。

### 3.4 冻结快照模型（dominion 特有，必须说明）

- planner 系统提示词中的长期记忆是**冻结快照**：一次烘焙、跨多次复盘保持不变。
- **关键**：通过 `memory` 工具 add/replace/remove 改记忆后，变更**立即持久化**，但**不会立即出现在你的系统提示词里**——要到**压缩边界（每 5 局）**才刷新进快照。
- 因此：不要因"刚 add 的记忆没在当前快照里"而重复 add 同一洞察。
- 工具结果（tool result）始终反映 live 状态（成功/失败/当前条目），可作为操作反馈。

### 3.5 条目写作风格

参考 hermes "compact, information-dense"：每条精炼、信息密集、可跨局复用（如"player 倾向在数字格附近过度标记，应在复盘指令中强调先 deduce 再 flag"），非流水账。

---

## 4. 验证要点

- `src/skill/memory/SKILL.md` 存在，frontmatter `name: memory` === folder name，符合 `specs/020-agent-resources-layout/contracts/skill-md-format.md`。
- `skill-loader.ts` 的 `BUILTIN_SKILL_NAMES` 含 `"memory"`；`loadSkillBody("memory")` 返回非空 body。
- planner 系统提示词含 memory skill body（经 `appendSkillBodyToPrompt(base, ["memory"])` + `SKILL_PROMPT_SEPARATOR`）；player 系统提示词**不含** memory skill（player 仅 saolei skill）。
- skill body 覆盖 §3.1-3.5 要点（工具用法、何时记、跳过什么、冻结快照模型、写作风格）。
- skill body 烘焙进**静态 systemPrompt**（非 input SystemMessage），不随复盘变化（prefix cache 友好）。
