# Revision: ChatEvent step 序号 1 起裁定（phase11-step-numbering）

**Feature**: [spec.md](../spec.md) | **日期**: 2026-09-04 | **性质**: 执行期契约修订（Phase 12 T027 大型测试 `TestAgentV2StepSegmentedBlocksAndHistory` 失败暴露；本文为裁定与契约修订的权威描述）

**状态**: 054 契约中"turn 内从 0 起"的 step 起始值表述为**设计时概念混淆**——dsh 生态既定语义为 **1 起**。本文裁定契约修订为 1 起；实现（driver 步进、history 透传、store 缺省）终态正确、零改动，仅契约文本、代码注释与测试期望修订。

---

## 0. 缺口定性（已核实）

### 0.1 缺陷表现

T027 执行 `guitar run projects/game/testplan/system_test.yaml` 时，`TestAgentV2StepSegmentedBlocksAndHistory`（`projects/game/testplan/agent_v2_conversation_test.go`）失败：三步游戏链（init 调用 → operate 调用 → 总结正文）的块事件实际携带 `step = 1/2/3`，用例按 [contracts/agent-api-changes.md](../contracts/agent-api-changes.md) §1 的"0 起单调"契约期望 `step = 0/1/2`。

### 0.2 根因：契约"0 起"是设计时概念混淆，方案偏离现实

step 序号并非 054 新造，而是 dsh session 事件的既有事实（服务端 step 循环序号透传，[data-model.md](../data-model.md) §1.1）。054 设计时把 **proto3 int32 的缺省值 0**（字段未设置时的序列化缺省）误当作**序号基**，据此写下"从 0 单调递增"的契约表述——把"字段缺失哨兵"与"序列起始值"混为一谈。被观察的真实链路自始就是 1 起。

---

## 1. 证据链（全部实读核实）

1. **dsh-session invariant checker 强制 1 起**：`@deepseek-ai/dsh-session`（catalog pin 0.1.1-rc.2）`lib/types/invariant.js` 的 trace 不变量——`freshTrace()` 初始 `nextStep: 1`；`turn/start` 事件处置 `nextStep = 1`；`step/start` 事件校验 `event.data.step !== trace.nextStep` 即 `fail("step/start expected step ${trace.nextStep} …")`；每步收束后 `nextStep += 1`。官方校验器下 step 序列只能是 1, 2, 3, …（包主页 https://www.npmjs.com/package/dsh-session ，源仓库 https://github.com/deepseek-ai/deepseek-harness ）。
2. **官方 loop 步进语义 1 起**：step 循环实现为 `const step = phase.step + 1`（`phase.step` 初始 0）——首个 step 即 1，`step/start` 与后续 per-step 事件携带同一序号。本仓库 driver 对齐该语义：`common/js/dsh-plugins/saolei-loop/src/driver.ts:679`（`phase.step` 初始 0 见 :746）。
3. **051 交付即 1 起**：commit `4d2402a`（051 phase 3）引入 driver 时步进即为 `phase.step + 1`；同 commit `driver.test.ts` 即断言 `{ turn: 1, step: 1 }`（现行 `common/js/dsh-plugins/saolei-loop/src/driver.test.ts:377` 同形态）。
4. **哨兵无歧义性**：1 起语义下 0 永不为真实 step 值，可无歧义地充当"字段缺失哨兵"——事件 `data.step` 缺失时置 0（`projects/game/agent_v2/src/history.ts` 的 `chunkToChatEvent` 缺省参数、`projects/game/web/frontend/src/store/chat.ts` 的 `step ?? 0` 归组），消费端据此走退化路径而不与真实序号混淆。

---

## 2. 裁定

**结论 A：方案偏离现实——修订契约为 1 起（dsh 生态既定语义），实现零改动。**

- 契约文本（[data-model.md](../data-model.md) §1.1、[contracts/agent-api-changes.md](../contracts/agent-api-changes.md) §1）、proto 字段注释（`projects/game/agent_v2.proto` BlockStart/Delta/End 的 step 字段）、前端类型注释（`projects/game/web/frontend/src/api/conversation.ts`）修订为"1 起单调（服务端 step 循环序号透传；0 仅作字段缺失哨兵）"。
- 实现终态正确、不动：driver 的 1 起步进（对齐官方）、history.ts 的 chunk→ChatEvent step 透传、store/chat.ts 的 `?? 0` 缺失哨兵归组。
- 测试期望修订：T024/T027 大型测试 `wantSpans` 期望 `0/1/2` → `1/2/3`；前端多 step 流式 mock 的 wire 真值对齐 1 起（"缺 step 归组 0"退化路径用例保留——它们测的正是哨兵语义）。
- 备选否决：**修订实现为 0 起**——要求 driver 偏离官方 loop 语义（invariant checker 直接拒绝），且 051 既有测试与历史事件全部为 1 起，为迁就一处契约笔误重写生态事实，方向颠倒，弃。

---

## 3. 契约修订文本（终态）

### 3.1 data-model.md §1.1（3 处）

(1) proto 摘录注释改为：

```proto
int32 step = <next>;  // 块所属的模型输出步骤序号（turn 内从 1 单调递增，服务端 step 循环序号透传）
```

(2) 语义 bullet 改为（明确与服务端 session 事件序号同源）：

```markdown
- 语义：turn 内 step 序号与服务端 step 循环一致——即 dsh session 事件（step/start、assistant/chunk）携带的 step 序号，turn 内从 1 单调递增；同 step 的块共享序号；step 变化即新分段。
```

(3) 兼容性 bullet 的"置 0"明确为缺失哨兵：

```markdown
- 兼容性：proto3 可选字段——旧客户端忽略未知字段（051 既有 forward-compat 方向不变）；事件 `data.step` 缺失时置 0——该 0 仅为字段缺失哨兵（真实 step 自 1 起，0 永不为真实 step 值）。
```

节末追加裁定引用一行：

```markdown
- step 起始值裁定见 [revisions/phase11-step-numbering.md](revisions/phase11-step-numbering.md)。
```

### 3.2 contracts/agent-api-changes.md §1（1 处 + 引用行）

§1 表首行语义列改为：

```markdown
| `blockStart` / `delta` / `blockEnd` | +`int32 step` | turn 内模型输出步骤序号（1 起单调——服务端 step 循环序号透传；0 仅作字段缺失哨兵）；同 step 块共享；step 变化=新分段 |
```

节末追加：

```markdown
- step 起始值裁定见 [revisions/phase11-step-numbering.md](revisions/phase11-step-numbering.md)。
```

---

## 4. 代码与测试修订（终态）

| 文件 | 修订 |
|---|---|
| `projects/game/agent_v2.proto` | BlockStartEvent/BlockDeltaEvent/BlockEndEvent 的 step 字段注释：`(0-based, monotonic within a turn)` → `(1-based: the server step loop's sequence number, monotonic within a turn; 0 is only the missing-field sentinel)`；保留对 agent-api-changes.md §1 的引用。codegen 后 gateway/proxy 零源码改动（注释不参与生成类型面） |
| `projects/game/web/frontend/src/api/conversation.ts` | blockStart.step 注释改为"turn 内从 1 单调递增（服务端 step 循环序号；0 为缺失哨兵）；分组维度，index 仍为块序维度" |
| `projects/game/testplan/agent_v2_conversation_test.go` | `TestAgentV2StepSegmentedBlocksAndHistory`：`wantSpans` 期望 step `0/1/2` → `1/2/3`；链路注释与报错文案同步（init call step 1 → operate step 2 → summary step 3） |
| `projects/game/web/frontend/src/components/ChatView.test.tsx` | 全部正常路径 mock 的显式 step 值 1 起：`liveOf` helper 与流式/live 用例的 step mock 均为 1 起值；仅"缺 step 归组 0"退化路径用例保留 0（哨兵语义本身） |
| `projects/game/web/frontend/src/store/chat.test.ts` | 全部正常路径事件 mock 的显式 step 值 1 起（step 路由/历史投影/tool_result 关联/ERROR/CANCELED 用例）；保留"缺 step 归组 0"（stepless 事件）退化用例 |

不改动（终态已正确）：`common/js/dsh-plugins/saolei-loop/src/driver.ts`（1 起步进）、`projects/game/agent_v2/src/history.ts`（透传 + 缺省哨兵）、`projects/game/web/frontend/src/store/chat.ts`（`?? 0` 归组）、`history.test.ts`/`session.test.ts`/`driver.test.ts`（已为 1 起形态）。

---

## 5. 验收

1. 清理自查：`rg -n "0-based|0 起单调|从 0 单调" specs/054-agent-v2-bugfixes/ projects/ common/js/dsh-plugins/`——除本文档（裁定记录）与历史 spec/无关域（扫雷棋盘坐标 0 起、reasoning chunk 0 起）外，step 契约面零命中。
2. proto 注释修订后 `bazel build //projects/game/agent_v2/... //projects/game/gateway/... //projects/game/proxy/...` 通过（codegen 刷新）。
3. `bazel test //projects/game/agent_v2/... //projects/game/web/frontend/...` 全绿（含前端 mock 对齐用例）；`bazel build //projects/game/testplan/...` 通过。
4. T027 重跑（executor）：`TestAgentV2StepSegmentedBlocksAndHistory` 期望与实际一致（step 1/2/3）。
