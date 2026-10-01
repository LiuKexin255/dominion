# Contract: memory 公共库导出面（@dominion/common-js-memory）

**Feature**: [spec.md](../spec.md) | **契约层级**: 能力（领域无关公共库——无插件形态、无组装职责、无部署知识）

`common/js/memory/`（`@dominion/common-js-memory`，原 `@dominion/dsh-memory` 退化迁址）。无 name/inject/apply 导出（纯库）；包内 MUST NOT 出现场景词汇（player/planner）、scope 自足派生、组装流程、部署布局知识（target/绝对 protoPath）。

## 1. 原子能力单元工厂（唯一组装入口形态）

```ts
buildMemoryCapabilities(deps: {
  scope: MemoryScope;                 // {template, preset}
  memoryClient: MemoryServiceCalls;   // 结构化调用面（§3）
  wikiClient: WikiServiceCalls;
}): MemoryCapabilities;               // 纯函数：无 I/O、无上下文副作用
```

`MemoryCapabilities`（原子单元——消费方整单元挂载，库不提供部分导出面）：

| 字段 | 内容 |
|---|---|
| `faces` | `[{ key: "memory", value: MemoryFace }]` —— `createMemoryFace(createMemoryStore(...), scope)` 产出 |
| `tools` | `memory` / `wiki_search` / `wiki_get` / `wiki_write` ×4（schema、文案、结果文本化、守则文本全部不变；execute 经 `exec.agent.ctx.memory` 解析，不闭包绑定服务） |
| `sections` | `memory:guidance`(110)、`wiki:guidance`(120)、`memory:snapshot`(200, `text: () => face.snapshotText()`) |
| `ready` | `() => face.prefetch()` —— **fail-loud 预取**：由编排在成员 setup 内 `await unit.ready()`，reject → setup throw → 物化回滚（`CreateAgentOptions` 既有 rollback；specs/071 与 memory-plugin survey 决策 ⑧ 语义不变。adapter 只提供 `build`，不调用 `ready`；机制见 [team-assembly.md](team-assembly.md) §4/§6） |

section 名与 order 不变 → system prompt 组装输出零变化。

## 2. 能力原语

```ts
createMemoryStore(memoryClient: MemoryServiceCalls, wikiClient: WikiServiceCalls): MemoryStore;
createMemoryFace(store: MemoryStore, scope: MemoryScope): MemoryFace;
renderMemorySnapshot(entries: MemoryEntry[]): string;   // 10 条、update_time 降序、冻结语义（specs/071 FR-005）
memorySnapshotSection(text: string): PromptSection;
```

`PromptSection`（`name` / `order` / `text`，见 `@deepseek-ai/dsh-system-prompt` — `common/js/dsh-plugins/memory/node_modules/@deepseek-ai/dsh-system-prompt/lib/types/index.d.ts:47`）为 system prompt section 的注册规格类型；库产出规格（type-only 依赖），由编排经 `agentCtx.systemPrompt.section(...)` 注册。库据此声明 `@deepseek-ai/dsh-system-prompt`（type-only，peerDependency）。

`MemoryFace`：`prefetch()` / `applyCall(args)` / `wikiSearch(query)` / `wikiGet(titles)` / `wikiWrite(input)` / `snapshotText()`。face 创建时绑定 scope（取代现状 `PlannerMemoryService` 的按 agent 解析 scope Maps——`memory-service/src/service.ts:132-158`）。

## 3. 结构化调用面类型

`MemoryServiceCalls` / `WikiServiceCalls`：库内声明的 8 个 RPC 方法结构类型（复用既有手写消息类型）。消费方以 `ctx.grpc.client<MemoryServiceCalls>(...)` 构造后传入；TS 结构兼容在消费方调用点编译期校验；生成接口（agent_v2_types）为可选的兼容性校验参照。库 MUST NOT import projects/game 生成代码或 saolei-loop。

## 4. 领域语义（等价迁入，契约不变）

memory 操作（content ≤1024 rune、batch 两阶段预检/提交、id=sha256[:32]）、wiki 语义（校验/分页/单条上限/两阶段 upsert）——specs/068-wiki-round-pause、specs/069-wiki-template-web、specs/071-memory-preset-scope 契约全部继续有效。

## 5. 工具 execute 面解析与缺面语义

execute 经 `exec.agent.ctx.memory` 属性穿越解析（isolate 注册面；saolei 同型，`common/js/dsh-plugins/saolei/src/index.ts:91-104`）。缺面 fail-loud（四工具同型文案）：

```
memory tools require the agent-scoped "memory" service; agent "${exec.agent.id}" has none
(the team-layer materialization setup registers it from the memory capability mount)
```

## 6. dsh-saolei（对照：自包含能力保持插件形态，改造为静态单元提供方）

`@dominion/dsh-saolei` 保持 cordis 插件形态，**轻度改造**：挂载位置从 agent scope（模板行）变为组合级（saolei-loop config.plugins 子插件，[team-assembly.md](team-assembly.md) §3.1）——组合级 apply 内 `ctx.tools.register` 会全局注册（全员泄漏），故改为 `ctx.provide("saoleiTools", { tools: [...], sections: [...] })`；工具定义/schema/文案与 sections 名/order/文本零变化。工具 execute 的 `exec.agent.ctx.saoleiGame` 解析不变；`saoleiGame` 运行时挂载仍属 createAgentGameRuntime（`common/js/dsh-plugins/saolei-loop/src/game/runtime.ts:430-440`）；缺面文案沿用现状（`saolei/src/index.ts:100`）。
