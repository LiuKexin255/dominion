# Research: gRPC client 基建插件、memory 能力化与 preset 模板简化

**Feature**: [spec.md](spec.md) | **Date**: 2026-09-30

**调研基线**: 本文档承接 [survey/dsh-grpc-runtime-client.md](../../../survey/dsh-grpc-runtime-client.md)（三轮收敛、15 项已裁定决策）与 spec Assumptions 移交清单，对 survey §9 的全部移交项作出 plan 阶段裁定。用户在本 plan 阶段追加裁定多项（D4 组装机制四轮收敛、D5/D6 adapter 与库化、D7 canonical protoPath——2026-09-30，记录于 spec Clarifications）。

---

## D1. grpc-runtime 基建插件形态与包名

**Decision**: 新建包 `common/js/dsh-plugins/grpc-runtime/`，包名 `@dominion/dsh-grpc-runtime`，cordis 行 id `grpc-runtime`，服务面 `ctx.grpc`。组合清单一件、与 `@dominion/dsh-core` 基线行同级（`projects/game/agent_v2/cordis.yml`）；`inject = []`（纯提供方）；内部组合 `@dominion/common-js-grpc-resolver` / `@grpc/grpc-js` / `@grpc/proto-loader`（源码依赖）与 `@dominion/common-js-grpc-otel`（部署闭包依赖，源码不 import），**不拆独立 common lib**（survey 决策 ④、§6.1）。

**Rationale**: `@dominion/dsh-*` 是本仓库 dsh 插件包既有命名约定；`ctx.grpc` 与 survey §6.1 签名示意一致，且与 `ctx.tools` / `ctx.systemPrompt` 框架面命名同构。基建行生命周期随组合：apply 时幂等注册 resolver + 建立惰性 runtime（channel 缓存不预建，唯一创建点 `ctx.grpc.client()`），`ctx.effect` teardown 关闭全部缓存 channel；gRPC 插桩注册归服务 bootstrap 两段式入口（[048 otel 契约](../048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md) §2），插件以 channel 惰性创建保持 otel 先于 grpc-js 的顺序约束。

**Alternatives considered**: (a) 迁入 `third_party/dsh/core` 框架基线——推迟至第二个 agent 服务出现（survey R4）；(b) 独立 `common/js/grpc/runtime` 库——被 survey §6.1 否决；(c) 服务面名 `ctx.grpcClient`——冗长，弃。

## D2. 基建服务面 API 定稿（含 canonical protoPath 解析）

**Decision**: 服务面两个方法（完整契约见 [contracts/grpc-runtime.md](contracts/grpc-runtime.md)）：

```ts
interface GrpcClientOptions {
  readonly target: string;                 // dominion URI，如 "dominion:///game/memory:50051"
  readonly protoPath: string;              // canonical import path（如 "projects/game/game.proto"）
                                           // 或绝对路径（逃逸出口）
  readonly includeDirs?: readonly string[];// 缺省 [解析出的 service root]
  readonly serviceName: string;            // 完全限定名，如 "projects.game.MemoryService"
  readonly label?: string;                 // 日志标签，缺省 serviceName
}

interface GrpcRuntime {
  client<T>(options: GrpcClientOptions): T;
  callUnary<T>(
    client: grpc.Client, label: string,
    invoke: (client: grpc.Client, metadata: grpc.Metadata,
             options: { deadline: Date },
             callback: (err: grpc.ServiceError | null, response?: T) => void) => void,
    deadlineMs?: number,                   // 缺省 10_000，等价现状
  ): Promise<T>;
}
```

- **canonical protoPath 解析（用户裁定 2026-09-30）**：`protoPath` 携带 canonical import path（构建系统无关的逻辑身份——runtime_protos 部署标准物化 proto 于 service root 下的标准 import 路径，`tools/release/deploy/README.md` §runtime_protos；`server.ts:80` `PROTO_PATH` 同约定先例）；基建按模块相对深溯解析 service root（memory-service `serviceRoot()` 生产布局已验证的同型 walk——部署 tar 内 `node_modules/@dominion/<pkg>/src` 四级上溯即 service root）并拼接为绝对路径；绝对路径直接使用（逃逸出口）。依据：runtime_protos 是**仓库统一部署标准**，与 TLS `/etc/tls/ca.crt`（D3 已入基建）同级——survey"部署布局知识不入基建"按此重新裁定（个性布局知识仍不入，仓库标准约定入）。dev source tree 中该 walk 不达 proto（与现状 memory-service 自建分支同性质——proto 加载本就是部署态关切，测试经 DI 注入）。
- `client<T>`：packageDefinition 按 (protoPath + includeDirs) 缓存 `loadSync`（`longs:String, enums:String, defaults:true, oneofs:true`——与 `ts_proto_library` 生成选项严格一致）；channel 按 target 缓存（凭据/channel 策略为基建统一常量，键退化为 target——策略分化时必须扩键）、经 `channelOverride` 官方缝隙共享（survey R1/grpc-node#2214）；`loadPackageDefinition` 后按 `serviceName` 逐段取构造器，单点 `as T` 泛型收口（全仓唯一 proto 强转点）。
- `callUnary`：**逐字段照搬**现状 `memory-service/src/client.ts:217-249` 策略（Metadata waitForReady、10s deadline、pre-call `probeChannel`、failure 后 channel 状态日志），仅增加可选 `deadlineMs`；`probeChannel`（`client.ts:458-473`）随迁为基建内部函数。
- teardown：`ctx.effect` 注册清理，组合 dispose 时关闭全部缓存 channel（承接现状 host row `client.close()` 语义，`memory-service/src/index.ts:35-36`）。

**Rationale**: 调用辅助与探测策略已在 Memory/Wiki 两 client 生产验证，照搬零语义漂移；canonical 解析进基建使 config 只携带稳定逻辑身份，adapter/宿主无需绝对路径知识。

**Alternatives considered**: (a) 各 adapter 自解析 canonical path——布局知识散布（用户裁定否决）；(b) `callUnary` 绑定到返回 client 上——要求 T 为基建包装类型，破坏结构化调用面直接可用，弃。

## D3. 连接策略迁移（含 why 注释清单）

**Decision**: 现状 `client.ts` 策略常量与构建函数整体迁入基建，**逐条带 why 注释**（spec FR-004）：

| 策略 | 迁移自 | why（注释内容） |
|---|---|---|
| `ROUND_ROBIN_SERVICE_CONFIG`（round_robin 覆盖 pick_first） | `client.ts:137-143` | 滚动升级期间多实例路由 |
| 不做 app 级 keepalive | `client.ts:145-152` | 对齐 grpc-go `ClientDefault`；unary server 默认 enforcement 会 GOAWAY 多余 ping |
| 重连退避 1s/15s | `client.ts:156-159` | 既有生产参数 |
| TLS：`/etc/tls/ca.crt` 存在则 `createSsl` + `TLS_SERVER_NAME` override，否则 insecure | `client.ts:120,128-135,162-172` | 部署统一 mTLS 布局 |

**Rationale**: 部署级统一量属基建；宪章原则 I 要求 why 随代码迁移。

## D4. 组装机制（用户裁定 2026-09-30，四轮收敛终态）

**Decision**: **依赖声明（扁平 config.plugins）× 提供方挂载（ctx.plugin + provide）× 编排消费（saolei-loop 代码）**——cordis 原生 DI 惯用法，不依赖官方 preset registry：

- **cordis.yml**：saolei-loop 行 config 增 `plugins`（**扁平列表，无角色分组**）——纯依赖声明："saolei-loop 声明有哪些依赖，这些插件实现依赖并挂载，saolei-loop 拿着依赖再去组装和编排"（用户裁定原话要点）。角色分配（player→saolei、planner→memory）是编排逻辑，归 saolei-loop 代码——**MUST NOT 下放进配置**（依赖注入不需要按 role 拆分）。
- **saolei-loop apply（依赖装配，fail-loud）**：按裸包名动态解析 config.plugins 行 → `await ctx.plugin(entry, row.config)`（config 经插件 Config schema 校验，失败 reject → 组合启动失败）→ `reflect.get("saoleiTools")` / `reflect.get("memoryMount")` 读提供面（缺 → throw）。子插件 fiber 随 saolei-loop 生命周期 dispose。
- **createMember setup（编排）**：player 分支注册 saoleiTools 静态单元（tools + sections → agentCtx）→ 既有 mountPlayerRuntime；planner 分支 `memoryMount.build(scope)`（scope 显式传参，编排层既有 memoryScope 流——无 isolate 属性穿越约定）→ faces→tools→sections 注册 → `await unit.ready()`（fail-loud 预取 → setup throw → 回滚）。注册序固定 faces → tools → sections → ready。
- **`loadPlannerMemory` dep 与私有方法移除**（`orchestrator.ts:262-269,828-839`）——预取并入编排对 `ready()` 的 await。

**Rationale（四轮否决谱系）**: 第一轮副作用回调（宿主直接修改编排域）→ 否决；第二轮 bundle provider seam（session.ts 闭包提供——机制自创、部署知识进代码）→ 否决；第三轮 role 分组插件列表 + 官方 agentPresets.mount（把角色编排逻辑泄漏进配置；且官方 registry 语义为 agent-scope preset 挂载，非组合级依赖注入的原生形态）→ 用户澄清目标后否决 role 分组与 mount 依赖。终态四要素：扁平声明（配置只管有什么）+ 插件提供（框架原生 provide/inject）+ 编排消费（分配权威在代码）+ 显式传参（scope 无隐式通道）。组合可见性：提供方插件在 cordis.yml 声明式可见（收束 survey §8 担忧）。

**Alternatives considered**: (a) 回调注入（第一轮）；(b) MemberCapabilities provider（第二轮）；(c) role 分组 + 官方 mount（第三轮——实现可用但非架构目标；如实现选用 registry 挂载行，其 async-await/audit 语义见 D13 备注）；(d) 宿主顶层平铺行 + saolei-loop 静态 inject——依赖声明散于宿主组合、部署 config 与消费方分离，不如嵌套自包含。

## D5. 部署知识与 client 构造归属（用户裁定 2026-09-30）

**Decision**: **client 构造在 adapter（组合级提供方）apply 内一次性完成，连接参数进插件 config**：

```yaml
# projects/game/agent_v2/cordis.yml（终态节选，扁平列表见 D4）
- id: saolei-loop
  config:
    plugins:
      - name: '@dominion/dsh-saolei'
      - name: '@dominion/dsh-saolei-memory'
        config:
          target: 'dominion:///game/memory:50051'
          protoPath: 'projects/game/game.proto'   # canonical import path（D2）
```

- 分层：**scope 派生**是 team 编排语义（orchestrator 既有 memoryScope 流，显式传参给 `build(scope)`）；**target/protoPath/client 构造**是部署拓扑知识，落在 adapter 的 config 与 apply（组合级构造一次、channel 共享、per-member 只建 face）；部署决策归部署数据（cordis.yml），session.ts 无组装代码。
- adapter 构造 client 的泛型 T = 公共库结构化调用面类型；生成接口（agent_v2_types）为可选兼容性校验（决策 ⑥ 精确化，spec Clarifications）。

**Rationale**: 用户裁定原话要点——"相比要通过回调来注入这些信息，可能放到配置里会更好"；组合级构造使 channel/client 天然单例（每服务一条连接的共享目标由基建缓存 + 组合级构造双重保证）。

**Alternatives considered**: (a) 宿主 session.ts 构造（第二轮）——部署知识进代码，随回调方案否决；(b) per-member 构造——client/channel 重复创建（grpc-js channel 泄漏风险，survey R1），否决；(c) 公共库自持 config——库依赖场景/部署，否决。

## D6. memory 公共库与 adapter、saolei 插件形态（用户裁定 2026-09-30：干脆库化）

**Decision**:

**(a) `@dominion/dsh-memory` 退化为公共 JS 库**：迁至 `common/js/memory/`，更名 **`@dominion/common-js-memory`**（公共库惯例：`common/js/<域>` → `@dominion/common-js-*`，resolver/logs/mongo 等先例；`dsh-*` 前缀保留给 cordis 插件）。无 name/inject/apply 导出。导出面（完整契约见 [contracts/memory-library.md](contracts/memory-library.md)）：

```ts
/** 原子能力单元工厂：纯函数，产出整单元（不可部分消费） */
buildMemoryCapabilities(deps: {
  scope: MemoryScope;                 // {template, preset}
  memoryClient: MemoryServiceCalls;   // 结构化调用面（D5）
  wikiClient: WikiServiceCalls;
}): MemoryCapabilities;               // { faces:[{key:"memory", value:face}], tools×4,
                                      //   sections×3, ready: () => face.prefetch() }

/** 能力原语（无副作用、无组装流程） */
createMemoryStore(memoryClient, wikiClient): MemoryStore;
createMemoryFace(store, scope): MemoryFace;      // {prefetch, applyCall, wikiSearch, wikiGet, wikiWrite, snapshotText}
renderMemorySnapshot(entries): string;
memorySnapshotSection(text): PromptSection;      // PromptSection 来自 @deepseek-ai/dsh-system-prompt（type-only）
```

- 领域语义（`operations.ts` 478 / `wiki-operations.ts` 379 / `snapshot.ts` 66 / `errors.ts` 10 行）**等价迁入**（文件级搬运，零逻辑改动）；工具定义 schema/文案/结果文本化不变，execute 经 `exec.agent.ctx.memory` 解析（不闭包绑定）；section 名与 order 不变（`memory:guidance` 110 / `wiki:guidance` 120 / `memory:snapshot` 200）——prompt 输出零变化。
- isolate label `memory`（弃 `plannerMemory` 场景词，spec FR-013/SC-002）；face 即绑定 scope（现状 `service.ts:132-158` scope 解析 Maps 消失）。

**(b) 薄 adapter `@dominion/dsh-saolei-memory`**（`common/js/dsh-plugins/saolei-memory/`，新包）：cordis 插件，`inject: ["grpc"]`，config schema `{ target, protoPath, label? }`；apply：一次性构造 client（`ctx.grpc.client<MemoryServiceCalls>` ×2，config 驱动、组合级共享 channel）→ `ctx.provide("memoryMount", { build(scope) => buildMemoryCapabilities({scope, ...clients}) })`。**scope 经 `build(scope)` 显式传参**（编排代码传入，无属性穿越约定）；per-member 只建 face。adapter 依赖公共库与 grpc 面，不依赖 saolei-loop。

**(c) `@dominion/dsh-saolei` 轻度改造为静态单元提供方**：挂载位置从 agent scope（模板行）变为组合级（saolei-loop config.plugins 子插件）——组合级 apply 内 `ctx.tools.register` 会全局注册（全员可见，泄漏），故改为 `ctx.provide("saoleiTools", { tools: [三工具], sections: [saolei:guidance, saolei:game] })`；工具定义/schema/文案与 section 名/order/文本零变化；execute 的 `exec.agent.ctx.saoleiGame` 解析不变；`saoleiGame` 运行时挂载仍属 `createAgentGameRuntime`（`game/runtime.ts:430-440`）。

**Rationale（用户裁定原话要点）**: "如果退化的话就干脆点，不要保留插件的形态，改成一个公共的 js 库"——adapter 承接全部插件职责后，dsh-memory 保留插件形态只剩死代码。自包含能力（saolei）保持插件形态（改为 provide 提供方——组合级挂载下全局注册会泄漏，apply 内注册必须改为 provide）；组装依赖能力（memory）= 库 + adapter——形态差异即即插即用判据（D9）的自然延伸。

**Alternatives considered**: (a) 库并入 adapter 单包——memory 领域语义被 saolei 场景包包裹，违反领域无关（survey ⑨）；(b) dsh-memory 保持插件形态 + config——插件形态名不副实（无独立挂载价值），用户否决；(c) 库留在 dsh-plugins 目录——目录语义误导；(d) dsh-saolei 零改动（round-3 方案）——组合级挂载下其 apply 全局注册工具会泄漏给全员，必须改为 provide 提供方（round-4 连带后果）。

## D7. 工具面缺面 fail-loud 错误（行为差异 ①）

**Decision**: 错误文案对齐 saolei 既有风格（`saolei/src/index.ts:100`），四工具同型：

```
memory tools require the agent-scoped "memory" service; agent "${exec.agent.id}" has none
(the team-layer materialization setup registers it from the memory capability mount)
```

契约化于 [contracts/memory-library.md](contracts/memory-library.md) §缺面语义。

## D8. templateRules 移除面（精确代码清单）

**Decision**: 移除以下全部（spec FR-022）：

| 位置 | 内容 |
|---|---|
| `preset-authoring/src/index.ts:69` | Config 类型 `templateRules?` 字段 |
| `preset-authoring/src/index.ts:99-106` | Config schemastery schema `templateRules` 项 |
| `preset-authoring/src/index.ts:26-31,36-37` | `validateTemplateRows` 的 import 与再导出、`TemplateRowRules` 类型的 import 与再导出 |
| `preset-authoring/src/index.ts:194,258` | deps 注入与捕获 |
| `preset-authoring/src/index.ts:383-386` | create 时校验调用 |
| `preset-authoring/src/index.ts:570` | `templateRules: config.templateRules` 向 `createPresetAuthoring` 的透传 |
| `preset-authoring/src/derive.ts:82-87` | `TemplateRowRules` 类型 |
| `preset-authoring/src/derive.ts:157-180` | `validateTemplateRows` 校验器 |
| `projects/game/agent_v2/cordis.yml:156-170` | 规则数据与注释 |
| `projects/game/agent_v2/cordis.yml:136-139` | `templates` 快照注释（语义变更后终态化，非模板快照本身） |
| `preset-authoring/src/index.test.ts:571-605`、`preset-authoring/src/derive.test.ts:109-159`、`agent_v2/src/dsh.test.ts:740-766` | 相关 fixture/断言 |

**Rationale**: 校验对象是部署自控常量，无运行时灵活性收益；场景 wiring 归场景代码（saolei-loop 编排）与场景依赖声明（config.plugins 扁平列表），部署数据只承载部署决策（survey 决策 ⑭）。

## D9. templates.{}.plugins 契约与 registry 瘦身边界

**Decision**:

- `templates.{}.plugins` 语义 = **即插即用自包含插件列表**（spec FR-023）：自包含 = 无 per-agent 状态、无 scope、无物化期动作；挂载经既有 registry/compose（deriveRows clone + persona patch 机制不变，`derive.ts:94-146`）；无校验、无额外语义。当前内容 = persona 行。分界判据（是否需要上层组装）契约化于 [contracts/preset-templates.md](contracts/preset-templates.md)——需要上层组装的能力经 **saolei-loop 能力依赖插件列表**（扁平声明，`ctx.plugin` 挂载 + `ctx.provide` 提供实现，由编排代码消费）。
- registry 瘦身保留边界（survey 决策 ⑮）：store CRUD / role 分池与模板选择 / persona-only 编辑 / fail-loud resolve / 修订保留 / restore / derive **全部不变**；对成员生效面的贡献 = persona 挂载（composed.setup → registry.mount）+ `meta.agentPreset` 身份；provider/model 经 `agentOptions` 直达（`orchestrator.ts:745-755`）不变。
- 模板 description 文案改写；角色默认 persona 行保留（纯配置项）。

**Alternatives considered**: 模板快照迁出 cordis.yml——非本特性诉求，弃。

## D10. 行为差异三处契约化（② ③）

**Decision**（① 见 D7）:

- **② section 注册点迁移**：memory:guidance/wiki:guidance/memory:snapshot 从"preset 行挂载（registry.mount 期 row apply）"迁移为 **planner 成员 setup 编排注册**（saolei-loop `createMember` setup 内 `agentCtx.systemPrompt.section`）；saolei:guidance/saolei:game 从模板行 apply 迁移为 **player 成员 setup 编排注册**（saolei-loop 编排代码同一处消费 saoleiTools 单元）。名/order/文本零变化 → prompt 组装输出零变化。
- **③ session.ts planner memory seam 形态**：`loadPlannerMemoryBinding()`（`session.ts:860-874`）与 orchestrator `loadPlannerMemory` dep（`orchestrator.ts:262-269,828-839`）消失；`ctx.get("plannerMemory")` 面引用清零；session.ts 无新增组装代码（memoryScope 传递流不变）。

## D11. 迁移分步与验收门禁

**Decision**: 四步，每步 `bazel build //...` + 相关 `bazel test //...` 通过（宪章原则 IV 门禁）：

| 步 | 内容 | 门禁 |
|---|---|---|
| **P1 基建** | grpc-runtime 包落地（D1–D3：client/callUnary/缓存/策略/canonical 解析/teardown/otel 顺序）+ 单测 + cordis.yml 加行（无消费方，行为零变化） | build + 新增单测 |
| **P2 库与提供方（并行准备）** | `common/js/memory/` 公共库（领域语义等价迁入 + 单元工厂/原语导出 + 工具改 exec.agent.ctx.memory）+ `common/js/dsh-plugins/saolei-memory/` adapter（inject grpc/config/apply + provide memoryMount 工厂）+ dsh-saolei 改造为 provide 提供方（saoleiTools 静态单元）+ 各自单测。memory-service 与 dsh-memory 旧包仍在（并行可构建） | build + 新单测全绿 |
| **P3 切换（原子）** | saolei-loop：config.plugins schema + apply 依赖装配（ctx.plugin 挂载 + reflect.get fail-loud）+ setup 编排消费序 + loadPlannerMemory 移除；cordis.yml 终态（grpc-runtime 行已有、memory-service 行删除、模板工具行与 templateRules 删除、saolei-loop config 增扁平 plugins 列表）；旧 `dsh-plugins/memory/` 与 `memory-service/` 两包删除；preset-authoring templateRules 移除（D8）；dsh.test.ts 断言更新。**必须一步完成**：模板行与编排注册并存会双重注册 | build + 全量单测 + **四 suite 大型测试实际执行全过**（宪章 VI） |
| **P4 终态** | 契约修订（059/070/071，见 [contracts/preset-templates.md](contracts/preset-templates.md) §修订清单）+ README/注释/模板文案终态化 + 引用清零复查（SC-002/SC-003） | review + 引用检索零命中 |

**单测重组映射**：memory-service `operations.test`(629)/`wiki-operations.test`(500) 文件级迁入公共库；`client.test`(331)/`wiki-client.test`(412) 拆分——策略部分归基建测试、页游走/解析部分归公共库适配层测试；`service.test`(483) 重写为 face/单元工厂测试（scope 绑定语义改 face 绑定）；新增 adapter 挂载测试（scope 读取、client 构造、整单元注册、预取失败 reject）与 saolei-loop 角色挂载测试（声明注册、setup 挂载序、rollback 传播）。preset-authoring templateRules fixture 删除；agent_v2 dsh.test 断言终态化。

**Alternatives considered**: P3 拆两步（先角色挂载、后删模板行）——中间态工具双重注册，不可行；P2/P3 顺序对调（先删旧再建新）——违反每步可构建约束。

## D12. 其他协议同构扩展位

**Decision**: 不写代码脚手架；在 [contracts/grpc-runtime.md](contracts/grpc-runtime.md) 注记扩展位约定：未来 HTTP/Mongo 等协议基建化遵循同模式——组合级基建行 + `ctx.<proto>` 服务面 + config 携带连接参数；命名在各自特性定稿。

## D13. 机制依据与实现期复核清单（cordis 原生 API）

**Decision**: 终态机制的依据是 cordis 4.0.4 原生 API（已核验 `lib/types/registry.d.ts` / `reflect.d.ts`），实现期与 cordis 版本升级时复核：

1. **`ctx.plugin(plugin, config?)`**（registry.d.ts:120,198）：运行时挂载插件（function/class/{apply} 对象）；config 经插件 Config schema 校验；返回 fiber，"awaiting it settles once loading finished (rejecting on config or startup errors)"——**挂载失败 = 组合启动 fail-loud**。子插件 fiber 随挂载方生命周期 dispose。
2. **`ctx.provide(name, value)`**（reflect.d.ts:41）/ **`reflect.get(name)`**（reflect.d.ts:7-12：无 inject 要求读取，未提供返回 undefined）——saolei-loop apply 挂载完成后读取提供面，undefined 即 throw（fail-loud）。
3. **裸包名解析**：config.plugins 行按裸说明符动态 import（Node ESM exports map 解析——cordis.yml invariant 行注释既有机制："Loader 按裸说明符动态 import"）；实现复用 loader 同型路径或直接 `await import(name)`。
4. **回滚传播**：编排 setup 内 `await unit.ready()` reject → `CreateAgentOptions` setup throw → 物化回滚（框架既有语义，无需额外机制）。
5. **（备注）官方 registry 路径**：若实现选择经 `ctx.agentPresets` 挂载行（第三轮方案，非终态），其 `mountPreset` 的 async-apply await（`fiber.await()`/`tree.await()`，dsh-agent-preset-registry@0.1.7 lib/index.js:262-285）与 `leakedServices` isolate-realm 审计（:271-273）语义已核验可用。

---

## D14. 依赖方式边界：正向依赖 vs 配置式依赖注入（2026-09-30 用户提出，裁定为设计原则）

**Decision**: 判据是**"依赖的对象是什么 + 归属"**，而非"是否解耦"（正向 inject 本身即面解耦）：

- **正向依赖**（inject/ctx.get，宿主级行提供）：依赖对象是**服务**——消费方代码直接调用的 API（调用形态、代码期闭集契约、组合级共享、provider config 是其自身部署关注）。例：desktopBridge（saolei-loop 与 agent_v2 宿主双消费的共享资源）、ctx.agents、ctx.grpc（基建）。
- **配置式依赖注入**（config.plugins 子插件）：依赖对象是**能力单元**——消费方装配进自己域的组装材料（装配形态、整单元进成员域、provider 携带消费方场景的部署 config、场景私有随消费方部署消亡、列表加项=能力集扩展）。例：saoleiTools、memoryMount。
- **取舍规则**：当服务用 → 正向；当能力单元装配 → 配置式；不分时共享 → 正向、场景私有+config 内聚 → 配置式；**迁移触发**为既有正向依赖演化出"能力单元 + 场景部署 config + 专属"三特征——不为对称而对称。
- **反证**：desktop-bridge 改配置式 = 错误（共享资源，子插件化导致重复部署或宿主依赖 saolei-loop 子树的倒挂）；memory 用 inject 形态 = 次优（adapter 行散于宿主清单，失去场景部署单元内聚与能力集声明语义）。

**Rationale**: 四轮收敛的起因虽是"saolei-loop 与 memory 不直接依赖"，但解耦在两种形态下均可达成（face 契约）；真正把 memory 推向配置式的是：依赖对象为能力单元（非调用 API）、provider 携带场景部署 config（target/protoPath 属 saolei 部署单元）、场景私有（saolei-memory 胶水离开 saolei-loop 无意义）。边界规则契约化于 [contracts/team-assembly.md](contracts/team-assembly.md) §7。

**Alternatives considered**: (a) "解耦与否"作判据——两种形态均为面解耦，不构成区分度；(b) 全部统一为配置式（含 desktop-bridge）——共享资源子插件化的倒挂问题，否决；(c) 全部统一为正向（memory adapter 作宿主行）——场景部署内聚与能力集扩展语义丢失，round-2/3 已否决其变体。

## 参考文档索引（供 tasks.md 引用）

- 仓库内：[survey/dsh-grpc-runtime-client.md](../../../survey/dsh-grpc-runtime-client.md)（决策 ①–⑮ 与 R1–R6 机制事实）、[survey/deepseek-harness-memory-plugin.md](../../../survey/deepseek-harness-memory-plugin.md)（memory 物化 setup 时序）、[survey/mcp-ecosystem-trends.md](../../../survey/mcp-ecosystem-trends.md)（R5 跨框架对照）、[survey/planner-memory-and-agent-communication.md](../../../survey/planner-memory-and-agent-communication.md)（快照冻结哲学）、`specs/071-memory-preset-scope/`（preset 作用域与快照契约）、`specs/070-dsh-upgrade/contracts/`（composition-manifest / preset-model-mapping）、`specs/059-agent-v2-team-mode/contracts/`（preset-api / dsh-plugins）、`tools/release/deploy/README.md` §runtime_protos（canonical protoPath 部署标准）、`common/js/dsh-plugins/preset-authoring/src/index.ts`（官方 registry 面消费先例）。
- 仓库外：grpc-js channel 共享维护者模式（https://github.com/grpc/grpc-node/issues/2214）、one client per service per process（https://github.com/grpc/grpc-node/issues/2896）、隐式连接共享要求相同 credentials/options（https://github.com/grpc/grpc-node/issues/158）。
