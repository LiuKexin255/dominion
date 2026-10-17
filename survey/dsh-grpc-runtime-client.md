# 调研：gRPC client 基建与能力/组装分层（RPC 依赖型工具的宿主基建化）

> **状态**：调研完成（经三轮收敛：MCP 桥否决 → 共享 runtime client → 自上而下三层模型）。
> **已确认决策（2026-09-30，多轮）**：
> ① **否决"MCP 桥"**——薄转换层把 gRPC/AIP 服务暴露为 MCP 在本拓扑下为负价值：无生态边缘收益、
> 模型面绕圈、AIP 语义有损、工具产品设计不可从 proto 生成、新增单点（§3.1）。
> ② **方向 = 共享 runtime client**——概念澄清：传输协议选择与模型面 token 成本无关；MCP 的 token
> 开销是其客户端约定（schema 全量入上下文），不是协议 wire（§2.1）。
> ③ **不启用 PTC 模式**——当前定位非 code agent；~7 个工具量级下 SDK 固定成本为负收益（§3.3）。
> ④ **runtime 由 cordis row 承载**（需要有实例承载缓存与生命周期；纯库消费方也只有该 row），
> 消费形态 = 组合级基建行——与 `dsh-core` 基线行同级，工具消费与 `inject: ["tools"]` 同构（§6.1）。
> ⑤ **范围 = agent 宿主承载 gRPC client 侧基建**；不含 gRPC server 配置、不含 HTTP/Mongo 等其他
> 协议（留同构位置）（§6）。
> ⑥ **类型面消费 ts_proto_library 生成的 client 接口**（`agent_v2_types` 已生成 Memory/Wiki/
> SessionService 接口），消除 `(proto as any)`；不改 types-only + 运行时 proto-loader 约定（§6.4）。
> ⑦ **目标按自上而下三层模型定义**（推翻过程版"memory-service 瘦身保留"与"mount 原语放 memory 包"
> 两版结论）：基建 / 能力 / 组装分离（§2.2）。
> ⑧ **双维度层级模型**——能力维度（基建/能力/组装）× 运行时维度（team 代码驱动层 > agent LLM
> 基座 > tools 堆叠）。适配方向单向：tools 被添加进 agent，agent 不为工具适配；system prompt
> 注入不属工具也不属 agent，组装归 **team 层**（代码驱动，非 LLM 驱动）（§2.2）。
> ⑨ **memory 能力化**——memory 包 = 查询 memory 的功能 + memory 工具 + 渲染原语，领域无关，
> **无组装职责**（用户原则原话："memory 是一个与场景无关的部分……组装 memory 不是他的职责"）；
> 组装（scope 派生、fail-loud 预取、挂面、section 注入）在 team 层（session.ts / orchestrator）（§6.3）。
> ⑩ **快照就绪语义保留**——物化期 fail-loud 预取不变（specs/071、memory-plugin survey 决策 ⑧）。
> ⑪ **memory-service 整体移除**——memory 专属服务行消失；grpc-runtime 基建行为唯一新增组合行（§6.2）。
> ⑫ **scope 进入工具世界的唯一通路 = team 层组装注册**（框架事实：Agent 公共面仅 id，
> meta.agentPreset 不暴露——工具自足派生 scope 被封死）（§5.2）。
> ⑬ **角色能力组装并入 team 层编排**（orchestrator 成员 setup，对称既有 `mountPlayerRuntime`；
> 组装点现成——`orchestrator.ts:757-765` 中唯一委托模板机制的是 `composed.setup` 一行）；
> 否决独立组装插件壳（"为了插件而插件"：无独立生命周期/配置/多消费方价值）（§6.3）。
> ⑭ **模板行机制重构为即插即用列表**——templateRules 移除（校验的本来就是部署自控常量：
> 用户 preset 从不能改组合，无运行时灵活性收益）；`templates.{}.plugins` 保留，语义 =
> **自包含通用插件**（web 浏览/搜索类）的即插即用挂载：无校验、无额外语义，"往里添加插件
> 就表明你知道你在做什么"；需要上层组装的能力（saolei、memory 类）不列入此表。分界判据
> 唯一：**是否需要上层组装**（per-agent 状态/scope/物化期动作）（§2.2/§6.2）。
> ⑮ **registry 瘦身保留**——preset 语义/概念不变（分池、persona-only 编辑、fail-loud resolve、
> 修订保留）；成员 provider/model 经 `agentOptions` 直达 loop（`orchestrator.ts:746-756` 已验证），
> registry 对成员生效面的贡献收缩为 persona 挂载 + `meta.agentPreset` 身份；角色默认 persona
> 为纯配置项（去留不影响架构与系统运行）（§6.2）。
> **日期**：2026-09-30
> **前置调研**：`survey/mcp-ecosystem-trends.md`（MCP 生态趋势——决策 ①②③ 宏观依据、R5 跨框架
> 对照来源）、`survey/deepseek-harness-memory-plugin.md`（memory 插件机制与物化 setup 时序）、
> `survey/planner-memory-and-agent-communication.md`（hermes 冻结快照哲学）
> **范围**：agent_v2（dsh 组合）内工具访问外部 gRPC 服务的基建化调研——目标模型、框架与
> grpc-js 机制事实、memory 案例的目标形态。**不含** spec/plan 阶段的 API 细节与迁移方案
> （§9 一行式移交）。

---

## 1. 背景与调研问题

`projects/game/agent_v2/` 以 dsh 进程内嵌入（B1 模式）宿主 deepseek-harness，插件（cordis row）
装配 agent。依赖外部 RPC 服务的插件（现状：memory-service）需内嵌完整 gRPC client 基建，每新增
RPC 依赖工具都要复制样板并单独抽一个 host row。调研历经三轮收敛：

1. 是否应以 MCP 桥复用生态？（否，决策 ①）
2. "远程工具调用（协议定义代替固定协议）"能否解决 token 问题？（问题定位修正——模型面/传输面
   两层切分，决策 ②）
3. 共享 runtime 的正确形态？（B′，决策 ④）
4. 目标应自上而下定义而非基于现状调整：基建/能力/组装三层 × 运行时层级维度（决策 ⑦⑧⑨）。

## 2. 概念基础

### 2.1 模型面与传输面的两层切分

| 层 | 内容 | 谁消耗 token |
|---|---|---|
| **模型面** | LLM 实际看到的：工具名 + JSON schema（native FC）/ `run_code` + SDK（PTC）/ 搜索目录 | **只有这一层** |
| **传输面** | 执行体如何到达服务：进程内函数 / gRPC client / HTTP / MCP wire | **零 token** |

推论：植入 client 是工程成本不是 token 成本；"像一般工具调用、无额外 token"现状已满足；
要消除的是**每域一行的 client 基建样板**。用户类比"MCP 像 protobuf 序列化、proto 定义被放入
消息内容"的精确化：被放进内容的是模型面 JSON Schema，与传输协议无关。

### 2.2 双维度层级模型（核心产出，决策 ⑦⑧）

```
运行时层级（自上而下）        能力维度（归谁所有）
──────────────────────────────────────────────────────────
 team 层（代码驱动，非 LLM）    ◄─ 组装职责所在
 │  session.ts + saolei-loop      物化成员、scope 派生、fail-loud 预取、
 │  orchestrator（驱动时机+阶段机； 挂 agent-scoped 面、system prompt
 │  含非 LLM 的"扫雷系统"成员）     section 注入
 ▼
 agent 层（LLM 驱动的基座）     ◄─ 被组装（system prompt 由组装层拼装；
 │  player / planner               基座不感知具体工具）
 ▼
 tools 层（堆叠在 agent 之上）  ◄─ 能力提供
    saolei / memory 工具           工具被添加进 agent；执行期经
    （standing 注册）               exec.agent.ctx 解析 per-agent 面

 ▎基建（纵向贯穿）：grpc runtime（ctx.grpc）、tools/system-prompt registry
 ▎——组合级一件，各层经 inject 消费
```

**适配方向单向（用户原则）**：常见的是把 tools 添加到 agent 当中，一般不需要 agent 为某个
tool 适配——为 system prompt 注入 memory 不属于 tools 的职责；归到 agent 也很奇怪（agent 反向
依赖 tools）。组装者是**比 agent 更高的层级**：其他框架中 OpenClaw 的编排网关、hermes 的编排层
承担此角色（R5），在本仓库是 **team 层**。仓库内已有完全同型生产先例：orchestrator 在成员
setup 里挂 `saoleiGame`（`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts:817-824`）——
team 层组装 agent 的既有实践，memory 回到同一模式即可。

**能力的两类归宿（决策 ⑭ 的判据）**：自包含能力（无 per-agent 状态、无物化期动作——web
浏览/搜索类通用工具，及未来的 RPC 自足工具）→ **声明式即插即用**（`templates.{}.plugins`，
经 registry/compose 既有挂载，可自行 `inject: ["grpc"]`）；组装依赖能力（需要 scope 派生/
面注册/物化期预取——saolei、memory）→ **team 层组装**。判据唯一：是否需要上层组装。

### 2.3 MCP 类比的精确映射

| MCP 世界 | 对应物 |
|---|---|
| host 内建 MCP client runtime | grpc-runtime 基建行（组合级一件，非领域行） |
| 提供 http endpoint 即可 | `ctx.grpc.client({target, protoPath, serviceName})` |
| 消费侧无需连接插件 | 工具行 `inject: ["grpc"]`，与 `inject: ["tools"]` 同构（cordis 中基建与领域的区别不在是否 inject，而在组合级一件/领域无关/众家共用） |
| endpoint → 自动 tools | **刻意不做**：endpoint → client，工具产品设计（schema/脱敏/结果文本化）不可从 proto 生成（决策 ① 依据） |

## 3. 方案空间与裁决（压缩存档）

- **A. MCP 桥（否决，①）**：唯一消费者（无生态边缘收益）；dsh-tools 已拥有模型面（MCP 化后还需
  适配回 `ctx.tools`，绕圈）；AIP 资源名/field mask/分页/错误码有损；工具产品设计不可从 proto
  推导（Thoughtworks Hold 环反模式）；新增单点。
- **B′. 共享 runtime（采纳，②④）**："引入协议与植入 client 本质无差"对一半——缝放编译期/组合期
  实例则零动态成本，放运行时通用桥则全是动态税。
- **B″. PTC（暂缓，③）**：积木已备（dsh-ptc-runtime 等）；触发条件=工具面膨胀或组合调用成主要
  模式（saolei/memory 的 `operations[]` 批量形态是观察哨）。

## 4. 仓库现状盘点

- 手工 gRPC client：`memory-service/src/client.ts`（473 行）+ `wiki-client.ts`（377 行，已复用
  前者的策略导出——共享化停在包级）；`agent_v2/src/server.ts:197-204` 为 server 侧（范围外）。
- memory-service 解剖：~680 行连接基建 + 1,139 行领域语义（service.ts 206 / operations.ts 478 /
  wiki-operations.ts 379 / snapshot.ts 66 / errors.ts 10）+ host row（index.ts 91）。
- 已有积木：`common/js/grpc/resolver`（幂等）、`common/js/grpc/otel`（**载入顺序约束：须先于
  grpc-js 模块加载**）、`ts_proto_library`（types-only 约定）、`callUnary`/`probeChannel` 等策略
  雏形、`common/gopkg/grpc/default.go`（Go 侧策略参照，非对齐目标）。
- 消费先例：plannerMemory host service（现状模式）、desktop-bridge class-form Service、
  **saolei agent-scoped 模式**（`saolei-loop/src/game/runtime.ts:415-440` isolate 注册 +
  `saolei/src/index.ts:91-104` exec.agent.ctx 解析）、session.ts `deps.mountPlayerRuntime`
  setup seam（`session.ts:825-827`）、agent-scoped section（`team/src/team.ts:118`）。

## 5. 机制事实核验（调研问题清单，全部闭合）

| # | 问题 | 结论 |
|---|---|---|
| R1 | grpc-js（1.14.4）连接机制 | 无 channel 全局池（`client.js:41-66` 每个 Client 各建 Channel）；`channelOverride` 是官方跨 service client 共享缝隙（grpc-node#2214 维护者模式）；one client per service per process（#2896/#2402，频繁创建泄漏 channelz）；隐式连接共享要求相同 credentials/options 对象（#158）——现状 Memory/Wiki 两 client 实为两条连接，基建化顺带修正 |
| R2 | scope 如何进入工具世界 | **Agent 公共面仅 `id`**（`@deepseek-ai/dsh-agent/lib/types/types.d.ts:11-14`）；`CreateAgentOptions.meta.agentPreset`（`index.d.ts:64-71`）不暴露给执行期 → 工具自足派生 scope 被框架封死；唯一通路 = team 层组装注册（isolate + exec.agent.ctx 属性穿越，saolei 同型先例生产验证） |
| R3 | 组装落 team 层是否有框架障碍 | 无：`mountPlayerRuntime` setup seam 先例；`CreateAgentOptions` rollback-covered（setup throw = 物化回滚，`session.ts:17` 注释）；agent-scoped section 先例（team.ts:118）；orchestrator 挂 saoleiGame 即同型组装 |
| R4 | 基建承载与默认性 | 组合级基建行（dsh-core 基线行同级，`third_party/dsh/core` 已验证存在为框架基线机制）；仓库级基线化推迟至第二个 agent 服务出现 |
| R5 | 能力/组装分界的跨框架有效性 | OpenClaw（skills+CLI 能力 / 编排网关组装）与 hermes（编排层注入冻结快照）同构；见 `survey/mcp-ecosystem-trends.md` §3.1/§7、`survey/planner-memory-and-agent-communication.md` |
| R6 | 成员生效面的实际通道（model/persona/工具如何到达成员） | provider/model 经 `agents.create` 的 `agentOptions` 直达 loop（`orchestrator.ts:746-756`）；persona 与工具经 `composed.setup`（registry 行挂载）——**registry 对生效面的贡献 = persona 挂载 + `meta.agentPreset` 身份**，为决策 ⑭⑮ 提供事实基础 |

**必须随代码迁移的策略（why-注释）**：round_robin 覆盖 pick_first（滚动升级路由）；**不做** app 级
keepalive（对齐 grpc-go `ClientDefault`，unary server 默认 enforcement 会 GOAWAY 多余 ping，
`client.ts:145-159`）；重连退避 1s/15s；`/etc/tls/ca.crt` + `TLS_SERVER_NAME`。

## 6. 目标架构（决策 ⑦-⑫ 的落地形态）

### 6.1 基建层：grpc-runtime 基建行（决策 ④⑤）

组合清单一件（`cordis.yml` 与 dsh-core 同级），领域无关，生命周期随组合；内部直接组合
`common-js-grpc-resolver`/`common-js-grpc-otel`/grpc-js，不拆独立 common lib。服务面（签名示意）：

```ts
ctx.grpc.client<T>({ target, protoPath, includeDirs?, serviceName, deadlineMs?, label? }): T
ctx.grpc.callUnary<T>(client, label, invoke, deadlineMs?): Promise<T>
```

channel 按 (target, 凭据策略, channel 策略) 键缓存经 `channelOverride` 共享；packageDefinition
按 protoPath 缓存；protoPath 由消费边缘解析（部署布局知识不入基建）。

### 6.2 组合清单与 preset 机制变化（决策 ⑪⑬⑭⑮）

```
  + grpc-runtime 基建行（唯一新增组合行）
  - memory-service 行（整体移除）
  - planner/player 模板快照中的工具行（@dominion/dsh-memory / @dominion/dsh-saolei——
    工具与配套 section 的注册移 team 层组装）
  - cordis.yml preset-authoring 行 Config 的 templateRules（校验随工具行一同消亡）
  ± templates.{}.plugins 保留：即插即用插件列表（自包含通用插件——web 浏览/搜索类；
    当前内容为 persona 行；无校验、无额外语义）
  ± preset-authoring 行与 registry（瘦身保留）：store CRUD / 分池 / persona-only 编辑 /
    fail-loud resolve / 修订保留不变；derive 机制保留（persona patch + 即插即用行）
```

现状模板行机制的层序问题一并修正：templateRules 把场景知识（"player → 恰含 saolei 行、
禁 memory 行"）放进部署数据（cordis.yml），是层序倒置；重构后场景能力归场景代码（team 层），
部署数据只承载真正的部署决策（即插即用插件的取舍）。角色默认 persona（模板 persona 行
prefix）为纯配置项，去留不影响架构（决策 ⑮）。

### 6.3 能力层与组装层（决策 ⑧⑨⑬）

```
能力包（领域无关，无场景词汇）导出——dsh-memory / dsh-saolei 同构：
  ① 工具注册函数 + 工具定义（execute 经 exec.agent.ctx 解析 agent-scoped 调用面，
     saolei 模式，缺面 fail-loud）
  ② 能力原语（无副作用、无组装流程）：
     dsh-memory：createStore(grpc) / createFace(store, scope) /
                 renderSnapshot(entries) / snapshotSection(text)
     dsh-saolei：registerSaoleiTools(agentCtx) + guidance section 工厂
                （GameRuntime 挂载已由 createAgentGameRuntime 承担）

team 层组装（orchestrator 成员 setup，orchestrator.ts:757-765 组装点现成）：
  player：composed.setup（persona）+ registerSaoleiTools + guidance +
          mountPlayerRuntime（既有）
  planner：composed.setup（persona）+ createStore(ctx.grpc) → createFace(scope) →
          fail-loud 预取 → agent-scoped 注册面 + memory/wiki 工具 +
          guidance + 快照 section
```

自包含工具（无物化期需求者）不经组装：或为即插即用插件列入 `templates.{}.plugins`
（声明式，可自行 `inject: ["grpc"]` + `ctx.grpc.client(...)`），或作为能力包被组装层调用。

### 6.4 类型面（决策 ⑥）

消费 `agent_v2_types` 生成接口（已验证含 MemoryService.ts/WikiService.ts/SessionService.ts），
泛型传入去 any 强转。

## 7. memory 案例效果（量化）

memory-service 2,080 行（8 文件）的去处：连接基建 ~680 行 → `ctx.grpc`；领域语义 1,139 行
**等价迁入** memory 能力包（store/face/渲染原语）；host row 91 行消失。memory 工具插件 576 行
中工具定义/schema/文案/守则不变，注册方式变更（standing 行挂载 → 能力包导出 + team 层组装
调用，execute 面解析从 closure 绑定改为 exec.agent.ctx）。player 侧对称：saolei 工具行同样
移出模板。附带收益：Memory/Wiki 首次真正共享一条连接（R1）。

## 8. 已知边界与风险

- 组合可见性分化（决策 ⑬⑭ 的另一面）：组装依赖能力（saolei/memory）的存在性由 team 层
  代码与编译期 import 保证，不再体现于组合清单；自包含插件仍声明式可见
  （templates.{}.plugins）。分界判据（是否需要上层组装）需在契约文档中明确——误挂到
  即插即用列表的组装依赖插件只会缺面 fail-loud，无静默错误。
- grpc-otel 载入顺序约束延续（基建行不得模块顶层副作用初始化）。
- channel 共享放大同 target 故障域（风险等价：本就同实例集）。
- 行为差异三处（错误信息/section 注册点/测试 seam）——属 spec 阶段契约化内容。

## 9. 移交 spec/plan 阶段事项（一行式，不展开）

grpc-runtime 包名/服务名/API 签名定稿；memory/saolei 能力包导出面粒度与命名（工具注册函数
形态）；orchestrator 组装回调的结构（seam 形态，对称 mountPlayerRuntime）；templateRules 移除
与 templates.{}.plugins 语义的契约化（059/070/071 契约修订清单）；行为差异三处的契约更新
（缺面错误、section 注册点迁移、session.ts seam 形态）；迁移分步与验收门禁（单测重组 +
大型测试四 suite 零回归 + 缺面 fail-loud 断言）；protoPath 解析的部署布局约定；HTTP 等其他
协议的同构扩展位命名。
