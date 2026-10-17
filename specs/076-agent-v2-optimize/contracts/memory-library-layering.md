# Contract: memory 公共库两层导出面（功能单元层 / 组合层）与 saolei-memory memory-only 注入

**Feature**: [../spec.md](../spec.md)（FR-001–FR-006）| **结构定义**: [../data-model.md](../data-model.md) §1
**实现锚点**: `common/js/memory/src/`（单元/组合重组）、`common/js/dsh-plugins/saolei-memory/src/index.ts`（memory-only 注入）
**基线契约**: `specs/074-dsh-grpc-runtime/contracts/memory-library.md`（本契约为其导出面修订——组合面语义等价承接；工具 schema/文案、领域语义、gRPC 调用面类型、unary 策略自持等条款全部继续有效）

## 1. 功能单元层（每个单元可独立构造、独立挂载、独立测试）

```ts
/** memory 单元工厂——只依赖注入的 MemoryService client 与 unary 策略函数。 */
buildMemoryUnit(deps: {
  scope: MemoryScope;                 // {template, preset}
  memoryClient: MemoryServiceCalls;
  callUnary: UnaryPolicy;             // 结构类型策略函数（与 ctx.grpc.callUnary 结构兼容；research.md D10）
}): CapabilityUnit;

/** wiki 单元工厂——只依赖注入的 WikiService client 与 unary 策略函数。 */
buildWikiUnit(deps: {
  template: string;
  wikiClient: WikiServiceCalls;
  callUnary: UnaryPolicy;
}): CapabilityUnit;
```

`CapabilityUnit`（原子四件套，消费方整单元挂载）：

| 单元 | faces | tools | sections | ready |
|---|---|---|---|---|
| memory | `[{key:"memory", value: MemoryFace}]` | `memory` ×1 | `memory:snapshot`(200)——**无 guidance section**（单工具说明由 `MEMORY_TOOL_DESCRIPTION` 承载） | fail-loud prefetch（reject → 物化回滚语义保持） |
| wiki | `[{key:"wiki", value: WikiFace}]` | `wiki_search` / `wiki_get` / `wiki_write` ×3 | `wiki:guidance`(120)（三工具协作工作流） | no-op（wiki 无预取义务） |

义务：

1. **单元独立性**：任一单元单独挂载时其工具全部可用——memory 工具经 `exec.agent.ctx.memory` 解析，wiki 工具经 `exec.agent.ctx.wiki` 解析（缺面 fail-loud 文案同型）；`MemoryStore` 只持 memoryClient、`WikiStore` 只持 wikiClient（现状合并 store/face 拆分，wiki 方法从 `MemoryFace` 移除）。
2. **unary 策略注入 seam**（research.md D10）：库内**不自持** unary 调用策略副本；策略以结构类型函数（`UnaryPolicy`，与 `ctx.grpc.callUnary` 结构兼容）随 client 一起由组装方注入，策略实现唯一归属 grpc-runtime（`common/js/dsh-plugins/grpc-runtime/src/unary.ts`）。库测试注入策略替身（与 client 替身同型）。
3. **guidance 归属**：memory 单元不携带 guidance section——单工具的使用说明与快照语义已由 `MEMORY_TOOL_DESCRIPTION` 完整承载（`common/js/memory/src/tool.ts:40-49`；跨局持久意图由 persona 文本承载）；wiki 单元保留 `wiki:guidance`（三工具协作工作流，单工具 description 无法承载）；跨存储分工文本（memory vs wiki 边界）归组合层所有（§2）。
4. **领域语义冻结**：memory 操作（content ≤1024 rune、batch 两阶段、id=sha256[:32]）、wiki 语义（校验/分页/单条上限/两阶段 upsert）、工具 schema 与结果文本化全部不变（`specs/068-wiki-round-pause/`、`specs/069-wiki-template-web/`、`specs/071-memory-preset-scope/` 契约继续有效）。
5. 库形态不变项：无 name/inject/apply（纯库）、无场景词汇、无部署知识、`@deepseek-ai/dsh-system-prompt` type-only peerDependency（074 契约 §2 条款继续有效）。

## 2. 组合层（多单元组合为单一可整体挂载的能力面）

```ts
/** 组合 memory + wiki 两单元——等价锚点：产物与现状 buildMemoryCapabilities 输出一致。 */
buildMemoryWikiComposition(deps: {
  scope: MemoryScope;
  memoryClient: MemoryServiceCalls;
  wikiClient: WikiServiceCalls;
  callUnary: UnaryPolicy;             // 与单元工厂同型的策略注入 seam（§1 义务 2；research.md D10）
}): CapabilityUnit;
```

组合产物（**等价锚点**，spec FR-004/SC-003）：

- tools：`memory` + `wiki_search` + `wiki_get` + `wiki_write` ×4（与现状一致）；
- sections：`memory:guidance`(110, **分工文本**)、`wiki:guidance`(120)、`memory:snapshot`(200)——section 名与 order 逐项一致，系统 prompt 组装输出零变化；
- ready：memory prefetch（wiki 无预取）。

分工文本 = 现状 `MEMORY_GUIDANCE`（memory vs wiki 边界描述）归组合层所有，仅在组合挂载时注入——memory 单元不携带 guidance section，分工只在两存储并存时有意义。

## 3. saolei-memory 适配器（memory-only 注入）

1. 适配器只构造 **MemoryService** client（`grpc.client<MemoryServiceCalls>`）；WikiService client 构造与 `WikiServiceCalls` 依赖移除。
2. `memoryMount` provide 面不变：`build(scope: MemoryScope): CapabilityUnit`——`build` 改为产出 **memory 单元**（`buildMemoryUnit`），组装时把 `ctx.grpc.callUnary` 随 client 一起传入（策略注入 seam，§1 义务 2）。
3. 物化后的 planner 能力面零 wiki 痕迹：工具清单无 wiki 工具、system prompt 无 wiki section、无 wiki 文本引用（spec SC-002）。
4. 结构类型 seam：saolei-loop 的 `MemoryCapabilities` 为结构类型（faces value `unknown`），库导出面重组不破坏编排器编译面（research.md D5）。

## 4. 不改动面（spec FR-005）与基线契约修订

不改动面：`projects/game/game.proto` WikiService、memory 服务 wiki 实现（`projects/game/memory/`）、gateway `/api/v1` wiki 管理路由、web `WikiView.tsx`、`projects/game/testplan/wiki_test.go`（`system_test.yaml` 内 target）、grpc-runtime 插件代码（策略已导出于服务面，零代码变更）全部零改动；wiki 能力（单元层）保留完整能力供独立使用与组合复用。

基线契约修订：`specs/074-dsh-grpc-runtime/contracts/memory-library.md` §3 的"两处各持一份是依赖方向的必然，非待收敛的重复"表述撤销——改为策略注入 seam 描述（策略实现唯一归属 grpc-runtime，库经组装 seam 消费注入的策略函数；research.md D10）。

## 5. 验收面（对应 spec SC-002/SC-003/SC-004）

单测：单元独立挂载（各自 faces/tools/sections/ready 可用、工具解析自洽）；组合等价（工具集、section 名/order、组装输出逐项一致）；memory 单元无 guidance section（planner 能力 prompt 仅含 `memory:snapshot`）；saolei-memory 不构造 wiki client、产物为 memory 单元、策略函数经组装传入；库内无私有 `callUnary`/`probeChannel` 副本（策略行为由 `common/js/dsh-plugins/grpc-runtime/src/unary.test.ts` 覆盖）。大型测试：wiki 面（`//projects/game/testplan:wiki_test`）零改动回归通过。
