# Contract: web wiki 管理视图（CRUD + search）

> **Feature**: specs/069-wiki-template-web/spec.md（FR-005~007）
> **锚点**: `projects/game/web/frontend/src/api/wiki.ts`（新）、`src/components/WikiView.tsx`（新）、`src/App.tsx`（视图切换扩展）
> **基线**: `specs/051-agent-v2-dsh-migration/contracts/web-frontend.md`（视图切换与 API 客户端模式）；交互语汇沿用 `specs/054-agent-v2-bugfixes/contracts/web-ui.md` §6（独占表单、二步删除、错误呈现）
> **API 面**: [wiki-service.md](wiki-service.md) §2（template 级 REST，本视图的唯一后端依赖）

## 1. 视图入口与组织

- 侧栏视图切换增加第三视图 `wiki`（`sessions | presets | wiki`），主区条件渲染 `<WikiView template={TEMPLATE_SAOLEI} />`——固定 saolei（knownTemplateIDs 封闭集当前仅此一个，对齐 PresetsView 固定 template 做法；spec Assumptions）。
- 视图为纯前端 state（搜索词、分页 token、表单模式、删除确认），切换视图不保留服务器会话态；与 session 视图互斥渲染（PresetsView 同构）。

## 2. API 客户端（`src/api/wiki.ts`）

- 全部相对路径（`/api/v1/...`）经 `requestJson`/`ApiError`——同源 gateway，零 CORS（web README 拓扑）。
- 方法：`createWiki(template, wikiId, title, content)`、`listWikis(template, {pageSize, pageToken})`、`searchWikis(template, query, {pageSize, pageToken})`、`getWiki(name)`、`updateWiki(name, {title?, content?})`、`deleteWiki(name)`。
- `wiki_id` 生成：`sha256(title_norm)[:32]`（JS 侧 NormalizeTitle 孪生，与服务端/工具面同源——data-model §4）。派生依赖 `crypto.subtle`（Web Crypto `SubtleCrypto.digest`）——该 API 仅 secure context（HTTPS 或 localhost）可用；部署面由 gateway 终止 TLS、入口 `https://game.liukexin.com` 满足该前提（`projects/game/web/frontend/src/api/wiki.ts` 的 `wikiIdForTitle`；https://developer.mozilla.org/en-US/docs/Web/API/SubtleCrypto/digest）。
- protojson 投影：`Wiki { name, title, content, createTime?, updateTime? }`；Search 轻量条目 `{ name, title }`。

## 3. 视图行为（`WikiView.tsx`，PresetsView 模式）

1. **搜索**：关键词非空 → `searchWikis`（命中 title 列表，语义与服务端一致：子串、大小写/全半角不敏感）；关键词为空 → `listWikis`（服务端固定 id 升序——spec Clarifications 2026-09-20 裁定，不引入 title 浏览序）。分页经 `nextPageToken`（上一页/下一页）。
2. **列表条目**：title + update_time（locale 格式化）+ 操作（查看/编辑、删除）；搜索结果条目点击 → `getWiki` 展示完整 content。
3. **创建/编辑**：独占表单（编辑期间列表不渲染——054 §6 独占语义）；title 用 `Input`、content 用原生 `<textarea>`（PresetsView persona 先例）+ 主题样式；编辑提交 `PATCH /api/v1/{wiki.name=templates/*/wikis/*}?update_mask=<改动字段>`（按实际修改字段构造 mask）。
4. **删除**：inline 二步确认（待确认态 → 确认执行），成功后从列表移除。
5. **状态呈现**：loading（请求中）、error（请求失败，`ApiError.message` 呈现、不静默）、空态（无结果引导创建/换关键词）。
6. **错误场景**：创建重名（409 ALREADY_EXISTS）、编辑改 title 冲突（409）、目标被并发删除（404）、超限（400：title > 128 rune / content > 64KB）——均呈现服务端错误消息。

## 4. 边界与竞态

- 多标签页并发编辑：最后写入胜出（无乐观锁，服务端 Update 无 etag——spec Edge Cases）。
- 删除与 planner 写入并发：删除后同名 upsert 走创建路径重建（语义自然闭合）。
- 正在编辑的条目被外部删除：提交时 404 呈现并返回列表（不吞错误）。
- 表单超限不做事前阻断：提交后由服务端 400 呈现（服务端为唯一校验源，避免前端重复维护 128 rune/64KB 规则——简化取舍，记录于 research R5 范围内）。

## 5. 验收锚点

- vitest：`api/wiki.ts` fetch mock 断言（每方法 URL/method/body/query 形状与响应投影，含 `update_mask` query 传递）；`WikiView` 组件测试（搜索/空关键词 List、创建成功入列、重名错误呈现、编辑保存、二步删除、空态/错误态）；`App` 三视图切换渲染。
- testplan：REST 闭环由 `wiki_test.go` 覆盖（[wiki-service.md](wiki-service.md) §7）；本视图不新增大型测试面（无浏览器执行面，UI 逻辑由组件测试覆盖——research R8）。
