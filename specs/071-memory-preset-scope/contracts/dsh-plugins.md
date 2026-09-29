# Contract: dsh 插件面（memory-service / memory / saolei-loop / agent_v2 装配）

**Feature**: specs/071-memory-preset-scope/spec.md | **基线修订**: specs/064-memory-split-fold-remain/contracts/dsh-plugins.md（双面拆分契约）、specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md（快照近因）——本文生效后，作用域两半由 `{template, session}` 修订为 `{template, preset}`，绑定/冻结/dedupe 语义不变。

## 1. PlannerMemoryScope（形态变更）

```ts
// common/js/dsh-plugins/memory-service/src/service.ts 与
// common/js/dsh-plugins/saolei-loop/src/orchestrator.ts 两处同型声明（不共享依赖）
interface PlannerMemoryScope {
  readonly template: string
  readonly preset: string    // 原 session：物化来源 preset 的 id 半
}
```

- parent 构造：`templates/{template}/presets/{preset}`（client.ts `memoryName` 与 create/list 的 parent 随迁）。
- wiki 面不受影响：`wikiSearch/wikiGet/wikiWrite` 仍仅转发 `scope.template`（069 template 级契约不变）。

## 2. MemoryClient（`memory-service/src/client.ts`，签名随迁）

```ts
memoryName(template, preset, memoryId): string        // templates/{t}/presets/{p}/memories/{m}
createMemory(template, preset, memoryId, content)     // parent = templates/{t}/presets/{p}
updateMemory(template, preset, memoryId, content)     // name 寻址 + mask ["content"]
deleteMemory(template, preset, memoryId)
listMemories(template, preset, opts?)                 // opts: {orderBy?, pageSize?, pageToken?}
```

- gRPC 目标不变（`dominion:///game/memory:50051`）；错误映射不变（ALREADY_EXISTS code 6 等）。

## 3. PlannerMemoryService（`memory-service/src/service.ts`，绑定语义不变）

- `load(agentCtx, scope)`：一次 `listMemories(scope.template, scope.preset, {orderBy: "update_time desc", pageSize: 10})` 预取，快照渲染缓存按 agent `ScopeKey` 键存；失败 fail-loud（member 物化整体回滚，memory-down 拓扑既有验收不变）。
- `applyCall(agent, args)`：以绑定 scope 的 preset 寻址写存储；`generateMemoryId = sha256(content).hex[:32]` 不变；ALREADY_EXISTS = 同内容去重成功（dedupe 文本）不变。
- `snapshot(scope)`：空快照 → `undefined`/`""`，prompt 段丢弃（不变）。

## 4. 物化装配（`agent_v2/src/session.ts` + `saolei-loop`）

- `TeamMaterializeOptions.memoryScope: PlannerMemoryScope` 选项保留（team 原语不引入 preset 语义）；`doMaterialize` 构建：

```ts
memoryScope: { template: parsedSession.template, preset: this.presetId(planner.preset) }
```

- planner member `setup` 钩子的 `loadPlannerMemory(agentCtx, options.memoryScope)` 调用形态不变。
- team 刷新换 planner preset → 新 scope → 新集合快照（spec FR-009）；跨 session 同 preset → 同集合（spec FR-007）。
- player 角色挂载不变（templateRules 禁挂 `@dominion/dsh-memory`）。

## 5. 模型面文本修订（`memory/src/guidance.ts`）

- `MEMORY_GUIDANCE` 分工段：memory 补充跨局持久属性——同 preset 的所有对局共享记忆、错误条目会持续影响后续每一局（与 wiki 的"跨会话大块知识"表述对齐分工边界）；单条 ~1000 字符的体量预期与 wiki 引导不变。
- `MEMORY_TOOL_DESCRIPTION` 不变（scope-agnostic）。
- section 名与 order（`memory:guidance` 110、`wiki:guidance` 120、`memory:snapshot` 200）不变。

## 6. 验收锚点

1. JS 单测：同 `{template, preset}` 双 agent 绑定命中同一集合（fake store 断言相同 parent 寻址）；不同 preset 零交叉。
2. 装配单测：`doMaterialize` 传出的 memoryScope 两半来自 planner member preset id（非 session）。
3. 模型面单测：分工段含跨局持久表述、无 session 作用域措辞；工具描述逐字不变。
4. memory-down fail-loud 回滚行为不变（testplan `game-memory-down` suite）。
