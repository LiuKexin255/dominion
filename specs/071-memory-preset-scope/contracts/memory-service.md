# Contract: MemoryService（preset 级作用域）

**Feature**: specs/071-memory-preset-scope/spec.md | **基线修订**: specs/039-planner-memory-calibration/contracts/memory-service-contract.md（创立）、specs/068-wiki-round-pause/contracts/memory-page-token-context.md（排序/分页，**不变**）、specs/069-wiki-template-web/contracts/memory-content-limit.md（长度上限，**不变**）——本文生效后，寻址作用域由 (template, session) 修订为 (template, preset)，其余条款零变化。

## 1. 资源与寻址

- 资源 pattern：`templates/{template}/presets/{preset}/memories/{memory}`（`projects/game/game.proto` Memory 资源注解）；`{memory}` 段承载 memory_id，资源消息无独立 id 字段（specs/068-wiki-round-pause/spec.md FR-017 延续）。
- parent 解析：`game.ParsePresetName`（agent_v2.proto Preset 资源的 codegen 产物，同 Go 包 `dominion/projects/game`；template 级父解析先例 `handler/wiki_handler.go`）+ `gameconst.ValidateTemplateName(presetName.Parent())`。
- **不做** parent preset 存在性校验（跨服务零依赖；名称寻址下写入不存在 preset = 为该名字预写集合，spec FR-012）。

## 2. RPC 契约（HTTP 面经 gateway `/api/v1`，注解自动生成）

### 2.1 CreateMemory

```text
POST /api/v1/{parent=templates/*/presets/*}/memories?memory_id=<id>
body: {"memory": {"content": "..."}}
```

- `parent` REQUIRED（preset 资源名）；`memory_id` REQUIRED，匹配 `[a-z0-9_-]+`（否则 INVALID_ARGUMENT）；`memory.content` ≤ 1024 rune（否则 INVALID_ARGUMENT，错误信息含上限与实际 rune 数）。
- 冲突：同 `(template, preset_id, memory_id)` 已存在 → `ALREADY_EXISTS`（HTTP 409）。
- 返回创建后的 Memory（name 含 preset 段）。

### 2.2 UpdateMemory

```text
PATCH /api/v1/{memory.name=templates/*/presets/*/memories/*}?update_mask=content
body: {"memory": {"name": "...", "content": "..."}}
```

- 目标身份 = `memory.name`；唯一可变字段 `content`（mask 仅支持 `content`）；content 上限校验同 2.1。
- 不存在 → `NOT_FOUND`；无 create-or-update。

### 2.3 DeleteMemory

```text
DELETE /api/v1/{name=templates/*/presets/*/memories/*}
```

- 成功 → 空（HTTP 200/204）；不存在 → `NOT_FOUND`。

### 2.4 ListMemories

```text
GET /api/v1/{parent=templates/*/presets/*}/memories
    ?page_size=&page_token=&order_by=
```

- 返回全量资源（含 content）；`order_by` 白名单 `memory_id`/`update_time`（语法、tie-breaker 补全、AIP-193 拒绝项不变——specs/068-wiki-round-pause/contracts/memory-page-token-context.md）；`page_token` 携带 resolved order_by 上下文，续页不一致 → INVALID_ARGUMENT（不变）。
- `page_size` 默认 100、上限 1000（不变）。
- 固定序补全基准：`memory_id asc`（不变）。

## 3. 生命周期与边界（引用 spec 裁定）

- 删除 session 不影响任何 memory 数据；删除 preset 不级联删除集合，同 id 重建 preset 后集合重新可达（spec FR-010）。
- 单 preset 集合无容量上限、无淘汰、无超限拒绝（spec FR-018）。
- 同内容（同摘要 id）重复创建 → ALREADY_EXISTS；调用方语义：agent 工具路径视为去重成功（dedupe 文本），web 前端呈现"已存在"提示（见 dsh-plugins.md / web-memory-view.md）。

## 4. 存储（引用 data-model.md §2）

文档字段 `preset_id`（替换 `session_id`）；索引 unique `(template, preset_id, memory_id)` + `(template, preset_id, update_time desc, memory_id asc)`；启动幂等 clean break（drop legacy session 维度索引 + 清 `session_id` 存在的旧文档）。

## 5. 验收锚点

1. preset 级四路由 CRUD/List 全通（testplan `projects/game/testplan/memory_test.go`）：创建 → 列表可见 → 更新生效 → 删除后不可见；409（同集合重复 id）、404（更新/删除不存在）、400（非法 id / 超限 content / 非法 order_by / token 上下文不匹配）。
2. 跨 preset 隔离：preset P1 的条目不出现在 P2 的 List 结果（零泄漏，spec SC-002）。
3. session 段寻址（旧形态）→ 404 路由不存在（gateway 仅注册 preset 形态路由）。
