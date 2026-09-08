# Research: 058 dsh preset roster demo

> **Phase 0 研究产物**（`/speckit.plan`）：消解 spec 与 `discussion-2026-09-08.md` §8 全部待定项的实现级决策。
> **上游**：`specs/058-dsh-preset-roster-demo/spec.md`（需求）、`specs/058-dsh-preset-roster-demo/discussion-2026-09-08.md`（架构讨论存档：机制事实锚点 §4、架构设计 §6、验证矩阵 §7）。
> **日期**：2026-09-08

---

## R1 依赖版本线：roster 与 persona 均有 0.1.1-rc.2 同线版本

**Decision**: catalog 增补 `@deepseek-ai/dsh-agent-presets: 0.1.1-rc.2` 与 `@deepseek-ai/dsh-persona: 0.1.1-rc.2`，与仓库现有 dsh 线（`pnpm-workspace.yaml` catalog）精确同线。

**Rationale**: registry 核实（https://registry.npmjs.org/@deepseek-ai/dsh-persona）：dsh-persona 存在 `0.1.1-rc.2`（2026-08-21 发布，peerDeps `dsh-system-prompt ^0.1.1-rc.2`——同线）；roster 包本地已物化 0.1.1-rc.2（discussion §4.1 信息源）。npm `latest` dist-tag（0.0.1-rc.1）不可信，与 demo README 已知限制一致。

**Alternatives**: 升级整条线到 0.1.2-rc（dsh-persona 已有 0.1.2-rc.1）——被拒：0.x-rc 破坏性变更风险，升级以 lockfile PR 整体进行，不与本验证 feature 混合。

## R2 `installSelection` 不引入 demo

**Decision**: demo 的会话物化 setup 只做 preset mount（`composeAgent` 的核心分支），不移植 apiproxy 的 `installSelection`。

**Rationale**: 源码核实（`node_modules/.pnpm/@deepseek-ai+dsh-host-apiproxy@0.1.1-rc.2_*/.../lib/index.js:1717-1722`）：`installSelection(agentCtx)` → `selectionFor(agent)` → `installModelSelection`——是 web 宿主的**模型选择状态面**（WebUI 的模型切换 UI 支撑），与 preset 机制无关。demo 无此 UI 面。

## R3 YAML patch：js-yaml 结构化 round-trip + 模板自控约定

**Decision**: copy-then-patch 的 patch 步骤用 **js-yaml load → 定位 persona 行 → 改 `config.text` → dump 写回**（js-yaml 已在 catalog：`^5.2.3`）。模板约定（部署数据自控）：组合文件无注释依赖、无 `!!js` 表达式（preset 组合不需要动态 env）、有且仅有一行 `name: '@deepseek-ai/dsh-persona'` 的行。

**Rationale**: 文本级定点替换依赖 persona 行的 YAML 字符串样式（plain/quoted/block scalar），脆弱；结构化 round-trip 的代价（注释丢失、格式重排）在模板自控约定下不存在。展示名分流：**create 经 roster `copy(from, id, name?)` 的第三参写入副本 `preset.yml`**（roster README "Authoring" 节：copy 重写 preset.yml 保留 description、丢 name/order——`name` 参数即我们传入的展示名）；**update displayName 时插件自写 `preset.yml`**（两字段 YAML，直接写文件；preset.yml 不在 stamp 键内，不影响 generation）。

**Alternatives**: 文本级定点替换——被拒（字符串样式脆弱）；模板即代码生成（选项 A 回潮）——被拒（模板必须是部署数据，C1 决策）。

## R4 CreateConversation 幂等/重建语义

**Decision**: 同 conversation 资源再次 CreateConversation——**同 preset 幂等 no-op**（返回现有视图）；**异 preset 重建**：dispose 旧 agent（在途 round 按 047 既有链路失败返回）+ 按新 preset 新建。preset 字段可选，缺省走 roster `default`。

**Rationale**: 对齐 agent_v2 UpdateAgent 的"Update 即刷新"语义（`projects/game/agent_v2/src/session.ts` materialize）；demo 会话为内存态、无历史回填，重建成本为零。

**Alternatives**: apiproxy 拒绝式（`assertPresetUnchanged`，同 id 异 preset 抛 AgentPresetConflict）——被拒：那是"长驻会话 + 断线重连"语境的保护（reconnect/resume 不得换组合）；demo 是显式 create-or-update 面（AIP-134 `allow_missing`），刷新是正确语义。记录对照供 saolei 设计时参考：**会话采用式（adopt）用拒绝式，资源更新式（update）用刷新式**。

## R5 不加 ListTemplates RPC

**Decision**: PresetService 只管创作副本（store 记录）；模板不经 API 列出。CreateConversation/CreatePreset 引用未知 id 时，错误透传 roster `resolve()` 的失败信息（roster README：resolve 失败 "Throws naming the available ids"——可用集合自然呈现）。preset 字段接受模板 id 或副本 id（两者都能被 roster resolve）。

**Rationale**: YAGNI/047 minimal 风格；可用集合信息已由 roster 错误面携带；模板是部署资产，测试用已知 id。

**Alternatives**: ListTemplates RPC 或 ListPresets 标记 template 字段——被拒（增加面无消费者）。

## R6 端到端组合断言：fake-llm 增加 `system_keywords` 匹配条件

**Decision**: fake-llm 模板增加可选 `system_keywords` 匹配条件（system 消息关键词，语义与既有 `history_keywords` 同型：every keyword must hit）——命中条件并入现有多轮条件模板判定框架。persona 与工具 guidance 均进 system prompt，端到端断言统一走 system 关键词。工具 schema（请求 `tools` 数组）的断言走 agent 侧单测（`llm` 请求面事件或 tool registry scope 查询）。

**Rationale**: 核实 fake-llm 匹配面仅覆盖 `messages[]`（`specs/047-dsh-chat-demo/contracts/fake-llm-templates.md` §3：U/history/turn，无 system）——persona 断言无法借既有条件；`system_keywords` 是 047 "最小扩展"先例（history_keywords/min_turn）的同型延续，比 echo 模板更贴合既有机制、断言更精确。

**Alternatives**: echo 模板（回显请求 system/tools 摘要）——被拒（输出非确定性断言面、改动更大）；仅单测承载——被拒（V1-1/V2-3 需要端到端证明组合真的到达模型请求，spec SC-001 要求覆盖）。

## R7 命名与包布局落定

**Decision**:
- 扩展插件：`@dominion/dsh-preset-authoring`，目录 `common/js/dsh-plugins/preset-authoring`，ctx 服务 key `presetAuthoring`，inject `["agentPresets"]`。
- demo 工具插件：`@dominion/dsh-demo-echo`，目录 `experimental/dsh/demo/agent-plugins/demo-echo`；注册 model-facing 工具 `demo_echo`（入参 `{text}`，出参确定性格式）+ 同 `apply()` 内注册配套 guidance prompt section。
- workspace packages 增补条目：`experimental/dsh/demo/agent-plugins/*`。

**Rationale**: 通用基座进 `common/js/dsh-plugins/`（D3 决策，与 llm-glm 同级）；demo 内容不进 common（D7 决策）；目录名与包名遵循既有 kebab-case 惯例。

## R8 preset 行包解析：宿主依赖与 catalog 增补

**Decision**: demo agent（`experimental/dsh/demo/agent/package.json`）新增依赖：`@deepseek-ai/dsh-agent-presets`、`@deepseek-ai/dsh-persona`、`@dominion/dsh-demo-echo`（workspace:*）、`@dominion/dsh-preset-authoring`（workspace:*）、`js-yaml`（插件包声明）。前三个中 dsh-persona 与 demo-echo **不写进 cordis.yml**——它们是 preset 组合行引用的包，按 roster 包解析规则从 host 组合 base（demo agent 的 node_modules）解析（roster README "How a preset's rows resolve"）。

**Rationale**: roster 裸包名解析规则：preset 内 `@deepseek-ai/dsh-persona` 与 `@dominion/dsh-demo-echo` 行必须能从 host 能 resolve 的位置导入；js-yaml 归插件包依赖。

## R9 roots 的部署形态：模板随镜像、可写区 emptyDir、路径经 env 注入

**Decision**: 模板 root = demo agent 镜像内数据目录（`experimental/dsh/demo/agent/presets-templates/`，`artifact_pkg_js` data 分发，两份模板：`demo-standard` 仅 persona 行、`demo-tools` persona 行 + demo-echo 工具行）；可写 root = 容器 `emptyDir` 卷；两路径经环境变量（如 `PRESET_TEMPLATES_ROOT`/`PRESET_WRITABLE_ROOT`）注入，cordis.yml 的 `!!js process.env.*` 表达式读取（与既有 `FAKE_LLM_BASE_URL` 同模式）。可写 root 是 roster roots 中**第一个也是唯一 user-trust root**（`remove()`/`copy()` 的目标）。

**Rationale**: roster README 未承诺 roots path 支持相对路径（仅 `~` 展开）——env 注入绝对路径最稳，且与 demo 既有 env 注入模式一致；emptyDir 与"内存态 preset、重启丢失"的已知限制一致（模板不受影响）。

**Alternatives**: 文件持久化卷（hostPath/PVC）——被拒（D5 决策：内存语义，重启丢失接受）。

## R10 插件服务接口面（service 对接契约的核心）

**Decision**: `ctx.presetAuthoring` 服务接口（完整契约见 `specs/058-dsh-preset-roster-demo/contracts/preset-authoring-plugin.md`）：

```text
compose(presetId?)        → { agentPreset: string, setup(agentCtx): Promise<void> }
create(input)             → AuthoredPreset 视图
get(id) / list()          → 视图（store 记录）
update(id, patch)         → 视图（重物化文件）
remove(id)                → void（roster remove + store 删）
```

service 层（server.ts/session.ts）只消费：CreateConversation → `compose(presetId)` → `ctx.agents.create({meta: {agentPreset}, setup})`；PresetService handlers → CRUD。service 全程零 roster API、零 fs。

**Rationale**: `compose()` 复刻 apiproxy `composeAgent()` 的形状（resolve 提前 + setup 内 mount + id 返还进 meta）——把"mount 是 roster API"这一事实也封装进插件，service 边界彻底干净（V4-2 审计项）。

**Alternatives**: service 直接 inject `["agentPresets"]` 自行 mount——被拒（违反 FR-005 边界；mount 属 preset 领域机制）。

## R12 基座对齐 agent_v2：组合清单从 spine 打包改为直组

**Decision**: demo agent 的组合与依赖结构改造为 agent_v2 同型（2026-09-08 用户追加约束"贴近 agent_v2"）：

1. **移除 `agent-spine-demo` 行**，dsh 核心插件**逐行直组**。改造后 cordis.yml（`!!js` FAKE_LLM 注入不变）：
   - 基线行（经 `//third_party/dsh/core:runtime_pkg` 物化，**不进服务 package.json**，agent_v2 cordis.yml 注释同款约定）：`timer`、`invariants`、`system-prompt`（`includeHarnessIdentity/includeRuntimeContext: false` 从 047 的 spine config 移到本行 config；deployment persona 配默认文案——被 preset 的 persona 行遮蔽，遮蔽语义留一次单测断言）。
   - 服务声明行：`llm`、`session`、`tools`、`agents`、**`agent-loop`（新增声明——047 经 spine 闭包传递，直组后显式化；demo 无自研 loop，官方 loop 提供 factory+驱动）**、`llm-deepseek`（保留）。
   - 本 feature 新增行：`agent-presets`（roster）、`preset-authoring`。
   - **不挂** agent_v2 的三个 `/invariant` subpath 行与 `llm-retry`：前者是"自研 loop 的高风险面防护"（specs/051 contracts/saolei-plugins.md，demo 用官方 loop 无此风险面）；后者是可靠性件（fake-llm 场景无重试需求）。记录差异理由，不盲抄。
2. **package.json 增删**：删 `@deepseek-ai/dsh-agent-spine-demo`；增 `dsh-agent-loop`、`dsh-session`、`dsh-tools`（直组行）、`dsh-agent-presets`、`dsh-persona`（R8：preset 行的裸包名从 host base 解析）、`@dominion/dsh-demo-echo`、`@dominion/dsh-preset-authoring`（workspace:*）。保留 dsh-agent/dsh-app-boot/dsh-llm/dsh-llm-deepseek。
3. **catalog 增补**：`@deepseek-ai/dsh-agent-loop: 0.1.1-rc.2`（本地 .pnpm 已物化同线）+ R1 两项。
4. **BUILD.bazel**：`npm_deps` 随行清单增删（Loader 物理需要的行包 + preset 解析需要的 persona/demo-echo）；`runtime_deps` 基线不变（047 已挂 `//third_party/dsh/core:runtime_pkg`）。closure audit test 的 expected 集随 package.json 变化重算。

**Rationale**: 047 的 spine 是"最小 chat 链路"的打包捷径，但 spine 把 agent 平面行（tools/system-prompt 注册）黑盒进自己的层——**roster 验证需要 host/agent 两平面行边界透明**（哪些行留 host、哪些下放 preset 的判断基础），直组是官方 web-app bundle 的既有形态（`survey/deepseek-harness-preset.md` §2.3：web surface 禁用 agent 平面行改用 per-session preset）。且验证结论要迁移的目标（agent_v2/saolei）就是直组形态。

**Alternatives**: 保留 spine 仅加 roster 两行——被拒（spine 与 scope parent chain 的兼容性未经验证且不透明，验证结论无法迁移到 agent_v2 直组形态）；完整照抄 agent_v2 全部行（invariant subpaths/llm-retry）——被拒（上述差异理由，YAGNI）。

## R11 验证矩阵断言承载分配（更新 discussion §7）

| 编号 | 承载（最终） |
|---|---|
| V1-1 per-session 组合差异 | 大型测试（fake-llm `system_keywords`）+ 单测 |
| V1-2 default | 大型测试 |
| V1-3 header 记录 | 单测（session/event 或 header 断言） |
| V2-1 scope 链 | 单测（tool registry scope 视图） |
| V2-2 standing mount 共享 | 单测 |
| V2-3 工具↔guidance 一致性 | 单测（schema 集合）+ 大型测试（guidance 经 system_keywords） |
| V3-1 热创作 | 大型测试 |
| V3-2 generation 切换 | 大型测试（US2 场景 2） |
| V3-3 broken | 单测（写坏文件 → list/resolve/compose 行为） |
| V4-1 C1 闭环 | 单测（物化算法）+ 大型测试（全链路） |
| V4-2 边界审计 | review checklist |
| V4-3 删除语义 | 大型测试（US2 场景 3） |
