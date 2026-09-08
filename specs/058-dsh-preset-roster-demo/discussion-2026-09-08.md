# Research: dsh preset roster 验证 — 讨论结论存档与验证底稿

> **状态**：中间数据（2026-09-08 讨论结论，全部为纸面推断 + 源码级事实，**未经运行实证**）。
> **用途**：① plan/tasks 阶段的输入（架构与验证矩阵在此）；② **验证完成后的 survey 写作底稿**——按实践过程将 roster 验证结果写入 `survey/`（FR-010），被实践修订的结论以 survey 为准并在此对照记录。
> **上游讨论**：`survey/deepseek-harness-team-mode.md`（team 模式调研，其 §2.3/§3 的 preset 机制结论是本验证的对象）。
> **日期**：2026-09-08

---

## 1. 问题溯源：agent_v2 现状的"preset"实现分析（第一轮讨论）

问题："`projects/game/agent_v2/cordis.yml` 是否即 preset 模型中的 `agent.cordis.yml`？Mongo 是否即 `preset.yml` 元数据？"

**结论：两半都不对应，但"静态/可变分工"直觉方向正确。** agent_v2 是 survey 所述"无 roster 部署（B1 直组形态）"：

- `projects/game/agent_v2/cordis.yml` 是 **host composition（进程级组合清单）**：boot 时由 `src/dsh.ts:67` 读取、整进程挂载一次（specs/051 contracts/saolei-plugins.md §5）。preset 模型中的 `agent.cordis.yml` 是 **agent scope** 组合文件（roster per-agent 挂载）——scope 不同。`agent.cordis.yml` 本应承载的 per-agent 内容（工具行 + prompt sections）全部烘焙在 host 组合，所有 agent 经工具 registry 全局层共享。
- Mongo（`projects/game/agent_v2/src/presets.ts:53-58`：`{name, player_prompt, create_time, update_time}`）存的是 **persona 文本**：roster 模型里对应 `agent.cordis.yml` 内 persona 行的数据库化，而非 `preset.yml`（后者是纯展示元数据）。消费路径：物化时取 persona 快照经 `AgentOptions.persona`（`@dominion/dsh-saolei-loop` 的 declaration-merge 扩展，`src/session.ts:27-29, 294-297`）编程式传入——不是组合文件行。

**用户裁定（边界批评）**：cordis.yml 作为所有 preset 启用插件的超集没有问题；但 preset 的组织方式拼接强行、边界划分混乱——**preset 扩展逻辑（拼装/生成/动态组合）是场景绑定的扩展能力，不应该放在 service 层代码中**。此实践与落地方式纳入验证目标。

## 2. 三个候选形态与机制约束（第二轮讨论）

| 选项 | 形态 | 机制判定 |
|---|---|---|
| A | 代码完全拼装 `agent.cordis.yml`，存储只存需修改的增量 | 可行（生成器写文件） |
| B | 直接管理最终 `agent.cordis.yml`（persona 经 API 改，最终保存仍是组合文件） | 可行 |
| C1 | 代码提供静态模板（组合文件），存储提供动态数据（persona 等），物化 = copy-then-patch | **已选**（2026-09-08） |
| C2 | 静态 `agent.cordis.yml` + 动态 sidecar 数据文件 | **不存在**——`@deepseek-ai/dsh-persona` Config 仅 `text`（行内模板字符串）+ `complete`，无 file 输入（https://www.npmjs.com/package/@deepseek-ai/dsh-persona README） |

**C1 入选理由（用户原话归纳）**：完整文件入库太重；其中大部分内容逻辑上的确属于"模板"；数据库只存必要部分，冗余数据加大负担。

**关键机制约束**（决定所有选项的公共形态）：

- dsh preset 层**无继承/合并语义**——官方立场是全量拷贝（copy 即 authoring 路径），patch 语义刻意留在 bundle 层（进程级）；最终产物必然是磁盘上的完整组合文件。
- roster CRUD API **copy-only**："no composition text crosses this seam"（安全考量：组合=能力注入）——任意内容编辑面只能是写文件。
- **动态字段物化必然落组合文件的 persona 行**（`config.text`）+ `preset.yml` 展示名。

## 3. 社区参考（第三轮讨论检索，2026-09-08）

| 平台 | 管理模式 | 对应选项 |
|---|---|---|
| LM Studio（https://lmstudio.ai/docs/app/presets） | 文件即 source of truth：`~/.lmstudio/config-presets/*.preset.json`，GUI 编辑即写文件；文件即分享 artifact（Hub/URL import）；社区 preset manager 是独立第三方工具 | ≈ B |
| Dify（https://docs.dify.ai/en/self-host/use-dify/workspace/app-management；roster 架构 https://deepwiki.com/langgenius/dify/15.1-agent-roster-service-and-lifecycle） | DB 为 source of truth：Draft→publish→不可变 Snapshot+Revision 审计；YAML DSL 仅导入/导出交换格式 | ≈ A |
| CrewAI（https://docs.crewai.com/v1.15.17/en/concepts/agents） | 文件声明（`agents/<name>.jsonc`：role/goal/backstory/tools 名单）+ 代码绑定（`@agent` 装饰器组装工具实例，config-code correspondence） | ≈ C |
| OpenClaw（https://docs.openclaw.ai/concepts/soul） | 多文件分层：SOUL.md/IDENTITY.md（persona）与 openclaw.json/TOOLS.md（能力）分离；local-first 文件、version-controllable | ≈ C 多文件变体 |

**收敛结论**：除 Dify（平台级版本管理需求）外，社区主流是**文件为 artifact/source of truth、编辑面是外挂工具**——B/C 方向与社区实践一致；A 的价值在需要 DB 级版本化/审计时，对 saolei N≈1 场景疑似过度（与 `survey/deepseek-harness-preset.md` §8/§9.1 N 计价结论一致）。

## 4. roster 机制事实锚点（源码级，实现对照）

1. **roster README**（`node_modules/.pnpm/@deepseek-ai+dsh-agent-presets@0.1.1-rc.2_5200ead8959daeaefdf3dd69ba905368/node_modules/@deepseek-ai/dsh-agent-presets/README.md`）：
   - "files are the only composition editor"；authoring copy-only；发现 unmemoized（每次 list/resolve 重扫 roots，运行期写文件即时可见）；generation 以组合文件 stamp（mtime+size）为键——新会话拿新组合，已加入会话保持旧组合；文件删除后 joined 会话照跑。
   - Config：`default`（必填）/ `roots[]`（path+trust，先命中者胜）/ `includeUserRoot`（默认 true，demo 须 false 保确定性）。
   - `remove()` 只删第一个 user root 下的 preset；`copy()` 落第一个 user root——**可写 root 必须是第一个（唯一）user root**。
   - `preset.yml` 仅展示（name/description）；id=目录名（`[a-z0-9][a-z0-9-]*`）、trust=root 决定，不可写。
   - broken preset：目录组合缺失/不可解析 → list 携带 reason 而非跳过；mount 前置拒绝。
   - 已知限制：superseded generation 不回收（每个编辑-创建循环累积 watcher）；root 扫描无 watch（每次 list 落盘 readdir）。
2. **挂载缝**：`CreateAgentOptions.setup?: AgentSetup` + `meta.agentPreset`（`node_modules/.pnpm/@deepseek-ai+dsh-agent@0.1.1-rc.2_*/.../lib/types/index.d.ts`）。官方接线范例 `composeAgent()`：roster 未组合 → 退化 host 组合；已组合 → resolve + `presets.mount(agentCtx, resolvedId)`（`node_modules/.pnpm/@deepseek-ai+dsh-host-apiproxy@0.1.1-rc.2_7a1c54e2b954eca6f88bad802758761b/node_modules/@deepseek-ai/dsh-host-apiproxy/lib/index.js:1754-1765`）。**注意：agent-loop/spine 本身不含 roster 接线——接线归创建路径所有者（demo 的 session.ts）**。
3. **persona 行**：`@deepseek-ai/dsh-persona`（Config：`text`/`complete`；scope-only——只能挂 preset 内，host 挂载撞 `deployment:persona` fail-loud；text 是 `{{…}}` 模板）。npm 最新索引 0.0.1-rc.1，**实现时以 registry versions 核对 0.1.1-rc.2 同线版本**（dist-tag 不可信）。
4. **demo 基线**：`experimental/dsh/demo/agent/cordis.yml` 两行（agent-spine-demo + llm-deepseek）；会话 lazy 创建于首条消息（`src/session.ts` `getOrCreate`）；spine 的 persona config = deployment persona（被 preset 的 persona 行遮蔽）。
5. **Dominion 插件包形态参照**：`common/js/dsh-plugins/llm-glm/src/index.ts`（name/inject/Config/apply 四导出 + effect-based 注册）。

## 5. 已确认决策清单（2026-09-08，全部）

| # | 决策 |
|---|---|
| D1 | 载体：扩展 047 demo 本身 |
| D2 | 扩展模式：C1 copy-then-patch（存储只存动态字段：persona、展示名、模板引用、时间戳） |
| D3 | 扩展边界：Dominion 插件包（`common/js/dsh-plugins/` 下通用 authoring 基座） |
| D4 | 会话绑定面：显式 CreateConversation RPC（对齐 agent_v2 物化形态） |
| D5 | demo 存储：内存实现（Store seam；Mongo 生产化延后至 agent_v2 迁移） |
| D6 | 插件定位：通用基座（场景差异只在模板内容与动态字段；patch 规则先硬编码 persona text + 展示名，YAGNI） |
| D7 | demo 工具插件：demo 本地 workspace 包（工具 + guidance 同包，preset 行裸包名引用） |
| D8 | 中间数据：本文件；验证完成后按实践写 survey/（FR-010） |

## 6. 架构设计

### 6.1 组件与组合变化（demo agent cordis.yml）

```
host 组合（进程级）                         preset（会话级，roster roots）
├─ agent-spine（保留：loop/factory；        ├─ templates root（部署数据，trust: system）
│   persona config = deployment persona，   │   ├─ demo-standard/  （persona 行）
│   被 preset 的 persona 行遮蔽）           │   └─ demo-tools/     （persona 行 + demo-echo 工具行）
├─ llm-deepseek（保留）                     └─ writable root（emptyDir，trust: user——
├─ agent-presets（官方 roster，新增）            第一个且唯一 user root）
│   config: default / roots / includeUserRoot: false
└─ preset-authoring（@dominion/dsh-preset-authoring，新增）
    inject: ["agentPresets"]；提供 ctx 服务 presetAuthoring
```

### 6.2 service ↔ 插件边界（核心交付物）

| 职责 | service 层（proto/server.ts/session.ts） | 插件（preset-authoring） |
|---|---|---|
| API 契约 | RPC、AIP 资源命名/错误映射、请求校验 | — |
| preset CRUD 领域逻辑 | — | create/update/delete/get/list、id 规则、幂等与回滚 |
| 动态数据持久化 | — | Store seam（接口 + 内存默认） |
| 模板物化 | — | copy-then-patch：roster copy() → patch 副本 persona 行 text → 写 preset.yml 展示名 |
| roster 消费 | — | inject agentPresets；copy/remove/list 视图 |
| 会话↔preset 绑定 | CreateConversation 收 preset id → create({meta:{agentPreset}, setup: mount}) | 只读校验（id 可解析） |
| API 输出 | 响应组装（读插件 get/list） | 返回记录 + roster 健康视图 |

**一句话边界**：service 拥有传输与资源语义；插件拥有 preset 领域机制；对接面 = `ctx.get("presetAuthoring")`（与 `ctx.agents` 同型）。service 不 import roster/fs；插件不触碰 RPC。此为 agent_v2 `presets.ts`+server.ts 内嵌逻辑的目标下沉形态。

### 6.3 C1 数据流

```
Create:  store 查重 → roster.copy(templateId, presetId) → patch 副本（persona 行 text +
         preset.yml name）→ store 落 {id, template, persona, displayName, createTime}；
         失败回滚目录（无半物化）
Update:  store 更新 → 原地 patch 副本文件（stamp 变化 → 新 generation：新会话生效、
         旧会话保持）
Delete:  roster.remove(presetId) + store 删（已加入会话不受影响——roster 保证）
```

### 6.4 API 变更（chat.proto）

- **Chat.CreateConversation**（新增）：显式创建会话绑定 preset（可选，缺省 default）；重复创建：同 preset 幂等、异 preset 重建；未创建的 SendMessage → FAILED_PRECONDITION。
- **PresetService**（新增）：Create/Get/List/Update/Delete；动态字段 persona + displayName；Create 指定 template。
- gateway 路由：配置面（PresetService）与会话面（Chat）说明更新（demo 单实例直连，拓扑不变）。

## 7. 验证矩阵（FR-007 断言承载）

| 编号 | 验证点 | 断言 | 承载 |
|---|---|---|---|
| V1-1 | per-session 选择 | 不同 preset 会话 persona/system prompt 不同 | 大型测试（fake-llm echo 或等价端到端断言）+ 单测 |
| V1-2 | default 语义 | 不传 preset → default 模板生效 | 大型测试 |
| V1-3 | header 记录 | SessionHeader.agentPreset 落对 | 单测 |
| V2-1 | scope 链 agent→preset→global | preset 行工具仅该 preset 成员可见；host 全局行全员可见 | 单测 |
| V2-2 | standing mount 共享 | 同 preset 多会话共享一份组合注册 | 单测 |
| V2-3 | 工具↔guidance 一致性 | 挂 demo-echo 行：schema+守则都在；不挂：都无 | 单测 + 端到端 |
| V3-1 | 热创作 | API create → roster 即时可见 → 新会话可用 | 大型测试 |
| V3-2 | generation | API update persona → 旧会话不变、新会话生效 | 大型测试 |
| V3-3 | broken preset | 组合文件损坏 → list 报 broken、会话创建 fail-fast | 单测 |
| V4-1 | C1 闭环 | create/update/delete 全链路 store↔文件↔roster 一致 | 单测 + 大型测试 |
| V4-2 | 边界 | service 层零 roster/fs 引用 | review 审计（checklist 项） |
| V4-3 | 删除语义 | delete 后已加入会话不受影响、新创建拒绝 | 大型测试 |

## 8. 待定项（plan 阶段定）

- CreateConversation 幂等/重建语义细节（在途消息处置）；preset 字段是否必填（当前 spec：可选缺省 default）
- YAML patch 实现：文本级定点替换 vs 结构读写（模板自控、约定无注释依赖）
- API 是否暴露模板列表（ListTemplates 或标记字段；CreateConversation 的 preset 字段当前接受模板或副本 id）
- `installSelection` 事件是否随 demo 引入（对照 apiproxy 的 composeAgent 全貌）
- fake-llm echo 模板是否引入（优先单测承载，Assumptions 已记）
- dsh-persona 同线版本号核对（`pnpm view @deepseek-ai/dsh-persona versions`）
- 插件包最终命名（preset-authoring 为工作名）

## 9. 验证后 survey 写作指引（FR-010 待办）

验证完成、全部用例通过后，新增 `survey/deepseek-harness-roster-verification.md`（或并入既有 preset 调研的修订），内容依据**实践过程**：

1. **roster 机制实证结论**：V1-V3 各验证点的实测结果，与 §4 机制事实锚点、`survey/deepseek-harness-team-mode.md` §2.3/§3 结论的对照（证实/证伪/差异）。
2. **C1 扩展实践结论**：copy-then-patch 物化的实操摩擦（YAML patch 方式、回滚、stamp 行为）、与社区参考（§3）的印证。
3. **service↔插件边界最佳实践**：ctx 服务对接模式的实践评价、对 agent_v2 迁移的具体建议（presets.ts/server.ts 下沉路径）。
4. 本文档中被实践修订的结论，在 survey 中给出对照记录（纸面 vs 实测）。
