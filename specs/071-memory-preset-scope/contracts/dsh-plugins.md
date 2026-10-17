# Contract: dsh 插件面（memory 公共库 / saolei-memory adapter / saolei-loop / agent_v2 装配）

**Feature**: specs/071-memory-preset-scope/spec.md | **基线修订**: specs/064-memory-split-fold-remain/contracts/dsh-plugins.md（双面拆分契约）、specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md（快照近因）——作用域两半为 `{template, preset}`，绑定/冻结/dedupe 语义不变。能力通路契约（公共库 `@dominion/common-js-memory` + 薄 adapter `@dominion/dsh-saolei-memory` + saolei-loop 编排消费）见 specs/074-dsh-grpc-runtime/contracts/memory-library.md 与 specs/074-dsh-grpc-runtime/contracts/team-assembly.md；本文承载 agent_v2 场景下的作用域语义与装配。

## 1. 作用域类型（`MemoryScope` / `PlannerMemoryScope`）

```ts
// common/js/memory/src/face.ts（MemoryScope）与
// common/js/dsh-plugins/saolei-loop/src/orchestrator.ts（PlannerMemoryScope）
// 两处结构兼容声明（不共享依赖；编排将同一对象显式传入 memoryMount.build(scope)）
interface MemoryScope {
  readonly template: string
  readonly preset: string    // 物化来源 planner preset 的 id 半
}
```

- parent 构造：`templates/{template}/presets/{preset}`（`common/js/memory/src/store.ts` 的 `memoryName` 与 create/list 的 parent）。
- wiki 面不受影响：`wikiSearch/wikiGet/wikiWrite` 仍仅转发 `scope.template`（069 template 级契约不变）。

## 2. MemoryClient（`common/js/memory/src/store.ts`，签名不变）

```ts
memoryName(template, preset, memoryId): string        // templates/{t}/presets/{p}/memories/{m}
createMemory(template, preset, memoryId, content)     // parent = templates/{t}/presets/{p}
updateMemory(template, preset, memoryId, content)     // name 寻址 + mask ["content"]
deleteMemory(template, preset, memoryId)
listMemories(template, preset, opts?)                 // opts: {orderBy?, pageSize?, pageToken?}
```

- client 构造通路：adapter `@dominion/dsh-saolei-memory` 以 config（`target: dominion:///game/memory:50051` + canonical protoPath）经组合级 `ctx.grpc.client` 构造（specs/074-dsh-grpc-runtime/contracts/grpc-runtime.md §2、specs/074-dsh-grpc-runtime/contracts/team-assembly.md §3.2）；连接基建（channel 缓存共享/unary 策略/TLS）归 grpc-runtime 行，MemoryService 与 WikiService 同 target 共享 channel。
- 错误映射不变（ALREADY_EXISTS code 6 等）。

## 3. MemoryFace（`common/js/memory/src/face.ts`，绑定语义不变）

face 经 `createMemoryFace(store, scope)` 创建即绑定 scope（scope 随 face 走，无按 agent 键查表）：

- `prefetch()`：一次 `listMemories(scope.template, scope.preset, {orderBy: "update_time desc", pageSize: 10})` 预取并冻结快照文本；失败 fail-loud reject——编排在成员 setup 内 `await unit.ready()`，reject → setup throw → member 物化整体回滚（memory-down 拓扑既有验收不变）。
- `applyCall(args)`：以绑定 scope 的 preset 寻址写存储；`generateMemoryId = sha256(content).hex[:32]` 不变；ALREADY_EXISTS = 同内容去重成功（dedupe 文本）不变。
- `snapshotText()`：空快照 → `undefined`/`""`，prompt 段丢弃（不变）。
- 工具通路：四工具（memory/wiki_search/wiki_get/wiki_write）execute 经 `exec.agent.ctx.memory` 解析本 face（isolate label `memory`）；缺面 fail-loud 文案见 specs/074-dsh-grpc-runtime/contracts/memory-library.md §5（specs/074-dsh-grpc-runtime/contracts/team-assembly.md §5①）。

## 4. 物化装配（`agent_v2/src/session.ts` + `saolei-loop`）

- `TeamMaterializeOptions.memoryScope: PlannerMemoryScope` 选项保留（team 原语不引入 preset 语义）；宿主物化入口（`doMaterialize`）构建：

```ts
memoryScope: { template: parsedSession.template, preset: this.presetId(planner.preset) }
```

- planner member `setup` 钩子：`unit = memoryMount.build(options.memoryScope)`（scope 显式传参）→ 注册序 faces(`memory` isolate) → tools×4 → sections×3 → `await unit.ready()`（fail-loud 预取，reject → setup throw → 物化回滚；specs/074-dsh-grpc-runtime/contracts/team-assembly.md §4/§5③）。
- team 刷新换 planner preset → 新 scope → 新集合快照（spec FR-009）；跨 session 同 preset → 同集合（spec FR-007）。
- player 隔离为结构性保证：编排 player 分支只消费 saoleiTools 单元（无 memory 痕迹），不经模板行校验（specs/074-dsh-grpc-runtime/contracts/preset-templates.md §3、specs/074-dsh-grpc-runtime/contracts/team-assembly.md §6）。

## 5. 模型面文本（`common/js/memory/src/guidance.ts`）

- `MEMORY_GUIDANCE` 分工段：memory 补充跨局持久属性——同 preset 的所有对局共享记忆、错误条目会持续影响后续每一局（与 wiki 的"跨会话大块知识"表述对齐分工边界）；单条 ~1000 字符的体量预期与 wiki 引导不变。
- `MEMORY_TOOL_DESCRIPTION` 不变（scope-agnostic）。
- section 名与 order（`memory:guidance` 110、`wiki:guidance` 120、`memory:snapshot` 200）不变。

## 6. 验收锚点

1. JS 单测（`common/js/memory/src/`）：同 `{template, preset}` 双 face 绑定命中同一集合（fake store 断言相同 parent 寻址）；不同 preset 零交叉。
2. 装配单测（`common/js/dsh-plugins/saolei-loop/src/orchestrator.test.ts`）：物化传出的 memoryScope 两半来自 planner member preset id（非 session）。
3. 模型面单测（`common/js/memory/src/guidance.test.ts`）：分工段含跨局持久表述、无 session 作用域措辞；工具描述逐字不变。
4. memory-down fail-loud 回滚行为不变（testplan `game-memory-down` suite）。
