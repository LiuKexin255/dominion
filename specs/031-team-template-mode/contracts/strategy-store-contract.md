# Contract: Strategy 长期记忆持久化（agent mongo-backed memory）

**Feature**: `031-team-template-mode` | **Spec**: [`spec.md`](../spec.md) | **Research**: D4（修订 #5）

> 策略（长期记忆）由 **agent 服务自身**持久化到 MongoDB（当前 mongo 实例）——agent 实现 mongo-backed memory store，team graph 经 `StrategyStore` 接口访问。**不经 prompt 服务**（prompt 服务仅管 TeamProfile 静态配置）。满足 directive ③"存数据库"+ 修订 #5"agent 服务通过 memory 存储在数据库中"。

---

## 1. agent 侧接口（`projects/game/agent/src/strategy-store.ts`）

```ts
/** 策略长期记忆存储接口（team graph 依赖，DI；便于测试用 fake）。 */
export interface StrategyStore {
  /** 取策略；无记录返回空字符串 ""（需求方 #3）。 */
  get(sessionId: string): Promise<string>;
  /** 写/更新策略（planner update_strategy 调用）。 */
  put(sessionId: string, content: string): Promise<void>;
}
```

- team graph（player/planner 节点、`update_strategy` 工具）依赖此接口（DI）。
- 生产 impl：`MongoStrategyStore`（agent 直连 mongo）。
- 测试 impl：fake（内存 Map）。

## 2. 生产实现：agent 直连 mongo（memory store）

- agent（TS）新增 **mongo 客户端依赖**，连接**当前 mongo 实例**（与 prompt 服务同一 mongo；连接配置经 secrets，类同 prompt 服务连法）。
- impl 为 mongo-backed memory store：`get` 按 session_id 查；无文档返回 `""`；`put` 按 session_id upsert。
- 是否进一步对齐 LangGraph `BaseStore`（`runtime.store` 访问）属 impl 细节（research D4）；契约仅约束 get/put 语义 + mongo 持久 + 初始 `""`。

## 3. mongo 文档形状（`strategies` 集合）

```text
{
  _id: ObjectId,
  session_id: string,          // 唯一索引（FR-013 键）
  content: string,             // 策略文本（初始无文档 → get 返回 ""）
  create_time: ISODate,
  update_time: ISODate
}
```

- 唯一索引：`session_id`。
- upsert 语义：`put` 用 session_id 为键 upsert（存在则更新 content+update_time，否则插入）。
- 集合由 agent 服务创建/使用；与 prompt 服务的 `team_profiles` 集合同库不同集合。

## 4. 初始值（需求方 #3）

- `get(sessionId)` 无记录返回 **空字符串 `""`**。
- **不**存在"模板内嵌初始策略"——策略内容由 planner 首次 `update_strategy` 写入。
- planner 的 system 上下文 = [复盘指令（模板定义）] + [当前策略，初始 `""`]；player 的"当前态势"注入同理。

## 5. 生命周期与一致性

- 键 = session id（`teamMemoryId` = session id，FR-013）。
- 跨局累积、跨 turn 持久、跨进程重启持久（mongo）。
- `RefreshTeam`（FR-018）**不影响**策略（仅清短期 messages）。
- **session 删除暂不级联清理 strategy**（需求方 #7：strategy 管理后续优化；可留孤儿文档，后续再加清理）。

## 6. 备选（D4，记录）

- ❌ 经 prompt 服务 gRPC 中转（原 D4 初版）：语义过载 prompt 服务（修订 #5 已否决）。
- ❌ 进程内 `InMemoryStore`：不满足 directive ③（重启即失）。
- 注：`StrategyStore` 接口已隔离存储 impl；未来可切换为 LangGraph `BaseStore` 或其他后端。

## 7. 验证要点

- `get(无记录)` 返回 `""`（非 null/undefined）。
- `put` 后 `get` 返回新策略；进程重启后仍可读（持久）。
- 策略以 session id 隔离（不同 session 互不干扰）。
- `RefreshTeam` 后策略仍可读（FR-018）。
- prompt 服务**不**参与 strategy 读写（仅 TeamProfile）。
