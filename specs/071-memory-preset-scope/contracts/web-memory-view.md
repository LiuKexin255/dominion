# Contract: web memory 管理视图（preset 选择 + CRUD）

**Feature**: specs/071-memory-preset-scope/spec.md | **模式先例**: specs/069-wiki-template-web/contracts/web-wiki-view.md（视图组织/交互语汇/API 客户端形态）；REST 契约见 [memory-service.md](memory-service.md)

## 1. 视图入口与组织

- 视图经侧栏两级导航的浮出菜单进入（见 [web-nav.md](web-nav.md)）；主区与其他管理视图互斥渲染。
- 组织两级：顶部 preset 选择器（数据 = `api/agent.ts listPresets(template)`，全角色、不按 role 过滤——场景无关，spec FR-016）→ 选定后管理该 preset 的 memory 集合。
- 未选择 preset 时呈现选择引导；被删 preset 不再出现在选择器（集合数据保留于存储，spec FR-010）。

## 2. API 客户端（`src/api/memory.ts`）

- 形态沿用 `api/wiki.ts`：相对路径 + `requestJson`/`ApiError`（同源 gateway，零 CORS）；protojson 投影（camelCase、Timestamp 为 RFC 3339 字符串）。

```ts
interface Memory { name: string; content: string; createTime?: string; updateTime?: string }

listMemories(template, preset, {pageSize?, pageToken?, orderBy?}): Promise<MemoryPage>
// GET /api/v1/templates/{t}/presets/{p}/memories —— 视图默认 orderBy="update_time desc"、pageSize=20

createMemory(template, preset, content): Promise<Memory>
// id 前端派生：sha256(utf8(content)).hex[:32]（WebCrypto crypto.subtle，与工具路径 generateMemoryId 同值域）
// POST /api/v1/templates/{t}/presets/{p}/memories?memory_id=<digest>；body {"memory":{"content"}}

updateMemory(name, content): Promise<Memory>
// PATCH /api/v1/{name}?update_mask=content

deleteMemory(name): Promise<void>
// DELETE /api/v1/{name}
```

- 错误呈现：409 →"已存在"提示（同内容重复创建）；400 → 参数错误说明（含超限）；404 → 条目不存在；网络/5xx → 通用错误态。均不静默失败。

## 3. 视图行为（`MemoryView.tsx`，PresetsView/WikiView 模式）

- **列表**：分页（page token 续页）；行内容 = 条目 content 与 updateTime；默认序 update_time 降序（最近优先，spec Clarifications Q3）。无搜索框（memory 无 title 字段）。
- **查看**：条目全文直接来自列表行数据（Memory 无单条 Get RPC，List 返回全量资源）。
- **创建**：独占表单，仅 content 字段（≤1024 rune，前端预检 + 服务端 400 兜底）；id 由客户端派生，用户不可见不可改。
- **编辑**：独占表单改 content（update_mask=content）；保存后列表行即时反映新值。
- **删除**：inline 二步确认（行内确认按钮，无模态）。
- **状态**：loading / error / 空态三态齐备；空态文案场景无关并带创建入口。
- 切换 preset 重置列表与分页（不保留旧集合页 token）；表单/删除确认态随切换关闭。

## 4. 文案约束（场景无关，spec FR-016）

- 全部用户可见文案不出现场景角色词（planner/player）与场景示例（扫雷局面等）；消费方以中性词指代（"agent/助手"）。

## 5. 验收锚点

1. vitest：选择 preset → 列表默认序断言（update_time desc）→ 创建（摘要 id 派生断言、409 提示）→ 编辑（mask 传递）→ 二步删除。
2. vitest：场景词扫描——渲染输出（含空态/占位/错误文案）不含 `planner`/`player`/`saolei`/`扫雷` 等场景词。
3. REST 闭环（testplan memory_test.go，preset 级路由）覆盖同集合 CRUD；前端仅验证 UI 逻辑（无浏览器执行面，069 R8 同款裁断）。
