# Contract: memory 插件拆分（dsh-plugins 修订）

> 修订 `specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §3（memory 插件双面形态）与 §5（组合清单行）。§3 的 team/saolei-loop/saolei 条目不变；本文档只承载 memory 拆分终态。行为语义（059 §3 的三条功能面描述）零变化——变化仅在包归属与挂载面。

## 1. 包与挂载面（一包一面）

| 插件 | 包 | 挂载面 | cordis 名 | inject / provide |
|---|---|---|---|---|
| memory 基建 | `@dominion/dsh-memory-service`（`common/js/dsh-plugins/memory-service/`） | host 组合行（仅此一处） | `memory` | provide `ctx.plannerMemory`；inject 无 |
| memory 工具 | `@dominion/dsh-memory`（`common/js/dsh-plugins/memory/`） | preset 组合行（planner 池模板） | `memory-row` | inject `["plannerMemory", "tools", "systemPrompt"]` |

- 基建插件包名 MUST NOT 出现在任何 preset 模板组合行或 templateRules（required/forbidden）——结构事实，非守卫约定。
- 工具包依赖基建包（`workspace:*`）：跨包面 = `PlannerMemoryService` 类型、`MEMORY_ACTIONS`/`MemoryToolArgs` 校验常量、snapshot section 标识常量；运行时服务解析仍经 cordis `inject` 按名注入。
- `@dominion/dsh-memory` 的 exports map 仅 `.` 主入口（无 `./preset-row` 子路径，无兼容别名）。

## 2. 行为规范（承 059 §3，不变量重申）

1. **工具行**：`defineTool` 注册单一 `memory` 工具（add/replace/remove + `operations[]` 批量原子、`old_text` 子串定位、无 read、失败也是文本结果）；执行期经行上下文绑定的 `ctx.plannerMemory` 访问存储（isolate 边界语义，T023）。
2. **快照 section**：函数式 `text: (context) => snapshot(context.scope) ?? ""`，order 200+，空自动不渲染；物化 setup `load` 预取填充。
3. **host 服务面**：`load(agentCtx, {template, session})` 读 memory 服务（gRPC，`dominion:///game/memory:50051`）→ 渲染快照 → 绑定 scope；失败 throw（物化整体回滚，fail-loud）；写路径同面持久化。
4. **不注册 guidance section**（单工具无跨调用协调需求）。

## 3. 组合面终态（agent_v2）

- `projects/game/agent_v2/cordis.yml` host 行：`{ id: memory, name: '@dominion/dsh-memory-service' }`。
- templateRules：`player: { required: ['@dominion/dsh-saolei'], forbidden: ['@dominion/dsh-memory'] }`；`planner: { required: ['@dominion/dsh-memory'], forbidden: ['@dominion/dsh-saolei'] }`。
- planner 池模板行：`- { id: memory, name: '@dominion/dsh-memory' }`。
- 组合三面（package.json ⟷ cordis.yml ⟷ 镜像物化）原子变更；`projects/game/agent_v2/package.json` 同时声明两个包（`workspace:*`）。
