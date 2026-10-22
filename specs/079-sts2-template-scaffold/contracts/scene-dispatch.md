# Contract: agent_v2 场景分派（scene dispatch）

> Feature: [specs/079-sts2-template-scaffold/spec.md](../spec.md)（FR-001~FR-004）| 数据模型：[data-model.md](../data-model.md) §1/§2/§6 | 决策：[research.md](../research.md) D1/D2/D4
> 本契约定义 agent_v2（及 Go 各服务）对 `templates/{template}` 的**接受面与场景化操作语义**——泛化后的接口行为（宪法原则 III：接口优先）。saolei 列全部为现状平移（零变化），sts2 列为本期新增。

## §1 模板接受面

| 服务/RPC 面 | `saolei` | `sts2` | 未知模板 |
|---|---|---|---|
| SessionService（Create/Get/List/Delete） | ✅ 现状 | ✅ 同构接受（模板分区隔离） | ❌ `ErrInvalidTemplate` / `INVALID_ARGUMENT` |
| MemoryService（template×preset 作用域）/ WikiService（template 作用域） | ✅ 现状 | ✅ 同构接受（memory 本期仅结构验证：不校验 preset 存在性；sts2 无 preset 业务词汇，见 quickstart V3 注） | ❌ 同上 |
| ProxyService（gRPC 转发；`projects/game/proxy/handler/agent.go` 的 owner 模板校验） | ✅ 现状 | ✅ 白名单扩容后同构接受（无对外 CRUD 面） | ❌ `ErrInvalidTemplate` / `INVALID_ARGUMENT` |
| AgentService 会话流（Connect/消息/历史等） | ✅ 现状 | ✅ 资源解析通过 | ❌ 解析失败 `INVALID_ARGUMENT` |
| AgentService `UpdateTeam`（team 物化） | ✅ 现状 | ❌ §3 fail-loud | ❌ 解析失败 |
| PresetService（Create/List/Get/Update/Delete） | ✅ 现状 | ❌ §3 fail-loud | ❌ 解析失败 |

Go 侧唯一事实源 `projects/game/pkg/gameconst/const.go`（`knownTemplateIDs`）；TS 镜像 `projects/game/agent_v2/src/server.ts`（`KNOWN_TEMPLATES`）。gateway 路由由 proto 通配生成，不感知模板集合。

## §2 场景能力矩阵（SceneSpec 消费视图）

| 能力 | saolei | sts2（本期） | sts2（后续场景 feature，仅示意） |
|---|---|---|---|
| team 物化 roster | `player`、`planner`（恰 2 成员） | 无（`orchestratable=false`） | 待场景设计裁定 |
| preset 角色词表 | `player`、`planner` | 无 | 待场景设计裁定 |
| preset 池模板映射 | player→`player`、planner→`planner` | 无 | 经 `preset-authoring` `templates` 快照扩展 |
| team 编排 | saolei-loop（`TeamOrchestrator`） | 未接线 | sts2 编排/接入方式另行设计 |
| 宿主任务目标文案 | 扫雷协作目标（原文零变化） | 不适用 | 待定 |
| 游戏工具/玩法 section | saolei 三工具 + `saolei:game`/`saolei:guidance` | 无（spec FR-009 排除） | 后续 feature |

## §3 fail-loud 语义（未接线场景）

未接线场景（场景表中 `orchestratable = false` 或 `presetRoles` 为空）的场景化操作**在入口层整体拒绝**，不进入编排/存储路径、不产生半成品状态：

1. **`UpdateTeam`**（含 roster 任意取值）：`INVALID_ARGUMENT`，文案模式 `scene "{template}" is not wired for team orchestration yet`。拒绝发生在结构校验与 roster 校验**之前**（无论成员配置为何——含空 `members`——sts2 一律同错，不进入任何校验/存储路径）。
2. **`CreatePreset`**：`INVALID_ARGUMENT`，文案模式 `template "{template}" has no preset vocabulary (scene not wired)`——role 参数无论是否合法词均同错（无词表可校验）。
3. **`ListPresets` / `GetPreset` / `UpdatePreset` / `DeletePreset`**：parent 或资源名 template 段为未接线场景时同上句式拒绝（List 不返回空列表——空列表是"合法无内容"语义，会误导调用方认为场景已接线，见 `specs/079-sts2-template-scaffold/research.md` D4）。

**错误码约束**：不新增错误码；全部走 `INVALID_ARGUMENT`（与既有场景校验同码，`session.ts` `TeamSessionErrorCode` 不变）。

**数据面不受影响**：§1 中标 ✅ 的 sts2 行为与 saolei 完全同构（分区隔离、分页、错误码映射等）。

## §4 泛化不变量（saolei 零变化承诺的可验面）

- `saolei` 的一切请求——preset 五个 RPC、team 物化（`{player, planner}` roster 校验、preset/model 校验时序与文案）、会话流、组合行为——响应与泛化前逐字节等价（错误文案含 "saolei scene role" 等措辞不变；roster/角色词表/资源父名均由场景表供给，文案由 `template` + 场景表字段构造后与旧字面量逐字节相同）。
- preset 资源名的 template 段来自请求 parent（泛化后）；对 saolei 请求产出与旧常量拼接相同的字符串。
- 组合清单 saolei 相关行（`saolei`、`saolei-loop` 及其 config）内容零变化；仅新增 sts2 惰性行（[dsh-plugins-layout.md](dsh-plugins-layout.md) §3）。
- 宿主通用代码路径不残留 saolei 场景字面量：场景差异（`roster`/`presetRoles`/`rolePoolTemplate`/`teamGoal`）只存在于场景表（`scenes.ts`），`server.ts`/`session.ts` 仅保留模板白名单、模块导入路径、注释与场景表消费点（spec SC-005，可按 quickstart V7 的 grep 验证）。

## §5 前端常量契约

| 端 | 契约 |
|---|---|
| desktop | `projects/game/desktop/frontend/src/api.ts` `TEMPLATES = [TEMPLATE_SAOLEI, TEMPLATE_STS2]`——本地常量控制面，禁止模板列表请求（specs/031 FR-024 延续）；模板下拉随集合自动呈现 |
| web | 模板常量为集合 + 当前场景选择常量（值 `saolei`）；无场景切换 UI；`scene-words.ts` 场景词表含 `saolei`/`扫雷`/`sts2`/`杀戮尖塔` |
| fake-desktop | `FAKE_DESKTOP_TEMPLATE` 默认值 `saolei` 不变 |
