# Contract: planner 长期记忆快照——更新时间倒排注入最近 10 条

**Feature**: specs/065-agent-v2-team-refine/spec.md（FR-007，US3）
**实现锚点**: `common/js/dsh-plugins/memory-service/src/{client,snapshot}.ts`
**基线契约**: `specs/064-memory-split-fold-remain/contracts/dsh-plugins.md` §2（快照固定与 section）——注入策略增量修订，冻结时机与写路径零改动。

## §1 条目更新时间捕获（client）

1. `MemoryEntry` 增可选字段 `updateTime?: number`（epoch ms 整数）。
2. `listMemories` 解析 proto `Memory.update_time`：proto-loader（当前选项 `longs: String`）下 Timestamp 以 `{seconds: string, nanos: number}` 到达——归一化 `Math.round(Number(seconds) * 1000 + nanos / 1e6)`。
3. 服务未返回/不可解析（字段缺失、seconds 非数值）→ `updateTime` 保持 undefined（不 throw——旧快照/异常数据降级为"最旧"，见 §2）。
4. `memory_id` 仍仅用于内部定位，MUST NOT 渲染进任何模型可见文本（既有义务）。

## §2 快照渲染（snapshot）

`renderMemorySnapshot(entries)` 终态语义：

1. **排序**：按 `updateTime` 降序（undefined 视为最旧，排最后）；并列（相同 updateTime 或同为 undefined）以 `memory_id` 升序打破——结果确定，不因调用时点漂移。
2. **截取**：仅保留排序后前 **10** 条；不足 10 条全量；0 条渲染空串（section 不渲染——既有语义）。
3. **渲染**：`长期记忆：` 头 + 每条一行 `entry.content`（原样，不标注条目总数/截断元信息）。
4. **调用点不变**：`service.ts` 的 `load` 预取时渲染一次并冻结（实例生命周期固定，运行中写入待下次物化生效——064 §2 既有语义）；`applyCall` 写路径与 `old_text` 定位面向全量存储，不受截断影响。

## §3 边界

- 条目数 > 10：仅最近更新的 10 条进入 system prompt；被截断条目仍可经 memory 工具操作（存储全量）。
- 全部条目 updateTime 并列（如一次批量导入）：memory_id 升序的前 10 条。
- Go memory 服务零改动（proto `Memory.update_time` 已由 `memoryToProto` 返回，`projects/game/memory/handler/handler.go:219-234`）。

## §4 验收面（对应 spec FR-007、SC-003）

单测（memory-service）：

- client：`{seconds: "1757900000", nanos: 5e8}` → `1757900000500`；缺字段的响应条目 → undefined。
- snapshot：15 条互异 updateTime → 恰最近 10 条、降序；3 条 → 全量降序；并列 updateTime → memory_id 升序；空 → 空串；`memory_id` 不出现在输出。
- service 回归：load 冻结、写后快照不变（064 既有用例）。
