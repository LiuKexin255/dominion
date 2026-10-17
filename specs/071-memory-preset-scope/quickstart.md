# Quickstart: memory preset 级作用域 + web memory 管理 + 侧栏二级导航

**Feature**: specs/071-memory-preset-scope/spec.md | 契约索引: [contracts/](contracts/)（[memory-service.md](contracts/memory-service.md)、[dsh-plugins.md](contracts/dsh-plugins.md)、[web-memory-view.md](contracts/web-memory-view.md)、[web-nav.md](contracts/web-nav.md)）| 数据模型: [data-model.md](data-model.md)

## 前置

- 仓库构建/测试入口：`bazel`（AGENTS.md）；大型测试经 testplan skill（`guitar run`，`style/large_test.md`）。
- 涉及服务：memory（MemoryService）、gateway（REST 注册随 proto 注解自动变更，零代码改动）、agent_v2（物化装配）、web（静态托管，前端重新构建生效）。

## 场景 1：服务端 preset 级 memory API（testplan 大型测试）

`projects/game/testplan/memory_test.go`（preset 级路由修订后）覆盖：Create/Update/Delete/List、同集合重复 id 409、跨 preset 隔离（零泄漏）、order_by/分页/token 上下文、超限拒绝 400、旧 session 形态路由 404。

```bash
# 经 testplan skill 执行（部署→测试→清理闭环，宪法 VI）
guitar run projects/game/testplan/system_test.yaml   # game-system suite 含 memory/agent_v2 cases
```

通过标准：全部用例通过（all cases passed）。

## 场景 2：跨 session 共享（SC-001/SC-004，新大型用例）

`projects/game/testplan/agent_v2_preset_test.go` 新用例：同一 planner preset 在两个 session 物化——session A 的 planner 写入 memory（或经 REST 以该 preset 为 parent 预写），session B 以同 preset 物化 team，`GetTeamMember`（planner）`system_prompt` 的 `memory:snapshot` 段命中该条目；换用另一 planner preset 物化则快照为空（隔离对照）。

```bash
guitar run projects/game/testplan/system_test.yaml
```

## 场景 3：单测回归（bazel，宪法 IV）

```bash
# Go：handler preset 父解析 / domain PresetID / mongo preset 维度索引与游标
bazel test //projects/game/memory/... //projects/game/gateway/... //projects/game:game_test
# JS：scope 形态、client preset 寻址、operations 签名、guidance 跨局持久措辞、装配传递
bazel test //common/js/dsh-plugins/memory-service/... //common/js/dsh-plugins/memory/... \
  //common/js/dsh-plugins/saolei-loop/... //projects/game/agent_v2/...
```

## 场景 4：web memory 管理闭环（vitest + 手工 walkthrough）

```bash
bazel test //projects/game/web/frontend:lib_test
```

手工 walkthrough（部署后浏览器，契约 [web-memory-view.md](contracts/web-memory-view.md) §3）：

1. 侧栏「知识」入口 → 浮层 → memory → 选择 preset → 空态引导（中性文案）可见。
2. 创建条目（仅 content）→ 列表出现（最近更新在前）；同内容再创建 → "已存在"提示。
3. 编辑条目 content → 列表行反映新值；提交 >1024 rune → 参数错误说明。
4. 删除条目（二步确认）→ 列表移除；切换 preset → 列表与分页重置。

## 场景 5：侧栏两级导航（契约 [web-nav.md](contracts/web-nav.md)）

1. 一级仅 sessions/presets 直达 + 「知识」入口；wiki/memory 不平铺。
2. 点入口弹浮层 → 选 wiki/memory 切视图且浮层关闭；点浮层外部仅关闭、视图不变；关闭后入口呈现当前选中视图。

## 场景 6：场景无关文案（SC-007）

`WikiView`/`MemoryView` 组件测试的场景词扫描断言（渲染输出不含 planner/player/saolei/扫雷等）：

```bash
bazel test //projects/game/web/frontend:lib_test
```

手工抽查：wiki 视图空态与创建表单占位为中性措辞（无 planner、无扫雷示例）。

## 场景 7：旧作用域数据一次性清理（新 memory 服务首次启动前的运维动作）

在旧 memory 服务停止后、新 memory 服务**首次启动前**，对 `game_memory.memories` 集合执行一次性清理（data-model §2.3；服务启动仅创建 preset 维度新索引，清理不由服务代码承载；legacy 索引/旧文档不存在 = fresh 环境，无需清理）。顺序约束：unique `(template, preset_id, memory_id)` 索引在服务启动时创建，legacy 文档缺 `preset_id` 字段（MongoDB 唯一索引对缺失字段按 null 参与唯一性），同 template 下相同 `memory_id` 的旧文档（`generateMemoryId = sha256(content)[:32]` 确定性派生，跨 session 同内容高发）尚存会使该索引构建失败（E11000）且错误被启动代码按已存在忽略——必须**先清理、后首启**。

```js
// mongosh：删除两个 legacy session 维度索引（默认命名如下，以 db.memories.getIndexes() 实名为准）
db.memories.dropIndex("template_1_session_id_1_memory_id_1")
db.memories.dropIndex("template_1_session_id_1_update_time_-1_memory_id_1")
// 清除全部旧作用域文档
db.memories.deleteMany({session_id: {$exists: true}})
```

若新服务已在旧数据尚存时启动过：完成上述清理后**重启一次**新服务（索引仅启动时创建，清理不触发补建）。

核验：
- `db.memories.getIndexes()` 无 session 维度索引，且**包含两个 preset 维度新索引**（默认命名 `template_1_preset_id_1_memory_id_1`、`template_1_preset_id_1_update_time_-1_memory_id_1`）——unique 索引缺失即构建失败被静默忽略，服务运行在无唯一索引状态（duplicate-key → ALREADY_EXISTS 映射失效），须查 mongod 日志（E11000）排查后重启补建；
- `db.memories.countDocuments({session_id: {$exists: true}})` 为 0，新文档仅含 `preset_id`。

## 回归

```bash
bazel test //projects/game/... //common/js/dsh-plugins/...   # memory 契约零回归（SC-003）；wiki/preset/team 面零改动验证
guitar run projects/game/testplan/system_test.yaml           # 全 suite 通过（含 game-memory-down fail-loud 分支）
```
