# Contract: preset 派生化（store 唯一事实源 + 使用时挂载）

> `@dominion/dsh-preset-authoring` 插件与 agent_v2 消费面的行为契约（替换 059 `contracts/preset-api.md` §2"创作与编辑"中的 copy-then-patch 条款；PresetService RPC 面与 role 语义不变）。
> 决策依据：[research.md](../research.md) R2。

## 1. 唯一事实源

- 用户创作 preset 的全部持久事实存于 Mongo store（`game_agent_v2.presets`，记录字段不变：id/role/template/persona/displayName/时间戳）。
- **不存在被维护的组合文件副本**：Create/Update/Delete 只写 store；组合文件作为**派生视图**仅在使用时生成（§2），任何时刻删除派生物不影响 store 数据与后续派生正确性（重建幂等）。
- 池模板（`projects/game/agent_v2/preset-templates/{player,planner}/`）保持镜像内打包数据形态（部署产物，非运行时维护文件）；Create 时 `templateRules` 行级校验读模板文件，规则与失败语义（INVALID_ARGUMENT、不落任何产物）不变。

## 2. compose：使用时派生挂载

`compose(presetId)`：

1. `store.get(presetId)` → 无记录 NOT_FOUND（fail-fast，先于任何成员创建）。
2. 读模板组合（roster 发现的模板 `agent.cordis.yml`）；以记录 persona 替换 persona 行 `config.text`；**persona 为空 → 保留模板行原文**（角色默认 base，空值回退语义不变）。
3. 派生物落地：`os.tmpdir()` 下 mkdtemp 会话目录写 `agent.cordis.yml`（**纯临时产物**：不维护与 store 的一致性、无清理承诺〔进程/容器生命周期承载〕、删除无副作用）。
4. 返回 `{agentPreset: 记录 id, setup}`；setup(agentCtx) 经官方 **`mountPreset(agentCtx, {id, trust: 'user', path: 临时文件})`** 挂载——合成 AgentPreset 无需 roster root 发现；挂载保障（inactive-rows 检查、root-realm 泄漏检查、scope 隔离）完整保留；挂载 fiber 随成员 agent 卸载。
5. 派生是幂等纯函数：同一 store 记录任意次 compose 产出内容等价的组合（临时文件路径可不同）。

## 3. roster 面收缩

- `cordis.yml` agent-presets 行 roots = 两个模板 system 根（**user root 条目删除**；`PRESET_WRITABLE_ROOT` 语义消亡，部署清单不再声明）。
- roster 服务面保留：模板发现（resolve/list）与 Create 校验支撑；`copy`/`remove` 创作面不再被调用（插件 seam 保留导出，无消费方）。
- `default` 仍指向空 id（preset 必选语义不变：无 id resolve fail-loud）。

## 4. 不变的面

- PresetService RPC 面（CRUD/ListModels/role 过滤）、资源名与错误映射（059 `contracts/preset-api.md` §1/§3）。
- 物化校验链（preset 存在 + role 匹配 + model 目录）、UpdateTeam 幂等/回滚语义。
- 已物化成员内容固化（物化后 preset 编辑不影响已物化实例；再次物化取新值）。
- 模板不可写/不可删（system 信任根）。

## 5. 环境变量

| 变量 | 来源 | 语义 |
|---|---|---|
| `DOMINION_ARTIFACT_DIR` | 平台注入（deploy-env.md §1） | 模板根默认派生基座：`${DOMINION_ARTIFACT_DIR}/preset-templates` |
| `PRESET_TEMPLATES_ROOT` | 显式覆盖（本地/测试） | 已设则直用（宿主 boot 前注入组合 env 的既有模式）；与 ARTIFACT_DIR 皆缺 → boot fail-loud |
| `PRESET_WRITABLE_ROOT` | — | **移除**（派生临时文件经 `os.tmpdir()`，不经配置） |
