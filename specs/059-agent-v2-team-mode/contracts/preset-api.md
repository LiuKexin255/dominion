# Contract: Preset API（role 分池扩展）

> 对外契约：`projects/game/agent_v2.proto` PresetService 的演进规范（配置面，gateway 直连 agent-v2，路由不变）。
> 决策依据见 [research.md](../research.md) R3；实体定义见 [data-model.md](../data-model.md) §2 Preset。

## 1. RPC 面（路由不变，消息扩展）

| RPC | HTTP | 演进点 |
|---|---|---|
| `CreatePreset` | `POST /api/v2/{parent=templates/*}/presets` | 请求增加必填 `role`（PLAYER/PLANNER，caller-supplied id 惯例不变）；creation 经 copy-then-patch 从对应池的模板 preset 物化 |
| `ListPresets` | `GET /api/v2/{parent=templates/*}/presets` | 增加可选 `role` 过滤参数；返回项含 role |
| `GetPreset` | `GET /api/v2/{name=templates/*/presets/*}` | 返回含 role |
| `UpdatePreset` | `PATCH /api/v2/{name=templates/*/presets/*}` | update_mask 仍仅 `persona`（role 不可变）；create_time 保留 |
| `DeletePreset` | `DELETE /api/v2/{name=templates/*/presets/*}` | 无 fan-out 语义不变（已物化 team 不受影响） |
| `ListModels` | `GET /api/v2/models` | 零变更（两成员共用同一目录） |

## 2. 行为规范

- **role 语义**：role 决定 preset 所属池与绑定的工具插件行（PLAYER→saolei 工具组、PLANNER→memory 工具组），编辑期固定（模板 preset 内置对应插件行，用户编辑面仅 persona——"选择单位是插件组"）。role 在 create 时必填且不可变；对已存在 preset 的 role 更新请求 → `INVALID_ARGUMENT`。
- **唯一性**：preset id 同 template 内全局唯一（跨池不重复）；重复创建 → `ALREADY_EXISTS`。
- **模板与创作**：每池提供内置模板 preset（内置角色插件行 + 角色 persona 占位）；Create = 模板拷贝 + persona patch（copy-then-patch，058 实证算法）；模板不可写/不可删（`FAILED_PRECONDITION`）。创作后零重启即可被新物化引用（热创作，V3-1 实证）。
- **persona 空值**：物化时 persona 为空回退该角色默认 base（PLAYER/PLANNER 各一份，第一人称身份声明开头，R2）。
- **generation**：persona 编辑产生新 generation，新物化命中新内容、已物化成员保持旧内容（V3-2 实证）——刷新 team 即取新。
- **错误语义**：AIP-133/134/135 + AIP-193，沿用现状（`INVALID_ARGUMENT`/`ALREADY_EXISTS`/`NOT_FOUND`/`FAILED_PRECONDITION`，`cause` 链保留）。

## 3. 存储

Mongo `game_agent_v2.presets`（preset-authoring 的 `PresetStore` seam 之 Mongo 实现）：文档含 name/role/persona/create_time/update_time；组合文件副本（含插件行）为派生物，可从 store 记录重建（进程内存态语义，重启丢失可接受——store 为 source of truth）。
