# 调研：dsh preset roster 机制实证（B1 直组形态）与 preset 扩展实践

> **状态**：实证完成。验证矩阵 V1-V4 全部验证点有断言承载且全部通过——证据链：单测（`bazel test //experimental/dsh/demo/... //common/js/dsh-plugins/...`）+ 大型测试完整闭环（`guitar run experimental/dsh/demo/testplan/interface_test.yaml`：部署→047 既有回归 + 058 preset 全部用例→清理，零 failed/flaky）+ 边界审计（`specs/058-dsh-preset-roster-demo/checklists/boundaries.md`）。
> **日期**：2026-09-09
> **上游调研**：`survey/deepseek-harness-team-mode.md`（§2.3 preset 挂载模型与 §3 preset 池组织——本文的纸面对照对象；其 §9.5"SDK 直组 + preset roster 组合无先例"由本实证消解）、`survey/deepseek-harness-preset.md`（preset 机制母本）。
> **载体与依据**：`specs/058-dsh-preset-roster-demo/`（spec.md FR-010/SC-005 验收锚点、discussion-2026-09-08.md §4 机制锚点与 §7 验证矩阵、research.md R1-R12 纸面结论、contracts/ 五契约、checklists/boundaries.md）；实现 `experimental/dsh/demo/` 与 `common/js/dsh-plugins/preset-authoring/`。
> **说明**：本文为实证调研（实测结论 + 实践摩擦 + 边界实践 + 迁移建议），全部结论有测试、审计或代码承载；纸面结论被实践修订处按 FR-010 给对照记录（§5），聚焦结论对照而非过程叙事。

---

## 1. 背景与验证形态

058 之前，仓库内全部 roster 结论停留在源码级纸面推断（`survey/deepseek-harness-team-mode.md` §2.3/§3、`specs/058-dsh-preset-roster-demo/discussion-2026-09-08.md` §4），无任何运行实证。058 以扩展 047 demo（`experimental/dsh/demo/`）为载体完成三重实证：roster 机制验证、C1 copy-then-patch 扩展实践、service↔插件边界实践（`specs/058-dsh-preset-roster-demo/spec.md` Motivation）。

验证形态是 **B1 嵌入直组**（grpc-js 进程内嵌 dsh、组合清单直组、无官方 web host）——与迁移目标 agent_v2（`projects/game/agent_v2/`）同型。该形态在 team-mode 调研中列为"无官方先例项"（`survey/deepseek-harness-team-mode.md` §9.5：SDK 直组 + preset roster 组合需最小 PoC 验证），本实证将其消解：**roster 行挂载直组组合 + 显式会话创建 + factory setup hook mount 的组合在 B1 嵌入形态下完整成立**（组合清单：`experimental/dsh/demo/agent/cordis.yml`；接线：`experimental/dsh/demo/agent/src/session.ts`）。

大型测试计划含 16 个用例（047 既有 6 个回归 + 058 新增 10 个，`experimental/dsh/demo/testplan/interface_test.yaml`），单测覆盖物化算法、Store、compose 接线、组合行为与 broken 语义（承载分配表：`specs/058-dsh-preset-roster-demo/research.md` R11）。

---

## 2. roster 机制实证结论（对照纸面结论）

### 2.1 验证矩阵实测总表

对照对象：`survey/deepseek-harness-team-mode.md` §2.3/§3 与 `specs/058-dsh-preset-roster-demo/discussion-2026-09-08.md` §4/§7。

| 验证点 | 纸面结论 | 实测结果 | 承载与证据 |
|---|---|---|---|
| V1-1 per-session 组合差异 | 挂载模型"N agents : 1 preset standing mount + join"，组合随会话所选 preset（team-mode §2.3） | **证实**：demo-tools 与 demo-standard 两会话的 persona 与模型可见工具目录不同；host 组合的 deployment persona 被 preset persona 行遮蔽（scope 近遮蔽远） | 大型 `TestPresetConversationComposition`（fake-llm `system_keywords`，`specs/058-dsh-preset-roster-demo/contracts/fake-llm-system-keywords.md`）+ 单测 `experimental/dsh/demo/agent/src/composition.test.ts`（V1-1 用例） |
| V1-2 default 生效 | Config `default` 必填、缺省选择走默认（discussion §4.1） | **证实**：不传 preset 的会话绑定 roster 默认 `demo-standard` | 大型 `TestPresetDefaultSelection` |
| V1-3 header 记录 | preset id 是 durable 事实：`meta.agentPreset` 记入 SessionHeader（team-mode §2.3） | **证实**：resolved id 落创建 meta | 单测 `composition.test.ts`（V1-3 用例）、`experimental/dsh/demo/agent/src/session.ts` `doCreate()` |
| V2-1 scope 链 agent→preset→global | 工具/prompt 解析链 `agent → preset → global`，近遮蔽远（team-mode §2.3） | **证实**：preset 行工具仅该 preset 成员可见，host 全局行全员可见 | 单测 `composition.test.ts`（V2-1 用例） |
| V2-2 standing mount 共享 | roster 每进程挂载一次 standing scope，同 preset 会话 join 共享一份（team-mode §2.3） | **证实**：同 preset 两会话共享一份组合注册——对象同一性断言（同 parent scope key、同 tool-definition 实例；roster single-flight mount），非仅值相等 | 单测 `composition.test.ts`（V2-2 用例，显式注释 object identity） |
| V2-3 工具↔guidance 行级一致 | 选择发生在插件行层则工具与守则天然一致；per-agent restriction 不一致（team-mode §3.6） | **证实**：`demo_echo` 工具与 guidance 同 `apply()` 注册——挂行则两者端到端都在场，不挂则同时缺席 | 契约 `specs/058-dsh-preset-roster-demo/contracts/demo-echo-plugin.md` §4 + 大型 `TestPresetConversationComposition` + 单测 `experimental/dsh/demo/agent-plugins/demo-echo/src/index.test.ts` |
| V3-1 热创作 | 发现 unmemoized：运行期写文件即时可见（discussion §4.1） | **证实**：API create 后零重启，新会话即用创作 preset | 大型 `TestPresetAuthoringLifecycle`（create 段） |
| V3-2 generation 切换 | generation 以组合文件 stamp（mtime+size）为键：新会话新组合、已加入会话保持旧组合（discussion §4.1） | **证实**：persona 更新后旧会话回复不变、新会话命中新 persona | 大型 `TestPresetAuthoringLifecycle`（update 段） |
| V3-3 broken fail-fast | broken preset：list 携带 reason 而非跳过；mount 前置拒绝（discussion §4.1） | **证实**：broken 副本 resolve 携带原因 → compose 报 `INVALID_ARGUMENT` fail-fast，mount 不被调用、无半组合会话。承载修订见 §5 对照 5 | 单测 `common/js/dsh-plugins/preset-authoring/src/index.test.ts`（V3-3 用例）、`materialize.test.ts` |

### 2.2 实测确认的接线与错误面（纸面锚点的补充结论）

- **mount 调用点归创建路径所有者**：agent-loop 不含 roster 接线，`mount` 的唯一支持调用点是 factory 的 `setup` hook（discussion §4.2 纸面）——实测确认，demo 的接线形状为 **resolve 提前 + setup 内 mount**（`ctx.presetAuthoring.compose()` 返回 `{agentPreset, setup}`，service 传入 `ctx.agents.create({meta, setup})`；`experimental/dsh/demo/agent/src/session.ts` `doCreate()`、`common/js/dsh-plugins/preset-authoring/src/index.ts` `compose()`）。resolve 提前使 resolved id 落 meta 快照、broken/未知 preset 在任何会话存在之前 fail-fast；setup 内失败回滚整个创建，不产生半组合会话。
- **错误映射的判别面**：roster 以导出错误类拒绝（`UnknownPresetError`/`InvalidPresetIdError`/`PresetExistsError`/`PresetNotWritableError`），实践采用 **`instanceof` 判别 + `cause` 链保留**，未知错误统一 `INTERNAL`——禁止 message 字符串匹配（`common/js/dsh-plugins/preset-authoring/src/materialize.ts` `mapRosterError()`）。
- **多 root 配置语义**：demo 的 roots 为信任分工（模板 system root + 可写 user root），实测确认"可写 root 必须是第一个且唯一 user root"约束成立——`copy()`（V3-1）与 `remove()`（模板拒绝、V4-3 删除语义）均落该 root；`includeUserRoot: false` 保确定性（`experimental/dsh/demo/agent/cordis.yml`、`specs/058-dsh-preset-roster-demo/contracts/composition-manifest.md` §1-§2）。分池语义（player/planner 池）机制同源（roots 有序数组），saolei 落地时按 `survey/deepseek-harness-team-mode.md` §3.2 直接采用。

---

## 3. C1 copy-then-patch 扩展实践结论

### 3.1 物化算法的实测形态

`specs/058-dsh-preset-roster-demo/contracts/preset-authoring-plugin.md` §4 的算法经实现与测试定形（`common/js/dsh-plugins/preset-authoring/src/materialize.ts`）：

```text
create:  id 语法校验 → resolve 模板（broken → INVALID_ARGUMENT，先于 copy）
         → roster copy(template, id, displayName ?? id)（展示名经第三参入副本 preset.yml）
         → js-yaml load → 定位唯一 persona 行 → 置 config.text → dump 原子写回（临时文件 + rename）
         → store 落记录；任一步失败 → roster.remove best-effort 回滚目录 → 上抛（无半物化）
update:  persona → patch 组合文件（stamp 变化 → 新 generation）
         displayName → 重写 preset.yml（不触组合文件 → 不影响 generation）
remove:  roster.remove（模板命中 system-trust 拒绝 → FAILED_PRECONDITION 透传）→ store 删
```

- **回滚的双向性**：文件步骤失败回滚目录；store 落败同样回滚目录——store 记录与物化目录同生同灭（`common/js/dsh-plugins/preset-authoring/src/index.ts` `create()`；`specs/058-dsh-preset-roster-demo/data-model.md` §2）。回滚失败不掩盖原始错误，残留目录以 broken roster 行可见（`materialize.ts` `removeCopyBestEffort()`）。
- **US2-AS4 端到端证实**：重复 id / 未知模板 / 非法 id / 空 mask / 未知 id 更新全部拒绝且无半物化残留（大型 `TestPresetCreateRejections`）。

### 3.2 实操摩擦与定形决策

1. **YAML patch 方式**：js-yaml 结构化 round-trip（load → 定位 → dump）成立，前提是模板自控约定——无注释依赖、无 `!!js` 表达式、有且仅有一行 persona 行；违约在 patch 前显式报 `INVALID_ARGUMENT`（行数断言，`materialize.ts` `patchPersona()`）。文本级定点替换被弃用的理由（字符串样式脆弱）实践中确认。两个非显然细节：`dump` 须 `lineWidth: -1` 防 persona 长文本被折行；`!!js` 禁令使 round-trip 无表达式求值损失。
2. **stamp/generation 行为**：persona 走组合文件（stamp 变化 → 新 generation）、displayName 走 `preset.yml`（不在 stamp 键内 → generation 不动）——两字段独立生效实测成立，update_mask 语义由此获得机制支撑（`specs/058-dsh-preset-roster-demo/data-model.md` §2）。
3. **展示名分流**：create 经 roster `copy()` 第三参（roster 重写副本 `preset.yml`：保留 description、以传入值定 name）；update 由插件自写 `preset.yml`。两条路径分工实测成立（`specs/058-dsh-preset-roster-demo/research.md` R3 的分流决策被实践确认）。
4. **打包面（实践补充）**：workspace 包经 `npm_deps` 打包是 no-op（link target 不含可打包文件），必须走 `runtime_deps`（`JsRuntimePackageInfo` → artifact 拷贝为 `node_modules/{pkg}/` 真实文件）；`:pkg srcs` 的 manifest 在**解析面**（bazel runfiles 虚拟 store，单测真直组 boot）必需、**打包面**不需要——两层面各自成立，详见 §5 对照 2。

### 3.3 与社区参考的印证

discussion §3 的社区收敛结论——除 DB 级版本管理需求（Dify）外，主流是"文件为 artifact/source of truth、编辑面是外挂工具"（`specs/058-dsh-preset-roster-demo/discussion-2026-09-08.md` §3）——C1 实践与之吻合：roster 的 authoring seam 是 copy-only（"no composition text crosses this seam"，组合=能力注入的安全考量），存储只存动态字段、完整组合文件只存在于模板与副本，编辑面收敛为 patch 步骤。demo 规模下未见 DB 级版本化需求，A（DB 为 source of truth）对 N≈1 场景过度的判断维持（与 `survey/deepseek-harness-team-mode.md` §3.3 N 计价结论一致）。

---

## 4. service↔插件边界最佳实践与 agent_v2 迁移建议

### 4.1 ctx 服务对接模式的实践评价

边界一句话（`specs/058-dsh-preset-roster-demo/discussion-2026-09-08.md` §6.2）经实现与审计成立：**service 拥有传输与资源语义；插件拥有 preset 领域机制；对接面 = `ctx.get("presetAuthoring")`**（与 `ctx.agents` 同型）。V4-2 审计通过（`specs/058-dsh-preset-roster-demo/checklists/boundaries.md`）：service 层 `agentPresets` 零命中、零 preset 文件访问；插件零 proto/gateway/RPC 概念。实践要点：

- **错误语义穿边界不丢失**：插件抛稳定域错误码（`PresetAuthoringError` 五码 union，`materialize.ts:37-42`），service 层 `instanceof` 一对一翻译为 gRPC status（`experimental/dsh/demo/agent/src/server.ts` `domainStatusOf()`），`cause` 链保留原始错误；非域错误统一 `INTERNAL`。与 gRPC status 同名是刻意的映射便利设计（词汇表是领域错误词汇，非传输概念——boundaries.md 判定）。
- **可测试性 seam 顺滑**：`RosterSeam`/`PresetStore`/`MaterializeFs` 构造注入（`createPresetAuthoring(ctx, deps)`），单测 `vi.fn()` doubles、无模块拦截（`specs/058-dsh-preset-roster-demo/contracts/preset-authoring-plugin.md` §5；`style/javascript.md` Mock 约定）。V3-3/V4-1 单测经 fs seam 写坏副本/断言文件内容即得，无需真实 roster。
- **模板拒删的错误路径**：remove 先经 roster（模板 id 命中 system-trust 拒绝 → `FAILED_PRECONDITION`），再做 store 删——顺序保证模板拒绝不被 store 的 NOT_FOUND 遮蔽（`index.ts` `remove()`）。

### 4.2 agent_v2 现状对照与迁移路径

**现状（对照）**：agent_v2 是"无 roster 部署"——组合由进程级 `projects/game/agent_v2/cordis.yml` 一份固定；Mongo 只存 persona 单字段（`player_prompt`，`projects/game/agent_v2/src/presets.ts` `PresetRecord`）；preset 物化逻辑内嵌 service 层（`projects/game/agent_v2/src/server.ts` 取 persona 快照，经 `AgentOptions.persona` declaration-merge 编程式传入，`projects/game/agent_v2/src/session.ts:296`）。差距：persona 单字段无法承载工具行等组合差异（无 per-session 组合能力）；物化与组合接线占据 service 层；无 scope 链、standing mount、generation 语义。

**迁移路径要点**（按依赖顺序）：

1. **组合改造原子变更**：`package.json` ⟷ `cordis.yml` ⟷ tar 物化三面受闭包审计双向一致约束（`experimental/dsh/demo/testplan/closure_audit_test.go`：基线零插件、服务声明可溯源、同名包版本唯一）——依赖增删与组合行改造必须同一变更交付，不可分步（实践结论，§5 对照 3）。行清单分层（基线行走 `runtime_pkg` 物化不进 package.json、服务声明行走 `npm_deps`、workspace 包走 `runtime_deps`）参照 `specs/058-dsh-preset-roster-demo/contracts/composition-manifest.md` §1/§4。
2. **挂 roster 行 + authoring 插件**：`agent-presets`（roots/default/includeUserRoot）+ authoring 插件可直接复用 `@dominion/dsh-preset-authoring`——其 `PresetStore` seam 与 agent_v2 既有 `PresetStoreError` 错误码同型（ALREADY_EXISTS/NOT_FOUND，`projects/game/agent_v2/src/presets.ts:40-50`），生产化在该插件同 seam 增 Mongo 实现并经 `Config.storage` 切换（契约 §3 预留）。
3. **会话物化改 compose() 接线**：`meta.agentPreset` + setup 内 mount（§2.2 形状）；persona 由 preset 组合文件的 persona 行承载（`config.text`），废弃 declaration-merge 快照注入——工具行差异由此获得 per-session 表达。
4. **幂等语义采用 R4 对照结论**：会话采用式（adopt：长驻会话 + 断线重连）用拒绝式（apiproxy `assertPresetUnchanged`，重连不得换组合）；资源更新式（update 语义）用刷新式（同 id 异 preset dispose 重建）——agent_v2 UpdateAgent 属更新式，维持刷新式（`specs/058-dsh-preset-roster-demo/research.md` R4）。
5. **物化副本的持久化决策**：deploy 平台无用户服务卷通道（§5 对照 1），demo 以容器临时可写层 + 重启丢失接受；agent_v2 生产化二选一——持久卷承载副本目录，或接受"store 为 source of truth、副本可重建"语义（副本丢失后从 store 记录重放 create，热创作路径天然支持）。
6. **边界审计形态复用**：`specs/058-dsh-preset-roster-demo/checklists/boundaries.md` 的 grep 命令面 + 判定基准（含 AS1"对 preset 而言"限定语与测试文件观察者口径）可直接作为迁移验收的 checklist 模板。

---

## 5. 被实践修订的纸面结论对照记录

（FR-010/SC-005 要求的对照形态：纸面结论出处 vs 实践终态，结论对照而非过程叙事。）

| # | 纸面结论（出处） | 实践终态 |
|---|---|---|
| 1 | 可写 root 以 `emptyDir` 卷部署（`specs/058-dsh-preset-roster-demo/research.md` R9） | deploy v3 schema 无用户服务卷通道——artifact 服务属性 `additionalProperties: false`（`tools/release/deploy/pkg/schema/deploy.schema.json`），k8s builder 为用户服务生成的卷均为平台管理的只读投影卷（`projects/infra/deploy/runtime/k8s/builder.go`）。实施为**容器临时可写层固定路径**（`PRESET_WRITABLE_ROOT=/var/lib/dsh-demo/presets`，`experimental/dsh/demo/testplan/deploy.yaml`），丢失语义等价（Pod 重建即丢）。契约已终态化（`specs/058-dsh-preset-roster-demo/contracts/composition-manifest.md` §2） |
| 2 | preset 行的 workspace 包经 `npm_deps` 进入打包闭包（R8 依赖面的隐含路径） | `npm_deps` link target 仅对 registry 包携带真实文件，workspace 包为 no-op → **`runtime_deps`（`JsRuntimePackageInfo`）通道**（`experimental/dsh/demo/agent/BUILD.bazel`）；`:pkg srcs` 的 manifest 在解析面（runfiles 虚拟 store，`composition.test.ts` 真直组 boot）必需、打包面不需要——与 `common/js/dsh-plugins/llm-glm` 先例的两层面注释调和。契约已终态化（composition-manifest.md §4） |
| 3 | Phase 1/2 的依赖增删（T003）与组合改造（T010/T011）为分立任务（`specs/058-dsh-preset-roster-demo/tasks.md`） | 依赖闭包审计使 `package.json` ⟷ `cordis.yml` ⟷ tar 物化**双向一致**，三面必须原子变更——同类改造对 agent_v2 迁移同样不可分步交付（§4.2 要点 1） |
| 4 | dsh/demo/agent 的共享 bootstrap 接入 defer（`specs/053-js-bootstrap-migration/spec.md` FR-012）；deploy 为服务工作负载无条件附加 `:38080/healthz` 探针（`specs/052-deploy-health-probe/contracts/deploy-probe.md`"未适配服务"） | 两者叠加使未接入共享 bootstrap 的本服务部署不就绪（startupProbe 持续失败）。本 feature 实践清偿：接入 `@dominion/common-js-bootstrap`（`experimental/dsh/demo/agent/src/bootstrap.ts` 两组件生命周期——composition 先启、grpc-chat 后启、逆序停止），编排器 FIFO 停止顺序保持 047 契约语义（grpc 排空 → 会话/fiber dispose，`specs/047-dsh-chat-demo/contracts/dsh-agent-service.md` §1）。迁移启示：defer 接入的服务在 052 约定下不可部署，接入是部署前置而非可选优化 |
| 5 | V3-3 由"经 preset 创建后损坏文件"的大型测试步骤承载（tasks T019 编制期的承载设想） | 大型测试进程无法触达容器可写层内文件——归**单测**（fs seam 写坏副本；`specs/058-dsh-preset-roster-demo/research.md` R11 V3-3 行、`experimental/dsh/demo/testplan/preset_test.go` 文件头 :30-36 说明）。启示：断言面必须与被测状态的物理位置一致 |
| 6 | compose 接线形状（resolve 提前 + setup 内 mount）仅是官方宿主（apiproxy `composeAgent`）的参考实现（discussion §4.2） | 实践补充确认：该形状在 **B1 嵌入直组形态**下成立且必要——resolve 提前同时承担 meta 快照与 fail-fast 前置（`session.ts` `doCreate()` 注释），非 web host 专属（§2.2） |

---

## 6. 大型测试真实执行的必要性实证

**preset_test 的裸 id 资源名 bug**：gateway 以 URL 模板拼接资源名（`{name=conversations/*}` 路由），裸会话 id 的请求 miss 路由得到 404——该缺陷在编译与单测层均不可见（单测不经过 gateway 路由面），仅在 `guitar run` 真实部署执行中暴露（`experimental/dsh/demo/testplan/helpers_test.go` `conversationName()` 与 `experimental/dsh/demo/testplan/preset_test.go:356-358` 注释记载：全资源名是请求到达 handler 的前提）。修复形态为测试 helper 强制全资源名包装。

这是 `.specify/memory/constitution.md` 原则 VI（大型测试验收 MUST 实际执行、构建检查不构成验收）的实证注脚：**构建/单测证明代码自身正确，不证明部署产物在真实集成环境下可达**——路由、部署注入（env/模板数据）与探针就绪只有闭环执行能验证。

---

## 7. 风险与限制（实践确认）

1. **进程态组合**：创作 preset 的 store 记录（内存）与物化副本（容器可写层）随进程重启/Pod 重建一并丢失，模板为镜像部署数据不受影响——已知限制（`experimental/dsh/demo/README.md` 已知限制节），生产化路径见 §4.2 要点 5。
2. **roster 自身已知限制维持**：superseded generation 不回收（每次编辑-创建循环累积 watcher）；root 扫描无 watch（每次 list 落盘 readdir）——demo 规模无感知，高频 CRUD 或大池规模需评估（`specs/058-dsh-preset-roster-demo/discussion-2026-09-08.md` §4.1）。
3. **并发 create 的已知边界**：会话注册表的 per-conversation 串行链只链住首个 pending create——3+ 并发 create 且中间发生重建时，尾部 create 可观察到已被重建删除的注册并泄漏未 dispose 的 handle；demo 的单进程低并发场景接受（`experimental/dsh/demo/agent/src/session.ts` `create()` 注释）。生产化需 per-conversation 完备互斥。
4. **0.x-rc 版本线**：dsh 全家桶 0.1.1-rc.2 同线精确 pin（dist-tag 不可信），破坏性变更风险由实验性 demo 接受（`experimental/dsh/demo/README.md` 已知限制节）。

---

## 8. 对后续设计的输入

- **saolei（team 模式）**：机制前提全部实证成立——双顶层 agent 各绑一个 preset（V1-1/V1-3）、角色差异编辑期固定于 preset 行（V2-3 行级一致）、物化零定制（`ctx.agents.create({meta, setup})` 同构调用）、分池 roots（§2.2 多 root 语义）。`survey/deepseek-harness-team-mode.md` §9 的堆叠基线可直接进入 spec，其 §9.5 无先例项 1 已消解；无先例项 2（广播压力面）与 3（buffer 恢复顺序）仍属 team 层开发时验证。
- **agent_v2 迁移**：路径要点见 §4.2；authoring 插件与 Store seam 可复用，闭包原子变更与 compose 接线是两个硬约束。
- **实践方法论**：边界审计 checklist 形态（`specs/058-dsh-preset-roster-demo/checklists/boundaries.md`：可复现 grep 命令面 + 判定基准 + 限定语 + 回改复审）可作为后续"边界可审计"类验收的模板；V3-3 承载修订（§5 对照 5）提示验证矩阵规划时需核对断言面与被测状态的物理位置。

---

## 9. 引用来源汇总

仓库内（实现与验证）：

- `experimental/dsh/demo/`（058 载体：`agent/cordis.yml` 直组清单、`agent/src/session.ts` 会话 compose 接线、`agent/src/server.ts` RPC 面与错误映射、`agent/src/bootstrap.ts` 共享 bootstrap 生命周期、`agent/src/composition.test.ts` V1/V2 组合单测、`agent/presets-templates/` 模板数据）
- `common/js/dsh-plugins/preset-authoring/src/`（`index.ts` 服务装配与 compose/create/update/remove、`materialize.ts` copy-then-patch 与错误映射、`store.ts` Store seam、配套 `.test.ts`）
- `experimental/dsh/demo/agent-plugins/demo-echo/src/`（工具+guidance 同 apply 注册）
- `experimental/dsh/demo/testplan/`（`interface_test.yaml` 用例清单、`preset_test.go`、`helpers_test.go`、`closure_audit_test.go`、`deploy.yaml` env 注入）
- `experimental/dsh/demo/agent/BUILD.bazel`（npm_deps/runtime_deps 两通道、模板 data 分发）
- 规格文档：`specs/058-dsh-preset-roster-demo/spec.md`、`tasks.md`、`discussion-2026-09-08.md`、`research.md`、`quickstart.md`、`data-model.md`、`contracts/`（composition-manifest / preset-authoring-plugin / chat-api / demo-echo-plugin / fake-llm-system-keywords）、`checklists/boundaries.md`
- 关联 spec：`specs/047-dsh-chat-demo/contracts/dsh-agent-service.md`、`specs/052-deploy-health-probe/contracts/deploy-probe.md`、`specs/053-js-bootstrap-migration/spec.md`、`projects/game/agent_v2/src/{presets.ts,server.ts,session.ts}`
- 上游调研：`survey/deepseek-harness-team-mode.md`、`survey/deepseek-harness-preset.md`

仓库外（依赖与平台）：

- `node_modules/.pnpm/@deepseek-ai+dsh-agent-presets@0.1.1-rc.2_5200ead8959daeaefdf3dd69ba905368/node_modules/@deepseek-ai/dsh-agent-presets/README.md`（roster 服务面/authoring/发现/generation 各节）
- `node_modules/.pnpm/@deepseek-ai+dsh-host-apiproxy@0.1.1-rc.2_7a1c54e2b954eca6f88bad802758761b/node_modules/@deepseek-ai/dsh-host-apiproxy/lib/index.js:1717-1765`（`composeAgent`/`assertPresetUnchanged` 官方接线）
- `experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-agent/lib/types/index.d.ts`（`CreateAgentOptions.setup`/`meta.agentPreset`）
- [npm: @deepseek-ai/dsh-persona](https://www.npmjs.com/package/@deepseek-ai/dsh-persona)（persona 行 Config：text/complete，无 file 输入——C2 不存在的机制依据）
- [registry: @deepseek-ai/dsh-persona](https://registry.npmjs.org/@deepseek-ai/dsh-persona)（0.1.1-rc.2 同线版本核对，R1）
- [AIP-133 Create methods](https://google.aip.dev/133)、[AIP-134 Update methods](https://google.aip.dev/134)、[AIP-135 Delete methods](https://google.aip.dev/135)、[AIP-193 Errors](https://google.aip.dev/193)（资源面错误语义）
