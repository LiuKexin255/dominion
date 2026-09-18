# Contract: @dominion/dsh-preset-authoring（preset 扩展插件）

> 通用 authoring 基座插件（D3/D6）：C1 copy-then-patch 机制、Store seam、roster 消费。
> 形态参照 `common/js/dsh-plugins/llm-glm/src/index.ts`（name/inject/Config/apply 四导出 + effect-based 注册）。
> 插件 MUST NOT 含任何 RPC/传输概念（FR-005）；demo 是第一个消费者，agent_v2 迁移是第二个（Mongo Store 经同 seam 接入）。

## 1. 插件声明

```typescript
export const name = "preset-authoring";
export const inject = ["agentPresets"];   // 硬依赖官方 roster 在组合中
export const Config = z.object({ storage: z.const("memory").default("memory") });
```

ctx 服务 key：**`presetAuthoring`**（host 平面 Service，`ctx.get("presetAuthoring")`）。

## 2. 服务接口（service 层的唯一对接面，R10）

```typescript
interface PresetAuthoringService {
  /** 会话物化接线（复刻 apiproxy composeAgent 形状）：resolve 提前（header 快照约束）+
   *  setup 内 mount（失败回滚整个创建）。presetId 缺省 = roster default。 */
  compose(presetId?: string): Promise<{
    agentPreset: string;                      // resolved id（service 传入 create 的 meta.agentPreset）
    setup(agentCtx: unknown): Promise<void>;  // service 传入 create 的 setup
  }>;

  create(input: { id: string; template: string; persona: string; displayName?: string }): Promise<PresetView>;
  get(id: string): Promise<PresetView>;                    // NOT_FOUND
  list(): Promise<PresetView[]>;                           // Store 记录（创作副本；不含模板，R5）
  update(id: string, patch: { persona?: string; displayName?: string }): Promise<PresetView>;
  remove(id: string): Promise<void>;                       // NOT_FOUND；模板拒绝（透传 roster 错误）
}

interface PresetView { id: string; template: string; persona: string; displayName?: string;
                       createTime: Date; updateTime: Date; }
```

**边界**（FR-005/V4-2 审计项）：service 层（server.ts/session.ts）仅消费本接口 + `compose()` 返回值注入 `ctx.agents.create({meta, setup})`——零 roster API、零 fs。

## 3. Store seam（D5）

```typescript
interface PresetStore {
  create(record: PresetRecord): Promise<void>;        // 重复 id 抛 ALREADY_EXISTS
  get(id: string): Promise<PresetRecord>;             // 缺失抛 NOT_FOUND
  list(): Promise<PresetRecord[]>;
  update(record: PresetRecord): Promise<void>;        // 缺失抛 NOT_FOUND
  remove(id: string): Promise<void>;                  // 缺失抛 NOT_FOUND
}
```

- 唯一实现：内存 `Map`（demo；重启丢失 = 已知限制）。错误码稳定（`PresetStoreError`，agent_v2 `projects/game/agent_v2/src/presets.ts:40-50` 同型）。
- **MUST NOT 存组合文件内容**（D2：只存 data-model §2 字段）。
- 生产化（agent_v2）时在同包增加 Mongo 实现并经 Config.storage 切换——本 feature 不实现。

## 4. 物化算法（copy-then-patch，R3）

### create

```text
1. 校验：id 语法（[a-z0-9][a-z0-9-]*）；store 查重（ALREADY_EXISTS）
2. roster resolve(template) —— 未知/ broken → INVALID_ARGUMENT（含可用集合，错误透传）
3. roster copy(template, id, displayName ?? id)     ← 展示名经 copy 第三参写入副本 preset.yml
4. patch 副本 agent.cordis.yml：js-yaml load → 定位 name === '@deepseek-ai/dsh-persona' 的行
   → 置 config.text = persona → dump 写回（原子写：临时文件 + rename）
5. store 落记录
失败回滚：任一步抛错 → 删除半物化目录（best-effort）→ 上抛（无 store 残留）
```

### update

```text
persona 变化：patch 副本组合文件（同 create 第 4 步；stamp 变化 → 新 generation）
displayName 变化：直接重写副本 preset.yml（name + 模板 description；不影响 generation）
store 更新 updateTime；两字段独立生效（update_mask 语义）
```

### remove

```text
roster remove(id)（拒绝非首个 user root 下的资源——透传其错误：模板 id 会命中此拒绝）
store 删记录；已加入会话不受影响（roster 语义）
```

## 5. 可测试性（seam 约定）

- 构造注入：`createPresetAuthoring(ctx, config, deps?)`——deps = `{ roster?, store?, fs? }`（默认从 ctx.get("agentPresets") / Config.storage / node:fs 取）；单测传 `vi.fn()` doubles（`style/javascript.md` Mock convention——无模块拦截）。
- roster 服务面（插件消费的官方 API）：`resolve/copy/remove/list`（`node_modules/.pnpm/@deepseek-ai+dsh-agent-presets@0.1.1-rc.2_*/.../README.md` Service 节）。
- V3-3/V4-1 单测经 fs seam 写坏文件/断言文件内容，不触碰真实 roster。

## 6. 错误码汇总

| 场景 | 码（service 映射 gRPC status） |
|---|---|
| id 语法非法 / 未知模板 / broken 模板 | `INVALID_ARGUMENT` |
| id 已占用（store 或磁盘目录） | `ALREADY_EXISTS` |
| 未知 preset id（get/update/remove） | `NOT_FOUND` |
| remove 模板（roster 拒绝 system trust） | `FAILED_PRECONDITION` |
| 物化文件操作失败 | `INTERNAL`（fail-loud，回滚后上抛） |
