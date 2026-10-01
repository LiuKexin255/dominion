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
memorySnapshotSection(text: string | (() => string)): PromptSection;   // text：静态文本或零参闭包（§1 的 () => face.snapshotText()）
```

`PromptSection`（`name` / `order` / `text`，见 `@deepseek-ai/dsh-system-prompt` — `common/js/dsh-plugins/memory/node_modules/@deepseek-ai/dsh-system-prompt/lib/types/index.d.ts:47`）为 system prompt section 的注册规格类型；其 `text` 为 `string | ((context: AssembleContext) => string)`（同文件 `:60`），静态文本与 provider 闭包均为合法取值——零参闭包（如 §1 的 `() => face.snapshotText()`）按少参可赋值多参签名的结构兼容规则成立。库产出规格（type-only 依赖），由编排经 `agentCtx.systemPrompt.section(...)` 注册。库据此声明 `@deepseek-ai/dsh-system-prompt`（type-only，peerDependency）。

`MemoryFace`：`prefetch()` / `applyCall(args)` / `wikiSearch(query)` / `wikiGet(titles)` / `wikiWrite(input)` / `snapshotText()`。face 创建时绑定 scope（取代现状 `PlannerMemoryService` 的按 agent 解析 scope Maps——`memory-service/src/service.ts:132-158`）。

## 3. 结构化调用面类型

`MemoryServiceCalls` / `WikiServiceCalls`：库内声明的 10 个 RPC 方法（memory 4 + wiki 6）结构类型（复用既有手写消息类型）。消费方以 `ctx.grpc.client<MemoryServiceCalls>(...)` 构造后传入；TS 结构兼容在消费方调用点编译期校验；生成接口（agent_v2_types）为可选的兼容性校验参照。库 MUST NOT import projects/game 生成代码或 saolei-loop。

client 适配器运行期所需的调用局部 unary 策略（`waitForReady` + 缺省 10s deadline + pre-call 通道探测 + 失败后 channel 状态日志）由库内私有自持——与基建面 unary 策略（[grpc-runtime.md](grpc-runtime.md) §2 O3）同源的等价迁移副本。库的组装 seam 只收注入 client（无 ctx 访问），且纯库 MUST NOT 依赖插件包（`@dominion/dsh-grpc-runtime` 为 cordis 插件形态），适配器无法消费 `ctx.grpc.callUnary`，故两处各持一份是依赖方向的必然，非待收敛的重复。

## 4. 领域语义（等价迁入，契约不变）

memory 操作（content ≤1024 rune、batch 两阶段预检/提交、id=sha256[:32]）、wiki 语义（校验/分页/单条上限/两阶段 upsert）——specs/068-wiki-round-pause、specs/069-wiki-template-web、specs/071-memory-preset-scope 契约全部继续有效。

## 5. 工具 execute 面解析与缺面语义

execute 经 `exec.agent.ctx.memory` 属性穿越解析（isolate 注册面；saolei 同型，`common/js/dsh-plugins/saolei-tools/src/index.ts` 的 `resolveRuntime`）。缺面 fail-loud（四工具同型文案）：

```
memory tools require the agent-scoped "memory" service; agent "${exec.agent.id}" has none
(the team-layer materialization setup registers it from the memory capability mount)
```

## 6. dsh-saolei 游戏基础与 dsh-saolei-tools 工具插件（对照）

- `@dominion/dsh-saolei`（游戏基础）：组合清单**顶级基础行**（无 config），apply 提供组合级服务面 `ctx.saolei`（`{ createGameRuntime, gameSection }`）；不持有工具、不全局注册（specs/075-saolei-game-foundation/contracts/saolei-foundation.md §1/§2）。
- `@dominion/dsh-saolei-tools`（工具插件）：saolei-loop `config.plugins` 静态单元提供方（[team-assembly.md](team-assembly.md) §3.1）——apply 以 `ctx.provide("saoleiTools", { tools: [...], sections: [saolei:guidance] })` 暴露单元（`saolei:game` 归游戏基础面、由编排对双角色注册）；工具定义/schema/文案与 guidance 名/order/文本零变化。
- 工具 execute 的 `exec.agent.ctx.saoleiGame` 属性解析不变；`saoleiGame` 运行时为编排持有的 `@dominion/dsh-saolei` `GameRuntime` 实例，经 player setup `agent.ctx.isolate("saoleiGame")` 注册句柄（`common/js/dsh-plugins/saolei/src/game/runtime.ts`）；缺面 fail-loud 文案路径 `common/js/dsh-plugins/saolei-tools/src/index.ts` 的 `resolveRuntime`（冻结文案本身零改动，判定见 specs/075-saolei-game-foundation/contracts/cutover-contracts.md §5）。
