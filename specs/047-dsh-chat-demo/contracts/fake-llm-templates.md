# Contract: fake-llm 模板配置 schema 与匹配语义

**Feature**: [spec.md](spec.md) FR-006（澄清 Q4：多轮最小扩展） | **Date**: 2026-08-22

testdata 作者面契约：模板字段、文件形态、匹配规则与优先级。定位：**测试基建，稳定、可支撑测试即可**（澄清 Q4 原话），不做复杂对话模拟。参考母本：`specs/046-fake-llm-think-chunking/contracts/template-config.md`（game fake-llm 的作者面契约）。

## 1. 文件形态

testdata 位于 `experimental/dsh/demo/fake-llm/service/testdata/`，YAML 文件经 `go:embed` 内嵌；每文件一个 `messages:` 列表（按场景组织，一文件多模板——046 的场景分组模式）：

```yaml
# chat.yaml — US1/US2 验收场景的模板组
messages:
  - name: greeting
    keywords: [hello]
    text: "Hello! How can I help you today?"
  - name: greeting-again          # 多轮分支：历史出现过首轮关键词 + 轮次 ≥2
    keywords: [hello]
    history_keywords: [hello]
    min_turn: 2
    text: "Hello again! We have already met."
  - name: farewell                # 纯兜底模板：keywords 为空，不参与关键词匹配（§3.3）
    keywords: []
    text: "I'm sorry, I didn't catch that."
  - name: ...
```

## 2. 模板字段

| 字段 | 类型 | 必填 | 语义 |
|---|---|---|---|
| `name` | string | 是 | 唯一标识（同文件内唯一；日志/测试引用锚点） |
| `keywords` | string[] | 是 | 关键词集：**任一**命中（大小写不敏感子串）**最后一条 user 消息**即通过该条件；空数组 = 该模板为**纯兜底模板**（仅限非多轮条件模板声明：不参与关键词优先级匹配，只作为 §3.3 兜底候选）；多轮条件模板声明空数组 = 关键词条件恒通过 |
| `history_keywords` | string[] | 否 | 历史关键词集：**全部**须命中（大小写不敏感子串）**除最后一条 user 消息外**的任意历史消息（user 或 assistant 均可） |
| `min_turn` | int | 否（默认 1） | 请求中 user 消息总数 ≥ min_turn 才通过 |
| `text` | string | 是 | 确定性回复全文（SSE content deltas 的拼接目标） |
| `reasoning` | string | 否 | 保留字段（chat-completions 路径不发送；schema 与母本兼容，便于未来演进） |

## 3. 匹配语义

输入：请求 `messages[]`。设 `U = user 角色消息按序集合`，`last = U 的末条`，`history = messages 中除 last 外的全部消息`，`turn = len(U)`。

模板 T 命中当且仅当：

1. `keywords(T)` 非空时 ∃ k ∈ keywords(T): k ⊆ last.content（不区分大小写）；`keywords(T)` 为空的多轮条件模板 = 关键词条件恒通过；`keywords(T)` 为空的非多轮条件模板不参与本优先级，仅作 §3.3 兜底候选；
2. `history_keywords(T)` 未声明 **或** ∀ h ∈ history_keywords(T): h ⊆ history 中某条消息的 content（不区分大小写）；
3. `turn ≥ min_turn(T)`（未声明时默认 1）。

**优先级（择一返回）**:

1. **多轮条件模板**：声明了 `history_keywords` 或 `min_turn > 1`，且条件全满足——多轮条件模板之间冲突时取**声明条件数多者**（更具体优先），仍并列则按 name 字典序稳定选择；
2. **纯关键词模板**：未声明多轮条件、`keywords` **非空**且关键词命中；
3. **兜底模板**：以上皆未命中——`keywords` 为空的非多轮条件模板（纯兜底模板）若**唯一**则直接返回；多个或不存在时从全部非多轮条件模板中以稳定 seed（请求 messages 全文哈希）确定性选择，保证同请求同回复（US1-2 的确定性要求覆盖兜底路径）。

**默认语义兼容**：未声明 `history_keywords`/`min_turn` 的模板，匹配行为与 `projects/game/fake-llm/` 的关键词匹配一致（仅最后一条 user 消息参与）。

## 4. 验收场景 ↔ 模板映射（US1/US2 断言锚点）

| 场景 | 模板条件 | 断言 |
|---|---|---|
| US1-1/2 单轮往返 | `greeting`（keywords: [hello]） | reply == greeting.text，重复请求相同 |
| US1-3 兜底 | 无关键词命中 | reply == `farewell`.text（testdata 唯一 keywords 为空的纯兜底模板，确定性直返） |
| US2-1 多轮分支 | `greeting-again`（keywords:[hello] + history_keywords:[hello] + min_turn:2） | 第二轮（"hello"）→ greeting-again.text |
| US2-2 会话隔离 | 同 US2-1 模板 | 新 session 首轮（"hello"）→ greeting.text（history 条件不满足） |
| US2-3 并发交错 | 同上 | 两 session 各自正确分支 |

**测试设计要点**: US2 的第二轮消息可与首轮**相同**（"hello"）——分支切换完全由 `history_keywords`/`min_turn` 承载，这是"最小扩展"选择的直接验证方式。
