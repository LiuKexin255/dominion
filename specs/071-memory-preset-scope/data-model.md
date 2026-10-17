# Data Model: memory 作用域从 session 迁移至 preset、web memory 管理与侧栏二级导航

**Feature**: specs/071-memory-preset-scope/spec.md | **基线**: specs/069-wiki-template-web/data-model.md §1.2（本文生效后其 Memory 作用域表述由本特性修订）；排序/分页/资源身份契约沿用 specs/068-wiki-round-pause/contracts/memory-page-token-context.md

## 1. 资源与校验

### 1.1 Memory（作用域修订：preset 级）

```text
Memory {
  name        string    // templates/{template}/presets/{preset}/memories/{memory}（id 由 {memory} 段承载，无独立 id 字段——specs/068-wiki-round-pause/spec.md FR-017 延续）
  content     string    // ≤ 1024 rune（specs/069-wiki-template-web/contracts/memory-content-limit.md §1-2，utf8.RuneCountInString 度量）
  create_time Timestamp // output-only
  update_time Timestamp // output-only
}
作用域键：(template, preset)——session 不再参与 memory 寻址
```

- 校验（domain 层，违反 → INVALID_ARGUMENT）：`memory_id` 匹配 `[a-z0-9_-]+`（不变）；content ≤ 1024 rune（不变）；page_size 默认 100、上限 1000（不变）。
- 唯一性（违反 → ALREADY_EXISTS）：`(template, preset_id, memory_id)`——同一 preset 下 id 唯一；不同 preset 互不冲突（spec FR-002）。
- **id 派生约定**：两条写入路径共用同一内容摘要方案 `sha256(utf8(content)).hex[:32]`——agent 工具路径 `common/js/dsh-plugins/memory-service/src/operations.ts generateMemoryId`（node `createHash`）、web 前端 `projects/game/web/frontend/src/api/memory.ts`（WebCrypto `crypto.subtle`）；同内容在两路径下生成相同 id，"同内容同条目"全局一致（spec Clarifications Session 2026-09-28 Q1）。
- 排序与分页：`order_by`（`memory_id`/`update_time`，白名单与语法不变）、page token 携带 order_by 上下文校验（AIP-193/AIP-158）——全部沿用 specs/068-wiki-round-pause/contracts/memory-page-token-context.md，仅作用域键变更（spec FR-003）。
- 容量：单 preset 集合不设条数上限、无淘汰、无超限拒绝（spec FR-018）。

### 1.2 Preset 与 memory 的隐式关联

```text
Preset { name: templates/{template}/presets/{preset}, persona, role, ... }  // 属性零变更（specs/059-agent-v2-team-mode）
关联：每个 preset 名下一个 memory 集合（隐式，无外键、无存在性校验）
```

- memory 服务对 parent 仅做 `game.ParsePresetName` 解析 + `gameconst.ValidateTemplateName(presetName.Parent())` 校验，不调用 preset 面校验存在（spec FR-012；跨服务零依赖方向）。
- 生命周期：删除 preset 不级联删除其集合；同 id 重建 preset 后集合重新可达（spec FR-010）。
- 消费面：从 preset 物化的 planner member 经 `PlannerMemoryScope {template, preset}` 关联集合（§3）；player 角色不挂 memory 工具（不变）。

### 1.3 Session 与 memory 解耦

Session（`templates/{template}/sessions/{session}`）不再出现在任何 memory 寻址中；session 删除与 memory 数据无交集（spec FR-010）。`game.proto` Session 消息与 SessionService 零变更。

## 2. 存储（mongo `game_memory` 库）

### 2.1 memories 集合文档（终态）

```json
{
  "template":    "saolei",
  "preset_id":   "planner-default",
  "memory_id":   "sha256(content)[:32]",
  "content":     "记忆文本（≤1024 rune）",
  "create_time": ISODate(...),
  "update_time": ISODate(...)
}
```

`session_id` 字段**不再存在**（文档与查询全部去 session 维度）。

### 2.2 索引（启动时创建）

| 索引 | 用途 |
|---|---|
| unique `(template, preset_id, memory_id)` | 身份；Create 冲突 → ALREADY_EXISTS |
| `(template, preset_id, update_time desc, memory_id asc)` | List 排序（`update_time`/`memory_id` 双字段 order_by 与游标 seek）；快照 `update_time desc` 前缀复用 |

### 2.3 旧作用域数据处置（一次性运维清理，新服务首次启动前执行）

- 存储终态：`memories` 集合仅含 `preset_id` 维度的文档与索引（§2.1/§2.2）；服务启动仅创建新索引，**不含**旧索引/旧文档清理代码（一次性清理项不进代码）。
- 清理时机（顺序约束）：旧 memory 服务停止后、新 memory 服务**首次启动前**经 mongosh 单独执行一次；若新服务已先启动，清理后须重启一次（索引仅启动时创建，清理不触发补建）。顺序不可倒置：unique `(template, preset_id, memory_id)` 索引于启动时创建，legacy 文档缺 `preset_id`（按 null 参与唯一性），同 template 下相同 `memory_id` 的旧文档尚存会使该索引构建失败（E11000）且被启动代码按已存在忽略——服务将运行在无唯一索引状态。
- 清理对象：drop 两个 legacy 索引 `(template, session_id, memory_id)` unique 与 `(template, session_id, update_time desc, memory_id asc)`（默认命名 `template_1_session_id_1_memory_id_1`、`template_1_session_id_1_update_time_-1_memory_id_1`，以 `db.memories.getIndexes()` 实名为准）；`deleteMany({session_id: {$exists: true}})` 清除全部旧作用域文档。
- 清后核验：无 `session_id` 残留，且两个 preset 维度新索引存在（默认命名 `template_1_preset_id_1_memory_id_1`、`template_1_preset_id_1_update_time_-1_memory_id_1`）——缺失即 unique 索引静默构建失败（步骤见 `specs/071-memory-preset-scope/quickstart.md` 场景 7）。
- `game_agent_v2.presets`（preset 存储）零变更。

## 3. 作用域与绑定模型（JS）

### 3.1 PlannerMemoryScope（形态变更）

```text
PlannerMemoryScope { template: string, preset: string }
// 原 {template, session}；两处声明同形：common/js/dsh-plugins/saolei-loop/src/orchestrator.ts、
// common/js/dsh-plugins/memory-service/src/service.ts（结构同型不共享依赖，069 前例）
```

### 3.2 绑定与快照缓存（语义不变）

- `plannerMemory.load(agentCtx, scope)`：以 scope 两半构造 parent `templates/{template}/presets/{preset}`，一次 `listMemories(order_by="update_time desc", page_size=10)` 预取，渲染快照缓存按 agent 的 `ScopeKey` 键存；fail-loud（拒绝则整个 member 物化回滚，`specs/064-memory-split-fold-remain/contracts/dsh-plugins.md §2` 不变）。
- `applyCall(agent, args)`/`snapshot(scope)`：写路径与读面绑定语义不变；wiki 面（`wikiSearch/wikiGet/wikiWrite`）仍仅转发 `scope.template`（template 级，069 契约不变）。

### 3.3 物化装配（agent_v2）

```text
doMaterialize(): memoryScope = { template: parsedSession.template, preset: presetId(planner.preset) }
// planner.preset 为 member 物化输入的 preset 资源名；presetId() 既有解析（validateMemberPreset 已保证可解析）
// team 刷新换 planner preset → 新 memoryScope → 新集合快照（FR-009）
```

## 4. web 视图状态模型（纯前端）

```text
MemoryView {
  presetKey: string | null        // 选中 preset id（选择器来自 listPresets(template)，全角色）
  items: Memory[]                 // 当前页列表（ListMemories，默认 order_by="update_time desc"、page_size=20）
  pageToken: string               // '' = 首页/末页
  form: null | {mode: create|edit, content, targetName?}   // 独占表单
  deleteConfirm: string | null    // inline 二步删除的目标 name
  loading / error: 视图级状态
}
App 导航 {
  view: 'sessions' | 'presets' | 'wiki' | 'memory'   // 主区互斥渲染
  knowledgeMenuOpen: boolean                          // 浮出菜单瞬态（不承载视图状态）
}
```

## 5. 资源 name 形态总表（终态）

| 资源 | name 形态 | 说明 |
|---|---|---|
| Template | `templates/{template}` | 不变 |
| Session | `templates/{template}/sessions/{session}` | 不变 |
| Preset | `templates/{template}/presets/{preset}` | 不变 |
| Memory | `templates/{template}/presets/{preset}/memories/{memory}` | **变更**（原 `templates/{template}/sessions/{session}/memories/{memory}`） |
| Wiki | `templates/{template}/wikis/{wiki}` | 不变（069 终态） |
