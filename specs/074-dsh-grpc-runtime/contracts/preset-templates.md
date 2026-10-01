# Contract: preset 模板与 registry（即插即用语义、templateRules 移除、能力依赖列表分界）

**Feature**: [spec.md](../spec.md) | **契约层级**: 组装（部署数据只承载部署决策——survey 决策 ⑭）

## 1. `templates.{}.plugins` 语义（修订）

**即插即用自包含插件列表**：列表项为自包含通用插件（无 per-agent 状态、无 scope、无物化期动作），经既有 registry/compose 挂载（deriveRows clone + persona patch 机制不变）。无校验、无额外语义——"往里添加插件就表明你知道你在做什么"。

- 当前内容：persona 行（`@deepseek-ai/dsh-persona`，角色默认 prefix 为纯配置项，保留）。
- **不列入**：需要上层组装的能力（saolei、memory 类）——其声明式归处为 **saolei-loop 能力依赖插件列表**（[team-assembly.md](team-assembly.md) §2：扁平插件列表，经 `ctx.plugin` 挂载 + `ctx.provide` 提供实现，由编排代码消费——列表为纯依赖声明，无角色语义）。

## 2. 即插即用与组装依赖的分界判据（唯一：是否需要上层组装）

| 判据 | 即插即用（`templates.{}.plugins`） | 组装依赖（saolei-loop `config.plugins`） |
|---|---|---|
| per-agent 状态 | 无 | 有（agent-scoped 面） |
| scope | 不需要 | 需要（编排层派生、`build(scope)` 显式传参） |
| 物化期动作 | 无 | 有（fail-loud 预取、runtime 挂载序） |
| 挂载方式与时机 | 声明式，preset compose 期（registry.mount 经 composed.setup） | 声明式，saolei-loop apply 期（ctx.plugin 子插件 + provide；编排消费于成员 setup） |
| 角色语义 | 无 | **无**（扁平纯依赖声明；能力分配归编排代码） |
| 能力形态 | 自包含插件（dsh-saolei-tools 型 provide 静态单元） | 库 + 薄 adapter（common-js-memory + dsh-saolei-memory 型工厂）或静态单元 |
| 连接参数 | 经插件自身 config（可 `inject: ["grpc"]` + config target/protoPath） | adapter config（team-assembly §3.2） |

误挂兜底：配置无角色槽位可错填（扁平纯依赖声明）；非插件包或自身依赖不满足的插件列入任一列表 → 挂载失败 fail-loud（saolei-loop 列表为组合启动失败）；所需提供缺失 → saolei-loop apply fail-loud。无静默降级。

## 3. templateRules 移除（修订）

机制整体删除（Config 字段、schemastery schema、`validateTemplateRows`、`TemplateRowRules` 类型、`cordis.yml` 规则数据、测试 fixture——research.md D8 清单）。preset 创建不再有模板行校验拒绝路径。原"player 禁 memory / planner 恰含 memory"的场景保证由编排代码结构性承接（player 分支只消费 saoleiTools，team-assembly §6）。

层序修正（survey 决策 ⑭）：场景 wiring 归场景代码（saolei-loop 编排）与场景依赖声明（config.plugins 扁平列表），部署数据只承载部署决策。

## 4. registry 瘦身保留边界（不变项声明）

以下语义**全部不变**（specs/070-dsh-upgrade/contracts/composition-manifest.md §2、preset-model-mapping.md §3 继续有效）：

- store CRUD（create/get/list/update/remove）与 role 分池、模板选择（copy-then-patch 拷贝源池）；
- persona-only 编辑（update 仅收 persona/displayName）；
- fail-loud resolve（无 id compose 抛错、store miss 抛错、坏激活 `INVALID_ARGUMENT`）；
- 修订保留（update 后 dispose 旧注册再注册；运行中 agent 持旧引用）；
- restore（boot 时按模板快照重建 roster 投影）与 dispose；
- derive：模板 plugins 行 clone + persona patch（exactly-one persona row 前置不变）。

registry 对成员生效面的贡献收缩为：**persona 挂载**（composed.setup → registry.mount）+ **`meta.agentPreset` 身份**；provider/model 经 `agentOptions` 直达 loop（`orchestrator.ts:745-755`）。

## 5. 组合清单终态（`projects/game/agent_v2/cordis.yml`）

```diff
+ - { id: grpc-runtime,   name: '@dominion/dsh-grpc-runtime' }    # 组合级 gRPC 基建行
+ - { id: saolei,         name: '@dominion/dsh-saolei' }          # 游戏基础领域行（ctx.saolei；无 config）
- - { id: memory,          name: '@dominion/dsh-memory-service' } # 行移除
  - id: saolei-loop
    name: '@dominion/dsh-saolei-loop'
+   config:
+     plugins:                                     # 能力依赖声明（扁平，无角色语义；新增）
+       - name: '@dominion/dsh-saolei-tools'       # 静态单元提供方（自模板行时代迁入，改造为 provide）
+       - name: '@dominion/dsh-saolei-memory'      # 薄 adapter（新包）
+         config:
+           target: 'dominion:///game/memory:50051'
+           protoPath: 'projects/game/game.proto'
  # preset-authoring 行 config：
    templates:
      player:
        plugins:
          - name: '@deepseek-ai/dsh-persona'      # 保留
-       - name: '@dominion/dsh-saolei'             # 工具行移除（归 saolei-loop 能力依赖列表）
      planner:
        plugins:
          - name: '@deepseek-ai/dsh-persona'      # 保留
-       - name: '@dominion/dsh-memory'             # 工具行移除（包退化为公共库）
-   templateRules: ...                              # 机制移除
```

模板 description 文案改写为即插即用语义表述（终态化）。

## 6. 受影响契约修订清单（P4 执行）

| 契约 | 修订点 |
|---|---|
| `specs/059-agent-v2-team-mode/contracts/preset-api.md` §2 | "role 决定绑定的工具插件行（模板 preset 内置对应插件行）" → role 对应的能力组装归 saolei-loop 编排代码（依赖经 config.plugins 声明）；templateRules 引用删除 |
| `specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §3/§5 | `ctx.plannerMemory` host 服务面 → memory 公共库 + adapter 表述；组合清单行变化（§5 引用本文 §5） |
| `specs/070-dsh-upgrade/contracts/composition-manifest.md` | 组合行 delta（+grpc-runtime/-memory-service/saolei-loop config.plugins）与 preset 行解析说明更新 |
| `specs/070-dsh-upgrade/contracts/preset-model-mapping.md` §3 | 模板快照语义（plugins = 即插即用）；templateRules 段删除 |
| `specs/071-memory-preset-scope/contracts/dsh-plugins.md`、`memory-service.md` | memory 挂载/预取/快照语义迁移至公共库 + adapter 表述；`plannerMemory` 面名清零；缺面错误与 section 注册点差异（[team-assembly.md](team-assembly.md) §5） |

各契约的领域行为条款（校验/分页/上限/快照冻结/近因）零修订——仅通路与挂载点表述更新。
