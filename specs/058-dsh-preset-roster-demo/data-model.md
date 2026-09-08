# Data Model: 058 dsh preset roster demo

> 实体与状态转移（对应 spec [Key Entities](spec.md#key-entities)；实现级字段在 contracts 中细化）。
> 引用：roster 语义锚点见 [research.md](research.md) R1-R12 与 `discussion-2026-09-08.md` §4。

## 1. PresetTemplate（模板 preset）

部署数据，system 信任 root 下只读。**不进 API 资源面**（R5），不持久化到 Store。

| 字段 | 说明 | 约束 |
|---|---|---|
| id | 目录名（roster 发现） | `[a-z0-9][a-z0-9-]*`；`demo-standard` / `demo-tools` 两份 |
| composition | `agent.cordis.yml` 内容 | R3 模板约定：无注释依赖、无 `!!js`、**有且仅有一行** `name: '@deepseek-ai/dsh-persona'`；`demo-tools` 额外含一行 `@dominion/dsh-demo-echo` |
| display | `preset.yml`（name/description） | 展示元数据；copy 时 name 被丢弃（roster 语义） |
| trust | root 决定 | `system`：不可 `remove()`、不可经 API 修改 |

## 2. AuthoredPreset（创作 preset）

Store 记录（内存实现）+ writable root 物化副本的配对。

| 字段 | 类型 | 说明 |
|---|---|---|
| id | string | preset id = 资源 id（`presets/{id}`）；roster copy 的目标目录名 |
| template | string | 来源模板 id（物化时的 copy 源） |
| persona | string | 动态字段：物化进副本 persona 行 `config.text` |
| displayName? | string | 动态字段：物化进副本 `preset.yml` name（缺省 id） |
| createTime / updateTime | Date | 资源时间戳（AIP-134 create_time/update_time） |

**校验规则**（FR-004）：
- id 唯一（重复 create → `ALREADY_EXISTS`）；模板必须可被 roster resolve（未知 → `INVALID_ARGUMENT`，错误含可用集合）。
- **Store MUST NOT 保存组合文件内容**（动态字段之外的一切都在模板/副本文件中——D2 决策）。

**状态转移**：

```text
none ──create(成功)──▶ created ──update(persona/displayName)──▶ updated*（generation 随文件 stamp 递增，roster 管理）
  │                        │
  │ create(失败)            └──remove──▶ deleted（目录移除；已加入会话保持旧组合继续运行）
  ▼
none（无半物化残留：store 记录与目录同生同灭）
```

- `update(persona)`：副本文件 stamp 变化 → 新会话新 generation、已加入会话保持旧组合（roster 语义，V3-2 断言）。
- `update(displayName)`：仅重写 `preset.yml`，不影响 generation（stamp 只看组合文件）。

## 3. Conversation（会话）

显式创建的对话单元；进程内存态（047 既有语义延续）。

| 字段 | 说明 |
|---|---|
| name | `conversations/{id}`（资源名） |
| preset | 创建时绑定的 preset id（模板或副本 id 均可，R5）；缺省走 roster `default`；记录**resolved** id |
| createTime | 资源时间戳 |

**状态转移**：

```text
none ──CreateConversation──▶ live(preset P)
  │                              │
  │ 同 P 再 create：幂等 no-op     ├─ SendMessage*（round 循环，047 既有）
  └ 异 P 再 create：dispose 旧 agent ──▶ live(preset P')（在途 round 失败返回，R4）
                                 └─ SendMessage(未创建)：FAILED_PRECONDITION（FR-002）
```

- preset 绑定在创建时确定、会话存活期不变（US1 场景 4）；组合由 roster standing mount 提供。

## 4. 实体关系

```text
PresetTemplate(部署数据) ──copy-then-patch──▶ AuthoredPreset(store 记录 + writable root 副本)
                                                    │
Conversation ──── resolve(preset id) ───────────────┘（模板或副本）
     │
     └── mount(preset) ──▶ roster standing mount（同 preset 会话共享一份组合注册）
```

- Conversation → preset 是**引用**（id），不复制内容；删除 preset 不影响已创建会话（V4-3）。
