# Contract: preset-authoring 新模型映射（服务面冻结）

**Feature**: specs/070-dsh-upgrade/spec.md（FR-008、US3 语义 1–5）
**Research**: specs/070-dsh-upgrade/research.md（D2/D3/D5）
**Data model**: specs/070-dsh-upgrade/data-model.md §3

## 1. 对外服务面（冻结，不变）

`@dominion/dsh-preset-authoring`（`common/js/dsh-plugins/preset-authoring/src/index.ts`）：

- 插件标识：`name = "preset-authoring"`，`inject = ["agentPresets"]`（0.1.7 registry 提供同名服务键，注入连续）。
- `ctx.presetAuthoring: PresetAuthoringService`：
  - `compose(presetId?): ComposeResult`——`ComposeResult.agentPreset` 为可直接传入 `CreateAgentOptions.meta.agentPreset` 的 preset id；未知 id 报错（fail-loud）。
  - `create(input): PresetView` / `get(id)` / `list(role?)` / `update(id, patch)` / `remove(id)`。
- Store seam（`PresetStore`/`PresetRecord`/Mongo 实现）与 Config（storage/mongo\*/templates）不变。
  - `PresetRecord` 字段 schema **冻结**（`specs/070-dsh-upgrade/data-model.md` §3.4）；Mongo 一次性迁移（FR-012）不改字段形态，只确认 `template` 值对应声明式模板 id、persona `prefix` 物化可用。

**消费方不动**：`projects/game/agent_v2`（server.ts/presets.ts）、`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`（compose → `meta.agentPreset`）、demo。

## 2. 内部映射（重构面）

### 2.1 derive → register

| 旧（0.1.1） | 新（0.1.7） |
|---|---|
| 读模板目录 `agent.cordis.yml`（fs） | 读宿主注入的 `templates` 快照 `plugins`（preset-authoring 行 Config） |
| persona 行 `config.text` 替换为 `record.persona` | persona 子行 `config.prefix = record.persona` |
| `dump YAML → mkdtemp → mountPreset(dir)` | 内存构造 rows → `ctx.agentPresets.register({ id, plugins, name?, description? })` |
| 返回 `AgentPreset {trust:"user"}` | 返回 register disposer（插件持有） |

- `PERSONA_ROW_NAME = '@deepseek-ai/dsh-persona'` 常量与行定位逻辑保留（字段访问改 `config.prefix`）。
- `DeriveFs`/`nodeDeriveFs`/`COMPOSITION_FILE_NAME` 等 fs seam 随目录面删除（原则 II 简化，`common/js/dsh-plugins/preset-authoring/src/derive.ts`）。

### 2.2 生命周期管理

| CRUD 操作 | registry 动作 | 语义 |
|---|---|---|
| create | validate → store.create → register | 注册即激活；激活失败（如子行损坏）→ store 回滚（不落产物）或按 US3 场景 5 留 roster 拒绑定——**取后者语义但对 create 返回 INVALID_ARGUMENT**（创建校验与激活审计分离：校验拒绝（模板未知/撞部署模板 id/id 非法）不落；激活失败落 roster 且 create 报错） |
| update | store.update → unregister + register | 旧修订 retire；运行中 agent 保留（SC-005 增强点） |
| remove | store.remove → unregister | 引用释放后 tree 销毁 |
| boot 恢复 | `list()` 全量 register | Mongo 为唯一事实源，进程内物化 |

- disposer 由 preset-authoring 插件 effect 生命周期持有（插件卸载 → 全量 unregister）。
- **一致性边界**：store 为事实源，registry 为运行时投影；两者在同一插件内同步写（无最终一致窗口）。

### 2.3 模板声明注入

模板定义的单一事实源 = 宿主（agent_v2/demo）经 Config 注入的 `templates` 快照，不经 `@deepseek-ai/dsh-agent-preset` 声明行表达（2026-09-28 用户裁定，research.md D4 终态结论）。快照键 = 模板 id，值 = `{ name?, description?, plugins }`（`templates: { player: { name?, description?, plugins }, planner: {...} }`）；preset-authoring 不再读模板文件系统。`plugins` 为即插即用自包含插件列表（specs/074-dsh-grpc-runtime/contracts/preset-templates.md §1）——需要上层组装的能力（saolei、memory 类）不入列表，其依赖声明归 saolei-loop `config.plugins`（specs/074-dsh-grpc-runtime/contracts/team-assembly.md §2）。

## 3. 模板契约（`templates` 快照）

- **定义**：模板 = 宿主组合清单内 `preset-authoring` 行 Config 的 `templates` 快照键（agent_v2：`player`/`planner`；demo：`demo-standard`/`demo-tools`），值 = `{ name?, description?, plugins }`——plugins 为即插即用自包含插件列表（无 per-agent 状态、无 scope、无物化期动作；分界判据 specs/074-dsh-grpc-runtime/contracts/preset-templates.md §1/§2）：agent_v2 两池当前仅 persona 子行（`prefix`），demo 的 `demo-tools` 另含自包含 `@dominion/dsh-demo-echo` 工具行。该快照即模板定义的单一事实源，不经任何 `@deepseek-ai/dsh-agent-preset` 声明行表达（2026-09-28 用户裁定，research.md D4 终态结论）。
- **系统信任**：快照是宿主部署数据；用户面（PresetService API）无模板写删路径——`remove` 对模板 id 是 store NOT_FOUND，用户不可写删（US3 语义 1）。
- **身份空间**：模板 id 与用户 preset id 共享部署的 id 空间；模板不进 roster（roster 条目只经 preset-authoring 由 store 记录物化进入），撞名保护由 create 的快照 `hasOwnProperty` 检查承担（`common/js/dsh-plugins/preset-authoring/src/index.ts`）——用户 create 若撞模板 id → `ALREADY_EXISTS`（fail-loud）。

## 4. fail-loud 选择

- registry `default: ''`（无名解析 not-found；D5 门禁：schema 拒空串则 sentinel 回退 + 宿主全显式断言）。
- 宿主全部路径显式 `meta.agentPreset`；`compose()` 对未知 id 报 `agent-preset/not-found`（透传 registry 语义）。

## 5. 测试契约

- `common/js/dsh-plugins/preset-authoring/src/index.test.ts`：create/update/remove → register/unregister 调用序列断言（fake registry）；模板未知拒绝路径；store 回滚。
- `derive.test.ts`：rows 构造断言（persona `prefix` patch 与 sibling 行保留）；fs seam 删除后用例改内存断言。
- `projects/game/agent_v2/src/session.test.ts`：`meta.agentPreset` 物化路径不变（现用例保留）。
- 新增：激活失败 preset 留 roster 拒新绑定、update 修订保留（运行中 agent 不掉工具）两条单测（US3 场景 5 / SC-005）。

## 6. 验收

1. preset-authoring 单测全绿（含新增两条语义用例）。
2. agent_v2 `presets.test.ts`/`server.test.ts`（CRUD gRPC 面）不改动即全绿（服务面冻结证明）。
3. demo `preset_test`（16 用例）经适配后全绿。
