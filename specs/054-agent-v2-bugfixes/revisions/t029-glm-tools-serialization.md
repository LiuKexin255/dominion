# Revision: GLM adapter 工具定义序列化缺失修复（t029-glm-tools-serialization）

**Feature**: [spec.md](../spec.md) FR-001/FR-003 | **日期**: 2026-09-04 | **性质**: 执行期缺口补充设计（Phase 12 T029 排查定位；本文为设计产物与修复面权威描述）

**状态**: 本文是 llm-glm adapter `serializeRequest` 静默丢弃 `options.tools` 缺陷的定性、根因证据链与落地方案权威描述。修复涉及 `common/js/dsh-plugins/llm-glm/src/serialize.ts`、`serialize.test.ts` 与 049 契约 §4/§6（已随本文落地）。

---

## 0. 缺陷定性（已核实）

### 0.1 缺陷表现

正式环境（`projects/game/deploy.yaml`，真实 GLM 端点 + 真实 desktop）人工测试中，一个回合内同时观察到三现象（trace `e2cea46938e318798a7e2f7878653994`，session `50f5664713beba3309e3cf8d24df358e`，game.prod：gateway `:send` → proxy → agent-v2 `AgentService/Send`，25.38s，RPC 无 error）：

1. **模型未发起任何结构化 function call**：GLM 端点收不到工具定义，模型只能以正文文本"模拟"工具调用——输出操作序列与游戏状态的自然语言叙述（用户看到的"来路不明的游戏状态"），而非 `{type:'function_call'}` 输出项。
2. **desktop 零执行**：无结构化 tool-call → 无工具 dispatch → 桌面扫雷毫无动静；游戏状态叙述纯属模型虚构（违反 FR-003"不出现任何非真实执行产生的游戏状态"），且 web 呈现进一步放大误导。
3. **回合以流 error 收束、历史尾步 `interrupted=true`**（机理见 §0.3）。

### 0.2 根因证据链：`serializeRequest` 是工具定义的唯一丢失点

上游传递链路逐环核实（全部实读）：

| 环节 | 位置 | 事实 |
|---|---|---|
| 工具组装 | `common/js/dsh-plugins/saolei-loop/src/driver.ts:764` | `PromptAssembly.tools`（dsh-system-prompt 渲染 saolei 三工具的 `ToolSchema[]`）传入 `buildRequest` |
| header 携带 | 同文件 `:957` | `...(tools.length > 0 ? { tools } : {})` |
| 请求携带 | 同文件 `:990` | `...(header.tools !== undefined ? { tools: header.tools } : {})` —— `GenerateOptions.tools` 就绪 |
| adapter 透传 | `common/js/dsh-plugins/llm-glm/src/adapter.ts:118` | `serializeRequest(options)` 全量透传 |
| **序列化丢失** | `common/js/dsh-plugins/llm-glm/src/serialize.ts` | `serializeRequest` 全函数**零引用** `options.tools`，`ResponsesRequest` 无 `tools` 字段——请求体不含任何工具定义 |

**唯一丢失点**：driver 与 adapter 之间的传递面正确、wire 回程（`function_call` 事件映射、`function_call_output` 关联）正确，丢失只发生在序列化一步。

**契约违约**：dsh-llm `GenerateOptions.tools` 的契约注释（`@deepseek-ai/dsh-llm` `lib/types/types.d.ts:346-347`）明确 "Tool schemas (adapters map to the provider's `tools` field)"——工具序列化是 adapter 侧义务，静默丢弃即违约。同文件对不支持的 `stop`/`reasoningEffort` 采用 fail-loud（本 adapter `serializeRequest` 已按惯例抛 `UNSUPPORTED`），而 tools 属"支持但漏实现"——不抛错、静默缺失，危害反而更大。

**051 D11 方案级遗漏定性**：[specs/051-agent-v2-dsh-migration/research.md](../../051-agent-v2-dsh-migration/research.md) D11 只解除了 049 契约 §4 的 input 占位（assistant `tool-call` → `function_call` item、`tool-result` → `function_call_output` item），未把 `GenerateOptions.tools` 的上行序列化纳入范围——工具**回程**（调用与结果回传）通了，工具**去程**（定义上行）没通。属方案级遗漏，非实现走样；049 §4 映射表相应行随本修复终态化（§3）。

### 0.3 `interrupted=true` 机理：按设计固化，非独立缺陷

该 trace 历史尾步携带 `interrupted=true`，裁定：

- **排除 max-tokens**：`response.incomplete` → `finish{kind:'max-tokens'}` 是正常终局（glm-llm-plugin.md §5 表），走 steer 重开路径，不产生 error 收束，也不触发 interrupted 固化。
- **判定**：LLM 流以 error 收束（流异常/finish error）且 llm-retry 未恢复，driver 按设计执行 interrupted 前缀固化（[data-model.md](../data-model.md) §2，T007/T009b 语义：非 happy 路径的已产出前缀 append `assistant/message`（`interrupted: true`））——固化本身是按设计的正确行为。
- **与 tools 缺失的关系**：高概率连带（模型无工具定义时退化为不受控的长文本模拟输出，更易触发流异常），**不独立修复**；随 §1 落地后经正式环境复验观察 error 收束是否消失，若仍复现则按 [research.md](../research.md) D10 playbook 另行排查。

### 0.4 明确不修（复查裁定）

- **interrupted 成因**：§0.3 判定为按设计固化，成因随 tools 修复复验观察。
- **fake-llm**：不增强（testplan 拓扑的 fake LLM 无此缺陷面）。
- **driver/wire**：传递与回程已正确（§0.2 表），零改动。

---

## 1. 修复设计：`tools` 上行序列化（终态形态）

`common/js/dsh-plugins/llm-glm/src/serialize.ts`：

1. **新增 `ResponsesFunctionTool` 接口**：`{ type: "function"; name: string; description: string; parameters: Record<string, unknown> }`——OpenAI Responses API `FunctionTool` 的**平铺形态**（[openai-openapi](https://github.com/openai/openai-openapi) `FunctionTool` schema：`type`/`name`/`parameters` 必备，区别于 chat-completions 的嵌套 `function` 对象）；`description` 同步传递（模型判定是否调用与如何调用的依据）。
2. **`ResponsesRequest` 增加 `tools?: ResponsesFunctionTool[]`**。
3. **映射规则**：`options.tools` 非空（`!== undefined && length > 0`）时逐项映射 `request.tools = options.tools.map(t => ({type:"function", name: t.name, description: t.description, parameters: t.parameters}))`；`undefined` 或空数组**省略字段**——对齐官方 `dsh-llm-deepseek` adapter 的 `tools.length > 0` 才展开惯例与本文件可选字段省略惯例（不发送 null/空数组，provider 默认生效）。
4. **不发送 `strict` 字段**：saolei 双形式 schema（`saolei_operate` 的单操作 `type/x/y` 与批量 `operations` 互斥形式，`common/js/dsh-plugins/saolei/src/index.ts:184-211`）不满足 strict 模式对 JSON-schema 封闭子集的前提；`strict:false` 与缺省语义等价，省略即为正确终态。openai-openapi 的函数定义 schema 虽将 `strict` 列入 `required`（chat-completions `Function`；Responses `FunctionTool` 自身则为可选属性），但 OpenAI API reference 实际按可选处理（缺省即 false）；GLM 端点为该协议的 Codex 兼容实现，若其严格校验拒绝省略形态，正式环境复验将以 HTTP 400 立即暴露——该风险由 §4.3 复验闭环覆盖。
5. 头注释映射表说明同步（tools 行 + 依据：049 契约 §4 与 openai-openapi `FunctionTool`）。

## 2. 测试义务（constitution IV，`serialize.test.ts`）

| 用例 | 断言 |
|---|---|
| tools 非空映射 | fixture 用真实工具面形状（`saolei_operate` 双形式 schema + 无参工具两条），`request.tools` 逐字段平铺断言（`toEqual`） |
| 无 tools / 空数组 | 请求体**无 `tools` 字段**（`not.toHaveProperty` + `toEqual` 全量断言防字段漂移），两形态产物一致 |
| strict 零出现 | 序列化产物每个 tool 项 `not.toHaveProperty("strict")`（防未来字段漂移） |

既有 12 用例零回归。

## 3. 契约同步（049 契约，随本修复落地）

[specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md](../../049-agent-v2-dsh-init/contracts/glm-llm-plugin.md)：

- §4 请求体示例补 `tools` 行；映射表补 `GenerateOptions.tools → tools[]` 行（平铺 FunctionTool、非空才携带、strict 省略及理由、依据 openai-openapi）；tool item 行更新为 051 D11 之后的实现终态（`function_call`/`function_call_output`）。
- §6 序列化测试义务行同步（tools 平铺映射与非空才携带、strict 零出现）。

## 4. 验证证据

### 4.1 单测（编译 + 单测门禁）

- `bazel test //common/js/dsh-plugins/llm-glm/... --cache_test_results=no`：2/2 target PASSED（`lib_test`、`lib_typecheck_test`）。
- 用例明细（vitest 与 bazel 同 pipeline，`style/javascript.md` 测试执行模型）：15/15 全绿——既有 12 用例零回归 + 新增 3 用例（§2 表）。

### 4.2 大型测试零回归（constitution VI）

`guitar run projects/game/testplan/system_test.yaml`：部署→测试→清理闭环完成，两 suite 全绿——`game-system: success`、`game-disconnect: success`。

### 4.3 正式环境复验

复验证据待回填（待用户 T028 验收点 4 重测后回填）：真实 desktop 收到并执行操作、模型发出结构化 function_call、流 error 收束是否消失（§0.3），含 trace id。
