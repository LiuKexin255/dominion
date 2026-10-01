# Contract: team 层成员组装（依赖声明 × 提供方挂载 × 编排消费）

**Feature**: [spec.md](../spec.md) | **契约层级**: 组装（team 层：依赖声明在 config、能力实现在提供方插件、编排权威在 saolei-loop 代码）

## 1. 机制（2026-09-30 用户裁定，四轮收敛终态）

**saolei-loop 声明有哪些依赖，config.plugins 中的插件实现这些依赖并挂载（provide），saolei-loop 拿着这些依赖再去组装和编排**——cordis 原生 DI 惯用法（`ctx.plugin` / `ctx.provide` / `reflect.get`，cordis 4.0.4 已核验），不依赖官方 preset registry：

| 层 | 承载 | 内容 |
|---|---|---|
| **依赖声明**（cordis.yml，saolei-loop config.plugins） | 部署数据 | **扁平插件列表**（包名 + config）——纯依赖声明，**无角色语义、无白名单、无校验**；"这些能力分给谁"不是配置的职责 |
| **提供方插件**（列表项，挂载为 saolei-loop 子插件） | 能力实现 | apply 内 `ctx.provide` 暴露能力面；dsh-saolei-tools 提供工具静态单元，dsh-saolei-memory 提供 per-member 工厂；部署连接参数在插件 config |
| **编排**（saolei-loop 代码） | team 语义 + **权威** | apply 挂载列表行 → 消费提供面（缺 = fail-loud）；成员 setup 按角色消费能力单元并注册到成员上下文——"player 用 saolei、planner 用 memory"的权威说明在此代码 |

三段循环：① 声明依赖（config.plugins 扁平列表）→ ② 提供方挂载实现（ctx.plugin → ctx.provide）→ ③ 编排消费（saolei-loop 读 faces → 成员组装）。

## 2. 依赖声明（cordis.yml，`projects/game/agent_v2/cordis.yml`）

```yaml
- id: saolei-loop
  name: '@dominion/dsh-saolei-loop'
  config:
    plugins:                        # 扁平列表：saolei-loop 的能力依赖声明（无角色分组）
      - name: '@dominion/dsh-saolei-tools'
      - name: '@dominion/dsh-saolei-memory'
        config:
          target: 'dominion:///game/memory:50051'
          protoPath: 'projects/game/game.proto'   # canonical import path（基建解析）
```

**列表语义**：

- 填入列表的插件被挂载（按裸包名动态解析 + `ctx.plugin(entry, config)`，config 经插件 Config schema 校验，失败 reject → 组合启动 fail-loud）。
- 编排代码不消费的提供 = 惰性（无害）；插件自身 inject 不满足 = 其挂载失败 → 组合启动 fail-loud；所需提供缺失（列表漏项）= saolei-loop 读 faces 时 fail-loud。
- **无角色槽位**——不存在"把 X 错填到 planner"的配置形态；角色分配错误在编译期/启动期即不可能。

## 3. 提供方插件契约

### 3.1 `@dominion/dsh-saolei-tools`（静态单元提供方）

| 项 | 值 |
|---|---|
| 形态 | cordis 插件；`inject: []`；挂载于 saolei-loop 之下（组合级） |
| apply | `ctx.provide("saoleiTools", { tools: [saolei_init, saolei_operate, saolei_remain], sections: [saolei:guidance] })` —— 工具定义/schema/文案与 guidance section 名/order/文本**零变化**；sections 收窄为仅 `saolei:guidance`（`saolei:game` 拆归游戏基础 `@dominion/dsh-saolei`，由编排对 player/planner 双角色注册）；单元经 provide 暴露，组合级挂载下不做全局注册（会泄漏给全员） |
| 不变项 | 工具 execute 的 `exec.agent.ctx.saoleiGame` 解析不变；`saoleiGame` 运行时为编排持有的 `@dominion/dsh-saolei` `GameRuntime` 实例（`common/js/dsh-plugins/saolei/src/game/runtime.ts` 的 `createGameRuntime` 工厂）经 player setup 注册的 agent-scoped 句柄，**不计入静态单元**（specs/075-saolei-game-foundation/contracts/team-loop-assembly.md §2/§3） |

### 3.2 `@dominion/dsh-saolei-memory`（per-member 工厂提供方，新包）

| 项 | 值 |
|---|---|
| 形态 | cordis 插件（`common/js/dsh-plugins/saolei-memory/`）；`inject: ["grpc"]`；挂载于 saolei-loop 之下 |
| config schema | `{ target: string, protoPath: string (canonical), label?: string }` |
| apply | apply 内一次性构造 client：`ctx.grpc.client<MemoryServiceCalls>` / `<WikiServiceCalls>`（config 驱动；组合级共享 channel）→ `ctx.provide("memoryMount", { build(scope) })` |
| `memoryMount.build(scope: MemoryScope): MemoryCapabilities` | `buildMemoryCapabilities({ scope, memoryClient, wikiClient })`（公共库原子单元：faces/tools/sections/ready）——**scope 由编排显式传参**（无属性穿越约定） |
| 依赖方向 | adapter → 公共库（common-js-memory）+ grpc 面；**不依赖 saolei-loop 任何约定**（比属性穿越更纯） |

## 4. 编排消费（saolei-loop）

**apply 期**（依赖装配，fail-loud）：

```text
for row in config.plugins: await ctx.plugin(dynamicImport(row.name), row.config)
saoleiTools = ctx.get("saoleiTools")      # reflect 注册面的 Context 便捷读取；缺 → throw（组合启动失败）
memoryMount  = ctx.get("memoryMount")     # 同上
```

**createMember setup**（成员组装，`orchestrator.ts:756-766` 改造；注册序 faces → tools → sections → ready）：

```text
setup(agentCtx, agent):
  1. await composed.setup(agentCtx)                     # persona（不变）
  2a. player:  register saoleiTools 单元（tools×3 + sections[saolei:guidance] → agentCtx）
               agentCtx.systemPrompt.section(foundation.gameSection)              # saolei:game
               this.game = foundation.createGameRuntime(session, desktopBridge)   # 编排持有实例
               agentCtx.isolate("saoleiGame").provide("saoleiGame", this.game)     # agent-scoped 句柄
  2b. planner: agentCtx.systemPrompt.section(foundation.gameSection)          # saolei:game（074 遗留修复）
               unit = memoryMount.build(scope)          # scope 显式传参（编排层既有 memoryScope 流）
               register unit（faces("memory") → tools×4 → sections×3 → agentCtx）
               await unit.ready()                       # fail-loud 预取 → throw → 回滚
  3. （既有 idle/error 订阅等不变）
```

**编排权威**：player/planner 的能力分配、"planner 需要 memory"（tools + system prompt 记忆组装——guidance 与快照 section 的注入）全部由本代码承载；system prompt 的**内容语义**（section 规格/文本/快照渲染与冻结）归公共库，**注入动作**归编排。

## 5. 行为差异契约（相对 specs/059/070/071 既有契约的三处修订）

| # | 差异 | 终态 |
|---|---|---|
| ① | 缺面错误文案 | memory/wiki 四工具缺面文案见 [memory-library.md](memory-library.md) §5（`plannerMemory` 措辞清零，isolate label 改 `memory`） |
| ② | section 注册点 | memory:guidance/wiki:guidance/memory:snapshot 从 preset 行挂载迁移为编排注册（planner setup）；saolei:guidance 归 saoleiTools 单元、由编排 player setup 注册，saolei:game 归游戏基础 `@dominion/dsh-saolei`（来源非单元）、由编排对 player 与 planner 双角色注册；名/order/文本零变化 |
| ③ | session.ts planner memory seam | `loadPlannerMemoryBinding()` 与 orchestrator `loadPlannerMemory` dep 消失，代之 saolei-loop 依赖装配 + 编排消费；`ctx.plannerMemory` 面名清零；session.ts 无组装代码 |

## 6. 不变量

- **依赖声明纯度**：config.plugins 无角色语义/白名单/校验——角色编排逻辑 MUST NOT 下放进配置。
- **fail-loud 三点**：插件挂载失败（config 校验/inject 不满足）→ 组合启动失败；所需提供缺失 → saolei-loop apply throw；预取失败 → 成员物化回滚（`CreateAgentOptions` 既有 rollback；specs/059；memory-plugin survey 决策 ⑧）。
- **插件单元**：能力单元（tools + prompt + 其他能力）整单元注册，无拆分调用面（spec FR-015）。
- **能力单元边界**：saoleiTools 静态单元仅含三工具 + `saolei:guidance` 单一 section（`saolei:game` 归游戏基础 `@dominion/dsh-saolei`，由编排对双角色注册，不在单元内）；`saoleiGame` 运行时实例为编排持有的 `@dominion/dsh-saolei` `GameRuntime`（`common/js/dsh-plugins/saolei/src/game/runtime.ts` 的 `createGameRuntime` 工厂）经 player setup 注册的 agent-scoped 句柄，**不计入静态单元**——spec FR-015 的「其他能力」指单元内 tools/sections 的整体产出，不含运行时实例。
- **player 隔离**：player 分支只消费 saoleiTools → player 工具面与 prompt 无 memory/wiki 痕迹（spec FR-017；取代 templateRules forbidden 的结构性保证）。
- **依赖方向**：saolei-loop 以包名消费提供方（无能力实现编译依赖）；公共库无场景/部署知识；adapter 不依赖 saolei-loop。
- **成员生效面**：persona 经 composed.setup、身份经 `meta.agentPreset`、provider/model 经 `agentOptions`——三通路不变（specs/070 preset-model-mapping §3 继续有效）。
- **机制依据**：cordis 4.0.4 `ctx.plugin`（运行时挂载、config schema 校验、fiber reject 即启动失败）/ `ctx.provide` / `reflect.get`（research.md D13 已核验；官方 registry 非必需——如实现选用 registry 路径，async-await 与 audit 语义见 D13 备注）。

## 7. 依赖方式边界（正向依赖 vs 配置式依赖注入）

**判据是"依赖的对象是什么 + 归属"，而非"是否解耦"**——正向 inject 本身即面解耦（face 名契约、实现可替换）：

| 维度 | 正向依赖（inject / ctx.get，宿主级行提供） | 配置式依赖注入（config.plugins 子插件） |
|---|---|---|
| 依赖对象 | **服务**：消费方代码直接调用的 API（desktopBridge、ctx.agents、ctx.grpc） | **能力单元**：消费方装配进自己域的组装材料（tools + prompt + faces + ready 整体） |
| 消费形态 | 调用（call）——调用点在代码里显式 | 装配（assemble）——单元整体注册进成员域，内容不被逐个调用 |
| 归属与生命周期 | 组合级共享资源（desktopBridge 被 saolei-loop 与 agent_v2 宿主双消费；grpc 面向全体）；独立行、独立 config | 消费方私有（场景胶水，离开消费方无意义）；子插件随消费方部署与消亡 |
| 部署知识 | provider config 是其自身的部署关注 | provider config 是消费方场景的部署决策（内聚于消费方行下） |
| 扩展语义 | 换实现（同 face 替换 = 多态） | 加能力（列表加一项 = 能力集扩展） |

**取舍规则**：

1. 当服务用（固定 API、代码期闭集契约、逐点调用）→ 正向依赖。
2. 当能力单元装配（整单元进成员域、含物化期动作、provider 携带场景部署 config、场景私有）→ 配置式。
3. 前两条不分时：共享 → 正向；场景私有 + config 内聚 → 配置式。
4. 迁移触发：既有正向依赖演化出"能力单元 + 场景部署 config + 专属"三特征时才迁移（不为对称而对称）。反证：desktop-bridge 为共享资源（agent_v2 宿主亦消费），改为配置式会导致重复部署或宿主依赖 saolei-loop 子树的倒挂；memory 用 inject 形态则失去场景部署单元内聚与能力集声明语义。

