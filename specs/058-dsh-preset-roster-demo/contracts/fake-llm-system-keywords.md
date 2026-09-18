# Contract: fake-llm `system_keywords` 匹配条件（扩展）

> 对 047 fake-llm 模板匹配语义的**最小扩展**（R6）：`specs/047-dsh-chat-demo/contracts/fake-llm-templates.md` §3 的多轮条件模板增加 system 维度。
> 实现时同步修订 047 契约文件（终态表述：契约描述扩展后的完整匹配语义，不标注"新增于 058"）。
> 承载：V1-1（persona 差异）与 V2-3（guidance 在场）的端到端断言。

## 1. 扩展语义

模板 YAML 增加可选键 `system_keywords: []string`，语义与既有 `history_keywords` 同型（`specs/047-dsh-chat-demo/contracts/fake-llm-templates.md` §3）：

- 设 `S` = 请求 `messages[]` 中 `role == "system"` 的消息全文拼接（无 system 消息时为空串）。
- `system_keywords` 非空的模板即**多轮条件模板**：命中需 every keyword 在 `S` 中出现（大小写不敏感，与 history 匹配同约定）。
- 未声明 `system_keywords`（或为空）→ 不参与 system 维度判定（纯关键词/仅 history 条件模板行为不变——047 既有用例零影响）。

## 2. 匹配优先级

并入 047 既有判定框架不变：条件模板（history/system/min_turn 任意组合，全部满足才命中）> 纯关键词模板 > 兜底。

## 3. 测试模板（`experimental/dsh/demo/fake-llm/service/testdata/`）

新增场景文件（示例，命名随实现）：

```yaml
messages:
  - name: preset-persona-standard
    system_keywords: ["demo standard assistant"]
    keywords: []
    reply: "persona-standard-hit"
  - name: preset-persona-authored
    system_keywords: ["AUTHORED-PERSONA-MARKER"]   # 创作 preset 的 persona 文本由测试注入标记词
    keywords: []
    reply: "persona-authored-hit"
  - name: tool-guidance-present
    system_keywords: ["demo_echo"]
    keywords: []
    reply: "tool-guidance-hit"
  - name: tool-guidance-absent
    system_keywords: ["demo_echo", "demo standard assistant"]   # 同场含两者才命中——证明
    keywords: []                                                  # standard 模板无 guidance
    reply: "tool-guidance-miss"
```

（`reply` 值为实现时定形；模板文件组织沿用 046/047 场景分组模式。）

## 4. Go 实现面

- 匹配器扩展：`system` 集合计算 + every-hit 判定（复用 history 匹配的子串逻辑）；模板结构体增 `SystemKeywords []string`（YAML `system_keywords`）。
- 047 既有模板与用例**零行为变化**（不声明即不参与——向后兼容）。
