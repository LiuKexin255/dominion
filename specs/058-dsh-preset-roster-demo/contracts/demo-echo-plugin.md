# Contract: @dominion/dsh-demo-echo（demo 工具插件）

> demo 本地 workspace 包（D7/R7）：V2-3 验证点的载体——**工具 schema 与配套 guidance 封装于同一插件**，行级选择一致性（不挂该行则两者同时缺席，FR-006）。
> 位置：`experimental/dsh/demo/agent-plugins/demo-echo`；包名 `@dominion/dsh-demo-echo`。
> 形态：cordis 插件四导出；**不进 host 组合清单**——仅被 preset 行引用（`demo-tools` 模板，composition-manifest.md §3.2）；裸包名从 host base（demo agent node_modules）解析。

## 1. 插件声明

```typescript
export const name = "demo-echo";
export const inject = ["tools", "systemPrompt"];   // 工具注册表 + prompt 装配 registry（scope 归档）
// 无 Config
```

## 2. 工具面

- 工具名：**`demo_echo`**
- 输入 schema：`{ text: string }`（required）
- 输出（确定性，供 fake-llm 场景与单测断言）：

```text
echo: {text}
```

- 注册：`ctx.tools.register(...)` 于 `apply()` 内（scope 感知——preset mount 的 agent scope 层生效）。

## 3. Guidance section（同 `apply()` 注册）

- `ctx.systemPrompt.section(...)`：注册一条 PromptSection（order 100-199 频段，工具守则带，`survey/deepseek-harness-team-mode.md` §4.5 的 order 约定），标题含关键词 `demo_echo`（供 fake-llm `system_keywords` 端到端断言——R6：guidance 与 persona 同走 system prompt）。
- 内容要点：说明 `demo_echo` 的用途与输出格式（几行内）。

## 4. 一致性要求（FR-006 / V2-3 的机制保证）

工具注册与 guidance 注册**必须在同一 `apply()` 内**——插件不挂载（preset 行缺席）则两者同时不存在；插件挂载则两者同时在场。禁止把 guidance 注册挪到其他插件或 host 行（那会复现 `dsh-tools` restriction 不对称的坑，`survey/deepseek-harness-preset.md` §10.4）。

## 5. 可测试性

- 单测：mock ctx（tools/systemPrompt registry doubles）断言两者成对注册。
- V2-3 端到端：`demo-tools` preset 会话的请求 system prompt 含 guidance 关键词；`demo-standard` preset 会话不含且无 `demo_echo` schema（单测断言请求 tools 集合 + fake-llm system_keywords）。
