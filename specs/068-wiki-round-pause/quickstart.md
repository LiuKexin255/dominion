# Quickstart: planner wiki 知识库与 saolei-loop 每五轮自然停 验证指南

> **Feature**: specs/068-wiki-round-pause/spec.md
> 契约引用：[contracts/wiki-service.md](contracts/wiki-service.md)（API 面）、[contracts/dsh-plugins.md](contracts/dsh-plugins.md)（工具面）、[contracts/saolei-loop-round-stop.md](contracts/saolei-loop-round-stop.md)（自然停）、[contracts/memory-resource-identity.md](contracts/memory-resource-identity.md)（memory_id 移除）；数据模型 [data-model.md](data-model.md)。

## 前置

- 仓库构建/测试入口 bazel（`AGENTS.md`）；大型测试经 testplan skill 执行（`style/large_test.md`）。
- 场景 1/3 的 SUT 部署复用 testplan 既有模式（`projects/game/testplan/` 下的 deploy yaml + `guitar run`）。

## 场景 1：wiki 服务端 API（gateway HTTP，大型测试）

测试文件：`projects/game/testplan/wiki_test.go`（仿 `memory_test.go` 的 helper 形态）。

1. Create：`POST /api/v1/{parent}/wikis?wiki_id=...`（title/content）→ 201/200 返回资源，`name` 后缀 `/wikis/{wiki_id}`，**响应无 `wikiId` 字段**（FR-017）。
2. title 唯一：同 parent 再 create 规范化同名（如大小写/全角变体）→ 409 ALREADY_EXISTS。
3. Get/Update/Delete：按 name 寻址；Update 改 title 后旧 title 检索不到、新 title 可检索。
4. List：默认 `wiki_id asc` 固定序 + 分页续页（token 透传）；响应含完整 content。
5. Search：`GET .../wikis:search?query=...`——中文子串、大小写/全半角不敏感、任意位置命中；响应条目仅 `{name, title}`（无 content）；排序 精确>前缀>子串；单字符 query 走回退路径同样命中；续页 `page_token` 携带、换 query 续页 → 400。
6. 超限：title > 128 rune / content > 64KB → 400 INVALID_ARGUMENT。
7. Memory 回归：`memory_test.go` 改造后（name 断言）全绿。

预期：全部用例通过（宪法 VI：部署→测试→清理闭环，全绿才算验收）。

## 场景 2：planner 工具链路（agent_v2，单测）

1. **单测**（`common/js/dsh-plugins/`）：
   - memory 插件行注册断言：`wiki_search`（参数仅 query）/`wiki_get`/`wiki_write` 三工具 + `memory:guidance`(110)/`wiki:guidance`(120)/`memory:snapshot`(200) 三 section；player 侧因 templateRules 不挂载。
   - prompt/描述文本断言：memory 描述无场景名词、含 top-10 说明、英文；分工段无用法、用法段无分工；均无 memGPT/Letta 术语；系统提示 section 集合与既有一致、wiki 不注入任何内容。
   - 操作核：`wiki_write` upsert（同 title 覆盖 content）、批量 preflight 原子（任一条目超限全批不写）、`wiki_search` 跨页聚合标题（page_token 折叠）、`wiki_get` 批量部分未命中引导、空 `entries`/空输入引导、结果文本无 name/wiki_id、`wiki failed: …` 文本化失败。
   - 挂载面断言：planner 池模板物化组合含三工具与三 section；player 池模板不挂载任何 wiki 工具（templateRules，复用 `projects/game/agent_v2/src/dsh.test.ts`）。

预期：单测全绿；既有 agent_v2 大型测试（`projects/game/testplan/system_test.yaml` 各 suite）作零回归；本链路不单设大型测试（裁断与理由见 `research.md` §7）。

## 场景 3：memory_id 移除（零回归）

`projects/game/testplan/memory_test.go`：全部断言改为 `GetName()` 形态后随场景 1 的 testplan 一并实际执行，全绿即验收（契约 [memory-resource-identity.md](contracts/memory-resource-identity.md) §3）。

## 场景 4：自然停（orchestrator 单测）

`common/js/dsh-plugins/saolei-loop/src/orchestrator.test.ts` 扩展 [saolei-loop-round-stop.md](contracts/saolei-loop-round-stop.md) §5 的 8 条用例（fake 成员/终态记录注入）：

- 第 5 轮停（无驱动、activation=player、无错误态）、排队消化后停、submit 恢复驱动 player 且 relays 随行、中途重开不计数、失败重试后计数、新一轮从 1 计数、停点不消费 player relays。

预期：全部通过。E2E 裁断与理由见 research.md §7。

## 快速手验（可选，部署环境）

1. `bazel test //projects/game/memory/... //common/js/dsh-plugins/...`（相关 target 单测）。
2. testplan：`guitar run projects/game/testplan/system_test.yaml` 场景 1/3 的 suite（testplan skill，含部署与清理）。
