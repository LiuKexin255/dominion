# Revision: 失败回合部分文本尾块折叠缺口补充设计（phase4-failed-turn-folding）

**Feature**: [spec.md](../spec.md) | **日期**: 2026-09-03 | **性质**: 执行期缺口补充设计（Phase 4 T007–T009 review 发现；本文为设计产物，不含代码变更）

**状态**: 本文是失败/终止回合"部分文本尾步是否折叠"缺口的裁定与落地方案权威描述。tasks.md 修订文本见 §6（由执行者应用，本文不直接修改 tasks.md）；契约/设计文档修订文本见 §5（随 T009b 落地）。

---

## 0. 缺口定性（已核实）

### 0.1 缺陷表现

LLM 流中途失败（或 finish error）的 ERROR 回合，若尾步为**部分正文**（含非空 text、无 tool-call），该尾步命中 `projects/game/web/frontend/src/components/ChatView.tsx` 的最终答案内容判定（`isFinalAnswer`，:137–142——纯内容形态：最后一个含非空 text 且无 tool-call 的 step），`finalIndex > 0` → 此前的过程步骤（思考、工具调用）被折叠进"思考过程"摘要区（:158–176）。本地（turn_end{ERROR} 投影后）与回填（List）两路径同样命中——同一判定函数。

### 0.2 根因：前端无法从内容形态区分"完整正文（COMPLETED）"与"中断的部分正文（ERROR/CANCELED）"

HistoryMessage 无回合终态/interrupted 信号：

- **本地路径**：store 投影时知道 `turnEnd{ERROR}`（T008 已实现保留），但 `stepsToHistory`（`projects/game/web/frontend/src/store/chat.ts:179`）投影出的 HistoryMessage 无任何标记。
- **回填路径**：服务端 `SessionHistory.appendAssistant`（`projects/game/agent_v2/src/history.ts:273`）忽略 `assistant/message` 事件 data 的 `interrupted: true`（T007 driver 已固化该标记，见 `common/js/dsh-plugins/saolei-loop/src/driver.ts:779-801`）；`projects/game/agent_v2.proto` 的 `HistoryMessage`（:507，字段 1–4：message_id/role/create_time/blocks）不携带任何失败信号，List 响应无从推导。

### 0.3 与 spec 的冲突（四处，均实读核实）

| spec 表述 | 位置 | 内容 |
|---|---|---|
| FR-005 | [spec.md](../spec.md) :204 | "无最终答案的回合（**失败**/被终止/以纯工具调用结束）MUST 保持全部过程内容可见"——括号明确把"失败"归入无最终答案 |
| US2 场景 5 | [spec.md](../spec.md) :67 | "以无最终答案的方式结束（失败/被终止/以纯工具调用结束）…全部过程内容保持可见（不折叠）" |
| 回合终态处置 | [data-model.md](../data-model.md) §5.2 | ERROR → "错误提示独立，**不折叠（无最终答案）**"；CANCELED 同 |
| 渲染契约 | [contracts/web-ui.md](../contracts/web-ui.md) §2.2 | "无最终答案（ERROR/CANCELED/纯工具结束）→ 全部过程可见，不折叠" |

另 [spec.md](../spec.md) A1（:268）以官方 `'assistant-step'` 节点 **running/settled/interrupted 三态**为对齐基线——官方语义中 interrupted 步骤非 settled 终态答案（[research.md](../research.md) D2"中断内容"行）。当前实现缺的正是 interrupted 三态中的第三态在前端折叠判定上的表达。

### 0.4 Phase 4 现状（未提交，git diff 已核实）

- T007：driver ERROR/finish-error 路径统一 `appendInterrupted()`——append `assistant/message`（`interrupted: true`）进 session log ✅（服务端已有该事实，history.ts 尚未消费）。
- T008：store `turn_end{ERROR}` 保留 steps 入本地历史（`stepsToHistory` 投影，RUNNING tool-call 原样）✅。
- T009：ChatView historical 语境陈旧 RUNNING 工具块 → INTERRUPTED 呈现 ✅；`CompletedTurn` 折叠判定未考虑回合失败 ❌（本缺口）。

---

## 1. 裁定一：失败/终止回合的部分文本尾步**不是最终答案**

**结论：确认 spec 语义——不算最终答案，回合保持全部过程内容可见、不折叠。CANCELED（Phase 6）同样适用。spec 无需修订。**

依据（按 spec 为准绳）：

1. **FR-005 的 MUST 语言 + 括号分类**（spec.md :204）：括号把"失败"与"被终止/纯工具调用结束"并列为"无最终答案"的三种形态——分类输入是**回合终态**，不是尾步内容形态。部分正文尾步恰是"失败回合的过程证据"（US4 Why："失败回合的内容恰恰是排查链路问题最需要的证据"），折叠它违背 FR-005 的字面与目的。
2. **场景 5（spec.md :67）**："回合结束**或回填**"两时机都要求全可见——本地与回填两路径都必须不折叠。
3. **A1 官方三态基线（spec.md :268）**：`'assistant-step'` 的 interrupted 态区别于 settled 态；最终答案判定只应落在 settled 步骤上。部分正文是 interrupted 步骤，非终态答案。research.md D2 已裁定"对齐官方 interrupted 语义"。
4. **四份文档相互一致**：spec.md / data-model.md §5.2 / web-ui.md §2.2 / A1 无任何内部矛盾——冲突是**实现偏离 spec**，不是 spec 缺陷，因此不需要 spec 修订（也就不存在"给 spec 一句话修订"的必要；若强行把 spec 改成纯内容判定，将同时违反上述四处 MUST/基线表述，见 §2 备选 C）。

---

## 2. 裁定二与方案选定：`HistoryMessage.interrupted` 信号传递

### 2.1 选定方案（A）：proto `HistoryMessage` 增加 `bool interrupted = 5`

消息级单 bool 字段：服务端 `SessionHistory.appendAssistant` 记录 driver 事件 data 的 `interrupted`、List 透出；前端 `HistoryMessage` 类型同构扩展、折叠判定排除 interrupted 消息；本地路径 store 投影时对尾步消息同构标记。细节见 §3。

命名对齐三重先例：driver 事件 data 的 `interrupted: true`（T007/abort 既有）、官方 `'assistant-step'` 的 interrupted 态（A1 基线）、`game.proto` v1 面 PartCompletion 的"interrupted"语义（`projects/game/proto_test.go` :413 起）。备选命名（`partial`/`incomplete`）无额外表达力且失 Alignment，弃。

### 2.2 备选否决

| 备选 | 内容 | 否决理由 |
|---|---|---|
| B | 仅本地标记（store 投影标记），回填维持纯内容判定 | 直接违反 FR-013（"刷新后回填所见与流式结束时所呈现的一致"）与 US2 场景 4/US4 场景 1 的一致性方向——同一失败回合流式时不折叠、刷新后折叠，重演本 feature 要消除的"流式与回填形态不一致"（spec Motivation 第 1 缺陷） |
| C | 修订 spec 为官方内容判定（部分正文尾步算最终答案、折叠） | 与 FR-005 括号 MUST、场景 5、data-model §5.2、web-ui §2.2、A1 interrupted 三态基线**五处**直接冲突（§1）；且伤害失败回合的可诊断性。spec 无内耗，不应为迁就实现而改准绳 |
| D | 服务端 List 时按回合终态推导标注（collector 记 turn outcome，List 时标每回合末条消息） | `SessionHistory` 是纯消息追加模型、无回合边界/终态状态；为推导需新增 turn 跟踪状态，复杂度高于在 append 时消费**已存在**的事件 data 字段（T007 已固化），零信息增益 |
| E | 块级信号（`TextBlock.completion` 枚举，仿 v1 PartCompletion） | 一个 bit 的回合级事实要扩多个块消息；且与 §5 中 data-model §2"ToolStatus 不改 proto"的呈现层推导裁定相互缠绕。消息级字段最小 |

### 2.3 消费面影响评估（字段扩展 = additive，无破坏）

- **protojson**：proto3 默认 false 时字段缺省（grpc-gateway protojson 仅在 true 时输出 `"interrupted": true`）——正常回合的 List 响应字节形态不变；旧消费端按既有 forward-compat 方向忽略未知字段（049/051 契约面零变化，纯扩展同 [contracts/agent-api-changes.md](../contracts/agent-api-changes.md) §1 step 字段先例）。
- **gateway/proxy**：字段扩展经既有透传自动生效，零源码改动（同 §4 `desktop_connected` 的既有结论"proto 字段扩展经既有透传自动生效"）；Go 侧 `game_go_proto`（`projects/game/BUILD.bazel`）随 bazel codegen 重生成，proxy 转发不触碰该字段、gateway protojson 序列化为生成代码通用路径。
- **proto_test.go**（`projects/game/proto_test.go`）：无 HistoryMessage 穷举断言（实读核实），不需更新。
- **agent_v2 内存态**：051 A2 内存历史，无持久化迁移。
- **测试影响**：`appendAssistant` 采用**稀疏落字段**（仅 true 时写 `interrupted`），既有 `history.test.ts`/`session.test.ts` 的 per-field/`toEqual(blocks)` 断言零破坏；仅 §4 新增用例与两处 store 既有 ERROR 投影断言需补字段（chat.test.ts :490、:514）。

### 2.4 data-model §2"不改 proto"句的适用边界（澄清）

该句（data-model.md :75）的主语是**块级**："toolCall status 仍为 RUNNING 的陈旧块由前端在回填时按消息终态推导，不改 proto"——它裁定的是**不为 ToolCallBlock/ToolStatus 增加中断枚举值**（陈旧工具块的中断终态由呈现层推导）。本补充设计加的是**消息级** interrupted 信号（HistoryMessage 新字段），与该句不冲突；但句子易被误读为"历史面完全不改 proto"，§5 给出改写文本显式划界。

---

## 3. 变更细节（终态形态）

### 3.1 proto（`projects/game/agent_v2.proto`）

`HistoryMessage`（:507）追加字段 5（1–4 已占用，实读核实）：

```proto
message HistoryMessage {
  // Server-assigned per-session sequence id (e.g. "m1", "m2").
  string message_id = 1;
  Role role = 2;
  google.protobuf.Timestamp create_time = 3;
  repeated ContentBlock blocks = 4;
  // True when this assistant step's content is an interrupted prefix: the
  // LLM stream failed or was cancelled before the step settled, so the
  // step carries no final answer and failed turns stay unfolded
  // (specs/054-agent-v2-bugfixes/data-model.md §2/§1.5, FR-005). Absent
  // for settled steps and user messages.
  bool interrupted = 5;
}
```

codegen 验证门禁沿用 Phase 2 形态：`bazel build //projects/game/agent_v2/... //projects/game/gateway/... //projects/game/proxy/...`。**与 Phase 2 的关系**：Phase 2 已闭合（T002 [x]），本字段扩展不重开 Phase 2、不影响其"无行为变更"检查点（历史交付不追改）；落点为 Phase 4 新 task T009b（§6），先例即 [revisions/phase2-proxy-cancel.md](phase2-proxy-cancel.md) §1 的 T013b 模式（晚发现的 proto 依赖面在消费 phase 以字母后缀 task 落地）。

### 3.2 服务端（`projects/game/agent_v2/src/history.ts`）

- `AssistantMessageEvent.data` 补 `interrupted?: boolean`（driver T007 已在事件 data 顶层写入该键，类型只是补声明）。
- `SessionHistory.appendAssistant(content, interrupted = false)`：仅 `interrupted === true` 时在 push 的 HistoryMessage 上落 `interrupted: true`（稀疏——proto3 默认缺省语义，且保既有测试断言零破坏）。
- `TurnCollector.onSessionEvent`（:450–459）：`this.history.appendAssistant(message.content, (event as AssistantMessageEvent).data.interrupted === true)`。
- `server.ts` ListAgentMessages（:437）**零改动**：messages 原样透传，proto-loader 按加载的 proto 定义序列化新字段。

### 3.3 前端类型（`projects/game/web/frontend/src/api/conversation.ts`）

`HistoryMessage` 接口补 `interrupted?: boolean`（protojson 投影，camelCase 同名）。

### 3.4 store 本地路径（`projects/game/web/frontend/src/store/chat.ts`）

`stepsToHistory(steps, interrupted)`：`interrupted` 为 true 时**仅尾步（最后一个 step）消息**标记 `interrupted: true`，此前 step 不标记；`turn_end{ERROR}` 传 true（`turn_end{COMPLETED}` 保持 false/缺省；Phase 6 `turn_end{CANCELED}` 复用同路径传 true——T014 无需再设计）。steps 为空时不投影（不变）。

尾步标记与回填形态的对齐分析（已核实服务端 append 时序）：

| 场景 | 服务端 | 本地（尾步标记规则） | 一致性 |
|---|---|---|---|
| 流中途死亡，尾步部分正文（本缺口主场景） | 尾步 append interrupted 前缀 | 尾步标记 | ✅ 一致，两路径均不折叠 |
| finish error（waterfall 无 retry），前缀内容形态完整 | 尾步 append interrupted 前缀（`interruptedBlocks()` 保留 text/think） | 尾步标记 | ✅ 一致（"完整形态"仍是中断前缀，非终态答案） |
| 工具执行异常（step 消息已正常 append，含 tool-call） | 尾步消息**无** interrupted（正常 append），但含 tool-call → 内容判定已无最终答案 | 尾步标记（多标） | 折叠结果两路径一致（均不折叠）；标记差异不呈现（字段无渲染面），无害 |
| 失败发生在下一 step 起步（preStep/buildRequest 抛错，尾步为已完成的纯正文 step） | 尾步消息无 interrupted → 回填按内容判定折叠 | 尾步标记 → 本地不折叠 | ⚠️ 理论分歧（本地宽、回填严）。可达性极窄：纯正文 stop 的 step 正常收 turn（COMPLETED），需 max-tokens/turn-stopping steer 重开下一 step 且其起步即失败。两形态内容零差异（仅折叠摘要差异），接受为已裁定边界（§5 data-model 文本记一句） |

规则取向依据：本地"多标"（宁可不折叠）方向与 FR-013"不清空、不原地消失"、FR-005"失败回合全可见"的保护方向一致；"少标"会在 case 2（finish error，真实高频路径）本地折叠失败回合，直接违反场景 5。

### 3.5 折叠判定（`projects/game/web/frontend/src/components/ChatView.tsx`）

`isFinalAnswer` 从内容判定改为内容 + 信号判定（签名从 blocks 改为 message）：

```tsx
// isFinalAnswer 判定一个 step 是否为回合的最终答案：含非空 text 块且无
// tool-call 块，且非 interrupted（specs/054-agent-v2-bugfixes/contracts/
// web-ui.md §2.2 折叠规则；interrupted 消息是中断前缀、非终态答案——
// specs/054-agent-v2-bugfixes/revisions/phase4-failed-turn-folding.md §1，
// A1 官方 'assistant-step' interrupted 三态基线）。
function isFinalAnswer(message: HistoryMessage): boolean {
  return (
    !message.interrupted &&
    !message.blocks.some((b) => b.toolCall !== undefined) &&
    message.blocks.some((b) => (b.text?.content ?? '').trim() !== '')
  )
}
```

`CompletedTurn` 扫描循环传 `messages[i]`；trailing 防御分支不变。判定语义：interrupted 消息**不进入最终答案候选**——正常 COMPLETED 回合无 interrupted 消息，行为零变化（既有折叠用例即回归门禁）。

---

## 4. 测试义务（T009b 内嵌，constitution IV）

| 层 | 文件 | 断言 |
|---|---|---|
| 服务端记录 | `projects/game/agent_v2/src/history.test.ts` | `appendAssistant(content, true)` → list() 末条 `interrupted === true`；缺省调用不落字段（稀疏）；TurnCollector：emit `assistant/message`（data.interrupted: true）→ 历史末条标记（List handler 为原样透传、server.test.ts 已覆盖形态，不需 handler 级新用例） |
| store 投影 | `projects/game/web/frontend/src/store/chat.test.ts` | `turn_end{ERROR}` 多 step 投影：尾步消息 `interrupted: true`、此前 step 无标记；更新既有两处 ERROR 投影断言（:490、:514 补字段）；COMPLETED 投影无标记（既有用例即回归） |
| 渲染（回填路径） | `projects/game/web/frontend/src/components/ChatView.test.tsx` | 直接渲染含 `interrupted: true` 尾步（部分正文、无 tool-call）的多 step history → 无 `turn-process-toggle`、全部 step 可见、正文可见 |
| 渲染（本地路径） | 同上 | 经 store 驱动（applyEvent 流）构造 ERROR 部分正文尾回合 → 同断言；并与同构造的回填渲染形态一致（FR-013） |
| 渲染（回归） | 同上 | 既有 COMPLETED 折叠/展开/计数用例零回归（无 interrupted 字段时判定不变） |
| driver（minor b） | `common/js/dsh-plugins/saolei-loop/src/driver.test.ts` | finish{error} + waterfall 等待期间 abort → interrupted `assistant/message` 已 append、turn 以 aborted 收束（§7-2） |

testplan 级：T024 既有"注入 LLM 失败的 ERROR 回合已产出内容回填可见"用例补一条断言——List 响应尾步 `interrupted=true`（§6 修订 5）。

---

## 5. 契约/文档同步修订文本（随 T009b 落地）

**spec.md：零修改**（§1 裁定：spec 一致，实现偏离）。

### 5.1 data-model.md（4 处）

**(1) 新增 §1.5**（插于 §1.4 之后、`## 2.` 之前）：

````markdown
### 1.5 HistoryMessage 扩展 interrupted（FR-005）

`HistoryMessage`（:507）新增：

```proto
bool interrupted = 5;  // 该 assistant step 为中断前缀（流失败/终止前已产出）
```

- 语义：true 表示该消息内容是 §2 的中断固化前缀（`interrupted: true` append），非终态答案；仅 agent 消息可为 true（user 消息恒缺省）。消费面：web 折叠判定排除该消息（FR-005 无最终答案 → 全可见不折叠，前端无法从内容形态区分完整正文与中断部分正文——信号经本字段传递）。
- 传递：driver `assistant/message` 事件 data 的 `interrupted: true` → `SessionHistory.appendAssistant` 记录 → List 响应透出；本地路径 store 投影对尾步消息同构标记（§5.2）。
- 兼容性：proto3 默认 false 缺省（protojson 仅 true 时输出）；字段扩展经 gateway/proxy 既有透传自动生效（同 §1.4 `desktop_connected`）。设计裁定见 [revisions/phase4-failed-turn-folding.md](revisions/phase4-failed-turn-folding.md)。
````

**(2) 替换 §2 第三 bullet**（" `- SessionHistory.appendAssistant`/回填路径零改动（session-lifetime 收集既有）。` "）为：

```markdown
- `SessionHistory.appendAssistant` 记录事件 data 的 `interrupted`（§1.5）；List 透出。收集时机不变（session-lifetime 既有）。
```

**(3) 替换 §2 末 bullet**（" `- 回填后失败/终止回合的呈现：…不改 proto）。` "）为：

```markdown
- 回填后失败/终止回合的呈现：无最终答案 → 全部过程可见不折叠（FR-005）——判定基准为内容形态 + `HistoryMessage.interrupted`（§1.5）：内容形态无法区分"完整正文（COMPLETED）"与"中断部分正文（ERROR/CANCELED）"，前端最终答案判定排除 interrupted 消息。
- 无 result 的工具块按中断终态呈现（Edge Cases 既有裁定，回填侧 ToolCard 状态映射补 INTERRUPTED 呈现——toolCall status 仍为 RUNNING 的陈旧块由前端在回填时按消息终态推导）。此处的"不改 proto"指**块级** ToolStatus 枚举与块状态不加中断值；消息级 interrupted 信号是 §1.5 的独立字段扩展，二者边界如此。
- 两形态边界（已裁定）：中断固化只保留 text/think 安全前缀（assembler `interruptedBlocks()` 丢弃未派发的 tool-call，不虚构其参数与结果），mid-tool-call 流死亡的尾步本地保留 RUNNING tool-call draft（呈现"已中断"卡片，FR-013 不原地清空）、回填无该块（刷新后卡片消失）；失败发生在下一 step 起步时（尾步为已完成的纯正文 step），回填按内容判定折叠而本地不折叠。正文/思考前缀在上述边界外两路径一致，折叠分歧仅影响摘要形态、内容零丢失。
```

**(4) §5.2 表**：COMPLETED 行（:122）"最终答案 step（最后一个含非空 text 块且无 tool-call 块）"改为"最终答案 step（最后一个含非空 text 块且无 tool-call 块**且非 `interrupted`** 的 step——§1.5）"；ERROR 行（:123）"已呈现 step 入历史；未完成尾块 interrupted"改为"已呈现 step 入历史；尾步消息标记 `interrupted: true`（§1.5，未完成尾块原样投影）"（CANCELED 行"同 ERROR"自动继承）。

### 5.2 contracts/agent-api-changes.md（§6 整体替换）

```markdown
## 6. 服务端历史固化（FR-012，实现面契约）

- `@dominion/dsh-saolei-loop` driver：LLM 流失败/finish error 抛错前，assembler 有部分内容则 append `assistant/message`（`interrupted: true`）——与既有 abort 路径同构；finish error 经 `agent/request-error` waterfall 后 abort 的窗口同样固化（retry 已不可执行，[revisions/phase4-failed-turn-folding.md](../revisions/phase4-failed-turn-folding.md) §7-2）。
- `SessionHistory.appendAssistant` 记录事件 data 的 `interrupted`；List 响应以 `HistoryMessage.interrupted` 透出（proto 字段扩展，[data-model.md](../data-model.md) §1.5）：消费端据此判定失败/终止回合无最终答案（FR-005 不折叠）。字段扩展经 gateway/proxy 既有透传自动生效（§4 同类）。
- 验收锚点：注入 LLM 流失败的回合，其已流式内容经 List 回填可见且尾步 `interrupted=true`（tool-call 块的中断终态由消费端按消息终态推导）。
```

### 5.3 contracts/web-ui.md（4 处）

**(1) §2.1 ERROR bullet**（:28）改为：

```markdown
- `turn_end{ERROR/CANCELED}` → 已呈现 step 保留并入本地历史，尾步消息标记 `interrupted: true`（与回填 List 的 `HistoryMessage.interrupted` 同构——刷新前后判定一致，data-model §1.5），未完成尾块 interrupted 呈现；提示独立（error / "已终止"）。
```

**(2) §2.2 turn COMPLETED 行**（:37）："最终答案 step（最后一个含非空 text 且无 tool-call 的 step）"改为"最终答案 step（最后一个含非空 text 且无 tool-call 且非 `interrupted` 的 step——中断消息非终态答案，A1 interrupted 三态基线）"。

**(3) §2.2 无最终答案行**（:38）行尾追加：

```markdown
（判定基准：内容形态 + `HistoryMessage.interrupted` 标记——中断的部分正文不构成最终答案，data-model §1.5）
```

**(4) §8 义务清单**：第 1 条"ERROR/CANCELED 保留"后补"、ERROR/CANCELED 尾步 interrupted 标记投影"；第 2 条"无最终答案不折叠"后补"（含部分文本尾步，本地/回填两路径）"。

---

## 6. tasks.md 修订文本（6 处，由执行者应用；行号对应当前文件，应用时以引文锚点为准）

1. **Phase 4 Independent Test**（tasks.md:78，整行替换）：

   > **Independent Test**: `bazel test //common/js/dsh-plugins/saolei-loop/... //projects/game/agent_v2/... //projects/game/web/frontend/...`——driver interrupted 固化、HistoryMessage.interrupted 记录与 List 透出、store ERROR 保留与尾步标记、回填一致性（含失败回合不折叠）用例全绿

2. **Phase 4 文档清单·代码规范文档**（tasks.md:82，行尾追加）：

   > ；`style/api.md`；[AIP-140 Field names](https://google.aip.dev/140)（T009b 的 HistoryMessage 字段扩展）

3. **Phase 4 文档清单·技术文章/技术参考文档**（tasks.md:84，行尾追加）：

   > 、`specs/054-agent-v2-bugfixes/revisions/phase4-failed-turn-folding.md`（T009b 补充设计：失败回合折叠裁定与 interrupted 信号传递）

4. **新增 task**（插于 tasks.md:88 T009 行之后、Phase 4 Checkpoint 之前）：

   > - [ ] T009b [US4] 失败回合折叠缺口修复（依据 `specs/054-agent-v2-bugfixes/revisions/phase4-failed-turn-folding.md` §3）：`projects/game/agent_v2.proto`：`HistoryMessage` 增加 `bool interrupted = 5`（注释含 FR-005 语义；codegen 验证 `bazel build //projects/game/agent_v2/... //projects/game/gateway/... //projects/game/proxy/...`，gateway/proxy 零源码改动）；`projects/game/agent_v2/src/history.ts`：`AssistantMessageEvent.data` 补 `interrupted?: boolean`、`SessionHistory.appendAssistant` 增加 interrupted 参数（仅 true 落字段）、`onSessionEvent` 透传；`projects/game/web/frontend/src/api/conversation.ts`：`HistoryMessage` 补 `interrupted?: boolean`；`projects/game/web/frontend/src/store/chat.ts`：`stepsToHistory` 增加 interrupted 参数——ERROR 投影时仅尾步消息标记 `interrupted: true`（CANCELED 同构，Phase 6 T014 复用）；`projects/game/web/frontend/src/components/ChatView.tsx`：最终答案判定排除 interrupted 消息（`isFinalAnswer` 改收 message）；`common/js/dsh-plugins/saolei-loop/src/driver.ts`：finish-error waterfall 后 abort 窗口的 interrupted 固化补齐（revision §7-2）；契约文档同步（data-model.md §1.5/§2/§5.2、contracts/agent-api-changes.md §6、contracts/web-ui.md §2.1/2.2/§8，文本见 revision §5）；测试：`history.test.ts`（append 记录+TurnCollector 透出）、`chat.test.ts`（尾步标记+更新既有 :490/:514 断言）、`ChatView.test.tsx`（部分文本尾 ERROR 回合不折叠——本地/回填两路径、COMPLETED 折叠零回归）、`driver.test.ts`（waterfall abort 固化）

5. **T024 用例 clause**（tasks.md:223）："注入 LLM 失败的 ERROR 回合已产出内容回填可见"改为"注入 LLM 失败的 ERROR 回合已产出内容回填可见（断言尾步 `HistoryMessage.interrupted=true` 透出）"。

6. **Phase Dependencies·Phase 4 行**（tasks.md:257，行尾追加）：

   > ；T009b 为 review 补充任务（proto 仅涉 `HistoryMessage` 字段扩展，不重开已闭合的 Phase 2），依赖 T007–T009 顺序执行（driver/history/store/ChatView 同文件串行）

不改动：Phase 4 Checkpoint（"注入失败的回合内容'看过不再丢'，成功回合回填零回归"仍名实相符）；Parallel Opportunities（T009b 同文件串行，无新并行点）。

---

## 7. reviewer minor 项处置

### 7-1. mid-tool-call 流死亡的本地/回填形态差异——**裁定：保持现状（本地保留卡片），文档补边界说明**

- 本地：RUNNING tool-call draft 原样投影 → ChatView 历史语境推导为 INTERRUPTED 卡片（T009 已实现并有测试）。
- 回填：driver 中断固化经 `interruptedBlocks()` 只保留 text/think 安全前缀、丢弃未派发 tool-call（`common/js/dsh-plugins/saolei-loop/src/driver.ts:775-778` 注释：不虚构其参数与结果）→ 刷新后卡片消失。
- 处置依据：本地丢弃该卡片违反 FR-013"MUST 保留已流式呈现的分段内容（不清空、不原地消失）"；服务端补固化需要虚构未派发调用的参数与状态（违背历史"never fabricated"原则，`history.ts` settleToolResult 注释同源）。两害取轻：内容证据（正文/思考）两路径一致，卡片消失为可接受窄边界。文本已并入 §5.1(3) data-model §2 新 bullet，无额外文档改动。

### 7-2. driver waterfall 期间 abort 跳过固化——**裁定：补固化**（T009b 内）

窗口（`common/js/dsh-plugins/saolei-loop/src/driver.ts:821-845`）：流以 finish{error|aborted} 结束 → `await dispatch.waterfall("agent/request-error", …)` → abort 在 waterfall 等待期间到达 → `:837` `signal.throwIfAborted()` 抛出，**未**经 `appendInterrupted()`——assembler 已产出前缀丢失固化，而本地已流式呈现（违反 FR-012"已产出的步骤内容 MUST 固化"的该窗口）。

修复形态（retry 不可执行时先固化；retry 语义保持不变——无 abort 时 retry 路径仍跳过固化、下次重装配）：

```ts
// Abort during the waterfall: a retry decision can no longer execute (the
// next attempt would abort before producing anything), so the produced
// prefix fixates like every other non-happy stream exit
// (specs/054-agent-v2-bugfixes/revisions/phase4-failed-turn-folding.md §7-2).
if (action?.kind !== "retry" || signal.aborted) {
  appendInterrupted();
}
signal.throwIfAborted();
if (action?.kind !== "retry") {
  throw new LlmError(finish.failure.message, finish.failure.code, finish.failure);
}
continue;
```

（`step()` 内 `signal` 已解构可用；abort 后外层 `turn()` catch 按 `signal.aborted` 归类 aborted 终态，不受影响。）否决"仅注释说明取舍"：补固化是两行条件重排 + 一个用例，且该窗口正是 FR-012 要消灭的内容丢失类缺陷，注释豁免缺乏依据。

---

## 8. 下游执行指引（分步可恢复）

1. 应用 §6 六处 tasks.md 修订（纯文档步）。
2. T009b 执行序（同文件串行）：
   a. proto 字段 + codegen/编译门禁（§3.1）；
   b. 契约/文档同步（§5 文本，接口先行）；
   c. 服务端 `history.ts` + `history.test.ts`（§3.2、§4 表 1）；
   d. 前端 `conversation.ts`/`chat.ts`/`ChatView.tsx` + `chat.test.ts`/`ChatView.test.tsx`（§3.3–3.5、§4 表 2–5）；
   e. driver waterfall abort 固化 + `driver.test.ts`（§7-2、§4 表 6）。
3. 验证：`bazel test //common/js/dsh-plugins/saolei-loop/... //projects/game/agent_v2/... //projects/game/web/frontend/...` 全绿（Phase 4 Independent Test 修订后口径）。
4. Phase 6 T014 实现时直接复用 `stepsToHistory(steps, true)` 的 CANCELED 投影（§3.4），无需返回本设计。
