# Contract: agent_v2 场景分派（scene dispatch）

> Feature: [specs/079-sts2-template-scaffold/spec.md](../spec.md)（FR-001~FR-004）| 数据模型：[data-model.md](../data-model.md) §1/§2/§6 | 决策：[research.md](../research.md) D1/D2/D4
> 本契约定义 agent_v2（及 Go 各服务）对 `templates/{template}` 的**接受面与场景化操作语义**——泛化后的接口行为（宪法原则 III：接口优先）。saolei 列全部为现状平移（零变化）；sts2 列自 080-host-main-sub-model 起接入（终态见下）。

## §1 模板接受面

| 服务/RPC 面 | `saolei` | `sts2` | 未知模板 |
|---|---|---|---|
| SessionService（Create/Get/List/Delete） | ✅ 现状 | ✅ 同构接受（模板分区隔离） | ❌ `ErrInvalidTemplate` / `INVALID_ARGUMENT` |
| MemoryService（template×preset 作用域）/ WikiService（template 作用域） | ✅ 现状 | ✅ 同构接受（memory 仅结构验证：不校验 preset 存在性；sts2 的 preset 业务词汇自 080 起为 `main`） | ❌ 同上 |
| ProxyService（gRPC 转发；`projects/game/proxy/handler/agent.go` 的 owner 模板校验） | ✅ 现状 | ✅ 白名单扩容后同构接受（无对外 CRUD 面） | ❌ `ErrInvalidTemplate` / `INVALID_ARGUMENT` |
| AgentService 会话流（Connect/消息/历史等） | ✅ 现状 | ✅ 资源解析通过 | ❌ 解析失败 `INVALID_ARGUMENT` |
| AgentService `UpdateTeam`（team 物化） | ✅ 现状 | ✅ 单 `main` 成员（080-host-main-sub-model 开放） | ❌ 解析失败 |
| PresetService（Create/List/Get/Update/Delete） | ✅ 现状 | ✅ role `main` 词表（080 开放） | ❌ 解析失败 |

> 080-host-main-sub-model 起 sts2 已接入场景注册与最小 sts2 编排；本表为终态。saolei 列全部为 079 前的现状平移。

Go 侧唯一事实源 `projects/game/pkg/gameconst/const.go`（`knownTemplateIDs`）；TS 镜像 `projects/game/agent_v2/src/server.ts`（`KNOWN_TEMPLATES`）。gateway 路由由 proto 通配生成，不感知模板集合。

## §2 场景能力矩阵（SceneSpec 消费视图）

| 能力 | saolei | sts2（080 起） | 后续扩展（S3/S4/S6，仅示意） |
|---|---|---|---|
| team 物化 roster | `player`、`planner`（恰 2 成员） | `main`（单成员，主子形态） | 房间子代理编排动态派发，不进 face 名册 |
| preset 角色词表 | `player`、`planner` | `main` | 房间类型角色词表待场景设计裁定 |
| face 成员名（`faceMembers`） | `player`、`planner`、`announcer` | `main` | 房间子代理不进 face 名册 |
| preset 池模板映射 | player→`player`、planner→`planner` | `{main:"main"}` | 房间类型 preset 池模板（S6） |
| team 编排 | saolei-loop（`TeamOrchestrator`） | `@dominion/dsh-sts2-loop`（`Sts2Orchestrator`，官方 agent-loop 直驱主 agent） | 子代理派发/生命周期（S3/S4） |
| 宿主任务目标文案 | 扫雷协作目标（原文零变化） | `""`（无 team section，不消费） | — |
| 游戏工具/玩法 section | saolei 三工具 + `saolei:game`/`saolei:guidance` | 无（080 FR-010 排除） | sts2 工具与运行时后续 feature |

## §3 fail-loud 语义（未接线场景）

未接线场景（不在场景表中 / 场景表中 `orchestratable = false` 或 `presetRoles` 为空）的场景化操作**在入口层整体拒绝**，不进入编排/存储路径、不产生半成品状态：

1. **`UpdateTeam`**（含 roster 任意取值）：`INVALID_ARGUMENT`，文案模式 `scene "{template}" is not wired for team orchestration yet`。拒绝发生在结构校验与 roster 校验**之前**（对未接线场景，无论成员配置为何——含空 `members`——一律同错，不进入任何校验/存储路径）。
2. **`CreatePreset`**：`INVALID_ARGUMENT`，文案模式 `template "{template}" has no preset vocabulary (scene not wired)`——role 参数无论是否合法词均同错（无词表可校验）。
3. **`ListPresets` / `GetPreset` / `UpdatePreset` / `DeletePreset`**：parent 或资源名 template 段为未接线场景时同上句式拒绝（List 不返回空列表——空列表是"合法无内容"语义，会误导调用方认为场景已接线，见 `specs/079-sts2-template-scaffold/research.md` D4）。

> 080-host-main-sub-model 起场景表内 `saolei`/`sts2` 均已接线（sts2 = 单 `main` 成员），本节机制仅对未知模板键与未来新增未接线场景生效（080 [scene-registry.md §7](../../080-host-main-sub-model/contracts/scene-registry.md)）。

**错误码约束**：不新增错误码；全部走 `INVALID_ARGUMENT`（与既有场景校验同码，`session.ts` `TeamSessionErrorCode` 不变）。

**数据面不受影响**：§1 中标 ✅ 的 sts2 行为与 saolei 完全同构（分区隔离、分页、错误码映射等）。

## §4 泛化不变量（saolei 零变化承诺的可验面）

- `saolei` 的一切请求——preset 五个 RPC、team 物化（`{player, planner}` roster 校验、preset/model 校验时序与文案）、会话流、组合行为——响应与泛化前逐字节等价（错误文案含 "saolei scene role" 等措辞不变；roster/角色词表/资源父名均由场景表供给，文案由 `template` + 场景表字段构造后与旧字面量逐字节相同）。
- preset 资源名的 template 段来自请求 parent（泛化后）；对 saolei 请求产出与旧常量拼接相同的字符串。
- 组合清单 saolei 相关行（`saolei`、`saolei-loop` 及其 config）内容零变化；sts2 场景为惰性行（`@dominion/dsh-sts2`，080 起另有 `@dominion/dsh-sts2-loop` 惰性行）。
- 宿主通用代码路径不残留 saolei 场景字面量：场景数据（`roster`/`presetRoles`/`rolePoolTemplate`/`teamGoal`）只存在于场景表（`scenes.ts`），场景行为（编排工厂、announcer/memory 派生）在每场景注册模块（080 起的 `scene-saolei.ts`/`scene-sts2.ts`）；`server.ts`/`session.ts` 仅保留模板白名单、场景 registry 消费点与注释，不得直接 import 场景编排包（080 quickstart V3 固定检查面）。

## §5 前端常量契约

| 端 | 契约 |
|---|---|
| desktop | `projects/game/desktop/frontend/src/api.ts` `TEMPLATES = [TEMPLATE_SAOLEI, TEMPLATE_STS2]`——本地常量控制面，禁止模板列表请求（specs/031 FR-024 延续）；模板下拉随集合自动呈现 |
| web | 模板常量为集合 + 当前场景选择常量（值 `saolei`）；无场景切换 UI；`scene-words.ts` 场景词表含 `saolei`/`扫雷`/`sts2`/`杀戮尖塔` |
| fake-desktop | `FAKE_DESKTOP_TEMPLATE` 默认值 `saolei` 不变 |
