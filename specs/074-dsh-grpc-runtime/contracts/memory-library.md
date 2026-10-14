# Contract: memory 公共库导出面（@dominion/common-js-memory）

**Feature**: [spec.md](../spec.md) | **契约层级**: 能力（领域无关公共库——无插件形态、无部署知识）
**导出面修订**: [specs/076-agent-v2-optimize/contracts/memory-library-layering.md](../../076-agent-v2-optimize/contracts/memory-library-layering.md)——两层导出面（功能单元层/组合层）、独立挂载义务与 unary 策略注入 seam 的终态定义；本文 §1/§2/§3/§5 按其改述，细则该文为准。

`common/js/memory/`（`@dominion/common-js-memory`，原 `@dominion/dsh-memory` 退化迁址）。无 name/inject/apply 导出（纯库）；包内 MUST NOT 出现场景词汇（player/planner）、scope 自足派生、组装流程、部署布局知识（target/绝对 protoPath）。

## 1. 两层导出面

库分两层（细则见 [memory-library-layering.md](../../076-agent-v2-optimize/contracts/memory-library-layering.md) §1/§2）：

- **功能单元层**：`buildMemoryUnit({ scope, memoryClient, callUnary })` 与 `buildWikiUnit({ template, wikiClient, callUnary })`，各自产出原子能力单元 `CapabilityUnit`（`faces`/`tools`/`sections`/`ready` 四件套）——可独立构造、独立挂载、独立测试；任一单元单独挂载时其工具全部可用（memory 工具经 `exec.agent.ctx.memory` 解析、wiki 工具经 `exec.agent.ctx.wiki` 解析，§5），单元间互不依赖对方在场。
- **组合层**：`buildMemoryWikiComposition({ scope, memoryClient, wikiClient, callUnary })` 将两单元合并为单一可整体挂载的能力面，并注入组合层自有的跨存储分工 section；其产物与拆分前 `buildMemoryCapabilities` 输出**等价**（工具集、section 名与 order、组装文本逐项一致——组合等价锚点）。

| 单元 | faces | tools | sections | ready |
|---|---|---|---|---|
| memory | `[{ key: "memory", value: MemoryFace }]` | `memory` ×1 | `memory:snapshot`(200)——**无 guidance section**（单工具的使用说明由 `MEMORY_TOOL_DESCRIPTION` 承载） | fail-loud prefetch（reject → 物化回滚语义保持） |
| wiki | `[{ key: "wiki", value: WikiFace }]` | `wiki_search`/`wiki_get`/`wiki_write` ×3 | `wiki:guidance`(120)（三工具协作工作流） | no-op（wiki 无预取义务） |
| 组合（memory+wiki） | 两单元 faces 合并 | ×4 | `memory:guidance`(110, **跨存储分工文本**——组合层自有)/`wiki:guidance`(120)/`memory:snapshot`(200) | memory prefetch |

工厂均为纯函数：无 I/O、无上下文副作用。section 名与 order 为冻结契约 → 组合面的 system prompt 组装输出零变化。工具 schema、文案、结果文本化与守则文本全部不变；工具 execute 不闭包绑定服务（§5）。

## 2. 能力原语

```ts
createMemoryFace(memoryStore: MemoryStoreRpcs, scope: MemoryScope): MemoryFace;
createWikiFace(wikiStore: WikiStore, template: string): WikiFace;
renderMemorySnapshot(entries: MemoryEntry[]): string;   // 10 条、update_time 降序、冻结语义（specs/071 FR-005）
memorySnapshotSection(text: string | (() => string)): PromptSection;   // text：静态文本或零参闭包（单元工厂的 () => face.snapshotText()）
```

存储 seam 按服务分离：`MemoryStoreRpcs` 只持 memoryClient 的四 RPC，`WikiStore` 只持 wikiClient 的模板寻址面（`MemoryStore extends MemoryStoreRpcs, WikiStore` 合并类型保留）；其 client 适配器 `MemoryClient`/`WikiClient` 由单元工厂构造并同时注入 client 与 unary 策略（§3）。

`PromptSection`（`name` / `order` / `text`，见 `@deepseek-ai/dsh-system-prompt` — `common/js/dsh-plugins/memory/node_modules/@deepseek-ai/dsh-system-prompt/lib/types/index.d.ts:47`）为 system prompt section 的注册规格类型；其 `text` 为 `string | ((context: AssembleContext) => string)`（同文件 `:60`），静态文本与 provider 闭包均为合法取值——零参闭包（如单元工厂的 `() => face.snapshotText()`）按少参可赋值多参签名的结构兼容规则成立。库产出规格（type-only 依赖），由编排经 `agentCtx.systemPrompt.section(...)` 注册。库据此声明 `@deepseek-ai/dsh-system-prompt`（type-only，peerDependency）。

`MemoryFace`：`prefetch()` / `applyCall(args)` / `snapshotText()`——face 创建时绑定 `(template, preset)` scope（取代现状 `PlannerMemoryService` 的按 agent 解析 scope Maps——`memory-service/src/service.ts:132-158`）。`WikiFace`：`wikiSearch(query)` / `wikiGet(titles)` / `wikiWrite(input)`——face 创建时绑定 `template` 单键（wiki 集合为模板级共享，`specs/069-wiki-template-web/contracts/wiki-service.md` §1）。

## 3. 结构化调用面类型与 unary 策略注入

`MemoryServiceCalls` / `WikiServiceCalls`：库内声明的 10 个 RPC 方法（memory 4 + wiki 6）结构类型（复用既有手写消息类型）。消费方以 `ctx.grpc.client<MemoryServiceCalls>(...)` 构造后传入；TS 结构兼容在消费方调用点编译期校验；生成接口（agent_v2_types）为可选的兼容性校验参照。库 MUST NOT import projects/game 生成代码或 saolei-loop。

client 适配器运行期所需的调用局部 unary 策略（`waitForReady` + 缺省 10s deadline + pre-call 通道探测 + 失败后 channel 状态日志）**不由库自持**：它以结构类型 `UnaryPolicy`（与 `ctx.grpc.callUnary` 结构兼容；[grpc-runtime.md](grpc-runtime.md) §2 O3）随 client 一起经单元/组合工厂的 `callUnary` 参数注入，策略实现唯一归属 `common/js/dsh-plugins/grpc-runtime/src/unary.ts`（依据 [specs/076-agent-v2-optimize/research.md](../../076-agent-v2-optimize/research.md) D10）。库的组装 seam 只收注入值（无 ctx 访问）；纯库 MUST NOT 依赖插件包（`@dominion/dsh-grpc-runtime` 为 cordis 插件形态），故策略以结构类型注入而非 import——依赖方向规则不破，且不存在第二份策略副本。

## 4. 领域语义（等价迁入，契约不变）

memory 操作（content ≤1024 rune、batch 两阶段预检/提交、id=sha256[:32]）、wiki 语义（校验/分页/单条上限/两阶段 upsert）——specs/068-wiki-round-pause、specs/069-wiki-template-web、specs/071-memory-preset-scope 契约全部继续有效。

## 5. 工具 execute 面解析与缺面语义

execute 经属性穿越解析（isolate 注册面；saolei 同型，`common/js/dsh-plugins/saolei-tools/src/index.ts` 的 `resolveRuntime`）：memory 工具经 `exec.agent.ctx.memory`；三个 wiki 工具经 `exec.agent.ctx.wiki`（单元独立挂载时的自洽解析，§1）。缺面 fail-loud，两侧同型文案指名对应的 face 键与注册方：

```
memory tools require the agent-scoped "memory" service; agent "${exec.agent.id}" has none
(the team-layer materialization setup registers it from the memory capability mount)
```

```
wiki tools require the agent-scoped "wiki" service; agent "${exec.agent.id}" has none
(the team-layer materialization setup registers it from the wiki capability mount)
```

## 6. dsh-saolei 游戏基础与 dsh-saolei-tools 工具插件（对照）

- `@dominion/dsh-saolei`（游戏基础）：组合清单**顶级基础行**（无 config），apply 提供组合级服务面 `ctx.saolei`（`{ createGameRuntime, gameSection }`）；不持有工具、不全局注册（specs/075-saolei-game-foundation/contracts/saolei-foundation.md §1/§2）。
- `@dominion/dsh-saolei-tools`（工具插件）：saolei-loop `config.plugins` 静态单元提供方（[team-assembly.md](team-assembly.md) §3.1）——apply 以 `ctx.provide("saoleiTools", { tools: [...], sections: [saolei:guidance] })` 暴露单元（`saolei:game` 归游戏基础面、由编排对双角色注册）；工具定义/schema/文案与 guidance 名/order/文本零变化。
- 工具 execute 的 `exec.agent.ctx.saoleiGame` 属性解析不变；`saoleiGame` 运行时为编排持有的 `@dominion/dsh-saolei` `GameRuntime` 实例，经 player setup `agent.ctx.isolate("saoleiGame")` 注册句柄（`common/js/dsh-plugins/saolei/src/game/runtime.ts`）；缺面 fail-loud 文案路径 `common/js/dsh-plugins/saolei-tools/src/index.ts` 的 `resolveRuntime`（冻结文案本身零改动，判定见 specs/075-saolei-game-foundation/contracts/cutover-contracts.md §5）。
