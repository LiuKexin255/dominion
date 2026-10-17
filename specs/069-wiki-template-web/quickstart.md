# Quickstart: wiki template 级作用域 + memory 上限 + web wiki 管理

**Feature**: specs/069-wiki-template-web/spec.md | 契约索引: [contracts/](contracts/)（[wiki-service.md](contracts/wiki-service.md)、[memory-content-limit.md](contracts/memory-content-limit.md)、[dsh-plugins.md](contracts/dsh-plugins.md)、[web-wiki-view.md](contracts/web-wiki-view.md)）| 数据模型: [data-model.md](data-model.md)

## 前置

- 仓库构建/测试入口：`bazel`（AGENTS.md）；大型测试经 testplan skill（`guitar run`，`style/large_test.md`）。
- 涉及服务：memory（WikiService + MemoryService 同二进制）、gateway（REST 注册随 proto 注解自动变更）、web（静态托管，前端重新构建生效）。

## 场景 1：服务端 template 级 wiki API（testplan 大型测试）

`projects/game/testplan/wiki_test.go`（template 级路由修订后）覆盖：Create/Get/Update/Delete、title 唯一冲突（409）、List 固定序 + 分页、Search 子串/中文/大小写/短查询/续页一致性、超限拒绝（400）。

```bash
# 经 testplan skill 执行（部署→测试→清理闭环，宪法 VI）
guitar run projects/game/testplan/system_test.yaml   # 含 wiki/memory suites
```

通过标准：全部用例通过（all cases passed）。

## 场景 2：memory content 上限（单测 + REST）

```bash
# Go 单测：ValidateMemoryContent case 表 + handler 错误映射
bazel test //projects/game/memory/...

# JS 单测：预检文本（含 1024 上限与 wiki_write 引导）、批量原子、guidance 措辞
bazel test //common/js/dsh-plugins/memory-service/... //common/js/dsh-plugins/memory/...
```

REST 验证（testplan `memory_test.go` 增例，或部署后手工）：

```bash
curl -s -X POST "http://<host>/api/v1/templates/saolei/sessions/<s>/memories?memory_id=m1" \
  -H 'Content-Type: application/json' \
  -d '{"memory": {"content": "<超过 1024 rune 的内容>"}}'
# 期望：400 INVALID_ARGUMENT，消息含上限与实际长度
```

## 场景 3：跨 session 共享（工具面语义，JS 单测）

`common/js/dsh-plugins/memory-service/src/wiki-operations.test.ts`：两个 scope（同 template `saolei`、不同 session）分别驱动 `wikiWrite`/`wikiSearch`，断言命中同一集合（fake store 收到相同 template 寻址、无 session 维度）。

```bash
bazel test //common/js/dsh-plugins/memory-service/...
```

## 场景 4：web wiki 管理闭环（vitest + 手工 walkthrough）

```bash
# 组件与 API 客户端测试
bazel test //projects/game/web/frontend:lib_test
```

手工 walkthrough（部署后浏览器，契约 [web-wiki-view.md](contracts/web-wiki-view.md) §3）：

1. 打开 web → 侧栏切到 wiki 视图 → 空态引导可见。
2. 创建条目（title + content）→ 列表出现；再次以同名创建 → 呈现"已存在"错误。
3. 搜索关键词（中英文/大小写混合）→ 命中子串条目；清空关键词 → List 分页浏览。
4. 编辑条目（改 title/content）→ 保存后列表与详情反映新值。
5. 删除条目（二步确认）→ 列表移除，后续检索不命中。

## 场景 5：启动 clean break（存储）

部署新 memory 服务后，`game_memory.wikis` 集合：旧 session 维度索引被删除、`session_id` 字段存在的旧文档被清除（data-model §2.3 幂等清理）；可用 `mongosh` 抽查确认无 `session_id` 字段文档残留。

## 回归

```bash
bazel test //projects/game/... //common/js/dsh-plugins/...   # 既有 wiki 行为零回归（SC-004）；memory 面零回归（SC-003）
```
