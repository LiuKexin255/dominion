# Contract: saolei 工具插件（@dominion/dsh-saolei-tools）

**Feature**: [spec.md](../spec.md) FR-006–FR-009 | **决策**: [research.md](../research.md) D4/D5 | **数据模型**: [data-model.md](../data-model.md) §3

新建工具插件的契约：面向智能体的 saolei 工具能力提供方。内容全部自旧 dsh-saolei 等价迁入（比对基准 = `common/js/dsh-plugins/saolei/src/index.ts` 现状），工具/校验/文案零变化。包契约形态对齐 048 [esm-package-conventions.md](../../048-js-esm-migration/contracts/esm-package-conventions.md)。

## 1. 插件形态

```ts
// common/js/dsh-plugins/saolei-tools/src/index.ts
export const name = "saolei-tools";
export const inject: string[] = [];            // 纯提供方
export const SAOLEI_TOOLS_SERVICE = "saoleiTools";   // 面名沿用（074 契约连续性）
```

- 组合挂载：saolei-loop `config.plugins` 子插件行（`- name: '@dominion/dsh-saolei-tools'`，无 config）；由 saolei-loop apply 经裸包名动态 import 挂载（074 team-assembly §2 机制，不变）。

## 2. saoleiTools 静态单元（定义收窄）

```ts
export interface SaoleiToolsUnit {
  readonly tools: readonly ToolDefinition[];      // [saolei_init, saolei_operate, saolei_remain]
  readonly sections: readonly PromptSection[];    // [{ name: "saolei:guidance", order: 100, text: SAOLEI_GUIDANCE }]
}

export function apply(ctx: Context): void {
  ctx.provide(SAOLEI_TOOLS_SERVICE, buildSaoleiToolsUnit());
}
```

- **与 074 契约的差异唯一处**：单元 sections 从 `[saolei:guidance(100), saolei:game(50)]` 收窄为 `[saolei:guidance(100)]`——saolei:game 拆归游戏基础（[saolei-foundation.md](saolei-foundation.md) §2），由编排对 player 与 planner 注册（[team-loop-assembly.md](team-loop-assembly.md) §3）。
- 单元保持"tools + prompt 整单元产出、不可部分消费"（074 spec FR-015 约束对象——能力单元）。
- MUST NOT 在 apply 中全局注册（组合级注册泄漏全员，074 FR-020 语义延续）。

## 3. 工具资产（等价迁入，零变化冻结值）

- **三工具**：`saolei_init`（无参）、`saolei_operate`（双形式 single `{type,x,y}` XOR batch `{operations:[...]}`）、`saolei_remain`（无参只读）——定义/schema/描述文案与现状逐字一致（specs/051 contracts/saolei-plugins.md §3、specs/064 §2 权威）。
- **校验资产**：`AMBIGUOUS_ARGS_TEXT` / `MISSING_ARGS_TEXT` / `INCOMPLETE_ARGS_TEXT`（v1 字面量）、`normalizeOperateArgs`（双形式互斥校验）、`RESULT_OUTPUT_SCHEMA` 与 `resultOutput()`（`{result: string}` canonical 输出）。
- **执行语义**：`resolveRuntime`（`exec.agent.ctx.saoleiGame` per-agent 解析；`exec.agent` 缺失或 scope 无服务 = fail-loud，错误文案不变）→ `executeOutcome`（isError → throw 模型可见失败；`concludesTurn === true` → 先 `exec.concludeTurn()` 再返回——specs/062 contracts/saolei-turn-conclude.md §2）。
- **`SAOLEI_GUIDANCE`**：工具守则 section 文本（内容边界 = specs/060 contracts/prompt-sections.md §2），逐字不变。

## 4. 依赖声明

```
dependencies:      { "@dominion/dsh-saolei": "workspace:*" }   // 类型与运行时类型面（OperateInput/SaoleiGame/ToolOutcome）
peerDependencies:  { "@deepseek-ai/cordis", "@deepseek-ai/dsh-tools", "@deepseek-ai/dsh-system-prompt" }
```

- MUST NOT 依赖 saolei-loop（spec FR-008）——工具对编排零知识（运行时经 agent scope 解析，不经编排面）。

## 5. 测试义务（vitest 随交付）

`index.test.ts` 自旧 dsh-saolei `index.test.ts`（441 行）迁移改写：

1. 单元提供面内容：三工具 schema/描述逐字断言、`saolei:guidance` 名/order 100/text 逐字断言。
2. **单元 sections 仅含 guidance**（无 `saolei:game`——收窄断言）。
3. 无全局注册副作用（stub ctx 断言 `tools.register`/`systemPrompt.section` 未被调用）。
4. 双形式校验：三种非法组合的拒绝文本逐字断言。
5. 缺运行时 fail-loud：`exec.agent` 缺失 / agent scope 无 `saoleiGame` 的错误文案断言（迁移源原样）。
6. `concludesTurn` 转发与 isError 抛错语义（迁移源原样）。
